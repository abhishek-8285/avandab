package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"transport-app/internal/auth"
)

// CSRFProtect blocks state-changing requests that carry a session cookie
// but originate from a different site (CSRF). Requests authenticated with a
// Bearer token (non-browser clients) are exempt. Safe methods and requests
// without a session cookie are allowed through. Requests that omit both
// Origin and Referer are allowed for curl compatibility.
//
// This complements SameSite=Lax session cookies: Lax already blocks
// cross-site POST cookies in modern browsers; this adds a defense-in-depth
// origin check for older clients and SameSite=None deployments.
func CSRFProtect(store *auth.SessionStore) func(http.Handler) http.Handler {
	return csrfProtect(store, false)
}

// CSRFProtectStrict is CSRFProtect with header-stripping defense: state-changing
// requests that carry a session cookie but omit both Origin and Referer are
// rejected with 403. Enable it in deployments that can tolerate it; the
// lenient CSRFProtect remains the default for curl/server-to-server clients.
func CSRFProtectStrict(store *auth.SessionStore) func(http.Handler) http.Handler {
	return csrfProtect(store, true)
}

func csrfProtect(store *auth.SessionStore, strict bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Non-browser clients authenticate with Bearer tokens and are
			// not vulnerable to cookie-based CSRF.
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			// No session cookie: not a browser-authenticated request.
			if !store.HasSession(r) {
				next.ServeHTTP(w, r)
				return
			}

			if !sameOrigin(r) {
				http.Error(w, "Forbidden: cross-site request rejected", http.StatusForbidden)
				return
			}

			// Strict mode: an attacker can strip both headers, so their
			// absence is itself treated as cross-site.
			if strict && !hasOriginHeader(r) {
				http.Error(w, "Forbidden: cross-site request rejected", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// hasOriginHeader reports whether the request carries either an Origin or a
// Referer header.
func hasOriginHeader(r *http.Request) bool {
	return r.Header.Get("Origin") != "" || r.Header.Get("Referer") != ""
}

// sameOrigin verifies that the request's Origin (or, failing that, Referer)
// header, when present, matches the request Host. A missing Origin and
// Referer is allowed: non-browser clients (curl, server-to-server) and
// same-origin navigation requests may omit both.
//
// Under a trusted reverse proxy r.Host may differ from the origin host seen by the
// browser, so X-Forwarded-Host (set by the proxy) is accepted only if from a trusted proxy.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	host := r.Host
	if auth.TrustForwardedHeaders(r.RemoteAddr) {
		if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
			if idx := strings.Index(xfh, ","); idx >= 0 {
				host = strings.TrimSpace(xfh[:idx])
			} else {
				host = strings.TrimSpace(xfh)
			}
		}
	}
	return strings.EqualFold(u.Host, host)
}
