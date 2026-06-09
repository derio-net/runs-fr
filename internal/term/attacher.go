package term

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// defaultPingInterval is how often the attacher pings the browser peer to
// detect a dead connection (and the deadline it allows for the pong).
const defaultPingInterval = 30 * time.Second

// KubeAttacher bridges a browser websocket to a pod's tmux session over the
// real pods/exec stream. Its live path needs a kubelet, so it is exercised
// end-to-end in the Phase 8 manual verification rather than in unit tests.
type KubeAttacher struct {
	cfg          *rest.Config
	clientset    kubernetes.Interface
	namespace    string
	pingInterval time.Duration
}

// NewKubeAttacher builds an attacher for pods in namespace, using cfg for the
// exec transport credentials and clientset to construct the exec URL.
func NewKubeAttacher(cfg *rest.Config, clientset kubernetes.Interface, namespace string) *KubeAttacher {
	return &KubeAttacher{
		cfg:          cfg,
		clientset:    clientset,
		namespace:    namespace,
		pingInterval: defaultPingInterval,
	}
}

// Attach runs `tmux attach -t <sessionName>` in podName and streams it over
// conn. The session adapts conn to the executor's stdin/stdout/resize surface;
// StreamWithContext drives it and returns when the stream ends, ctx is
// cancelled, or the peer is detected dead via the ping loop.
func (a *KubeAttacher) Attach(ctx context.Context, conn *websocket.Conn, podName, sessionName string) error {
	executor, err := NewExecutor(a.cfg, BuildExecURL(a.clientset, a.namespace, podName, sessionName))
	if err != nil {
		return err
	}

	// A dead peer (or stream error) must cancel the other half, so derive a
	// child context both the ping loop and the executor share.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.pingLoop(ctx, cancel, conn)

	sess := newSession(ctx, conn)
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             sess,
		Stdout:            sess,
		Tty:               true,
		TerminalSizeQueue: sess,
	})
}

// pingLoop pings the peer on an interval; a ping that fails within its deadline
// means the browser is gone, so it cancels the session to tear the exec down.
func (a *KubeAttacher) pingLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
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
}
