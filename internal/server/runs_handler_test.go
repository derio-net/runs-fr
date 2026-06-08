package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/derio-net/runs-fr/internal/runs"
)

// stubLister is a fixed RunLister for handler tests.
type stubLister struct {
	runs []runs.Run
	err  error
}

func (s stubLister) List(context.Context) ([]runs.Run, error) { return s.runs, s.err }

func TestRunsHandler(t *testing.T) {
	want := []runs.Run{
		{ID: "run-1", Plan: "gateway", Phase: "2", Branch: "feature", Harness: "codex", Status: "Pending", PodName: "pod-abc"},
		{ID: "run-2", Plan: "gateway", Phase: "5", Branch: "main", Harness: "claude", Status: "Running", PodName: "pod-zed"},
	}

	h := New(testConfig(), WithLister(stubLister{runs: want}))

	// /api/runs is behind the forward-auth gate (Phase 3), so the handler test
	// supplies the trusted header an upstream proxy would set.
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set(authTestHeader, "tester")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/runs status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got []runs.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (body=%q)", err, rec.Body.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GET /api/runs = %#v\nwant %#v", got, want)
	}
}

func TestRunsHandlerListError(t *testing.T) {
	h := New(testConfig(), WithLister(stubLister{err: context.DeadlineExceeded}))

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	req.Header.Set(authTestHeader, "tester")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/runs (lister error) status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
