package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/coder/websocket"

	"github.com/derio-net/runs-fr/internal/runs"
)

// validRunID constrains the {id} path segment. It is the trust boundary for the
// attach endpoint: only ids matching this pattern are ever used to resolve a
// pod or derive a tmux session name, so a hostile segment can never be
// interpolated into a Kubernetes request.
var validRunID = regexp.MustCompile(`^(?:[a-z0-9-]+|node:[a-z0-9-]+)$`)

// RunResolver maps a validated run id to the name of the live pod backing it.
type RunResolver interface {
	Resolve(ctx context.Context, id string) (podName string, err error)
}

// Attacher bridges an accepted browser websocket to a pod's tmux session over
// the pods/exec stream. It owns the exec lifetime: it returns when the stream
// ends, the peer dies, or ctx is cancelled.
type Attacher interface {
	Attach(ctx context.Context, conn *websocket.Conn, podName, sessionName string) error
}

// WithAttach registers the forward-auth-gated GET /api/runs/{id}/attach route,
// backed by the given pod resolver and exec attacher. Without it the route is
// not served.
func WithAttach(resolver RunResolver, attacher Attacher) Option {
	return func(s *server) {
		s.resolver = resolver
		s.attacher = attacher
	}
}

// WithShutdownContext ties open attach sessions to the server's lifetime: when
// the given context is cancelled (graceful shutdown), in-flight exec streams
// are torn down. Without it, sessions are bound only to their request context.
func WithShutdownContext(ctx context.Context) Option {
	return func(s *server) { s.shutdownCtx = ctx }
}

// tmuxSessionName derives the tmux session to attach from the validated id. The
// run harness names its session after the run id; keeping this a single
// function localises the convention if it ever changes.
func tmuxSessionName(id string) string { return id }

// handleAttach upgrades GET /api/runs/{id}/attach to a websocket and bridges it
// to the run pod's tmux session. Validation and pod resolution happen BEFORE
// the upgrade so failures surface as plain HTTP status codes.
func (s *server) handleAttach(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validRunID.MatchString(id) {
		http.Error(w, "invalid run id", http.StatusBadRequest)
		return
	}
	if len(id) >= len("node:") && id[:len("node:")] == "node:" && r.Header.Get("X-Forwarded-Role") != "owner" {
		http.Error(w, "node shell requires owner role", http.StatusForbidden)
		return
	}
	if len(id) >= len("node:") && id[:len("node:")] == "node:" && !s.consumeNodeToken(r.URL.Query().Get("token"), id[len("node:"):], r.Header.Get(s.cfg.AuthHeader)) {
		http.Error(w, "invalid or expired node attach token", http.StatusForbidden)
		return
	}

	podName, err := s.resolver.Resolve(r.Context(), id)
	switch {
	case errors.Is(err, runs.ErrNotFound):
		http.Error(w, "no run for id", http.StatusNotFound)
		return
	case err != nil:
		s.log.Error("resolve run pod", "id", id, "err", err)
		http.Error(w, "failed to resolve run", http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept already wrote the HTTP error.
		s.log.Debug("websocket accept failed", "id", id, "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	// The exec stream lives until the request ends, the server shuts down, or
	// the stream itself errors — whichever comes first.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if s.shutdownCtx != nil {
		stop := context.AfterFunc(s.shutdownCtx, cancel)
		defer stop()
	}

	// Attach blocks until the stream ends; that is fine here because this
	// handler already owns a dedicated per-request goroutine — no extra
	// goroutine + join is needed (the attacher runs its own ping loop). The
	// exec target is the RESOLVED pod name, never the raw path segment.
	if err := s.attacher.Attach(ctx, conn, podName, tmuxSessionName(id)); err != nil && ctx.Err() == nil {
		s.log.Debug("attach ended", "id", id, "err", err)
	}
}
