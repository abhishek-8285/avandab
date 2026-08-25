package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository"
)

// setupUsersTestDB creates an in-memory SQLite DB with all migrations applied.
func setupUsersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_users_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../../db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestUserRepository_SearchUsersDateRange proves the from/to window filters on
// users.created_at used by the users list page calendar. Rows are seeded with
// RFC3339 timestamps on purpose: the window must use date(substr(col,1,10)),
// not date(col).
func TestUserRepository_SearchUsersDateRange(t *testing.T) {
	dbConn := setupUsersTestDB(t)
	repo, ok := interface{}(NewRepository(dbConn)).(interface {
		SearchUsersDateRange(ctx context.Context, query string, status string, from string, to string, limit int, offset int, tenantID string) ([]repository.UserWithRole, error)
		CountUsersDateRange(ctx context.Context, query string, status string, from string, to string, tenantID string) (int64, error)
	})
	require.True(t, ok, "user repo must implement date-range search")

	var adminRoleID int64
	require.NoError(t, dbConn.QueryRow(`SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID))

	ctx := context.Background()
	mk := func(email, name, status string, day int) {
		created := fmt.Sprintf("2026-08-%02dT08:00:00Z", day) // RFC3339, like Go inserts
		_, err := dbConn.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, created_at, updated_at)
			VALUES (?, ?, 'hash', ?, ?, ?, ?, ?)`, email, email, name, adminRoleID, status, created, created)
		require.NoError(t, err)
	}
	mk("aug01@x.com", "Aug One", "active", 1)
	mk("aug10@x.com", "Aug Ten", "inactive", 10)
	mk("aug20@x.com", "Aug Twenty", "suspended", 20)

	// Full-month window
	rows, err := repo.SearchUsersDateRange(ctx, "", "", "2026-08-01", "2026-08-31", 10, 0, "1")
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	total, err := repo.CountUsersDateRange(ctx, "", "", "2026-08-01", "2026-08-31", "1")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)

	// Single-day window (from == to)
	rows, err = repo.SearchUsersDateRange(ctx, "", "", "2026-08-10", "2026-08-10", 10, 0, "1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "aug10@x.com", rows[0].Email)
	total, err = repo.CountUsersDateRange(ctx, "", "", "2026-08-10", "2026-08-10", "1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// From-only bound (excludes the Aug 10 row)
	_, total, _ = bounds(repo, ctx, "2026-08-11", "")
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, _ = bounds(repo, ctx, "", "2026-08-05")
	assert.EqualValues(t, 1, total)

	// Status + search + date combined
	rows, err = repo.SearchUsersDateRange(ctx, "twenty", "suspended", "2026-08-01", "2026-08-31", 10, 0, "1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "aug20@x.com", rows[0].Email)
}

func bounds(
	r interface {
		SearchUsersDateRange(ctx context.Context, query string, status string, from string, to string, limit int, offset int, tenantID string) ([]repository.UserWithRole, error)
		CountUsersDateRange(ctx context.Context, query string, status string, from string, to string, tenantID string) (int64, error)
	}, ctx context.Context, from, to string,
) ([]repository.UserWithRole, int64, error) {
	rows, err := r.SearchUsersDateRange(ctx, "", "", from, to, 10, 0, "1")
	if err != nil {
		return nil, 0, err
	}
	total, err := r.CountUsersDateRange(ctx, "", "", from, to, "1")
	return rows, total, err
}
