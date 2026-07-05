// Package localterm is the gateway's local backend: it lists tmux sessions on
// the gateway's own host as runs and attaches to them over a local PTY. It is
// the CNC laptop-runner shape — the wake receiver creates a tmux session named
// after the run id in the same container, so no cluster or Docker socket is
// involved. The security contract is unchanged: a forward-auth proxy still
// fronts every /api route.
package localterm

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/derio-net/runs-fr/internal/runs"
)

// tmuxRunner executes a tmux invocation and returns its combined output —
// injectable so the lister/resolver logic is unit-testable without tmux.
type tmuxRunner func(ctx context.Context, args ...string) ([]byte, error)

// Lister lists tmux sessions as Runs and resolves attach targets. The
// "session name = run id" convention is shared with the wake receiver that
// creates the sessions.
type Lister struct {
	socket     string
	runnerName string
	runTmux    tmuxRunner
}

// NewLister builds a Lister over the given tmux socket (empty = tmux's
// default) tagging runs with runnerName (empty = untagged).
func NewLister(socket, runnerName string) *Lister {
	return &Lister{
		socket:     socket,
		runnerName: runnerName,
		runTmux:    execTmux,
	}
}

// execTmux shells out to tmux, combined output so "no server running" (which
// tmux prints to stderr) is classifiable by the caller.
func execTmux(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
}

// args prefixes the socket flag when configured.
func (l *Lister) args(rest ...string) []string {
	if l.socket == "" {
		return rest
	}
	return append([]string{"-S", l.socket}, rest...)
}

// List returns one Run per live tmux session, sorted by id. A missing tmux
// server means the last session ended — zero runs, not an error.
func (l *Lister) List(ctx context.Context) ([]runs.Run, error) {
	out, err := l.runTmux(ctx, l.args("ls", "-F", "#{session_name}")...)
	if err != nil {
		if strings.Contains(string(out), "no server running") {
			return []runs.Run{}, nil
		}
		return nil, fmt.Errorf("localterm: tmux ls: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	var rs []runs.Run
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		rs = append(rs, runs.Run{ID: name, Status: "Running", Runner: l.runnerName})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
	if rs == nil {
		rs = []runs.Run{}
	}
	return rs, nil
}

// Resolve reports whether a session named id is live, returning the session
// name itself as the attach handle (the local attacher targets sessions, not
// pods). The "=" prefix forces tmux's exact-match semantics.
func (l *Lister) Resolve(ctx context.Context, id string) (string, error) {
	if _, err := l.runTmux(ctx, l.args("has-session", "-t", "="+id)...); err != nil {
		return "", runs.ErrNotFound
	}
	return id, nil
}
