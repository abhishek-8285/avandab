package middleware

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/httpx"
	"transport-app/internal/logging"
	"transport-app/internal/operations/errors"
	"transport-app/internal/shared"
)

// RequestID adds a unique request ID to the context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}

		ctx := context.WithValue(r.Context(), auth.ContextReqID, reqID)
		w.Header().Set("X-Request-ID", reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logger logs request details and duration.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		reqID, _ := r.Context().Value(auth.ContextReqID).(string)
		slog.LogAttrs(r.Context(), slog.LevelInfo, "request completed",
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.statusCode),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// Recoverer recovers from panics, logs stack trace, and reports to ErrorReporter.
func Recoverer(reporter ...*errors.Reporter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := debug.Stack()
					msg := fmt.Sprintf("%v", rec)
					reqID, _ := r.Context().Value(auth.ContextReqID).(string)
					tenantID := shared.TenantIDFromContext(r.Context())
					var userID string
					if u, ok := r.Context().Value(auth.ContextUser).(domain.User); ok {
						userID = string(u.ID)
					}

					slog.Error("panic recovered",
						slog.String("request_id", reqID),
						slog.String("user_id", userID),
						slog.String("error", logging.Redact(msg)),
						slog.String("stack", string(stack)),
					)

					if len(reporter) > 0 && reporter[0] != nil {
						_, _ = reporter[0].Report(r.Context(), errors.ErrorReport{
							RequestID:  reqID,
							UserID:     userID,
							TenantID:   string(tenantID),
							URL:        r.URL.String(),
							Method:     r.Method,
							StatusCode: http.StatusInternalServerError,
							StackTrace: string(stack),
							Message:    msg,
							Severity:   errors.SeverityCritical,
							UserAgent:  r.UserAgent(),
							IPAddress:  r.RemoteAddr,
						})
					}

					httpx.Error(w, r, fmt.Errorf("%s", msg))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout wraps the handler with a request timeout.
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// RequireAuth is middleware that redirects unauthenticated users to login.
func RequireAuth(store *auth.SessionStore, tenantResolver TenantResolver) func(http.Handler) http.Handler {
	return AuthRequired(store, "/login", tenantResolver)
}

// AuthRequired is a simple middleware that redirects unauthenticated users to login.
func AuthRequired(store *auth.SessionStore, loginPath string, tenantResolver TenantResolver) func(http.Handler) http.Handler {
	if tenantResolver == nil {
		tenantResolver = DefaultTenantResolver
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, ok := store.ValidateSession(r)
			if !ok {
				target := loginPath
				if r.Method == http.MethodGet {
					if red := shared.SafeRedirect(r.URL.RequestURI()); red != "" {
						target = loginPath + "?redirect=" + url.QueryEscape(red)
					}
				}
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}

			// Derive the tenant through the resolver; a failure (unknown
			// user, suspended org, DB error) must NOT fall back to the
			// bootstrap tenant — kill the session and bounce to login.
			tenantID, err := tenantResolver(r.Context(), data.UserID)
			if err != nil {
				store.ClearSession(w)
				http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: err.Error(), Path: "/", HttpOnly: true, MaxAge: 30})
				http.Redirect(w, r, loginPath, http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), auth.ContextUser, data)
			ctx = context.WithValue(ctx, auth.ContextIP, auth.ClientIP(r))
			ctx = context.WithValue(ctx, auth.ContextLocation, auth.ClientLocation(r))
			ctx = shared.ContextWithTenantID(ctx, tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RoleRequired checks that the authenticated user has one of the specified roles.
func RoleRequired(allowedRoles ...int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
			if !ok || session == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			roleID := roleIDFromName(session.Role)
			allowed := false
			for _, r := range allowedRoles {
				if roleID == r {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func roleIDFromName(role string) int64 {
	switch role {
	case "admin":
		return 1
	case "dispatcher":
		return 2
	case "accountant":
		return 3
	case "viewer":
		return 4
	default:
		return 4
	}
}

// ResourcePermission checks if the user has permission to access a resource with an action.
func ResourcePermission(authSrv auth.AuthorizationService, resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
			if !ok || session == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			if !authSrv.Can(session.UserID, resource, action) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// NoCache sets headers to prevent caching of dynamic pages.
func NoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

// SpaResponseWriter wraps http.ResponseWriter to track if it's an SPA request.
type SpaResponseWriter struct {
	http.ResponseWriter
	isSPA bool
}

func (s *SpaResponseWriter) IsSPARequest() bool {
	return s.isSPA
}

func (s *SpaResponseWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *SpaResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// SPAMiddleware checks for X-SPA-Request header and wraps the response writer.
func SPAMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not apply SPA wrapping on file or PDF download endpoints
		if r.Header.Get("X-SPA-Request") == "true" && !isDownloadPath(r.URL.Path) {
			wrapped := &SpaResponseWriter{
				ResponseWriter: w,
				isSPA:          true,
			}
			next.ServeHTTP(wrapped, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders adds baseline HTTP security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// ContentSecurityPolicy sets Content-Security-Policy header when enabled is true (Spec 04 §2).
// Applied opt-in per route (tracking map, public shares, maintenance) to prevent
// breaking pages with inline Datastar / Alpine handlers.
func ContentSecurityPolicy(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enabled {
				w.Header().Set("Content-Security-Policy",
					"default-src 'self'; "+
						"script-src 'self' 'unsafe-inline'; "+
						"style-src 'self' 'unsafe-inline'; "+
						"img-src 'self' data: https://mt1.google.com https://tile.openstreetmap.org https://a.tile.openstreetmap.org https://b.tile.openstreetmap.org https://c.tile.openstreetmap.org; "+
						"connect-src 'self' https://nominatim.openstreetmap.org https://mt1.google.com; "+
						"font-src 'self'; "+
						"frame-ancestors 'none'")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SkipForPaths wraps a middleware so it does NOT apply to requests whose
// path has one of the given prefixes. Used to exempt long-lived SSE streams
// from the global chiMiddleware.Timeout(60s), which would otherwise kill them
// (Spec 04 §1.2, §13). Paths matched by prefix.
func SkipForPaths(m func(http.Handler) http.Handler, paths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := m(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range paths {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

func isDownloadPath(path string) bool {
	return strings.Contains(path, "/files/") ||
		strings.HasSuffix(path, "/pdf") ||
		strings.HasPrefix(path, "/static/") ||
		strings.HasPrefix(path, "/uploads/") ||
		path == "/robots.txt" ||
		path == "/sitemap.xml"
}
