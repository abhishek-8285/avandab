package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/repository/sqlite"
)

// TestTenantActive_MissingTenantFailsClosed guards audit finding M4
// (2026-11-12): tenantActive returned nil on sql.ErrNoRows, so a user whose
// tenant organization was deleted/suspended-without-row still logged in. A
// missing tenant must fail closed like a suspended one.
func TestTenantActive_MissingTenantFailsClosed(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "tenant.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, q := range []string{
		`CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '1')`,
		`INSERT INTO tenants (id, name, status) VALUES ('1', 'Default', 'active')`,
		`INSERT INTO tenants (id, name, status) VALUES ('t-suspended', 'Suspended Org', 'suspended')`,
		`INSERT INTO users (id, tenant_id) VALUES ('u-active', '1')`,
		`INSERT INTO users (id, tenant_id) VALUES ('u-orphan', 't-missing')`,
		`INSERT INTO users (id, tenant_id) VALUES ('u-suspended', 't-suspended')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	svc := &AuthService{baseService: baseService{
		store: sqlite.NewRepository(db),
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	ctx := context.Background()

	if err := svc.tenantActive(ctx, "u-active"); err != nil {
		t.Fatalf("active tenant must pass, got %v", err)
	}
	if err := svc.tenantActive(ctx, "u-orphan"); !errors.Is(err, auth.ErrTenantMissing) {
		t.Fatalf("missing tenant row must fail closed with ErrTenantMissing, got %v", err)
	}
	if err := svc.tenantActive(ctx, "u-suspended"); !errors.Is(err, auth.ErrTenantSuspended) {
		t.Fatalf("suspended tenant must be rejected with ErrTenantSuspended, got %v", err)
	}
}
