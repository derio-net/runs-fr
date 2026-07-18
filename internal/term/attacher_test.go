package term

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type fakePinger struct {
	err  error
	seen chan struct{}
}

func (p *fakePinger) Ping(context.Context) error {
	select {
	case p.seen <- struct{}{}:
	default:
	}
	return p.err
}

func TestPingLoopCancelsOnPeerFailure(t *testing.T) {
	a := &KubeAttacher{pingInterval: time.Millisecond}
	p := &fakePinger{err: errors.New("peer gone"), seen: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.pingLoop(ctx, cancel, p)

	select {
	case <-p.seen:
	default:
		t.Fatal("ping loop did not ping the peer")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("ping failure did not cancel the attach context")
	}
}

type fakeExecutor struct {
	stream func(context.Context, remotecommand.StreamOptions) error
}

func (e *fakeExecutor) Stream(options remotecommand.StreamOptions) error {
	return e.stream(context.Background(), options)
}

func (e *fakeExecutor) StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error {
	return e.stream(ctx, options)
}

func testAttacherClientset(t *testing.T) kubernetes.Interface {
	t.Helper()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: "https://k8s.example.com"})
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	return cs
}

func websocketPair(t *testing.T) (client, server *websocket.Conn, close func()) {
	t.Helper()
	ready := make(chan *websocket.Conn, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		ready <- conn
	})
	srv := httptest.NewServer(h)
	client, _, err := websocket.Dial(context.Background(), "ws"+srv.URL[len("http"):], nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial websocket: %v", err)
	}
	select {
	case server = <-ready:
	case <-time.After(time.Second):
		_ = client.Close(websocket.StatusNormalClosure, "")
		srv.Close()
		t.Fatal("websocket server did not accept connection")
	}
	return client, server, func() {
		_ = client.Close(websocket.StatusNormalClosure, "")
		_ = server.Close(websocket.StatusNormalClosure, "")
		srv.Close()
	}
}

func TestAttachUsesExecutorAndSession(t *testing.T) {
	client, server, closePair := websocketPair(t)
	defer closePair()

	started := make(chan remotecommand.StreamOptions, 1)
	a := NewKubeAttacher(&rest.Config{Host: "https://k8s.example.com"}, testAttacherClientset(t), "agents")
	a.newExecutor = func(_ *rest.Config, u *url.URL) (remotecommand.Executor, error) {
		if got, want := u.Path, "/api/v1/namespaces/agents/pods/node-pod/exec"; got != want {
			t.Errorf("exec path = %q, want %q", got, want)
		}
		// Assert the exec QUERY, not just the path: the command must attach the
		// resolved session by exact "=" match, and request a TTY with stdin/stdout.
		q := u.Query()
		if got, want := q["command"], []string{"tmux", "attach", "-t", "=run-1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("exec command = %v, want %v", got, want)
		}
		if got := q.Get("tty"); got != "true" {
			t.Errorf("exec tty = %q, want true", got)
		}
		if got := q.Get("stdin"); got != "true" {
			t.Errorf("exec stdin = %q, want true", got)
		}
		if got := q.Get("stdout"); got != "true" {
			t.Errorf("exec stdout = %q, want true", got)
		}
		return &fakeExecutor{stream: func(ctx context.Context, options remotecommand.StreamOptions) error {
			started <- options
			buf := make([]byte, 4)
			if _, err := options.Stdin.Read(buf); err != nil {
				return err
			}
			_, err := options.Stdout.Write([]byte("pong"))
			return err
		}}, nil
	}

	errCh := make(chan error, 1)
	go func() { errCh <- a.Attach(context.Background(), server, "node-pod", "run-1") }()

	select {
	case options := <-started:
		if !options.Tty || options.Stdin == nil || options.Stdout == nil || options.TerminalSizeQueue == nil {
			t.Fatal("attach did not wire a TTY session to all stream surfaces")
		}
	case <-time.After(time.Second):
		t.Fatal("executor was not started")
	}
	if err := client.Write(context.Background(), websocket.MessageBinary, []byte("ping")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	typ, data, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if typ != websocket.MessageBinary || string(data) != "pong" {
		t.Fatalf("stdout frame = (%v, %q), want binary pong", typ, data)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Attach() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("attach did not return after the exec stream ended")
	}
}

func TestAttachReturnsExecutorError(t *testing.T) {
	want := errors.New("executor unavailable")
	a := NewKubeAttacher(&rest.Config{Host: "https://k8s.example.com"}, testAttacherClientset(t), "agents")
	a.newExecutor = func(*rest.Config, *url.URL) (remotecommand.Executor, error) { return nil, want }
	_, server, closePair := websocketPair(t)
	defer closePair()
	if err := a.Attach(context.Background(), server, "pod", "run"); !errors.Is(err, want) {
		t.Fatalf("Attach() error = %v, want %v", err, want)
	}
}
