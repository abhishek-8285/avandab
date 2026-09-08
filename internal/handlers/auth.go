package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"transport-app/internal/comm"
	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// AuthHandlers handles authentication-related HTTP requests.
type AuthHandlers struct {
	*App
}

// ForgotPasswordAPI handles JSON password-reset requests from API clients
// (mobile driver app). The response is identical whether or not the account
// exists, to prevent enumeration. When SMTP is configured the reset email is
// queued durably via comm_outbox (Phase 2 outbox consumer); in development
// the reset link is returned so the flow is usable without a mailer (mirrors
// SubmitForgotPassword).
func (h *AuthHandlers) ForgotPasswordAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}

	resp := map[string]interface{}{
		"ok":      true,
		"message": "If an account exists for " + req.Email + ", password reset instructions have been sent.",
	}

	if user, err := h.Services.Users.GetUserByEmail(r.Context(), req.Email); err == nil && user.Status == domain.UserStatusActive {
		if token, err := h.App.ResetTokens.Create(req.Email); err == nil {
			link := fmt.Sprintf("%s://%s/reset-password?token=%s", requestScheme(r), r.Host, token)
			slog.Info("password reset link generated (api)", "email", req.Email)
			if !h.enqueueResetEmail(r, user, link) && h.Config.IsDevelopment() {
				resp["reset_link"] = link
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ResetPasswordAPI redeems a single-use reset token and sets a new password
// (JSON API counterpart of SubmitResetPassword).
func (h *AuthHandlers) ResetPasswordAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeJSONError(w, http.StatusBadRequest, "token and password are required")
		return
	}

	email, ok := h.App.ResetTokens.Consume(req.Token)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "this reset link is invalid or has expired")
		return
	}

	if err := h.Services.Users.SetPasswordByEmail(r.Context(), email, req.Password); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// NewAuthHandlers creates auth handlers.
func NewAuthHandlers(app *App) *AuthHandlers {
	return &AuthHandlers{App: app}
}

// LoginPage renders the login page.
func (h *AuthHandlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if isDatastarRequest(r) {
		h.renderFragment(w, "login_form.html", nil)
		return
	}

	pd := PageData{
		Title: "Login",
		Extra: map[string]interface{}{},
	}

	if cookie, err := r.Cookie("flash_error"); err == nil && cookie.Value != "" {
		pd.FlashError = cookie.Value
		pd.Extra["Error"] = cookie.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: "", Path: "/", MaxAge: -1})
	}

	if cookie, err := r.Cookie("flash_success"); err == nil && cookie.Value != "" {
		pd.FlashSuccess = cookie.Value
		pd.Extra["FlashSuccess"] = cookie.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_success", Value: "", Path: "/", MaxAge: -1})
	}

	if cookie, err := r.Cookie("auth_email"); err == nil {
		pd.Extra["Email"] = cookie.Value
	}

	if red := shared.SafeRedirect(r.URL.Query().Get("redirect")); red != "" {
		pd.Extra["Redirect"] = red
	}

	if h.App.GoogleEnabledFor() {
		pd.Extra["GoogleEnabled"] = true
	}

	h.renderAuthPage(w, "login_form.html", pd)
}

// RegisterPage renders the user onboarding registration page.
func (h *AuthHandlers) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if isDatastarRequest(r) {
		h.renderFragment(w, "register_form.html", nil)
		return
	}
	pd := PageData{Title: "Create Account"}
	if cookie, err := r.Cookie("flash_error"); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "flash_error",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   h.Config.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
		if pd.Extra == nil {
			pd.Extra = map[string]interface{}{}
		}
		pd.Extra["Error"] = cookie.Value
	}
	if h.App.GoogleEnabledFor() {
		if pd.Extra == nil {
			pd.Extra = map[string]interface{}{}
		}
		pd.Extra["GoogleEnabled"] = true
	}
	h.renderAuthPage(w, "register_form.html", pd)
}

// Register handles self-onboarding account creation.
func (h *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	name := r.PostFormValue("name")
	email := r.PostFormValue("email")
	phone := r.PostFormValue("phone")
	companyName := r.PostFormValue("company_name")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm_password")

	if password != confirm {
		h.renderRegisterError(w, r, "Passwords do not match", email, name, phone, companyName)
		return
	}

	user, isNewOwner, err := h.Services.Users.RegisterSelfServiceAccount(r.Context(), email, name, phone, password, companyName)
	if err != nil {
		h.renderRegisterError(w, r, err.Error(), email, name, phone, companyName)
		return
	}

	// New tenant owners bind the org_admin role — never platform admin, which
	// would expose /tenants, suspend, and global-admin minting to every signup.
	roleName := string(domain.RoleViewer)
	if isNewOwner {
		roleName = string(domain.RoleOrgAdmin)
	}
	_ = h.AuthSrv.AddRoleForUser(user.ID.String(), roleName)

	// Automatically log in the user upon onboarding with server-side session
	if sessResult, err := h.Services.Auth.CreateSessionForUser(r.Context(), user.ID); err == nil && sessResult != nil {
		h.AuthStore.CreateSessionWithToken(w, user.ID.String(), roleName, user.Name, sessResult.SessionToken)
	} else {
		http.Error(w, "session creation failed; please retry registration", http.StatusInternalServerError)
		return
	}

	targetURL := "/dashboard"
	if isNewOwner {
		targetURL = "/company/onboard"
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<script>window.location.href='" + targetURL + "'</script>"))
		return
	}

	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

func (h *AuthHandlers) renderRegisterError(w http.ResponseWriter, r *http.Request, errMsg, email, name, phone, companyName string) {
	if isDatastarRequest(r) {
		h.renderFragment(w, "register_form.html", map[string]interface{}{
			"Title":         "Create Account",
			"Error":         errMsg,
			"Email":         email,
			"Name":          name,
			"Phone":         phone,
			"CompanyName":   companyName,
			"GoogleEnabled": h.App.GoogleEnabledFor(),
		})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "flash_error",
		Value:    errMsg,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30,
	})
	http.Redirect(w, r, "/register", http.StatusSeeOther)
}

// Login processes the login form submission.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	result, err := h.Services.Auth.Login(r.Context(), service.LoginRequest{
		Email:    email,
		Password: password,
	})

	if err != nil {
		if isDatastarRequest(r) {
			h.renderFragment(w, "login_form.html", map[string]interface{}{
				"Title": "Login",
				"Error": err.Error(),
			})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "flash_error",
			Value:    err.Error(),
			Path:     "/",
			HttpOnly: true,
			Secure:   h.Config.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   30,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "auth_email",
			Value:    email,
			Path:     "/",
			HttpOnly: true,
			Secure:   h.Config.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   30,
		})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.AuthStore.CreateSessionWithToken(w, result.User.ID.String(), string(result.User.Role.Name), result.User.Name, result.SessionToken)

	// Clear flash cookies so old errors don't show after successful login
	http.SetCookie(w, &http.Cookie{
		Name:     "flash_error",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_email",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	targetURL := "/dashboard"
	// Check if user has incomplete setup profile (or org onboarding needed for
	// tenant owners returning before finishing company setup).
	if result.User.Phone == nil || *result.User.Phone == "" {
		targetURL = "/user/onboard"
	} else if result.User.Role.Name == "admin" || result.User.Role.Name == domain.RoleOrgAdmin {
		if company, err := h.Services.Settings.GetSettings(r.Context()); err == nil && company.CompanyName == "" {
			targetURL = "/company/onboard"
		}
	}

	// Return the user to the page they originally requested, unless onboarding
	// takes precedence (new users must complete setup first).
	if targetURL == "/dashboard" {
		if red := shared.SafeRedirect(r.PostFormValue("redirect")); red != "" {
			targetURL = red
		}
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<script>window.location.href='" + targetURL + "'</script>"))
		return
	}

	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

// Logout handles user logout with server-side revocation.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	h.AuthStore.RevokeSession(r, w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ProfilePage renders the user profile page.
func (h *AuthHandlers) ProfilePage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, err := h.Services.Auth.GetProfile(r.Context(), domain.UserID(session.UserID))
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	roles, _ := h.Services.Users.ListRoles(r.Context())

	pd := PageData{
		Title:      "My Profile",
		User:       session,
		UserDetail: user,
		Roles:      roles,
	}

	// Read and clear flash cookies
	if c, err := r.Cookie("flash_success"); err == nil && c.Value != "" {
		pd.FlashSuccess = c.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_success", Value: "", Path: "/", MaxAge: -1})
	}
	if c, err := r.Cookie("flash_error"); err == nil && c.Value != "" {
		pd.FlashError = c.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: "", Path: "/", MaxAge: -1})
	}

	h.renderPage(w, r, "profile_page.html", pd)
}

// ChangePasswordPage renders the change password page.
func (h *AuthHandlers) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	h.renderAuthPage(w, "change_password.html", PageData{
		Title: "Change Password",
	})
}

// ChangePassword processes password change.
func (h *AuthHandlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := domain.UserID(session.UserID)
	oldPassword := r.PostFormValue("old_password")
	newPassword := r.PostFormValue("new_password")
	confirmPassword := r.PostFormValue("confirm_password")

	if newPassword != confirmPassword {
		h.renderAuthPage(w, "change_password.html", PageData{
			Title:      "Change Password",
			FlashError: "Passwords do not match",
			User:       session,
		})
		return
	}

	if err := h.Services.Auth.ChangePassword(r.Context(), userID, oldPassword, newPassword); err != nil {
		h.renderAuthPage(w, "change_password.html", PageData{
			Title:      "Change Password",
			FlashError: err.Error(),
			User:       session,
		})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "flash_success",
		Value:    "Password changed successfully",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   5,
	})
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

// ForgotPasswordPage renders the forgot password request page.
func (h *AuthHandlers) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	h.renderAuthPage(w, "forgot_password.html", PageData{
		Title: "Forgot Password",
	})
}

// SubmitForgotPassword processes password reset requests. It issues a
// single-use reset token for the account (when it exists and is active) and
// surfaces the reset link. With SMTP configured the email is queued durably
// through comm_outbox (Phase 2 outbox worker); without a mailer we render the
// link directly in development so the flow is actually usable. The generic
// success message is always shown to avoid leaking whether an account exists.
func (h *AuthHandlers) SubmitForgotPassword(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	pd := PageData{
		Title: "Forgot Password",
		Extra: map[string]interface{}{},
	}
	if email == "" {
		pd.Extra["Error"] = "Please enter your email address"
		h.renderAuthPage(w, "forgot_password.html", pd)
		return
	}

	if user, err := h.Services.Users.GetUserByEmail(r.Context(), email); err == nil && user.Status == domain.UserStatusActive {
		token, err := h.App.ResetTokens.Create(email)
		if err == nil {
			link := fmt.Sprintf("%s://%s/reset-password?token=%s", requestScheme(r), r.Host, token)
			slog.Info("password reset link generated", "email", email)
			if !h.enqueueResetEmail(r, user, link) && h.Config.IsDevelopment() {
				// No mailer configured: dev convenience shows the link on-page.
				pd.Extra["ResetLink"] = link
			}
		}
	}

	pd.Extra["SuccessMsg"] = "If an account exists for " + email + ", password reset instructions have been sent."
	h.renderAuthPage(w, "forgot_password.html", pd)
}

// enqueueResetEmail durably queues a password-reset email through comm_outbox
// (Phase 2 — the outbox worker delivers via SMTP). The tenant is taken from
// the user's own record (never hardcoded); comm_outbox requires a tenant_id.
// Returns true when the email was queued, false when SMTP is unconfigured so
// callers can fall back to the dev link.
func (h *AuthHandlers) enqueueResetEmail(r *http.Request, user domain.User, link string) bool {
	if h.App == nil || h.App.DB == nil || h.App.Notify == nil || !h.App.Notify.EmailConfigured() {
		return false
	}
	body := "A password reset was requested for your account.\n\n" +
		"Reset your password using this single-use link (valid for a short window):\n" + link + "\n\n" +
		"If you did not request this, ignore this email."
	if _, err := comm.EnqueueEmail(r.Context(), h.App.DB, user.TenantID, user.Email, "password_reset", "Reset your password", body); err != nil {
		slog.Error("password reset email enqueue failed", "email", user.Email, "error", err)
	}
	return true
}

// requestScheme returns http or https based on the request context.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// ResetPasswordPage renders the password reset form for a valid token.
func (h *AuthHandlers) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	pd := PageData{
		Title: "Reset Password",
		Extra: map[string]interface{}{"Token": token},
	}
	if token == "" {
		pd.Extra["Error"] = "Missing or invalid reset link."
	}
	h.renderAuthPage(w, "reset_password.html", pd)
}

// SubmitResetPassword redeems a reset token and sets a new password.
func (h *AuthHandlers) SubmitResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	newPassword := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm_password")

	pd := PageData{
		Title: "Reset Password",
		Extra: map[string]interface{}{"Token": token},
	}

	if token == "" {
		pd.Extra["Error"] = "Missing reset token."
		h.renderAuthPage(w, "reset_password.html", pd)
		return
	}
	if newPassword != confirm {
		pd.Extra["Error"] = "Passwords do not match."
		h.renderAuthPage(w, "reset_password.html", pd)
		return
	}

	email, ok := h.App.ResetTokens.Consume(token)
	if !ok {
		pd.Extra["Error"] = "This reset link is invalid or has expired. Please request a new one."
		h.renderAuthPage(w, "reset_password.html", pd)
		return
	}

	if err := h.Services.Users.SetPasswordByEmail(r.Context(), email, newPassword); err != nil {
		pd.Extra["Error"] = err.Error()
		h.renderAuthPage(w, "reset_password.html", pd)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "flash_success",
		Value:    "Password reset successful. Please log in with your new password.",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   10,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// UserOnboardingPage renders the post-login setup page when user has not completed details.
func (h *AuthHandlers) UserOnboardingPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, _ := h.Services.Auth.GetProfile(r.Context(), domain.UserID(session.UserID))

	pd := PageData{
		Title:      "Account Setup",
		User:       session,
		UserDetail: user,
	}

	h.renderPage(w, r, "user_onboarding.html", pd)
}

// SaveUserOnboard persists the post-login setup form (name/phone/timezone)
// and routes onward: tenant owners with incomplete company profile go to
// /company/onboard, everyone else to /dashboard. Mirrors the Login routing
// so the phone gate actually clears instead of bouncing back here.
func (h *AuthHandlers) SaveUserOnboard(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
		return
	}

	phone := strings.TrimSpace(r.PostFormValue("phone"))
	if phone == "" {
		h.failPage(w, r, fmt.Errorf("official phone number is required"), http.StatusBadRequest, "Phone Number Required")
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		if user, err := h.Services.Auth.GetProfile(r.Context(), domain.UserID(session.UserID)); err == nil {
			name = user.Name
		}
	}
	timezone := strings.TrimSpace(r.PostFormValue("timezone"))

	if _, err := h.Services.Auth.UpdateProfile(r.Context(), domain.UserID(session.UserID), name, phone, timezone); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Could Not Save Profile")
		return
	}

	targetURL := "/dashboard"
	if session.Role == "admin" || session.Role == string(domain.RoleOrgAdmin) {
		if company, err := h.Services.Settings.GetSettings(r.Context()); err == nil && company.CompanyName == "" {
			targetURL = "/company/onboard"
		}
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<script>window.location.href='" + targetURL + "'</script>"))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "flash_success",
		Value:    "Profile setup completed.",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   10,
	})
	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

// SendVerificationEmail issues a single-use verification link for the
// signed-in user's address (POST /user/send-verification). Badge only:
// unverified users lose nothing, and already-verified users short-circuit.
// Delivery mirrors the password-reset path (outbox enqueue; dev fallback
// flashes the link when no mailer is configured).
func (h *AuthHandlers) SendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	flash := func(msg string) {
		http.SetCookie(w, flashCookie("flash_success", msg))
		http.Redirect(w, r, "/user/onboard", http.StatusSeeOther)
	}

	user, err := h.Services.Auth.GetProfile(r.Context(), domain.UserID(session.UserID))
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Could Not Load Profile")
		return
	}
	if user.EmailVerifiedAt != nil {
		flash("Email already verified.")
		return
	}
	if h.App == nil || h.App.VerifyTokens == nil {
		h.failPage(w, r, fmt.Errorf("email verification is not configured"), http.StatusInternalServerError, "Verification Unavailable")
		return
	}

	token, err := h.App.VerifyTokens.Create(user.Email)
	if err != nil {
		h.failPage(w, r, err, http.StatusInternalServerError, "Could Not Issue Link")
		return
	}
	link := fmt.Sprintf("%s://%s/verify-email?token=%s", requestScheme(r), r.Host, token)
	if h.enqueueVerifyEmail(r, user, link) {
		flash("Verification link sent to " + user.Email + ".")
		return
	}
	if h.Config.IsDevelopment() {
		// No mailer configured: dev convenience flashes the link.
		flash("No mailer configured (dev link): " + link)
		return
	}
	h.failPage(w, r, fmt.Errorf("email delivery is not configured"), http.StatusInternalServerError, "Verification Unavailable")
}

// VerifyEmailPage consumes ?token= (GET /verify-email, public): a valid
// token stamps email_verified_at and lands on /dashboard; anything else
// bounces to /login with a flash error. Rate-limited at the route.
func (h *AuthHandlers) VerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	derr := func(msg string) {
		http.SetCookie(w, flashCookie("flash_error", msg))
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	token := r.URL.Query().Get("token")
	if token == "" || h.App == nil || h.App.VerifyTokens == nil {
		derr("This verification link is invalid or has expired.")
		return
	}
	email, ok := h.App.VerifyTokens.Consume(token)
	if !ok {
		derr("This verification link is invalid or has expired.")
		return
	}
	if err := h.Services.Users.MarkEmailVerified(r.Context(), email); err != nil {
		slog.Error("email verification marking failed", slog.String("email", email), slog.Any("error", err))
		derr("Could not verify this address. Request a fresh link.")
		return
	}
	http.SetCookie(w, flashCookie("flash_success", "Email verified."))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandlers) enqueueVerifyEmail(r *http.Request, user domain.User, link string) bool {
	if h.App == nil || h.App.DB == nil || h.App.Notify == nil || !h.App.Notify.EmailConfigured() {
		return false
	}
	body := "Confirm your email address for your account.\n\n" +
		"Verify using this single-use link (valid 24 hours):\n" + link + "\n\n" +
		"If you did not request this, ignore this email."
	if _, err := comm.EnqueueEmail(r.Context(), h.App.DB, user.TenantID, user.Email, "email_verification", "Verify your email", body); err != nil {
		slog.Error("verification email enqueue failed", "email", user.Email, "error", err)
	}
	return true
}

// UpdateProfile handles profile updates.
func (h *AuthHandlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := domain.UserID(session.UserID)
	name := r.PostFormValue("name")
	phone := r.PostFormValue("phone")
	timezone := r.PostFormValue("timezone")

	updated, err := h.Services.Auth.UpdateProfile(r.Context(), userID, name, phone, timezone)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "profile_page.html", PageData{
			Title:      "My Profile",
			User:       session,
			UserDetail: updated,
		})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "flash_success",
		Value:    "Profile updated successfully",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   10,
	})
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}
