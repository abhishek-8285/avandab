package application

// Characterization + extraction tests for the driver SOS/emergency trigger
// service moved out of internal/handlers/sos.go (handlers-end-state ADR,
// Phase 2): driver identity rules (id-or-email, fallback to session user),
// emergency tenant attribution, active-trip lookup, idempotent outbox write +
// event publish + audit only on first delivery, and the legacy duplicate-ack
// behavior on insert failure.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

const sosTestSchema = `
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT
);
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT,
    email TEXT,
    tenant_id TEXT NOT NULL
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    driver_id TEXT,
    vehicle_id TEXT,
    status TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    published_at DATETIME
);
CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    action TEXT NOT NULL,
    table_name TEXT NOT NULL,
    record_id TEXT,
    new_values TEXT,
    tenant_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newSOSTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("sos_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(sosTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecSOS(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func countSOSRows(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func seedSOSDriver(t *testing.T, db *sql.DB, id, driverID, email, tenantID string) {
	t.Helper()
	mustExecSOS(t, db, `INSERT INTO drivers (id, driver_id, email, tenant_id) VALUES (?, ?, ?, ?)`, id, driverID, email, tenantID)
}

func TestSOSService_DriverIdentityAndEmergencyTenantAttribution(t *testing.T) {
	db := newSOSTestDB(t)
	ctx := context.Background()
	mustExecSOS(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com'), ('u-2', 'two@example.com')`)
	seedSOSDriver(t, db, "d-1", "DRV-1", "one@example.com", "tenant-1")
	seedSOSDriver(t, db, "d-2", "DRV-2", "two@example.com", "tenant-2")

	svc := NewSOSService(db, nil)

	// Email-linked identity.
	driverID, ok := svc.DriverForUser(ctx, "u-1")
	assert.True(t, ok)
	assert.Equal(t, "d-1", driverID)

	// drivers.id identity rule.
	driverID, ok = svc.DriverForUser(ctx, "d-2")
	assert.True(t, ok)
	assert.Equal(t, "d-2", driverID)

	// Emergency identity lookup is intentionally tenant-filter-free: the
	// session user resolves their driver row even across tenant boundaries.
	driverID, ok = svc.DriverForUser(ctx, "u-2")
	assert.True(t, ok)
	assert.Equal(t, "d-2", driverID)

	// Unresolvable identity falls back to the session user id, never dropped.
	driverID, ok = svc.DriverForUser(ctx, "u-missing")
	assert.True(t, ok)
	assert.Equal(t, "u-missing", driverID)

	// Header-auth drivers without tenant context are attributed to their own org.
	assert.Equal(t, "tenant-2", svc.DriverTenant(ctx, "d-2"))
	assert.Empty(t, svc.DriverTenant(ctx, "d-missing"))

	// nil DB degrades like a failed lookup (fallback id, empty tenant/trip).
	driverID, ok = NewSOSService(nil, nil).DriverForUser(ctx, "u-1")
	assert.True(t, ok)
	assert.Equal(t, "u-1", driverID)
	assert.Empty(t, NewSOSService(nil, nil).DriverTenant(ctx, "d-1"))
	tripID, vehicleID := NewSOSService(nil, nil).ActiveTrip(ctx, "d-1")
	assert.Empty(t, tripID)
	assert.Empty(t, vehicleID)
}

func TestSOSService_ActiveTripNewestActiveOnly(t *testing.T) {
	db := newSOSTestDB(t)
	ctx := context.Background()
	seedSOSDriver(t, db, "d-1", "DRV-1", "", "tenant-1")
	mustExecSOS(t, db, `INSERT INTO trips (id, driver_id, vehicle_id, status, created_at) VALUES
		('t-old', 'd-1', 'v-old', 'assigned', '2026-01-01 00:00:00'),
		('t-new', 'd-1', 'v-new', 'in_transit', '2026-02-01 00:00:00'),
		('t-done', 'd-1', 'v-done', 'completed', '2026-03-01 00:00:00')`)

	svc := NewSOSService(db, nil)
	tripID, vehicleID := svc.ActiveTrip(ctx, "d-1")
	assert.Equal(t, "t-new", tripID, "newest ACTIVE trip wins; completed trips are ignored")
	assert.Equal(t, "v-new", vehicleID)

	emptyTrip, emptyVehicle := svc.ActiveTrip(ctx, "d-missing")
	assert.Empty(t, emptyTrip)
	assert.Empty(t, emptyVehicle)
}

func TestSOSService_TriggerIdempotentOutboxPublishAndAudit(t *testing.T) {
	db := newSOSTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	bus := events.NewInMemoryBus()
	var published []events.Event
	bus.Subscribe(events.SOSEvent, func(_ context.Context, e events.Event) error {
		published = append(published, e)
		return nil
	})

	svc := NewSOSService(db, bus)
	trigger := func(sosID string) bool {
		t.Helper()
		first, err := svc.Trigger(ctx, SOSTrigger{
			SOSID:        sosID,
			TenantID:     "tenant-1",
			DriverID:     "d-1",
			VehicleID:    "v-1",
			TripID:       "t-1",
			Latitude:     28.5355,
			Longitude:    77.3910,
			Accuracy:     5,
			BatteryLevel: 82.5,
			Reason:       "panic button",
			TriggeredAt:  time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),
			ReceivedAt:   time.Date(2026, 9, 2, 10, 0, 1, 0, time.UTC),
		})
		require.NoError(t, err)
		return first
	}

	require.True(t, trigger("sos_cmd-1"))
	assert.Equal(t, int64(1), countSOSRows(t, db, `SELECT COUNT(*) FROM outbox_events WHERE event_type = ?`, events.SOSEvent))

	var outboxID, aggregateID, aggregateType, payload string
	require.NoError(t, db.QueryRow(`SELECT id, aggregate_id, aggregate_type, payload FROM outbox_events WHERE aggregate_id = 'sos_cmd-1'`).
		Scan(&outboxID, &aggregateID, &aggregateType, &payload))
	assert.Equal(t, "ob_sos_cmd-1", outboxID)
	assert.Equal(t, "sos_cmd-1", aggregateID)
	assert.Equal(t, "sos", aggregateType)
	var payloadMap map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(payload), &payloadMap))
	assert.Equal(t, "sos_cmd-1", payloadMap["SOSID"])
	assert.Equal(t, "d-1", payloadMap["DriverID"])
	assert.Equal(t, "v-1", payloadMap["VehicleID"])
	assert.Equal(t, "t-1", payloadMap["TripID"])
	assert.Equal(t, "d-1", payloadMap["driver_id"], "legacy duplicate lowercase keys are preserved")
	// Event bus publish + audit happened exactly once.
	require.Len(t, published, 1)
	assert.Equal(t, events.SOSEvent, published[0].Type)
	assert.Equal(t, int64(1), countSOSRows(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'driver_sos_triggered'`))
	var auditUser, auditTable, auditRecord, auditValues, auditTenant string
	require.NoError(t, db.QueryRow(`SELECT user_id, table_name, record_id, new_values, tenant_id FROM audit_logs WHERE action = 'driver_sos_triggered'`).
		Scan(&auditUser, &auditTable, &auditRecord, &auditValues, &auditTenant))
	assert.Equal(t, "d-1", auditUser)
	assert.Equal(t, "safety", auditTable)
	assert.Equal(t, "sos_cmd-1", auditRecord)
	assert.Contains(t, auditValues, "lat:28.535500")
	assert.Equal(t, "tenant-1", auditTenant)

	// Duplicate command id: idempotent acknowledgment, no re-publish, no re-audit.
	assert.False(t, trigger("sos_cmd-1"))
	assert.Equal(t, int64(1), countSOSRows(t, db, `SELECT COUNT(*) FROM outbox_events`))
	assert.Len(t, published, 1)
	assert.Equal(t, int64(1), countSOSRows(t, db, `SELECT COUNT(*) FROM audit_logs`))

	// A different command id is a new delivery.
	assert.True(t, trigger("sos_cmd-2"))
	assert.Equal(t, int64(2), countSOSRows(t, db, `SELECT COUNT(*) FROM outbox_events`))
	assert.Len(t, published, 2)
	assert.Equal(t, int64(2), countSOSRows(t, db, `SELECT COUNT(*) FROM audit_logs`))

	// nil bus: persist + audit still happen, publish is skipped.
	noBus := NewSOSService(db, nil)
	first, err := noBus.Trigger(ctx, SOSTrigger{SOSID: "sos_cmd-3", TenantID: "tenant-1", DriverID: "d-1"})
	require.NoError(t, err)
	assert.True(t, first)
	assert.Equal(t, int64(3), countSOSRows(t, db, `SELECT COUNT(*) FROM outbox_events`))
}

func TestSOSService_AuditTenantOrPlatformFallback(t *testing.T) {
	db := newSOSTestDB(t)
	svc := NewSOSService(db, nil)

	// No tenant context: the audit row falls back to the platform tenant.
	first, err := svc.Trigger(context.Background(), SOSTrigger{SOSID: "sos_ctxless", TenantID: "tenant-1", DriverID: "d-1"})
	require.NoError(t, err)
	require.True(t, first)
	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM audit_logs WHERE record_id = 'sos_ctxless'`).Scan(&tenant))
	assert.Equal(t, string(shared.DefaultTenant), tenant)

	// With tenant context, the audit row is scoped to it.
	ctxTwo := shared.ContextWithTenantID(context.Background(), "tenant-2")
	_, err = svc.Trigger(ctxTwo, SOSTrigger{SOSID: "sos_ctx2", TenantID: "tenant-1", DriverID: "d-1"})
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM audit_logs WHERE record_id = 'sos_ctx2'`).Scan(&tenant))
	assert.Equal(t, "tenant-2", tenant)
}

func TestSOSService_InsertFailureRendersDuplicateAck(t *testing.T) {
	db := newSOSTestDB(t)
	ctx := context.Background()
	bus := events.NewInMemoryBus()
	var published int
	bus.Subscribe(events.SOSEvent, func(_ context.Context, _ events.Event) error {
		published++
		return nil
	})
	mustExecSOS(t, db, `DROP TABLE outbox_events`)

	svc := NewSOSService(db, bus)
	first, err := svc.Trigger(ctx, SOSTrigger{SOSID: "sos_broken", TenantID: "tenant-1", DriverID: "d-1"})
	require.Error(t, err, "the insert failure is surfaced for logging")
	assert.False(t, first, "legacy rowsAffected==0 path => duplicate acknowledgment (HTTP 200) in the handler")
	assert.Zero(t, published, "no event may be published when the outbox row was not written")
	assert.Zero(t, countSOSRows(t, db, `SELECT COUNT(*) FROM audit_logs`), "no audit row on a failed insert")
}
