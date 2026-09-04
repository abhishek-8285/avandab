package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
)

// googleStateCookie carries the OAuth CSRF state between Begin and Callback.
// Path is scoped to the flow so it never leaks to other routes.
const googleStateCookie = "oauth_state"

// GoogleOAuthHandlers implements "Sign in with Google" and "Sign up with Google"
// for the web portal (zero-cost identity plan). Routes are public by design —
// the documented exception in AGENTS.md rule 5, same class as telemetry
// device-token ingest; every other route stays behind RequireAuth.
//
// Unconfigured (GOOGLE_CLIENT_ID empty) both handlers redirect to /login with
// a flash message and the UI hides the button — the feature degrades, it does
// not break.
type GoogleOAuthHandlers struct {
	*App
	oauth *auth.OAuthConfig
}

// NewGoogleOAuthHandlers wires the Google flow onto the shared app deps.
func NewGoogleOAuthHandlers(app *App, cfg *auth.OAuthConfig) *GoogleOAuthHandlers {
	return &GoogleOAuthHandlers{App: app, oauth: cfg}
}

// Enabled reports whether Google sign-in is configured.
func (h *GoogleOAuthHandlers) Enabled() bool {
	return h.oauth != nil && h.oauth.ClientID != ""
}

// GoogleEnabledFor exposes the flag to template data (login/register pages).
func (a *App) GoogleEnabledFor() bool {
	return a != nil && a.GoogleOAuth != nil && a.GoogleOAuth.ClientID != ""
}

// RegisterRoutes mounts the public Google OAuth2 routes.
func (h *GoogleOAuthHandlers) RegisterRoutes(r chi.Router) {
	r.Get("/auth/google", h.Begin)
	r.Get("/auth/google/callback", h.Callback)
}

// Begin starts the OAuth flow: issue a random state (HttpOnly, SameSite=Lax,
// 10-minute cookie scoped to /auth/google) and redirect to Google consent.
func (h *GoogleOAuthHandlers) Begin(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled() {
		h.flashRedirect(w, r, "Google sign-in is not configured.", "/login")
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		http.Error(w, "state generation failed", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    state,
		Path:     "/auth/google",
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, h.oauth.AuthCodeURL(state), http.StatusSeeOther)
}

// Callback completes the flow: verify state (CSRF), exchange the code, fetch
// the verified Google profile, resolve/provision the Avandab account, and
// create the session through the SAME server-side session machinery as
// password login.
func (h *GoogleOAuthHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	clearState := func() {
		http.SetCookie(w, &http.Cookie{Name: googleStateCookie, Value: "", Path: "/auth/google", MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: "g_state", Value: "", Path: "/auth/google", MaxAge: -1})
	}

	if !h.Enabled() {
		h.flashRedirect(w, r, "Google sign-in is not configured.", "/login")
		return
	}
	if q := r.URL.Query().Get("error"); q != "" {
		clearState()
		h.flashRedirect(w, r, "Google sign-in was cancelled. Please try again.", "/login")
		return
	}

	wantState, errState := r.Cookie(googleStateCookie)
	if errState != nil {
		wantState, errState = r.Cookie("g_state")
	}
	gotState := r.URL.Query().Get("state")
	if errState != nil || wantState.Value == "" || gotState == "" || wantState.Value != gotState {
		// CSRF / replay: state cookie and query must match exactly.
		clearState()
		h.flashRedirect(w, r, "Sign-in could not be verified (invalid state). Please try again.", "/login")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		clearState()
		h.flashRedirect(w, r, "Google did not return an authorization code. Please try again.", "/login")
		return
	}

	if h.Services == nil || h.Services.Users == nil {
		clearState()
		http.Error(w, "authentication service unavailable", http.StatusInternalServerError)
		return
	}

	tok, err := h.oauth.Exchange(r.Context(), code)
	if err != nil {
		clearState()
		slog.Error("google oauth token exchange failed", slog.Any("error", err))
		h.flashRedirect(w, r, "Google sign-in failed. Please try again or use your password.", "/login")
		return
	}
	info, err := h.oauth.FetchUserInfo(r.Context(), tok.AccessToken)
	if err != nil {
		clearState()
		slog.Error("google oauth userinfo fetch failed", slog.Any("error", err))
		h.flashRedirect(w, r, "Google sign-in failed. Please try again or use your password.", "/login")
		return
	}
	if !info.IsEmailVerified() {
		clearState()
		h.flashRedirect(w, r, "Your Google account email is not verified. Verify it at Google and retry.", "/login")
		return
	}

	user, isNewOwner, err := h.Services.Users.ResolveGoogleUser(r.Context(), info.GetID(), info.Email, info.Name)
	if err != nil {
		clearState()
		if err == domain.ErrUnauthorized {
			h.flashRedirect(w, r, "This account is suspended. Contact support.", "/login")
			return
		}
		slog.Error("google identity resolution failed", "email", info.Email, slog.Any("error", err))
		h.flashRedirect(w, r, "Google sign-in failed. Please try again or use your password.", "/login")
		return
	}

	roleName := string(auth.RoleNameForID(user.Role.ID))
	if user.Role.Name != "" {
		roleName = string(user.Role.Name)
	}
	if isNewOwner {
		// New tenant owners bind org_admin — never platform admin, which
		// would expose /tenants, suspend, and global-admin minting.
		roleName = string(domain.RoleOrgAdmin)
	}
	if h.AuthSrv != nil {
		_ = h.AuthSrv.AddRoleForUser(user.ID.String(), roleName)
	}

	// Session via existing machinery — server-side token + signed cookie,
	// identical to password login (no parallel session scheme).
	sess, err := h.Services.Auth.CreateSessionForUser(r.Context(), user.ID)
	if err != nil || sess == nil {
		clearState()
		slog.Error("google sign-in session creation failed", "user_id", user.ID, slog.Any("error", err))
		http.Error(w, "session creation failed; please retry sign-in", http.StatusInternalServerError)
		return
	}
	h.AuthStore.CreateSessionWithToken(w, user.ID.String(), roleName, user.Name, sess.SessionToken)
	clearState()

	slog.Info("google sign-in succeeded", "user_id", user.ID, "email", info.Email, "new_tenant", isNewOwner)

	target := "/dashboard"
	if isNewOwner {
		// New tenant owner lands in the mandatory onboarding wizard.
		target = "/company/onboard"
	} else if user.Role.Name == "admin" || user.Role.Name == domain.RoleOrgAdmin || user.Role.ID == 1 {
		if h.Services.Settings != nil {
			if company, err := h.Services.Settings.GetSettings(r.Context()); err == nil && company.CompanyName == "" {
				target = "/company/onboard"
			}
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// flashRedirect sets a short-lived flash_error cookie and redirects — the
// established auth-page error idiom (see SubmitResetPassword).
func (h *GoogleOAuthHandlers) flashRedirect(w http.ResponseWriter, r *http.Request, msg, target string) {
	secure := false
	if h.Config != nil {
		secure = h.Config.CookieSecure
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "flash_error",
		Value:    msg,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   10,
	})
	http.Redirect(w, r, target, http.StatusSeeOther)
}
