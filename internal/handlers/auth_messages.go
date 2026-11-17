package handlers

// Auth error mapping and request-scheme helpers, split out of auth.go to
// keep the god-file under its ratchet cap.

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
)

// requestScheme returns http or https based on the request context.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// X-Forwarded-Proto is client-controlled unless the request arrived from
	// a trusted proxy. Never let an arbitrary caller downgrade reset links.
	if auth.IsTrustedProxy(r.RemoteAddr) {
		switch proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto {
		case "http", "https":
			return proto
		}
	}
	return "http"
}

// resetPasswordErrorMessage keeps password-policy feedback but hides storage
// and hashing internals from the reset form/API.
func resetPasswordErrorMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrWeakPassword), errors.Is(err, domain.ErrWeakPassword):
		return auth.ErrWeakPassword.Error()
	case errors.Is(err, domain.ErrUserNotFound):
		return "this reset link is no longer valid"
	default:
		slog.Error("password reset failed", "error", err)
		return "could not update the password; please try again"
	}
}
