package apm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseRecorder) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Middleware creates an APM trace span for every incoming HTTP request and propagates
// X-Trace-ID, X-Request-ID, and X-Correlation-ID headers.
func (t *Tracer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract incoming trace ID if present
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = r.Header.Get("X-Request-ID")
		}
		if traceID == "" {
			traceID = r.Header.Get("X-Correlation-ID")
		}
		if traceID == "" {
			traceID = GenerateID(16)
		}

		ctx := context.WithValue(r.Context(), TraceIDKey, traceID)
		ctx = context.WithValue(ctx, "trace_id", traceID)
		ctx = context.WithValue(ctx, "request_id", traceID)

		ctx, span := t.StartSpan(ctx, "http.request")
		span.SetTag("http.method", r.Method)
		span.SetTag("http.path", r.URL.Path)
		span.SetTag("http.proto", r.Proto)

		// Set response trace headers
		w.Header().Set("X-Trace-ID", traceID)
		w.Header().Set("X-Request-ID", traceID)
		w.Header().Set("X-Correlation-ID", traceID)

		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			span.SetTag("http.route", route)
			span.SetTag("http.status_code", fmt.Sprintf("%d", rec.statusCode))
			if rec.statusCode >= 500 {
				span.SetError(fmt.Errorf("http error %d", rec.statusCode))
			}
			span.End()
		}()

		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

// GlobalMiddleware wraps requests with the package Default tracer.
func Middleware(next http.Handler) http.Handler {
	return Default.Middleware(next)
}
