package apiversion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Negotiate: Accept vendor header wins, path is fallback, empty when neither.
func TestNegotiate(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		path   string
		want   string
	}{
		{"accept wins over path", "application/vnd.transport.v2+json", "/api/v1/trips", V2},
		{"path fallback", "", "/api/v1/trips", V1},
		{"path v2", "", "/api/v2/trips", V2},
		{"neither", "", "/health", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.accept != "" {
				r.Header.Set("Accept", tc.accept)
			}
			if got := Negotiate(r); got != tc.want {
				t.Errorf("Negotiate() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Middleware must inject the path version into context; FromContext round-trips.
func TestMiddleware_InjectsVersion(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
	})
	r := httptest.NewRequest(http.MethodGet, "/api/v2/trips", nil)
	Middleware(next).ServeHTTP(httptest.NewRecorder(), r)
	if got != V2 {
		t.Errorf("context version = %q, want %q", got, V2)
	}

	if v := FromContext(WithVersion(r.Context(), V1)); v != V1 {
		t.Errorf("WithVersion/FromContext round-trip = %q, want %q", v, V1)
	}
}

// Deprecated aliases must carry Deprecation + Sunset headers.
func TestDeprecationMiddleware_Headers(t *testing.T) {
	rec := httptest.NewRecorder()
	DeprecationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Deprecation") != "true" {
		t.Errorf("Deprecation header = %q, want %q", rec.Header().Get("Deprecation"), "true")
	}
	if rec.Header().Get("Sunset") == "" {
		t.Error("Sunset header missing")
	}
}

type stubRegistrar struct{}

func (stubRegistrar) Register(r chi.Router) {
	r.Get("/api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"pong": "v1"}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

// MountV2 must rewrite /api/v2/* to the v1 handler and add deprecation headers.
func TestMountV2_RewritesToV1(t *testing.T) {
	r := chi.NewRouter()
	MountV2(r, nil, nil, stubRegistrar{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v2/ping = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("v2 alias body is not JSON: %v", err)
	}
	if body["pong"] != "v1" {
		t.Errorf("v2 alias served %+v, want v1 handler response", body)
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Error("v2 alias response missing Deprecation header")
	}
}

// Discovery document must advertise current version + supported list.
func TestVersionsHandler_Discovery(t *testing.T) {
	rec := httptest.NewRecorder()
	VersionsHandler(rec, httptest.NewRequest(http.MethodGet, "/api/versions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("VersionsHandler = %d, want 200", rec.Code)
	}
	var doc struct {
		Current  string        `json:"current"`
		Versions []VersionInfo `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("discovery body is not JSON: %v", err)
	}
	if doc.Current != V1 {
		t.Errorf("current = %q, want %q", doc.Current, V1)
	}
	if len(doc.Versions) != len(Supported) {
		t.Errorf("versions count = %d, want %d", len(doc.Versions), len(Supported))
	}
}
