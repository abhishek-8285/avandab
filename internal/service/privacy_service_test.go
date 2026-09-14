package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/repository/sqlite"
)

func newPrivacyTestServices(t *testing.T) *Services {
	t.Helper()
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{AppEnv: "testing"}
	return NewServices(sqlite.NewRepository(db), cfg, logger, nil)
}

func TestBreach_LifecycleNotifyDetailClose(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_breach_1"

	require.NoError(t, execTenant(t, svcs, tenant))

	id, err := svcs.Privacy.ReportBreach(ctx, tenant, "lost laptop", "field laptop missing", "loss", "1 device", 3)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	b, err := svcs.Privacy.GetBreach(ctx, tenant, id)
	require.NoError(t, err)
	require.Equal(t, BreachStatusOpen, b.Status)

	// Foreign tenant reads as not-found (fail-closed).
	_, err = svcs.Privacy.GetBreach(ctx, "other_tenant", id)
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Partial notification keeps status open; both stamps advance it.
	require.NoError(t, svcs.Privacy.MarkNotified(ctx, tenant, id, true, false))
	b, _ = svcs.Privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusOpen, b.Status)
	require.True(t, b.BoardNotifiedAt.Valid)
	require.NoError(t, svcs.Privacy.MarkNotified(ctx, tenant, id, false, true))
	b, _ = svcs.Privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusNotified, b.Status)

	require.NoError(t, svcs.Privacy.FileDetail(ctx, tenant, id, "found; wiped remotely"))
	b, _ = svcs.Privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusDetailed, b.Status)
	require.True(t, b.DetailedAt.Valid)

	require.NoError(t, svcs.Privacy.CloseBreach(ctx, tenant, id))
	b, _ = svcs.Privacy.GetBreach(ctx, tenant, id)
	require.Equal(t, BreachStatusClosed, b.Status)

	// Closed rows reject further mutation.
	require.ErrorIs(t, svcs.Privacy.FileDetail(ctx, tenant, id, "again"), sql.ErrNoRows)
}

// Overdue = open/notified past the 72h filing deadline. Backdate detected_at
// directly: the deadline clock is DB time, deterministic by construction.
func TestBreach_OverdueWatchlist(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_breach_2"

	db := svcs.DB()
	require.NotNil(t, db)
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant_breach_2', 'B2', 'b2')`)
	require.NoError(t, err)

	fresh, err := svcs.Privacy.ReportBreach(ctx, tenant, "fresh", "", "", "", 0)
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

	overdue, err := svcs.Privacy.ListOverdueBreaches(ctx, tenant)
	require.NoError(t, err)
	require.Len(t, overdue, 2, "fresh %s must not be overdue", fresh)
	require.Equal(t, "brc-old-notified", overdue[0].ID, "oldest first")
	require.Equal(t, "brc-old-open", overdue[1].ID)

	list, err := svcs.Privacy.ListBreaches(ctx, tenant, "")
	require.NoError(t, err)
	require.Len(t, list, 3)
}

func execTenant(t *testing.T, svcs *Services, tenant string) error {
	t.Helper()
	db := svcs.DB()
	if db == nil {
		return errors.New("no raw DB")
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		tenant, tenant, tenant)
	return err
}
