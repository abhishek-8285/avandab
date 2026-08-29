package shared

import (
	"context"
	"testing"
)

func TestWithGlobalScope_RoundTrip(t *testing.T) {
	ctx := WithGlobalScope(context.Background())
	if !IsGlobalScope(ctx) {
		t.Fatal("IsGlobalScope = false, want true after WithGlobalScope")
	}
	if IsGlobalScope(context.Background()) {
		t.Fatal("IsGlobalScope = true on unmarked context")
	}
}

func TestWithGlobalScope_DoesNotSetTenant(t *testing.T) {
	ctx := WithGlobalScope(context.Background())
	if got := TenantIDFromContext(ctx); got != "" {
		t.Fatalf("TenantIDFromContext on global-scope ctx = %q, want empty (global scope is not a tenant)", got)
	}
}
