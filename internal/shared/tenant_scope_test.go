package shared

import (
	"context"
	"testing"
)

func TestRequireTenantID_Success(t *testing.T) {
	ctx := ContextWithTenantID(context.Background(), TenantID("tenant-xyz"))
	got, err := RequireTenantID(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tenant-xyz" {
		t.Fatalf("got %q want tenant-xyz", got)
	}
}

func TestRequireTenantID_FailsClosed(t *testing.T) {
	if _, err := RequireTenantID(context.Background()); err == nil {
		t.Fatal("expected error for bare context")
	}
}

func TestRequireTenantID_ErrorMessage(t *testing.T) {
	_, err := RequireTenantID(context.Background())
	if err == nil || err.Error() != "tenant not set in context" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestMustTenantID_Success(t *testing.T) {
	ctx := ContextWithTenantID(context.Background(), TenantID("acme"))
	if got := MustTenantID(ctx); got != "acme" {
		t.Fatalf("got %q want acme", got)
	}
}

func TestMustTenantID_PanicsWhenMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	MustTenantID(context.Background())
}

func TestMustTenantID_PanicMessageMentionsContext(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if msg == "" {
			t.Fatal("expected panic message")
		}
		if len(msg) < 10 {
			t.Fatalf("panic message too short: %q", msg)
		}
	}()
	MustTenantID(context.Background())
}

func TestRequireTenantID_ConsistentWithTenantRequired(t *testing.T) {
	ctx := ContextWithTenantID(context.Background(), TenantID("consistent"))
	a, _ := RequireTenantID(ctx)
	b, _ := TenantRequired(ctx)
	if a != b {
		t.Fatalf("alias diverged: %q vs %q", a, b)
	}
}
