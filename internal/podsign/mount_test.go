package podsign

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mountHandler replicates the /uploads/pod/* mount from cmd/server/main.go so
// the deployed route logic is tested end to end (signature verify → ServeFile)
// without depending on the full server wiring.
func mountHandler(t *testing.T, s *Signer, uploadDir string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filename, sigErr := s.Verify(r.URL.Path, r.URL.RawQuery)
		if sigErr != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, filepath.Join(uploadDir, "pod", filename))
	})
}

func setup(t *testing.T) (*Signer, string) {
	t.Helper()
	dir := t.TempDir()
	podDir := filepath.Join(dir, "pod")
	if err := os.MkdirAll(podDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(podDir, "secret-evidence.jpg"), []byte("PHOTO-DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New([]byte("0123456789abcdef0123456789abcdef"), DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestMount_SignedRequestServesFile(t *testing.T) {
	s, dir := setup(t)
	srv := httptest.NewServer(mountHandler(t, s, dir))
	defer srv.Close()

	signed, err := s.Sign("secret-evidence.jpg")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(srv.URL + signed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("signed GET status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "PHOTO-DATA" {
		t.Fatalf("body = %q, want file contents", string(body))
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff header")
	}
}

// TestMount_RejectsUnsignedRequest is the pre-fix attack: an anonymous caller
// hitting /uploads/pod/<file> directly with no signature. Must be 404.
func TestMount_RejectsUnsignedRequest(t *testing.T) {
	s, dir := setup(t)
	srv := httptest.NewServer(mountHandler(t, s, dir))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/uploads/pod/secret-evidence.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unsigned GET status = %d, want 404 (file must not be served)", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if strings.Contains(string(body), "PHOTO-DATA") {
		t.Fatal("unsigned request received the protected file contents")
	}
}

func TestMount_RejectsExpiredSignedURL(t *testing.T) {
	// Issue a URL that already expired by rewinding the clock.
	past := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	issued, err := New([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issued.clock = func() time.Time { return past }
	signed, err := issued.Sign("secret-evidence.jpg")
	if err != nil {
		t.Fatal(err)
	}

	// Verify with a "now" well past expiry.
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	verifier, err := New([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	verifier.clock = func() time.Time { return now }

	req := httptest.NewRequest(http.MethodGet, signed, nil)
	rec := httptest.NewRecorder()
	mountHandler(t, verifier, t.TempDir()).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expired signed URL status = %d, want 404", rec.Code)
	}
}

// TestMount_RegressionNoStripPrefix guards the production wiring in
// cmd/server/main.go. The pod mount must NOT wrap the handler in
// http.StripPrefix("/uploads/", ...): doing so rewrites r.URL.Path to
// "pod/<file>", the signer then correctly rejects the multi-element path,
// and every signed request 404s — the e-POD page renders broken images
// while unsigned attacks are blocked. Caught by live HTTP probing on
// 2026-09-17, invisible to the unit suite.
func TestMount_RegressionNoStripPrefix(t *testing.T) {
	s, dir := setup(t)

	req := httptest.NewRequest(http.MethodGet, "/uploads/pod/secret-evidence.jpg", nil)
	req.URL.RawQuery = "" // unsigned on purpose

	// Correct wiring: full path reaches Verify.
	rec := httptest.NewRecorder()
	mountHandler(t, s, dir).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unsigned via full path: %d, want 404", rec.Code)
	}

	// Signed request against the full /uploads/pod/ path must serve.
	signed, err := s.Sign("/uploads/pod/secret-evidence.jpg")
	if err != nil {
		t.Fatal(err)
	}
	signedReq := httptest.NewRequest(http.MethodGet, signed, nil)
	rec2 := httptest.NewRecorder()
	mountHandler(t, s, dir).ServeHTTP(rec2, signedReq)
	if rec2.Code != http.StatusOK {
		t.Fatalf("signed via full path: %d, want 200 (StripPrefix-style wiring would 404 here)", rec2.Code)
	}
}

// TestMount_RejectsTraversalSigned verifies traversal is blocked even
// through a full http.Server round trip.
func TestMount_RejectsTraversalSigned(t *testing.T) {
	s, dir := setup(t)
	srv := httptest.NewServer(mountHandler(t, s, dir))
	defer srv.Close()

	// Even a valid signature over a traversal filename is rejected by the
	// signer itself, so the handler can never escape the pod dir.
	for _, bad := range []string{"../../etc/passwd", "sub/x.jpg"} {
		if _, err := s.Sign("/uploads/pod/" + bad); err == nil {
			t.Errorf("Sign accepted traversal path %q", bad)
		}
		res, err := http.Get(srv.URL + "/uploads/pod/" + bad)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("traversal %q status = %d, want 404", bad, res.StatusCode)
		}
	}
}
