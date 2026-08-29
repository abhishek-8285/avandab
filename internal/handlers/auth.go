package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// AuthHandlers handles authentication-related HTTP requests.
type AuthHandlers struct {
	*App
}

// ForgotPasswordAPI handles JSON password-reset requests from API clients
// (mobile driver app). The response is identical whether or not the account
// exists, to prevent enumeration. In development the reset link is returned
// so the flow is usable without a mailer (mirrors SubmitForgotPassword).
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
			if h.Config.IsDevelopment() {
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
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm_password")

	if password != confirm {
		h.renderRegisterError(w, r, "Passwords do not match", email, name, phone)
		return
	}

	// First-run claim: the first self-registered account becomes the
	// deployment's admin; later registrations stay least-privilege viewer.
	user, isAdmin, err := h.Services.Users.RegisterSelfServiceAccount(r.Context(), email, name, phone, password)
	if err != nil {
		h.renderRegisterError(w, r, err.Error(), email, name, phone)
		return
	}

	roleName := string(domain.RoleViewer)
	if isAdmin {
		roleName = string(domain.RoleAdmin)
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
	if isAdmin {
		targetURL = "/company/onboard"
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<script>window.location.href='" + targetURL + "'</script>"))
		return
	}

	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

func (h *AuthHandlers) renderRegisterError(w http.ResponseWriter, r *http.Request, errMsg, email, name, phone string) {
	if isDatastarRequest(r) {
		h.renderFragment(w, "register_form.html", map[string]interface{}{
			"Title": "Create Account",
			"Error": errMsg,
			"Email": email,
			"Name":  name,
			"Phone": phone,
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
	// Check if user has incomplete setup profile (or admin company onboarding needed)
	if result.User.Phone == nil || *result.User.Phone == "" {
		targetURL = "/user/onboard"
	} else if result.User.Role.Name == "admin" {
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
// surfaces the reset link. In production the link would be emailed; with no
// SMTP mailer configured we render it directly in development so the flow is
// actually usable. The generic success message is always shown to avoid
// leaking whether an account exists.
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
			if h.App != nil && h.App.Notify != nil && h.App.Notify.EmailConfigured() {
				body := "A password reset was requested for your account.\n\n" +
					"Reset your password using this single-use link (valid for a short window):\n" + link + "\n\n" +
					"If you did not request this, ignore this email."
				if serr := h.App.Notify.SendEmail(r.Context(), ports.NotificationMessage{
					Recipient: email,
					Subject:   "Reset your password",
					Body:      body,
				}); serr != nil {
					slog.Error("password reset email delivery failed", "email", email, "error", serr)
				}
			} else if h.Config.IsDevelopment() {
				// No mailer configured: dev convenience shows the link on-page.
				pd.Extra["ResetLink"] = link
			}
		}
	}

	pd.Extra["SuccessMsg"] = "If an account exists for " + email + ", password reset instructions have been sent."
	h.renderAuthPage(w, "forgot_password.html", pd)
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
