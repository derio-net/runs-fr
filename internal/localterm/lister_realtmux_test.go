package localterm

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestListerAgainstRealTmux exercises the lister against a REAL tmux server on a
// private socket — not the fake. Its whole reason to exist: the fakes script
// tmux output that real tmux cannot actually emit. In particular the old
// `node:<name>` convention is impossible — tmux treats ":" as its
// session:window separator, silently rewriting `node:default` to `node_default`
// and erroring on `has-session -t "=node:default"`. This test proves the
// colon-free `node-<name>` convention round-trips through real tmux: it lists,
// classifies as a node shell, and resolves exactly.
//
// Skipped when tmux is absent (local dev without it); CI's ubuntu-latest Go job
// ships tmux, so it runs there.
func TestListerAgainstRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; skipping real-tmux integration test")
	}

	socket := filepath.Join(t.TempDir(), "tmux.sock")
	tmux := func(args ...string) error {
		cmd := exec.Command("tmux", append([]string{"-S", socket}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("tmux %v: %s", args, out)
		}
		return err
	}
	// Detached sessions: a node login shell and an ordinary run terminal.
	if err := tmux("new-session", "-d", "-s", "node-default", "sh"); err != nil {
		t.Fatalf("create node-default session: %v", err)
	}
	if err := tmux("new-session", "-d", "-s", "run-abc", "sh"); err != nil {
		t.Fatalf("create run-abc session: %v", err)
	}
	t.Cleanup(func() { _ = tmux("kill-server") })

	l := NewLister(socket, "runner-x")

	got, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	// Sorted: node-default < run-abc.
	if len(got) != 2 {
		t.Fatalf("List() = %#v, want 2 sessions", got)
	}
	if got[0].ID != "node-default" || got[0].Kind != "node" || got[0].NodeName != "default" {
		t.Errorf("node row = %#v, want id=node-default kind=node nodeName=default", got[0])
	}
	if got[1].ID != "run-abc" || got[1].Kind != "" {
		t.Errorf("run row = %#v, want id=run-abc kind=(run)", got[1])
	}

	// The node session resolves to itself (exact `=` match) via real has-session.
	if h, err := l.Resolve(context.Background(), "node-default"); err != nil || h != "node-default" {
		t.Fatalf("Resolve(node-default) = %q, %v; want node-default, nil", h, err)
	}
	// A non-existent node session does not resolve.
	if _, err := l.Resolve(context.Background(), "node-missing"); err == nil {
		t.Fatal("Resolve(node-missing) unexpectedly succeeded")
	}
}
