package rag

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHandler_PermissionGuards(t *testing.T) {
	svc := NewService(nil, nil, 500, 50, t.TempDir())
	h := NewHandler(svc)

	denied := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, name+" denied", http.StatusForbidden)
			})
		}
	}

	r := chi.NewRouter()
	h.WithPermissionGuards(denied("read"), denied("write")).RegisterRoutes(r)

	cases := []struct {
		method, path string
	}{
		{"POST", "/api/rag/search"},
		{"GET", "/api/rag/stats"},
		{"POST", "/api/rag/index"},
		{"POST", "/api/rag/reindex"},
		{"POST", "/api/rag/teach"},
		{"POST", "/api/rag/upload"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 from guard, got %d", c.method, c.path, w.Code)
		}
	}
}

func TestHandler_NoGuards_ReachesHandlers(t *testing.T) {
	store, err := NewVectorStore(t.TempDir() + "/vectors.db")
	if err != nil {
		t.Fatal(err)
	}
	// Close before t.TempDir() cleanup: Windows cannot unlink an open
	// vectors.db, so a leaked handle fails the test on cleanup.
	t.Cleanup(func() { _ = store.Close() })
	svc := NewService(NewHashEmbedder(64), store, 500, 50, t.TempDir())
	h := NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/api/rag/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Error("unguarded route must not return 403")
	}
}
