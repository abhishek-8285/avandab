package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// Gzip must fire on JSON/HTML, must skip SSE streams.
func TestCompressSkipsSSEStreams(t *testing.T) {
	jsonHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vehicles":[` + strings.Repeat(`{"id":"v1","lat":28.6,"lng":77.2},`, 50) + `]}`))
	})
	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	})

	compress := SkipForPaths(
		chiMiddleware.Compress(5),
		"/dashboard/stream",
		"/map/stream",
		"/api/v1/telemetry/stream",
	)

	// 1. JSON API with Accept-Encoding: gzip -> Content-Encoding: gzip
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/live", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	compress(jsonHandler).ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip on JSON API, got %q", rec.Header().Get("Content-Encoding"))
	}
	// Body must decode back to original JSON.
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip decode failed: %v", err)
	}
	raw, _ := io.ReadAll(gr)
	_ = gr.Close()
	if !strings.Contains(string(raw), `"vehicles"`) {
		t.Fatalf("decoded body corrupt, got %q", string(raw)[:50])
	}

	// 2. SSE stream must NOT be gzipped even with Accept-Encoding: gzip.
	for _, p := range []string{"/dashboard/stream", "/map/stream", "/api/v1/telemetry/stream"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		compress(sseHandler).ServeHTTP(rec, req)
		if enc := rec.Header().Get("Content-Encoding"); enc != "" {
			t.Fatalf("expected no compression on SSE %s, got %q", p, enc)
		}
	}

	// 3. Client without Accept-Encoding gets identity (no crash).
	req = httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/live", nil)
	rec = httptest.NewRecorder()
	compress(jsonHandler).ServeHTTP(rec, req)
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("expected identity without Accept-Encoding, got %q", enc)
	}
}
