package localterm

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeConn scripts inbound frames and captures outbound ones. Read blocks
// once the script is exhausted until Close, then errors — mimicking a browser
// that goes away.
type fakeConn struct {
	mu     sync.Mutex
	script []frame
	out    strings.Builder
	closed chan struct{}
}

type frame struct {
	typ  websocket.MessageType
	data []byte
}

func newFakeConn(script ...frame) *fakeConn {
	return &fakeConn{script: script, closed: make(chan struct{})}
}

func (f *fakeConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	f.mu.Lock()
	if len(f.script) > 0 {
		fr := f.script[0]
		f.script = f.script[1:]
		f.mu.Unlock()
		return fr.typ, fr.data, nil
	}
	f.mu.Unlock()
	select {
	case <-f.closed:
		return 0, nil, errors.New("fake conn closed")
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (f *fakeConn) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out.Write(p)
	return nil
}

func (f *fakeConn) Ping(ctx context.Context) error { return nil }

func (f *fakeConn) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String()
}

// catAttacher builds an Attacher whose "tmux attach" is plain cat — a
// well-behaved PTY citizen that echoes stdin.
func catAttacher() *Attacher {
	a := NewAttacher("")
	a.newCmd = func(socket, session string) *exec.Cmd { return exec.Command("cat") }
	return a
}

func TestAttachEchoesThroughPTY(t *testing.T) {
	conn := newFakeConn(
		frame{websocket.MessageText, []byte(`{"resize":{"cols":80,"rows":24}}`)},
		frame{websocket.MessageBinary, []byte("hello\n")},
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- catAttacher().attach(ctx, conn, "ignored") }()

	// Wait for the echo, then drop the peer so attach unwinds.
	deadline := time.After(4 * time.Second)
	for !strings.Contains(conn.output(), "hello") {
		select {
		case <-deadline:
			t.Fatalf("no echo through the PTY; output=%q", conn.output())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if strings.Contains(conn.output(), "resize") {
		t.Errorf("control frame leaked into stdin: %q", conn.output())
	}
	close(conn.closed)
	select {
	case err := <-done:
		// A peer close may end attach with or without an error; the test only
		// asserts that it returns.
		t.Logf("attach returned after peer close: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("attach did not return after peer close")
	}
}

func TestAttachEndsWhenCommandExits(t *testing.T) {
	a := NewAttacher("")
	a.newCmd = func(socket, session string) *exec.Cmd { return exec.Command("true") }
	conn := newFakeConn()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := a.attach(ctx, conn, "s"); err != nil && !errors.Is(err, io.EOF) {
		// A PTY read after child exit surfaces EIO — any prompt return is
		// the contract; just ensure we got here.
		t.Logf("attach returned: %v", err)
	}
	// A teardown regression that hangs until the ctx rescue would still
	// "pass" without this bound (review minor 5) — the return must come
	// from the child's exit, not the 5s timeout.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("attach took %v — teardown is hanging until ctx rescue", elapsed)
	}
}

func TestDefaultCommandShape(t *testing.T) {
	a := NewAttacher("/run/tmux/cnc.sock")
	cmd := a.newCmd(a.socket, "run-1")
	got := strings.Join(cmd.Args, " ")
	want := "tmux -S /run/tmux/cnc.sock attach -t =run-1"
	if got != want {
		t.Errorf("default cmd = %q, want %q", got, want)
	}
	// The attach client dies without a TERM ("open terminal failed:
	// terminal does not support clear") — the runner container has none,
	// which killed every local attach on the first live seam run. The
	// default cmd must force one deterministically (last entry wins).
	if len(cmd.Env) == 0 || cmd.Env[len(cmd.Env)-1] != "TERM=xterm-256color" {
		t.Errorf("default cmd env must end with TERM=xterm-256color; env tail = %v", cmd.Env)
	}

	bare := NewAttacher("")
	if got := strings.Join(bare.newCmd("", "run-2").Args, " "); got != "tmux attach -t =run-2" {
		t.Errorf("bare cmd = %q", got)
	}
}
