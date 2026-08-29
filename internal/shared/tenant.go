package shared

import (
	"context"
	"errors"
	"fmt"
)

// TenantID represents a company/tenant identifier in a multi-tenant system.
type TenantID string

// DefaultTenant is the single-tenant bootstrap tenant. It must be removed once
// migration 00056 (sessions.tenant_id) lands and tenants are derived from real
// user data instead of a constant. It exists as the single, controlled point
// that still assumes one tenant — no handler or middleware may hardcode the
// literal "1" anywhere else.
const DefaultTenant TenantID = "1"

// NewTenantID validates and creates a TenantID.
func NewTenantID(id string) (TenantID, error) {
	if id == "" {
		return "", errors.New("tenant ID cannot be empty")
	}
	return TenantID(id), nil
}

type contextKey string

const TenantIDKey contextKey = "tenant_id"

// ContextWithTenantID returns a new context containing the TenantID.
func ContextWithTenantID(ctx context.Context, id TenantID) context.Context {
	return context.WithValue(ctx, TenantIDKey, id)
}

// TenantIDFromContext retrieves TenantID from the context. It fails closed:
// an empty string is returned when no tenant is set, so callers never
// silently operate against a default tenant.
func TenantIDFromContext(ctx context.Context) TenantID {
	if val, ok := ctx.Value(TenantIDKey).(TenantID); ok && val != "" {
		return val
	}
	return ""
}

// TenantRequired returns the tenant from context or an error if absent.
// Use in handlers that must never operate without a tenant.
func TenantRequired(ctx context.Context) (TenantID, error) {
	t := TenantIDFromContext(ctx)
	if t == "" {
		return "", fmt.Errorf("tenant not set in context")
	}
	return t, nil
}

// RequireTenantID is an alias for TenantRequired — preferred name for lint.
// Lint enforces RequireTenantID/TenantRequired/MustTenantID presence.
func RequireTenantID(ctx context.Context) (TenantID, error) {
	return TenantRequired(ctx)
}

// MustTenantID panics if tenant is missing. Use only where panic is appropriate
// (e.g., background jobs where missing tenant is a programmer error).
func MustTenantID(ctx context.Context) TenantID {
	t, err := TenantRequired(ctx)
	if err != nil {
		panic("tenant not set in context: call RequireTenantID and handle error, or ensure middleware set tenant via ContextWithTenantID")
	}
	return t
}

// ── Global scope (system jobs) ──────────────────────────────────────────────
//
// Background workers (outbox relay, sweeps, tickers, bootstrap) legitimately
// run without a request tenant. They must mark that intent explicitly instead
// of relying on a silent default: repositories resolve a global-scope context
// to DefaultTenant, and any other context without a tenant fails closed
// (panics) at the repository seam.
//
// Never mark a request-derived context: that disables the fail-closed guard
// for the whole request.
type globalScopeKey struct{}

// WithGlobalScope marks ctx as intentionally tenant-less (system job).
func WithGlobalScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, globalScopeKey{}, true)
}

// IsGlobalScope reports whether ctx was marked with WithGlobalScope.
func IsGlobalScope(ctx context.Context) bool {
	v, _ := ctx.Value(globalScopeKey{}).(bool)
	return v
}
