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

// TestMigration00137AVCommands proves 00137 creates vehicle_commands and inserts av_operator role.
func TestMigration00137AVCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig137.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 137)
	require.NoError(t, err)

	// Verify table exists
	_, err = database.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t137','T137','t137')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v137', 't137', 'MH12AV0001', 'MH12AV0001', 'truck', 5000, 'diesel', '2027-01-01', '2027-01-01', '2027-01-01')`)
	require.NoError(t, err)

	_, err = database.Exec(`INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, issued_by)
		VALUES ('cmd1', 't137', 'v137', 'E_STOP', 'operator-1')`)
	require.NoError(t, err)

	var count int
	require.NoError(t, database.QueryRow(`SELECT count(*) FROM vehicle_commands WHERE id = 'cmd1'`).Scan(&count))
	require.Equal(t, 1, count)

	var roleExists int
	require.NoError(t, database.QueryRow(`SELECT count(*) FROM roles WHERE name = 'av_operator'`).Scan(&roleExists))
	require.Equal(t, 1, roleExists)

	// Test rollback (down)
	_, err = provider.DownTo(ctx, 136)
	require.NoError(t, err)

	// Table should be gone
	err = database.QueryRow(`SELECT count(*) FROM vehicle_commands`).Scan(&count)
	require.Error(t, err)
}
