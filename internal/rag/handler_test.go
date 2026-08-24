package rag

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
)

// allowAllAuthorizationService stands in for the Casbin-backed service so
// route-level permission wiring is exercised without an enforcer instance.
type allowAllAuthorizationService struct{}

func (allowAllAuthorizationService) Can(string, string, string) bool { return true }
func (allowAllAuthorizationService) Reload() error                   { return nil }
func (allowAllAuthorizationService) AddRoleForUser(string, string) error {
	return nil
}
func (allowAllAuthorizationService) DeleteRolesForUser(string) error { return nil }

func newSecuredRouter(t *testing.T) (*chi.Mux, *Handler, []byte, *auth.SessionStore) {
	t.Helper()
	store, err := NewVectorStore(filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewHashEmbedder(64), store, 500, 50, t.TempDir())
	h := NewHandler(svc)

	secret := []byte("rag-route-auth-test-secret-32bytes")
	sessionStore := auth.NewSessionStore(string(secret), false)
	authSrv := allowAllAuthorizationService{}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAPIAuth(sessionStore, secret, nil))
		h.WithPermissionGuards(
			middleware.RequirePermission(authSrv, "rag", "read"),
			middleware.RequirePermission(authSrv, "rag", "write"),
		).RegisterRoutes(r)
	})
	return r, h, secret, sessionStore
}

func apiToken(t *testing.T, secret []byte) string {
	t.Helper()
	token, err := auth.IssueAPIToken(secret, auth.APITokenClaims{
		UserID: "usr-rag-test",
		Role:   "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// TestRagRequiresAuth — Spec 10 §7.4: no credentials -> 401; valid API
// token -> handler reachable (200 on stats).
func TestRagRequiresAuth(t *testing.T) {
	r, _, secret, _ := newSecuredRouter(t)
	token := apiToken(t, secret)

	req := httptest.NewRequest("POST", "/api/rag/search", strings.NewReader(`{"query":"x"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated search: expected 401, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/rag/search", strings.NewReader(`{"query":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Fatalf("authenticated search rejected: %d %s", w.Code, w.Body.String())
	}
}

// TestRagIndexAllowList — Spec 10 §7.4: authenticated request for a
// non-allow-listed directory -> 403.
func TestRagIndexAllowList(t *testing.T) {
	r, _, secret, _ := newSecuredRouter(t)
	token := apiToken(t, secret)

	body, _ := json.Marshal(map[string]string{"directory": "/etc"})
	req := httptest.NewRequest("POST", "/api/rag/index", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("index outside allow-list: expected 403, got %d %s", w.Code, w.Body.String())
	}
}

func uploadRequest(t *testing.T, path, filename string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(fw, strings.NewReader("knowledge base content")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/rag/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestHandler_UploadRejectsPathTraversal: client-controlled multipart
// filenames must never escape the upload directory.
func TestHandler_UploadRejectsPathTraversal(t *testing.T) {
	store, err := NewVectorStore(filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatal(err)
	}
	uploadDir := t.TempDir()
	outside := t.TempDir()
	svc := NewService(NewHashEmbedder(64), store, 500, 50, uploadDir)
	h := NewHandler(svc)

	escapeTarget := filepath.Join(outside, "pwned.txt")
	rel, err := filepath.Rel(uploadDir, escapeTarget)
	if err != nil {
		t.Fatal(err)
	}
	traversalName := strings.Join([]string{strings.Repeat("../", strings.Count(rel, "..")+1), filepath.Base(escapeTarget)}, "")

	cases := []struct{ name, filename string }{
		{"dotdot relative", "../../pwned.txt"},
		{"backslash dotdot", `..\..\pwned.txt`},
		{"absolute path", "/etc/passwd"},
		{"crafted traversal", traversalName},
	}
	for _, c := range cases {
		r := chi.NewRouter()
		h.RegisterRoutes(r)
		req := uploadRequest(t, "/api/rag/upload", c.filename)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if _, statErr := os.Stat(escapeTarget); statErr == nil {
			t.Errorf("%s: file escaped upload dir to %s", c.name, escapeTarget)
		}
		entries, _ := os.ReadDir(uploadDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), "..") || filepath.IsAbs(e.Name()) {
				t.Errorf("%s: hostile name landed in upload dir: %s", c.name, e.Name())
			}
		}
		if w.Code >= 500 && strings.Contains(w.Body.String(), "pwned") {
			t.Errorf("%s: error leaks unsanitized name", c.name)
		}
	}
}

// TestHandler_UploadSanitizedNameIndexed: a benign filename uploads and
// indexes successfully, proving sanitization did not break the happy path.
func TestHandler_UploadSanitizedNameIndexed(t *testing.T) {
	store, err := NewVectorStore(filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatal(err)
	}
	uploadDir := t.TempDir()
	svc := NewService(NewHashEmbedder(64), store, 500, 50, uploadDir)
	h := NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	req := uploadRequest(t, "/api/rag/upload", "driver-policy.md")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("benign upload failed: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.File != "driver-policy.md" {
		t.Errorf("response echoes original name, got %q", resp.File)
	}
}
