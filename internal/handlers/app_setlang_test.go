package handlers

import (
	"net/http/httptest"
	"testing"
)

// TestSetLang_BackslashOpenRedirect: browsers normalise "/\evil.com" to
// "//evil.com", so the inline "not //" check is not sufficient.
func TestSetLang_BackslashOpenRedirect(t *testing.T) {
	for _, raw := range []string{`/lang?to=hi&next=/\evil.com`, `/lang?to=hi&next=%2F%5Cevil.com`, `/lang?to=hi&next=//evil.com`} {
		req := httptest.NewRequest("GET", raw, nil)
		w := httptest.NewRecorder()
		(&App{}).SetLang(w, req)
		if got := w.Header().Get("Location"); got != "/" {
			t.Errorf("SetLang(%q) Location = %q, want /", raw, got)
		}
	}
	valid := httptest.NewRequest("GET", "/lang?to=hi&next=/bookings", nil)
	w := httptest.NewRecorder()
	(&App{}).SetLang(w, valid)
	if got := w.Header().Get("Location"); got != "/bookings" {
		t.Errorf("valid next Location = %q, want /bookings", got)
	}
}
