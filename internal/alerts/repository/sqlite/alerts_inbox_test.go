package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newInboxTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_alerts_inbox_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := filepath.Join(cwd, "../../../../db/migrations")
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertAlert(t *testing.T, db *sql.DB, id, tenantID string, rank int, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO alerts (
		id, source, alert_type, severity, status, dedup_key,
		tenant_id, ack_status, severity_rank, money_at_risk,
		title, message, occurrences, first_seen_at, last_seen_at, created_at, updated_at
	) VALUES (?, 'telemetry', 'speeding', 'warning', 'open', ?,
		?, 'open', ?, 0,
		't', 'm', 1, ?, ?, ?, ?)`,
		id, "dk-"+id, tenantID, rank, createdAt, createdAt, createdAt, createdAt)
	require.NoError(t, err)
}

// TestListInbox_TenantIsolation — Spec 22 §7 S1: tenant A never sees
// tenant B alerts.
func TestListInbox_TenantIsolation(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insertAlert(t, db, "al-a1", "tenant-a", 2, now.Add(-2*time.Hour))
	insertAlert(t, db, "al-a2", "tenant-a", 1, now.Add(-1*time.Hour))
	insertAlert(t, db, "al-b1", "tenant-b", 1, now)

	rowsA, err := repo.ListInbox(ctx, "tenant-a", "all", 50)
	require.NoError(t, err)
	require.Len(t, rowsA, 2)
	for _, a := range rowsA {
		assert.Equal(t, "tenant-a", a.TenantID)
	}
	// Ranked: severity_rank ASC wins over created_at DESC.
	assert.Equal(t, "al-a2", rowsA[0].ID)
	assert.Equal(t, "al-a1", rowsA[1].ID)

	rowsB, err := repo.ListInbox(ctx, "tenant-b", "all", 50)
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
	assert.Equal(t, "al-b1", rowsB[0].ID)
}

// TestListInbox_StatusFiltersAndSnoozeVisibility — snoozed rows are hidden
// until snoozed_until passes (Spec 22 §5.1).
func TestListInbox_StatusFiltersAndSnoozeVisibility(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insertAlert(t, db, "al-open", "tenant-a", 1, now)
	insertAlert(t, db, "al-snoozed-future", "tenant-a", 2, now)
	_, err := db.Exec(`UPDATE alerts SET ack_status='snoozed', snoozed_until=? WHERE id='al-snoozed-future'`, now.Add(time.Hour))
	require.NoError(t, err)
	insertAlert(t, db, "al-acked", "tenant-a", 3, now)
	_, err = db.Exec(`UPDATE alerts SET ack_status='acked', status='acknowledged' WHERE id='al-acked'`)
	require.NoError(t, err)

	open, err := repo.ListInbox(ctx, "tenant-a", "open", 50)
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, "al-open", open[0].ID)

	snoozed, err := repo.ListInbox(ctx, "tenant-a", "snoozed", 50)
	require.NoError(t, err)
	require.Len(t, snoozed, 1)
	assert.Equal(t, "al-snoozed-future", snoozed[0].ID)

	acked, err := repo.ListInbox(ctx, "tenant-a", "acked", 50)
	require.NoError(t, err)
	require.Len(t, acked, 1)
	assert.Equal(t, "al-acked", acked[0].ID)

	// Expired snooze becomes visible in the open view.
	_, err = db.Exec(`UPDATE alerts SET snoozed_until=? WHERE id='al-snoozed-future'`, now.Add(-time.Minute))
	require.NoError(t, err)
	openAfter, err := repo.ListInbox(ctx, "tenant-a", "open", 50)
	require.NoError(t, err)
	assert.Len(t, openAfter, 2)
}

// TestInboxAck_GuardIsNoOp — second admin's ack is 200-but-no-op
// (Spec 22 edge case 10).
func TestInboxAck_GuardIsNoOp(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insertAlert(t, db, "al-ack-1", "tenant-a", 1, now)

	first, err := repo.InboxAck(ctx, "al-ack-1", "admin-1")
	require.NoError(t, err)
	assert.True(t, first)

	second, err := repo.InboxAck(ctx, "al-ack-1", "admin-2")
	require.NoError(t, err)
	assert.False(t, second, "second ack must be a no-op")
}

// TestInboxSnoozeAndSweep — snooze hides an alert; ReopenExpiredSnoozes
// flips expired ones back to open (Spec 22 §5.1 worker sweep).
func TestInboxSnoozeAndSweep(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insertAlert(t, db, "al-sn-1", "tenant-a", 1, now)
	insertAlert(t, db, "al-sn-2", "tenant-a", 2, now)

	until := now.Add(2 * time.Hour)
	ok, err := repo.InboxSnooze(ctx, "al-sn-1", "admin-1", until)
	require.NoError(t, err)
	require.True(t, ok)

	visible, err := repo.ListInbox(ctx, "tenant-a", "open", 50)
	require.NoError(t, err)
	require.Len(t, visible, 1)
	assert.Equal(t, "al-sn-2", visible[0].ID)

	stored, err := repo.ListInbox(ctx, "tenant-a", "snoozed", 50)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.NotNil(t, stored[0].SnoozedUntil)
	assert.WithinDuration(t, until, *stored[0].SnoozedUntil, time.Second)

	// Snooze-all only touches open rows.
	count, err := repo.InboxSnoozeAll(ctx, []string{"al-sn-1", "al-sn-2"}, "admin-1", until)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "already-snoozed row must be skipped")

	// Sweep: expire both, reopen returns them to the open view.
	future := now.Add(3 * time.Hour)
	n, err := repo.ReopenExpiredSnoozes(ctx, future)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	reopened, err := repo.ListInbox(ctx, "tenant-a", "open", 50)
	require.NoError(t, err)
	assert.Len(t, reopened, 2)
}

// TestMigrationFilesExist — Spec 22 §7: migration-exists assertions for
// the reserved 00092–00094 slots (00093/00094 land with S7/S8).
func TestMigrationFilesExist(t *testing.T) {
	cwd, _ := os.Getwd()
	dir := filepath.Join(cwd, "../../../../db/migrations")
	if filepath.Base(cwd) == "basic" {
		dir = filepath.Join(cwd, "db/migrations")
	}
	for _, name := range []string{"00092_alert_inbox.sql"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected migration on disk: %s (%v)", name, err)
		}
	}
}
