// Package localterm is the gateway's local backend: it lists tmux sessions on
// the gateway's own host as runs and attaches to them over a local PTY. It is
// the CNC laptop-runner shape — the wake receiver creates a tmux session named
// after the run id in the same container, so no cluster or Docker socket is
// involved. The security contract is unchanged: a forward-auth proxy still
// fronts every /api route.
package localterm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/derio-net/runs-fr/internal/runs"
)

// tmuxRunner executes a tmux invocation, returning stdout and, on failure,
// an error whose message carries stderr — injectable so the lister/resolver
// logic is unit-testable without tmux. Session names are parsed from stdout
// ONLY, so stderr chatter on a successful ls can't become phantom runs.
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

// execTmux shells out to tmux with SPLIT streams: stdout is returned for
// parsing; stderr rides the error message for classification ("no server
// running" — stable, unlocalized in tmux ≥2.1; older tmux says "failed to
// connect to server", which surfaces loudly as a 500).
func execTmux(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// attachableName mirrors the attach route's id constraint
// (server.validRunID, ^[a-z0-9-]+$): a session outside it would list as a
// run whose attach always 400s — e.g. a human's debug session named "WORK".
// Skip those instead of serving dead rows.
var attachableName = regexp.MustCompile(`^[a-z0-9-]+$`)

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
		// tmux exits its server with the last session; "no server running"
		// is zero runs, not an error.
		if strings.Contains(err.Error(), "no server running") {
			return []runs.Run{}, nil
		}
		return nil, fmt.Errorf("localterm: tmux ls: %w", err)
	}

	var rs []runs.Run
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || !attachableName.MatchString(name) {
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
