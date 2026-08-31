package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
)

func newAuthTestApp(t *testing.T) *AuthHandlers {
	t.Helper()
	if cwd, _ := os.Getwd(); filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	authStore := auth.NewSessionStore("test-secret-key-that-is-at-least-32-chars-long", false)
	cfg := &config.Config{
		AppEnv:       "development",
		CookieSecure: false,
	}

	app := &App{
		Templates:   tmpl,
		AuthStore:   authStore,
		Config:      cfg,
		ResetTokens: auth.NewResetTokenStore(0),
	}
	return NewAuthHandlers(app)
}

func TestLoginPage_FlashSuccessAndError(t *testing.T) {
	h := newAuthTestApp(t)

	// Test with flash_success cookie
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: "flash_success", Value: "Password reset successful."})
	w := httptest.NewRecorder()

	h.LoginPage(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Password reset successful.")

	// Verify cookie clearing
	cookies := w.Result().Cookies()
	var clearedSuccess bool
	for _, c := range cookies {
		if c.Name == "flash_success" && c.MaxAge == -1 {
			clearedSuccess = true
		}
	}
	assert.True(t, clearedSuccess, "flash_success cookie should be cleared")

	// Test with flash_error cookie
	req2 := httptest.NewRequest(http.MethodGet, "/login", nil)
	req2.AddCookie(&http.Cookie{Name: "flash_error", Value: "Invalid credentials."})
	w2 := httptest.NewRecorder()

	h.LoginPage(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	body2 := w2.Body.String()
	assert.Contains(t, body2, "Invalid credentials.")
}

func TestAuthPages_SplitScreenLayoutRenders(t *testing.T) {
	h := newAuthTestApp(t)

	pages := []struct {
		name     string
		url      string
		handler  func(w http.ResponseWriter, r *http.Request)
		contains []string
	}{
		{
			name:     "register",
			url:      "/register",
			handler:  h.RegisterPage,
			contains: []string{"Create your operator account", "Register Operator Account", "Already registered?"},
		},
		{
			name:     "forgot_password",
			url:      "/forgot-password",
			handler:  h.ForgotPasswordPage,
			contains: []string{"Forgot password", "Send Reset Link", "Back to Sign In"},
		},
		{
			name:     "reset_password",
			url:      "/reset-password?token=test-token",
			handler:  h.ResetPasswordPage,
			contains: []string{"Reset password", `name="token" value="test-token"`},
		},
	}

	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p.url, nil)
			p.handler(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			body := w.Body.String()
			for _, want := range p.contains {
				assert.Contains(t, body, want)
			}
			// Split-screen brand panel shared with the login page.
			assert.Contains(t, body, "background:#0f172a;", "left brand panel missing")
			// Responsive: brand panel hidden below lg breakpoint.
			assert.Contains(t, body, "hidden lg:flex", "responsive brand panel classes missing")
			// Auth pages are anonymous: theme.js must not sync preferences.
			assert.Contains(t, body, `data-authenticated="false"`, "anonymous pages must not enable preference sync")
		})
	}
}

func TestChangePassword_MismatchRendersHTML(t *testing.T) {
	h := newAuthTestApp(t)

	form := url.Values{}
	form.Set("old_password", "oldpass123")
	form.Set("new_password", "newpass123")
	form.Set("confirm_password", "differentpass")

	req := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Add authenticated session context
	sess := &auth.SessionData{
		UserID: "user-1",
		Role:   string(domain.RoleAdmin),
		Name:   "Test User",
	}
	ctx := context.WithValue(req.Context(), auth.ContextUser, sess)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.ChangePassword(w, req)

	// Should return 200 OK and render change_password.html with error alert, NOT raw plain text 400
	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Passwords do not match")
	assert.Contains(t, body, "Change Password")
}
