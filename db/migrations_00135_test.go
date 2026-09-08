package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00135PlatePerTenantUp proves 00135 converts the global plate
// UNIQUE to UNIQUE(tenant_id, registration_number): two tenants may hold the
// same plate (previously impossible), same-tenant dupes still fail, and
// vehicle_latest_position gains its composite tenant unique.
func TestMigration00135PlatePerTenantUp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 135)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t135a','T135 A','t135-a'), ('t135b','T135 B','t135-b')`)
	require.NoError(t, err)

	// Same plate in two tenants: the H3 fix.
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v135a','MH01SHARED','MH01SHARED','truck',15,'t135a')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v135b','MH01SHARED','MH01SHARED','truck',15,'t135b')`)
	require.NoError(t, err, "same plate in two tenants must succeed after 00135")

	// Same tenant dupe still rejected.
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v135c','MH01SHARED','MH01SHARED','truck',15,'t135a')`)
	require.Error(t, err, "same-tenant plate dupe must fail")

	// L10 composite index present.
	var idx string
	require.NoError(t, database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_vehicle_latest_position_tenant_vehicle'`).Scan(&idx))
	require.Equal(t, "idx_vehicle_latest_position_tenant_vehicle", idx)

	// Tenant index + FK triggers survived the rebuild.
	var trig int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND tbl_name='vehicles'`).Scan(&trig))
	require.GreaterOrEqual(t, trig, 2)
}

// TestMigration00135Down restores the global UNIQUE on a conflict-free DB.
func TestMigration00135Down(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 135)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 134)
	require.NoError(t, err)

	// Global UNIQUE back: cross-tenant dupe now fails.
	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t135c','T135 C','t135-c'), ('t135d','T135 D','t135-d')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v135d','MH01DOWN','MH01DOWN','truck',15,'t135c')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v135e','MH01DOWN','MH01DOWN','truck',15,'t135d')`)
	require.Error(t, err, "down must restore the global plate UNIQUE")

	// Re-up so the shared suite never leaves the scratch DB behind head.
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}
