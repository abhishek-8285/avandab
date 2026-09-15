package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// captured holds what the stub server saw for one request.
type captured struct {
	auth string
	body string
}

// captureServer records the Authorization header and body per request while
// replying 200 with a minimal JSON body each command can parse.
func captureServer(t *testing.T) (*httptest.Server, *[]captured) {
	t.Helper()
	seen := &[]captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*seen = append(*seen, captured{
			auth: r.Header.Get("Authorization"),
			body: string(raw),
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"chunks":[],"scores":[],"count":0}`)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

// TestAuthorizedRequestSendsBearerHeader pins the CLI fix: every RAG command
// must carry Authorization: Bearer <token>. Before the fix the CLI used bare
// http.Post/http.Get with no auth, so all four commands always got 401
// "api token invalid" from RequireAPIAuth.
func TestAuthorizedRequestSendsBearerHeader(t *testing.T) {
	srv, seen := captureServer(t)

	resp, err := authorizedRequest(srv.URL, "tok-123", http.MethodPost, "/api/rag/search", "application/json", bytes.NewReader([]byte(`{"query":"x"}`)))
	if err != nil {
		t.Fatalf("authorizedRequest: %v", err)
	}
	defer resp.Body.Close()

	if len(*seen) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*seen))
	}
	if got := (*seen)[0].auth; got != "Bearer tok-123" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer tok-123")
	}
}

// TestAuthorizedRequestHandlesNilBody guards the GET path (stats sends no body):
// a nil io.Reader must not panic.
func TestAuthorizedRequestHandlesNilBody(t *testing.T) {
	srv, seen := captureServer(t)

	resp, err := authorizedRequest(srv.URL, "tok-123", http.MethodGet, "/api/rag/stats", "", nil)
	if err != nil {
		t.Fatalf("authorizedRequest with nil body: %v", err)
	}
	defer resp.Body.Close()

	if got := (*seen)[0].auth; got != "Bearer tok-123" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer tok-123")
	}
}

func TestAuthorizedRequestOmitsHeaderWhenTokenEmpty(t *testing.T) {
	srv, seen := captureServer(t)

	resp, err := authorizedRequest(srv.URL, "", http.MethodPost, "/api/rag/search", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("authorizedRequest: %v", err)
	}
	defer resp.Body.Close()

	if got := (*seen)[0].auth; got != "" {
		t.Fatalf("Authorization header = %q, want empty when no token configured", got)
	}
}

// TestCommandsSendBearerHeader drives each command handler against a stub
// server and asserts the token reaches the wire.
func TestCommandsSendBearerHeader(t *testing.T) {
	srv, seen := captureServer(t)
	const token = "tok-abc"
	want := "Bearer " + token

	dir := t.TempDir()
	doc := filepath.Join(dir, "note.md")
	if err := os.WriteFile(doc, []byte("pricing policy"), 0o600); err != nil {
		t.Fatalf("write temp doc: %v", err)
	}

	handleStats(srv.URL, token)
	handleSearch(srv.URL, token, []string{"fuel", "3"})
	handleTeach(srv.URL, token, []string{"topic", doc})
	handleIndex(srv.URL, token, []string{dir})

	if len(*seen) != 4 {
		t.Fatalf("expected 4 requests (stats/search/teach/index), got %d", len(*seen))
	}
	for i, got := range *seen {
		if got.auth != want {
			t.Errorf("request %d: Authorization = %q, want %q", i+1, got.auth, want)
		}
	}
}

func TestGetEnvFallsBack(t *testing.T) {
	if got := getEnv("RAG_TEST_UNSET_VAR", "http://localhost:8080"); got != "http://localhost:8080" {
		t.Fatalf("getEnv fallback = %q", got)
	}
	t.Setenv("RAG_TEST_SET_VAR", "http://example.test")
	if got := getEnv("RAG_TEST_SET_VAR", "fallback"); got != "http://example.test" {
		t.Fatalf("getEnv override = %q", got)
	}
}

// TestTeachStdInContract guards the documented usage: `bin/rag teach <topic>`
// with content piped on stdin posts that content to /api/rag/teach.
func TestTeachStdInContract(t *testing.T) {
	srv, seen := captureServer(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		defer w.Close()
		_, _ = io.WriteString(w, "piped knowledge content")
	}()

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	handleTeach(srv.URL, "tok-stdin", []string{"piped topic"})

	if len(*seen) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*seen))
	}
	if got := (*seen)[0].auth; got != "Bearer tok-stdin" {
		t.Fatalf("stdin teach Authorization = %q, want %q", got, "Bearer tok-stdin")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte((*seen)[0].body), &payload); err != nil {
		t.Fatalf("teach body not valid JSON: %v", err)
	}
	if payload["name"] != "piped topic" {
		t.Errorf("posted name = %q, want %q", payload["name"], "piped topic")
	}
	if payload["content"] != "piped knowledge content" {
		t.Errorf("posted content = %q, want the piped stdin content", payload["content"])
	}
	if !bytes.Contains([]byte((*seen)[0].body), []byte("piped")) {
		t.Errorf("body missing piped content: %s", (*seen)[0].body)
	}
}
