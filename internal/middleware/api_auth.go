package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// apiPrincipal holds the resolved identity for an API request.
type apiPrincipal struct {
	UserID   string
	Role     string
	TenantID shared.TenantID
}

// TenantResolver derives the tenant for an authenticated user. DefaultTenantResolver
// serves single-org deployments; TenantForUserResolver resolves per-user from
// users.tenant_id and rejects suspended tenants.
type TenantResolver func(ctx context.Context, userID string) (shared.TenantID, error)

// DefaultTenantResolver is the single-tenant bootstrap resolver used when
// MULTI_TENANT_ENABLED=false.
func DefaultTenantResolver(_ context.Context, _ string) (shared.TenantID, error) {
	return shared.DefaultTenant, nil
}

// RequireAPIAuth protects REST API routes by accepting either: ...
func RequireAPIAuth(store *auth.SessionStore, secret []byte, tenantResolver TenantResolver) func(http.Handler) http.Handler {
	if tenantResolver == nil {
		tenantResolver = DefaultTenantResolver
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := resolveAPIPrincipal(r, store, secret, tenantResolver)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="transport-api"`)
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": err.Error(),
				})
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
				UserID: principal.UserID,
				Role:   principal.Role,
			})
			ctx = context.WithValue(ctx, auth.ContextIP, auth.ClientIP(r))
			ctx = shared.ContextWithTenantID(ctx, principal.TenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission checks Casbin RBAC permissions and returns JSON 403 on denial.
// Use this on API routes after RequireAPIAuth.
func RequirePermission(authSrv auth.AuthorizationService, resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
			if !ok || session == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized","message":"authentication required"}`))
				return
			}

			if !authSrv.Can(session.UserID, resource, action) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden","message":"insufficient permissions"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveAPIPrincipal tries Bearer token first, then falls back to session cookie.
func resolveAPIPrincipal(r *http.Request, store *auth.SessionStore, secret []byte, tenantResolver TenantResolver) (apiPrincipal, error) {
	// 1. Bearer token
	if raw := r.Header.Get("Authorization"); strings.HasPrefix(raw, "Bearer ") {
		token := strings.TrimPrefix(raw, "Bearer ")
		claims, err := auth.ParseAPIToken(secret, token)
		if err != nil {
			return apiPrincipal{}, err
		}

		role := claims.Role
		if store != nil && store.Validator() != nil {
			liveRole, active, err := store.Validator().ValidateAPITokenUser(r.Context(), claims.UserID)
			if err != nil || !active {
				return apiPrincipal{}, auth.ErrTokenRevoked
			}
			if liveRole != "" {
				role = liveRole
			}
		}

		// Tenant comes from the server-side resolver, never the token: the
		// claims tid is advisory only and must not decide authorization.
		tenantID, terr := tenantResolver(r.Context(), claims.UserID)
		if terr != nil {
			return apiPrincipal{}, terr
		}

		return apiPrincipal{
			UserID:   claims.UserID,
			Role:     role,
			TenantID: tenantID,
		}, nil
	}

	// 2. Session cookie (browser / Datastar requests)
	session, ok := store.ValidateSession(r)
	if !ok {
		return apiPrincipal{}, auth.ErrTokenInvalid
	}

	tenantID, err := tenantResolver(r.Context(), session.UserID)
	if err != nil {
		return apiPrincipal{}, err
	}
	return apiPrincipal{
		UserID:   session.UserID,
		Role:     session.Role,
		TenantID: tenantID,
	}, nil
}
