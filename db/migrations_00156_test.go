package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00156ContactAckUpAndDown proves 00156 applies and rolls
// back cleanly (Prove-It protocol #4).
func TestMigration00156ContactAckUpAndDown(t *testing.T) {
	content, err := Migrations.ReadFile("migrations/00156_contact_acknowledged_at.sql")
	require.NoError(t, err)

	mapFS := fstest.MapFS{
		"00156_contact_acknowledged_at.sql": &fstest.MapFile{Data: content},
	}
	var fsys fs.FS = mapFS

	dbPath := filepath.Join(t.TempDir(), "mig.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec(`CREATE TABLE contact_submissions (
		id TEXT PRIMARY KEY, ticket_number TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL, email TEXT NOT NULL, subject TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'general', message TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending')`)
	require.NoError(t, err)

	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, fsys)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('contact_submissions') WHERE name = 'acknowledged_at'`).Scan(&n))
	require.Equal(t, 1, n, "contact_submissions missing column acknowledged_at")

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)

	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('contact_submissions') WHERE name = 'acknowledged_at'`).Scan(&n))
	require.Equal(t, 0, n, "acknowledged_at should be dropped on down")
}
