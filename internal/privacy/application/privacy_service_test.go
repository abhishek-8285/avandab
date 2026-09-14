package application

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared/id"
)

func newPrivacyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_privacy_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newPrivacyService(t *testing.T, db *sql.DB) *PrivacyService {
	t.Helper()
	return NewPrivacyService(db, id.NewUUIDGenerator())
}

func seedPrivacyTenant(t *testing.T, db *sql.DB, tenant string) {
	t.Helper()
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		tenant, tenant, tenant)
	require.NoError(t, err)
}

func TestBreach_LifecycleNotifyDetailClose(t *testing.T) {
	db := newPrivacyTestDB(t)
	privacy := newPrivacyService(t, db)
	ctx := context.Background()
	const tenant = "tenant_breach_1"

	seedPrivacyTenant(t, db, tenant)

	id, err := privacy.ReportBreach(ctx, tenant, "lost laptop", "field laptop missing", "loss", "1 device", 3)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	b, err := privacy.GetBreach(ctx, tenant, id)
	require.NoError(t, err)
	require.Equal(t, BreachStatusOpen, b.Status)

	// Foreign tenant reads as not-found (fail-closed).
	_, err = privacy.GetBreach(ctx, "other_tenant", id)
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Partial notification keeps status open; both stamps advance it.
	require.NoError(t, privacy.MarkNotified(ctx, tenant, id, true, false))
	b, _ = privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusOpen, b.Status)
	require.True(t, b.BoardNotifiedAt.Valid)
	require.NoError(t, privacy.MarkNotified(ctx, tenant, id, false, true))
	b, _ = privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusNotified, b.Status)

	require.NoError(t, privacy.FileDetail(ctx, tenant, id, "found; wiped remotely"))
	b, _ = privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusDetailed, b.Status)
	require.True(t, b.DetailedAt.Valid)

	require.NoError(t, privacy.CloseBreach(ctx, tenant, id))
	b, _ = privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusClosed, b.Status)

	// Closed rows reject further mutation.
	require.ErrorIs(t, privacy.FileDetail(ctx, tenant, id, "again"), sql.ErrNoRows)
}

// Overdue = open/notified past the 72h filing deadline. Backdate detected_at
// directly: the deadline clock is DB time, deterministic by construction.
func TestBreach_OverdueWatchlist(t *testing.T) {
	db := newPrivacyTestDB(t)
	privacy := newPrivacyService(t, db)
	ctx := context.Background()
	const tenant = "tenant_breach_2"

	seedPrivacyTenant(t, db, tenant)

	fresh, err := privacy.ReportBreach(ctx, tenant, "fresh", "", "", "", 0)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO breach_incidents (id, tenant_id, title, detected_at)
		VALUES ('brc-old-open', $1, 'old open', $2)`,
		tenant, time.Now().UTC().Add(-73*time.Hour).Format("2006-01-02 15:04:05"))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO breach_incidents (id, tenant_id, title, status,
		board_notified_at, principals_notified_at, detected_at)
		VALUES ('brc-old-notified', $1, 'old notified', 'notified',
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, $2)`,
		tenant, time.Now().UTC().Add(-80*time.Hour).Format("2006-01-02 15:04:05"))
	require.NoError(t, err)

	overdue, err := privacy.ListOverdueBreaches(ctx, tenant)
	require.NoError(t, err)
	require.Len(t, overdue, 2, "fresh %s must not be overdue", fresh)
	require.Equal(t, "brc-old-notified", overdue[0].ID, "oldest first")
	require.Equal(t, "brc-old-open", overdue[1].ID)

	list, err := privacy.ListBreaches(ctx, tenant, "")
	require.NoError(t, err)
	require.Len(t, list, 3)
}
