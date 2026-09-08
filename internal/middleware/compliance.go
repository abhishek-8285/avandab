package middleware

import (
	"context"
	"net/http"
	"strings"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
)

// CompanySettingsReader defines the contract for reading company settings.
type CompanySettingsReader interface {
	GetSettings(ctx context.Context) (domain.CompanySettings, error)
}

// RequireCompanyCompliance checks that the tenant has completed mandatory
// company compliance details (Company Name, Address, Phone, Email) before granting
// access to operational routes. If incomplete, it redirects to /company/onboard
// with a flash error cookie.
func RequireCompanyCompliance(settings CompanySettingsReader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			if isComplianceExempt(path) {
				next.ServeHTTP(w, r)
				return
			}

			// Non-managers can never complete company onboarding (OnboardPage
			// bounces them to /dashboard), so enforcing the gate on them loops
			// dashboard<->onboard forever. Managers-only gate mirrors
			// Dashboard.Index; everyone else passes through.
			if !isComplianceManager(r) {
				next.ServeHTTP(w, r)
				return
			}

			if settings == nil {
				next.ServeHTTP(w, r)
				return
			}

			company, err := settings.GetSettings(r.Context())
			if err != nil || isCompanyIncomplete(company) {
				http.SetCookie(w, &http.Cookie{
					Name:     "flash_error",
					Value:    "Please complete mandatory company compliance details to unlock fleet operations.",
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   30,
				})
				http.Redirect(w, r, "/company/onboard", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isCompanyIncomplete(c domain.CompanySettings) bool {
	if strings.TrimSpace(c.CompanyName) == "" {
		return true
	}
	if c.Address == nil || strings.TrimSpace(*c.Address) == "" {
		return true
	}
	if c.Phone == nil || strings.TrimSpace(*c.Phone) == "" {
		return true
	}
	if c.Email == nil || strings.TrimSpace(*c.Email) == "" {
		return true
	}
	return false
}

func isComplianceExempt(path string) bool {
	if path == "/company/onboard" || strings.HasPrefix(path, "/company/onboard/") {
		return true
	}
	// Shadow mount: /settings and /company share Routes (main.go), so the
	// same wizard is reachable as /settings/onboard — exempt it identically.
	if path == "/settings/onboard" || strings.HasPrefix(path, "/settings/onboard/") {
		return true
	}
	// Phone self-heal: login routes users with empty phone to /user/onboard,
	// whose fix lives at POST /user/onboard and POST /profile. Gating any
	// of them strands the user.
	if path == "/user/onboard" || strings.HasPrefix(path, "/user/onboard/") {
		return true
	}
	if path == "/profile" || strings.HasPrefix(path, "/profile/") {
		return true
	}
	if path == "/change-password" || strings.HasPrefix(path, "/change-password/") {
		return true
	}
	if path == "/logout" || strings.HasPrefix(path, "/logout/") {
		return true
	}
	if strings.HasPrefix(path, "/static/") {
		return true
	}
	if path == "/login" || strings.HasPrefix(path, "/login/") ||
		path == "/register" || strings.HasPrefix(path, "/register/") ||
		path == "/forgot-password" || strings.HasPrefix(path, "/forgot-password/") ||
		path == "/reset-password" || strings.HasPrefix(path, "/reset-password/") ||
		strings.HasPrefix(path, "/auth/") ||
		strings.HasPrefix(path, "/pay/") ||
		strings.HasPrefix(path, "/epod/") ||
		strings.HasPrefix(path, "/share/") ||
		strings.HasPrefix(path, "/share") ||
		strings.HasPrefix(path, "/contact-us") ||
		path == "/privacy" || path == "/terms" || path == "/refunds" ||
		strings.HasPrefix(path, "/features") ||
		path == "/robots.txt" || path == "/sitemap.xml" || path == "/llms.txt" {
		return true
	}
	return false
}

// isComplianceManager reports whether the caller can own company onboarding.
// Fail-closed on missing session: RequireAuth normally guarantees one, and
// enforcing without it is the safe default.
func isComplianceManager(r *http.Request) bool {
	sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || sess == nil {
		return true
	}
	return sess.Role == string(domain.RoleAdmin) || sess.Role == string(domain.RoleOrgAdmin)
}
