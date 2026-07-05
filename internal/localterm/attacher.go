package localterm

import (
	"context"
	"os/exec"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/derio-net/runs-fr/internal/term"
)

// defaultPingInterval mirrors the kube attacher: detect a dead browser peer
// so an abandoned attach doesn't hold the PTY client open forever.
const defaultPingInterval = 30 * time.Second

// wsConn is the subset of *websocket.Conn the attacher needs, mirrored from
// internal/term so the bridge logic is unit-testable without a live socket.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	Ping(ctx context.Context) error
}

// Attacher bridges a browser websocket to a local tmux session over a PTY:
// the local-backend counterpart of term.KubeAttacher. The spawned client is
// `tmux attach -t =<session>`, so the gateway needs only the tmux binary and
// (shared-container) socket — no cluster, no Docker.
type Attacher struct {
	socket       string
	pingInterval time.Duration
	// newCmd builds the attach client; injectable for tests.
	newCmd func(socket, session string) *exec.Cmd
}

// NewAttacher builds an attacher over the given tmux socket (empty = tmux's
// default).
func NewAttacher(socket string) *Attacher {
	return &Attacher{
		socket:       socket,
		pingInterval: defaultPingInterval,
		newCmd:       tmuxAttachCmd,
	}
}

func tmuxAttachCmd(socket, session string) *exec.Cmd {
	args := []string{}
	if socket != "" {
		args = append(args, "-S", socket)
	}
	args = append(args, "attach", "-t", "="+session)
	return exec.Command("tmux", args...)
}

// Attach satisfies the server's Attacher seam. The first argument is the
// resolved attach handle, which for the local backend IS the session name —
// both name the same tmux session, so the session parameter is used.
func (a *Attacher) Attach(ctx context.Context, conn *websocket.Conn, _ string, sessionName string) error {
	return a.attach(ctx, conn, sessionName)
}

func (a *Attacher) attach(ctx context.Context, conn wsConn, session string) error {
	cmd := a.newCmd(a.socket, session)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	// Any exit path tears down both halves: cancel unblocks the ws read
	// (ctx-bound) and closing the PTY unblocks its read; killing the client
	// detaches tmux without touching the session itself.
	ctx, cancel := context.WithCancel(ctx)
	// A blocked PTY read only wakes when the fd closes, so the teardown that
	// unblocks the outbound pump must run on cancellation, not just on
	// return (the return depends on that read waking — see the pump below).
	go func() {
		<-ctx.Done()
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
	}()
	// Defer ORDER is load-bearing (LIFO; review finding 1): cancel must run
	// BEFORE Wait, else a return path with a live client (e.g. ws write
	// failure) blocks Wait on a process nothing will ever kill.
	defer func() { _, _ = cmd.Process.Wait() }()
	defer cancel()

	// Inbound pump: browser → PTY. Binary frames are keystrokes; text frames
	// are control (resize) — same demux-by-message-type rule as term.session.
	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if _, err := ptmx.Write(data); err != nil {
					return
				}
			case websocket.MessageText:
				if cols, rows, ok := term.ParseResize(data); ok {
					_ = pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})
				}
			}
		}
	}()

	// Peer liveness: a browser that vanishes without a close frame would
	// otherwise leave the attach client running until the session ends.
	go func() {
		ticker := time.NewTicker(a.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, done := context.WithTimeout(ctx, a.pingInterval)
				err := conn.Ping(pingCtx)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Outbound pump: PTY → browser, on the request goroutine. Ends on PTY
	// EOF/EIO (client exited) or a ws write failure.
	buf := make([]byte, 32*1024)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}
