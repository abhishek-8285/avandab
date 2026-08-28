package sqlite

import (
	"context"
	"strings"
	"testing"

	"transport-app/internal/shared"
)

func TestTenantIDFromCtx_UsesTenantFromContext(t *testing.T) {
	ctx := shared.ContextWithTenantID(context.Background(), "42")
	if got := tenantIDFromCtx(ctx); got != "42" {
		t.Fatalf("tenantIDFromCtx() = %q, want %q", got, "42")
	}
}

func TestTenantIDFromCtx_GlobalScopeResolvesDefaultTenant(t *testing.T) {
	ctx := shared.WithGlobalScope(context.Background())
	if got := tenantIDFromCtx(ctx); got != string(shared.DefaultTenant) {
		t.Fatalf("global scope: got %q, want %q", got, string(shared.DefaultTenant))
	}
}

func TestTenantIDFromCtx_MissingTenantFailsClosed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for context without tenant or global scope")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "no tenant in context") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	_ = tenantIDFromCtx(context.Background())
}

func TestTenantIDFromCtx_EmptyTenantFailsClosed(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for empty tenant in context")
		}
	}()
	_ = tenantIDFromCtx(shared.ContextWithTenantID(context.Background(), ""))
}
