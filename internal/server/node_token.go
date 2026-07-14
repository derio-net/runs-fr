package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"time"
)

var validNodeName = regexp.MustCompile(`^[a-z0-9-]+$`)

func (s *server) handleNodeToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Forwarded-Role") != "owner" {
		http.Error(w, "node shell requires owner role", http.StatusForbidden)
		return
	}
	node := r.PathValue("name")
	if !validNodeName.MatchString(node) {
		http.Error(w, "invalid node name", http.StatusBadRequest)
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		http.Error(w, "failed to issue attach token", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(buf)
	s.tokensMu.Lock()
	s.tokens[token] = nodeToken{node: node, user: r.Header.Get(s.cfg.AuthHeader), expires: time.Now().Add(2 * time.Minute)}
	s.tokensMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"token":"` + token + `"}`))
}

func (s *server) consumeNodeToken(token, node, user string) bool {
	s.tokensMu.Lock()
	defer s.tokensMu.Unlock()
	record, ok := s.tokens[token]
	if ok {
		delete(s.tokens, token)
	}
	return ok && record.node == node && record.user == user && time.Now().Before(record.expires)
}
