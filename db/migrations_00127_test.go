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

// TestMigration00127DeliveredEvent proves 00127 applies AND rolls back:
// event_type='DELIVERED' (written by the TripDeliveredEvent handler on every
// delivery) passes the CHECK after Up and is rejected after Down.
func TestMigration00127DeliveredEvent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	_, err = database.Exec(
		`INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by)
		 VALUES ('e1', 'EWB1', 't1', 'DELIVERED', '{}', 'system')`,
	)
	require.NoError(t, err, "'DELIVERED' must pass the widened CHECK after up")

	_, err = provider.DownTo(ctx, 126)
	require.NoError(t, err)

	_, err = database.Exec(
		`INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by)
		 VALUES ('e2', 'EWB1', 't1', 'DELIVERED', '{}', 'system')`,
	)
	require.Error(t, err, "'DELIVERED' must be rejected after down to 126")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertForeignKeyCheckClean(t, database, "00127 re-up")
}
