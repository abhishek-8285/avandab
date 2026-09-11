package database

import (
	"context"
	"fmt"
	"strings"

	"database/sql"
)

// PreGooseCutVersion is the last migration version before the first
// generated-identity insert into roles (00073 customer). Startup migrates PG
// in two phases — UpTo(PreGooseCutVersion), ResyncIdentitySequences, Up —
// because explicit-id seeds (00027 driver=5, 00064 org_admin=6) never advance
// the identity sequence, so generated ids collide on fresh chains.
const PreGooseCutVersion = 72

// ResyncIdentitySequences repairs PG identity-sequence desync behind
// explicit-id seeds (A7 tail proof). SQLite is unaffected (max+1 assignment)
// and non-PG handles are a no-op. Tables must exist — callers run it after
// migrating to at least PreGooseCutVersion. Idempotent and safe to repeat.
func ResyncIdentitySequences(ctx context.Context, db *sql.DB) error {
	// Fast path covers appdb.Open handles; the server probe covers directly
	// opened handles (tests, sidecars) that never registered a driver.
	if !IsPostgres(db) && !isPostgresServer(ctx, db) {
		return nil
	}
	for _, q := range []string{
		`SELECT setval('roles_id_seq', (SELECT COALESCE(MAX(id), 0) FROM roles), true)`,
		`SELECT setval('permissions_id_seq', (SELECT COALESCE(MAX(id), 0) FROM permissions), true)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("database: identity resync failed: %w", err)
		}
	}
	return nil
}

// isPostgresServer reports whether db speaks to PostgreSQL. Errors mean
// "unknown" (conservative no-op) — never fail startup on probe trouble;
// the migration run itself remains the authority.
func isPostgresServer(ctx context.Context, db *sql.DB) bool {
	var v string
	if err := db.QueryRowContext(ctx, `SELECT version()`).Scan(&v); err != nil {
		return false
	}
	return strings.Contains(v, "PostgreSQL")
}
