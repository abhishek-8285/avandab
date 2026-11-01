package apm

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPM_Middleware(t *testing.T) {
	tr := NewTracer(50)

	handler := tr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := TraceIDFromContext(r.Context())
		if traceID == "" {
			t.Error("expected traceID in context")
		}
		span := SpanFromContext(r.Context())
		if span == nil {
			t.Error("expected active span in context")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/test", nil)
	req.Header.Set("X-Trace-ID", "trace-abc-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if rec.Header().Get("X-Trace-ID") != "trace-abc-123" {
		t.Errorf("expected X-Trace-ID header echo, got %s", rec.Header().Get("X-Trace-ID"))
	}
	if rec.Header().Get("X-Request-ID") != "trace-abc-123" {
		t.Errorf("expected X-Request-ID header echo, got %s", rec.Header().Get("X-Request-ID"))
	}

	stats := tr.GetStats("http.request")
	if stats.Count != 1 {
		t.Fatalf("expected 1 recorded request, got %d", stats.Count)
	}
}
