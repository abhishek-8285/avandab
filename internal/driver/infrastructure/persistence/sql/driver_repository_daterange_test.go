package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	driverdomain "transport-app/internal/driver/domain"
	"transport-app/internal/shared"
)

const driverTestSchema = `
CREATE TABLE drivers (
    id                  TEXT PRIMARY KEY,
    driver_id           TEXT NOT NULL UNIQUE,
    first_name          TEXT NOT NULL,
    last_name           TEXT NOT NULL,
    phone               TEXT NOT NULL,
    email               TEXT,
    address             TEXT,
    license_number      TEXT NOT NULL,
    license_expiry      DATE NOT NULL,
    experience_years    INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'on_trip', 'leave', 'inactive')),
    emergency_contact_name TEXT,
    emergency_contact_phone TEXT,
    notes               TEXT,
    tenant_id           TEXT NOT NULL DEFAULT '1',
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at          DATETIME NOT NULL DEFAULT (datetime('now'))
);
`

func setupDriverTestDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "-", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(driverTestSchema)
	require.NoError(t, err)
	return dbConn
}

// TestDriverRepository_SearchReadModelsDateRange proves the from/to window
// filters on created_at used by the drivers list page calendar.
func TestDriverRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupDriverTestDB(t)
	repo, ok := NewDriverRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]driverdomain.DriverReadModel, int64, error)
	})
	require.True(t, ok, "driver repo must implement date-range search")

	ctx := context.Background()
	mk := func(id, firstName string, createdAt, status string) {
		_, err := dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, 'Kumar', '+91-90000-00000', 'DL-0000', datetime('now','+5 years'), ?, '1', ?, ?)`,
			id, "DRV-"+id, firstName, status, createdAt, createdAt)
		require.NoError(t, err)
	}
	// RFC3339 strings exactly as the Go driver writes them, plus CURRENT_TIMESTAMP-style text.
	mk("drv-1", "AUGONE", "2026-08-01T09:00:00Z", "available")
	mk("drv-2", "AUGTWO", "2026-08-10T09:00:00Z", "on_trip")
	mk("drv-3", "AUGTHREE", "2026-08-20T09:00:00Z", "inactive")
	mk("drv-4", "SEPFIVE", "2026-09-05T09:00:00Z", "available")
	// IST-day-boundary row: 2026-08-09T18:45:00Z = 00:15 IST Aug 10.
	mk("drv-5", "AUGISTTEN", "2026-08-09T18:45:00Z", "available")

	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, rows, 4)
	for _, r := range rows {
		assert.Contains(t, []string{"AUGONE", "AUGTWO", "AUGTHREE", "AUGISTTEN"}, r.FirstName)
	}

	// Single-day window (from == to) — the IST-boundary row belongs to Aug 10.
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	names := []string{rows[0].FirstName, rows[1].FirstName}
	assert.Contains(t, names, "AUGTWO")
	assert.Contains(t, names, "AUGISTTEN")

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + date combined
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "inactive", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "AUGTHREE", rows[0].FirstName)

	// Search + date combined
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "AUGT", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
}
