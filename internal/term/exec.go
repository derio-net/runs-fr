package term

import (
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// BuildExecURL constructs the method-agnostic *url.URL for a tmux attach against
// the named pod's exec subresource. The session attaches (not creates) the run
// pod's tmux session, so stdin/stdout and a TTY are requested; under a TTY
// stderr is multiplexed onto stdout and is left unset.
//
// The attach target carries the "=" exact-match prefix, matching the local
// backend (localterm.tmuxAttachCmd) so both backends resolve a session by an
// exact name rather than tmux's default prefix match — a node-<name> shell
// (colon-free per the naming correction) can never be shadowed by a longer
// session that shares its prefix.
//
// The .Post() builder is used only to assemble the URL — the HTTP method is
// supplied per-executor (websocket upgrades with GET, SPDY with POST), so this
// returns a bare URL, not a request.
func BuildExecURL(clientset kubernetes.Interface, namespace, pod, sessionName string) *url.URL {
	return clientset.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"tmux", "attach", "-t", "=" + sessionName},
			Stdin:   true,
			Stdout:  true,
			Stderr:  false,
			TTY:     true,
		}, scheme.ParameterCodec).
		URL()
}

// NewExecutor builds an executor that prefers a websocket transport and falls
// back to SPDY when the cluster rejects the websocket upgrade — covering older
// API servers. The websocket executor upgrades with GET and takes a URL string;
// the SPDY executor uses POST and a *url.URL.
func NewExecutor(cfg *rest.Config, u *url.URL) (remotecommand.Executor, error) {
	ws, err := remotecommand.NewWebSocketExecutor(cfg, "GET", u.String())
	if err != nil {
		return nil, err
	}
	spdy, err := remotecommand.NewSPDYExecutor(cfg, "POST", u)
	if err != nil {
		return nil, err
	}
	return remotecommand.NewFallbackExecutor(ws, spdy, httpstream.IsUpgradeFailure)
}
