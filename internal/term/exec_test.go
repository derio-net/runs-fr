package term

import (
	"reflect"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// testClientset builds a real clientset over a dummy host. BuildExecURL only
// uses the REST client to construct (not send) the request, so no API server is
// needed — the host just gives the resulting *url.URL a concrete base.
func testClientset(t *testing.T) kubernetes.Interface {
	t.Helper()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: "https://k8s.example.com"})
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	return cs
}

func TestBuildExecURL(t *testing.T) {
	cs := testClientset(t)

	u := BuildExecURL(cs, "agents", "run-pod-xyz", "fr-run-abc")

	if got, want := u.Path, "/api/v1/namespaces/agents/pods/run-pod-xyz/exec"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}

	q := u.Query()
	if got, want := q["command"], []string{"tmux", "attach", "-t", "fr-run-abc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("command = %v, want %v", got, want)
	}
	if got := q.Get("tty"); got != "true" {
		t.Errorf("tty = %q, want %q", got, "true")
	}
	if got := q.Get("stdin"); got != "true" {
		t.Errorf("stdin = %q, want %q", got, "true")
	}
	if got := q.Get("stdout"); got != "true" {
		t.Errorf("stdout = %q, want %q", got, "true")
	}
	// Under a TTY, stderr is multiplexed onto stdout, so it must NOT be
	// requested (the field is omitempty, so false means absent).
	if got := q.Get("stderr"); got == "true" {
		t.Errorf("stderr = %q, want it not requested under a TTY", got)
	}
}
