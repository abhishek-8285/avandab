package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/metrics"
)

func TestRender_CountersAndHistograms(t *testing.T) {
	reg := metrics.NewRegistry()

	r := chi.NewRouter()
	r.Use(reg.MiddlewareFor())
	r.Get("/ping/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("pong"))
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nf", 404) })

	srv := httptest.NewServer(r)
	defer srv.Close()

	for range 3 {
		resp, err := http.Get(srv.URL + "/ping/42")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()
	}

	out := reg.Render()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/ping/{id}",status=418} 3`,
		`http_request_duration_seconds_bucket{method="GET",route="/ping/{id}",le="+Inf"} 3`,
		`http_request_duration_seconds_count{method="GET",route="/ping/{id}"} 3`,
		"# TYPE http_requests_total counter",
		"app_uptime_seconds",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered metrics missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, `route="unmatched"`) && strings.Contains(out, `status=200`) == false {
		// unmatched routes are labelled but this router has a NotFound handler
		t.Log("note: unmatched label present")
	}
}

func TestMiddleware_UnmatchedRoute(t *testing.T) {
	reg := metrics.NewRegistry()
	r := chi.NewRouter()
	r.Use(reg.MiddlewareFor())
	r.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	out := reg.Render()
	if !strings.Contains(out, `route="unmatched"`) {
		t.Errorf("expected unmatched route label in output:\n%s", out)
	}
}

func TestHandler_ServesExposition(t *testing.T) {
	h := metrics.Handler().ServeHTTP
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type = %q, want Prometheus text version", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("empty metrics body")
	}
}

func TestGuard_LoopbackOpenWithoutToken(t *testing.T) {
	h := metrics.Guard("", metrics.Handler())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback: got %d want 200", rec.Code)
	}
}

func TestGuard_RemoteRequiresToken(t *testing.T) {
	h := metrics.Guard("secret-token", metrics.Handler())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "203.0.113.5:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: got %d want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "203.0.113.5:5000"
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: got %d want 200", rec.Code)
	}
}

func TestGuard_RemoteRefusedWhenNoTokenConfigured(t *testing.T) {
	h := metrics.Guard("", metrics.Handler())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "203.0.113.5:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("remote without configured token: got %d want 401", rec.Code)
	}
}
