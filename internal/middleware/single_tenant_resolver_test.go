package middleware

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
)

// TestSingleTenantResolver_RejectsForeignTenantUser guards the runtime half of
// audit finding H6 (2026-11-12): with MULTI_TENANT_ENABLED=false the default
// resolver returned tenant "1" for every user. Public self-registration creates
// a new tenant per signup, so a freshly registered org admin would be served as
// tenant 1 with access to the bootstrap organization's data. The startup guard
// only sees tenants that existed at boot; this resolver closes the gap on every
// request.
func TestSingleTenantResolver_RejectsForeignTenantUser(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/tenant_resolver.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, ddl := range []string{
		`CREATE TABLE tenants (id TEXT PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO tenants (id, status) VALUES ('1', 'active'), ('tenant_foreign', 'active')`); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	for _, row := range []struct{ id, tenant string }{
		{"u-bootstrap", "1"},
		{"u-foreign", "tenant_foreign"},
	} {
		if _, err := db.Exec(`INSERT INTO users (id, tenant_id) VALUES ($1, $2)`, row.id, row.tenant); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	resolve := SingleTenantResolver(db)
	ctx := context.Background()

	tenant, err := resolve(ctx, "u-bootstrap")
	if err != nil || tenant != "1" {
		t.Fatalf("bootstrap user must resolve to tenant 1, got tenant=%q err=%v", tenant, err)
	}

	if _, err := resolve(ctx, "u-foreign"); err == nil {
		t.Fatal("user belonging to another tenant must be rejected while multi-tenant is disabled")
	}

	if _, err := resolve(ctx, "u-missing"); err == nil {
		t.Fatal("unknown user must be rejected")
	}

	// A suspended bootstrap tenant must invalidate live sessions too, matching
	// the multi-tenant resolver's behaviour.
	if _, err := db.Exec(`UPDATE tenants SET status = 'suspended' WHERE id = '1'`); err != nil {
		t.Fatalf("suspend bootstrap tenant: %v", err)
	}
	if _, err := resolve(ctx, "u-bootstrap"); !errors.Is(err, auth.ErrTenantSuspended) {
		t.Fatalf("suspended bootstrap tenant must return ErrTenantSuspended, got %v", err)
	}
}
