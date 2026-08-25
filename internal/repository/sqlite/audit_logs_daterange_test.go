package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/repository"
)

// TestAuditLogRepository_ListAuditLogsDateRange proves the free-text and
// created_at window filters used by the audit logs list page. Rows are seeded
// with RFC3339 timestamps on purpose: the window must use
// date(substr(col,1,10)), not date(col).
func TestAuditLogRepository_ListAuditLogsDateRange(t *testing.T) {
	dbConn := setupUsersTestDB(t)
	repo, ok := interface{}(NewRepository(dbConn)).(interface {
		ListAuditLogsDateRange(ctx context.Context, query string, from string, to string, limit int, offset int) ([]repository.AuditLogWithUser, int64, error)
	})
	require.True(t, ok, "audit repo must implement date-range listing")

	ctx := context.Background()
	mk := func(action, table string, day int) {
		created := time.Date(2026, 8, day, 8, 0, 0, 0, time.UTC)
		_, err := dbConn.Exec(`INSERT INTO audit_logs (id, action, table_name, created_at)
			VALUES (?, ?, ?, ?)`, "log-"+action+"-"+table, action, table, created)
		require.NoError(t, err)
	}
	mk("create_booking", "bookings", 1)
	mk("update_trip", "trips", 10)
	mk("delete_invoice", "invoices", 20)

	logs, total, err := repo.ListAuditLogsDateRange(ctx, "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, logs, 3)

	// Single-day window (from == to)
	_, total, err = repo.ListAuditLogsDateRange(ctx, "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// From-only bound
	_, total, err = repo.ListAuditLogsDateRange(ctx, "", "2026-08-11", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.ListAuditLogsDateRange(ctx, "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Free-text query over action/table + date combined
	_, total, err = repo.ListAuditLogsDateRange(ctx, "trip", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}
