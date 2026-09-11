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

func TestMigration00145FacilitiesMaster(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig_00145.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 145)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after 00145 up")

	// Insert facility record
	_, err = database.Exec(`
		INSERT INTO facilities (
			id, tenant_id, facility_code, name, facility_type,
			plant, circle, profit_center, cost_center,
			address, city, state, pincode, latitude, longitude
		) VALUES (
			'fac-001', '1', 'MM21000000757', 'MMS Office - Bangalore', 'office',
			'PLANT-BLR', 'Karnataka', 'PC-100', 'CC-200',
			'Museum Road', 'Bangalore', 'Karnataka', '560001', 12.9716, 77.5946
		)
	`)
	require.NoError(t, err)

	var code, name, facType, city string
	err = database.QueryRowContext(ctx, `SELECT facility_code, name, facility_type, city FROM facilities WHERE id = 'fac-001'`).
		Scan(&code, &name, &facType, &city)
	require.NoError(t, err)
	require.Equal(t, "MM21000000757", code)
	require.Equal(t, "MMS Office - Bangalore", name)
	require.Equal(t, "office", facType)
	require.Equal(t, "Bangalore", city)

	// Verify permissions
	var permCount int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM permissions WHERE name IN ('facilities:read', 'facilities:write')`).Scan(&permCount)
	require.NoError(t, err)
	require.Equal(t, 2, permCount)

	// Verify Down rollback
	_, err = provider.Down(ctx)
	require.NoError(t, err)

	var tableCount int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='facilities'`).Scan(&tableCount)
	require.NoError(t, err)
	require.Equal(t, 0, tableCount)

	// Check clean FK after down
	assertForeignKeyCheckClean(t, database, "after 00145 down")
}
