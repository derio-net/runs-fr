package kube

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/derio-net/runs-fr/internal/config"
)

// testKubeconfig is a minimal but valid kubeconfig used to prove the
// out-of-cluster fallback path resolves a real *rest.Config.
const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://test.example.com:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: testtoken
`

func TestBuildSelector(t *testing.T) {
	cfg := &config.Config{LabelSelector: "fr.run/id"}

	sel, err := BuildSelector(cfg)
	if err != nil {
		t.Fatalf("BuildSelector() error = %v", err)
	}

	if !sel.Matches(labels.Set{"fr.run/id": "abc123"}) {
		t.Errorf("selector %q should match a pod carrying fr.run/id", sel)
	}
	if sel.Matches(labels.Set{"other": "x"}) {
		t.Errorf("selector %q should not match a pod without fr.run/id", sel)
	}
}

func TestBuildSelectorInvalid(t *testing.T) {
	cfg := &config.Config{LabelSelector: "a=b=c"}

	if _, err := BuildSelector(cfg); err == nil {
		t.Fatal("BuildSelector() with malformed selector: want error, got nil")
	}
}

func TestRestConfigFallsBackToKubeconfig(t *testing.T) {
	// Force the out-of-cluster path even when the tests happen to run inside a
	// pod: InClusterConfig() reports ErrNotInCluster when the service host/port
	// are empty.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	cfg := &config.Config{Kubeconfig: path}

	rc, err := RestConfig(cfg)
	if err != nil {
		t.Fatalf("RestConfig() error = %v", err)
	}
	if want := "https://test.example.com:6443"; rc.Host != want {
		t.Errorf("RestConfig().Host = %q, want %q", rc.Host, want)
	}
}
