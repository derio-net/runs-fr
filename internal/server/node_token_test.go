package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// bareServer builds a server with just the fields the node-token path needs,
// so token lifecycle can be exercised without the full mux.
func bareServer() *server {
	return &server{cfg: testConfig(), tokens: make(map[string]nodeToken)}
}

// mintToken drives handleNodeToken as an owner and returns the issued token.
func mintToken(t *testing.T, s *server, node string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node+"/attach-token", nil)
	req.SetPathValue("name", node)
	req.Header.Set(authTestHeader, "owner")
	req.Header.Set("X-Forwarded-Role", "owner")
	rec := httptest.NewRecorder()
	s.handleNodeToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint status = %d, want 200", rec.Code)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Token == "" {
		t.Fatalf("mint response = %q, err=%v", rec.Body.String(), err)
	}
	return body.Token
}

// A minted token is consumed exactly once: the first consume succeeds, the
// second (a URL replay) is refused. The prior TestNodeAttachTokenIsOwnerBound
// only checked the mint gate, never that the token is actually single-use.
func TestNodeAttachTokenSingleUse(t *testing.T) {
	s := bareServer()
	token := mintToken(t, s, "default")

	if !s.consumeNodeToken(token, "default", "owner") {
		t.Fatal("first consume of a valid owner-bound token must succeed")
	}
	if s.consumeNodeToken(token, "default", "owner") {
		t.Fatal("second consume must fail — the token is single-use")
	}
	if len(s.tokens) != 0 {
		t.Fatalf("consumed token left in map: %d entries", len(s.tokens))
	}
}

// A token minted for one node cannot be replayed against another node, and the
// refused attempt still consumes it (so it cannot then be used for its own
// node either — no interception-then-replay).
func TestNodeAttachTokenCrossNodeReplayRefused(t *testing.T) {
	s := bareServer()
	token := mintToken(t, s, "default")

	if s.consumeNodeToken(token, "other", "owner") {
		t.Fatal("token bound to node=default must be refused for node=other")
	}
	if s.consumeNodeToken(token, "default", "owner") {
		t.Fatal("a cross-node attempt must still consume the token — no replay for the correct node")
	}
}

// A token is bound to the minting user: another owner-role user cannot consume it.
func TestNodeAttachTokenUserBound(t *testing.T) {
	s := bareServer()
	token := mintToken(t, s, "default")
	if s.consumeNodeToken(token, "default", "someone-else") {
		t.Fatal("token bound to user=owner must be refused for a different user")
	}
}

// Minting sweeps expired-but-unconsumed tokens so the map cannot grow unbounded
// from "open shell" requests whose tabs were closed before attaching.
func TestNodeAttachTokenMintSweepsExpired(t *testing.T) {
	s := bareServer()
	s.tokens["stale-a"] = nodeToken{node: "default", user: "owner", expires: time.Now().Add(-time.Minute)}
	s.tokens["stale-b"] = nodeToken{node: "build", user: "owner", expires: time.Now().Add(-time.Hour)}
	s.tokens["fresh"] = nodeToken{node: "default", user: "owner", expires: time.Now().Add(time.Minute)}

	_ = mintToken(t, s, "default") // one more fresh token

	for _, k := range []string{"stale-a", "stale-b"} {
		if _, ok := s.tokens[k]; ok {
			t.Errorf("expired token %q not swept on mint", k)
		}
	}
	// The pre-existing fresh token plus the freshly minted one survive.
	if len(s.tokens) != 2 {
		t.Fatalf("after sweep+mint want 2 live tokens, got %d", len(s.tokens))
	}
}
