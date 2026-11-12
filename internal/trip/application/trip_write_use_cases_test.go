package application

// Characterization tests for the trip write side-effect use-cases extracted
// from internal/handlers/trips.go (handlers-end-state ADR, Phase 2). They pin
// tenant scoping, the exact operator-facing descriptions and the legacy
// best-effort semantics.

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	legacydomain "transport-app/internal/domain"
	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

// fakeAuditLogger records calls routed through the legacy audit facade.
type fakeAuditLogger struct {
	mu    sync.Mutex
	calls []fakeAuditCall
	err   error
}

type fakeAuditCall struct {
	userID    string
	action    string
	tableName string
	recordID  string
	oldValues *string
	newValues *string
}

func (f *fakeAuditLogger) LogAction(_ context.Context, userID *legacydomain.UserID, action, tableName, recordID string, oldValues, newValues *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	uid := ""
	if userID != nil {
		uid = string(*userID)
	}
	f.calls = append(f.calls, fakeAuditCall{userID: uid, action: action, tableName: tableName, recordID: recordID, oldValues: oldValues, newValues: newValues})
	return f.err
}

func seedPhase2Tenant(t *testing.T, db *sql.DB, tenantID string) {
	t.Helper()
	// slug carries a UNIQUE constraint and collides with seeded rows; the FK
	// triggers only look at tenants(id), so seed id+name only.
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name) VALUES (?, ?)`, tenantID, "Tenant "+tenantID)
	require.NoError(t, err)
	var seeded int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM tenants WHERE id = ?`, tenantID).Scan(&seeded))
	require.Equal(t, 1, seeded, "tenant fixture must exist: tenant FK triggers enforce tenants(id)")
}

func seedPhase2Trip(t *testing.T, db *sql.DB, tripID, tripNumber, tenantID, vehicleID string) {
	t.Helper()
	var veh any
	if vehicleID != "" {
		veh = vehicleID
	}
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, arrival_time, status, tenant_id, vehicle_id)
		VALUES (?, ?, 'b-phase2', 'r-phase2', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'delivered', ?, ?)`,
		tripID, tripNumber, tenantID, veh)
	require.NoError(t, err)
}

func seedPhase2Vehicle(t *testing.T, db *sql.DB, vehicleID, tenantID, status string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, tenant_id)
		VALUES (?, ?, ?, 'truck', 15, ?, ?)`,
		vehicleID, "REG-"+vehicleID, "MH-01-"+vehicleID, status, tenantID)
	require.NoError(t, err)
}

func seedPhase2Stop(t *testing.T, db *sql.DB, stopID, tripID, tenantID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address)
		VALUES (?, ?, ?, 1, 'drop', 'Depot', 'Industrial Area')`, stopID, tenantID, tripID)
	require.NoError(t, err)
}

func phase2VehicleState(t *testing.T, db *sql.DB, vehicleID string) (string, int, sql.NullString) {
	t.Helper()
	var status string
	var blocked int
	var reason sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status, blocked, blocked_reason FROM vehicles WHERE id = ?`, vehicleID).Scan(&status, &blocked, &reason))
	return status, blocked, reason
}

func phase2Ctx(tenantID string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(tenantID))
}

func TestRecordDispatchOverride_WritesOverrideRowAndAuditFallback(t *testing.T) {
	db := newTripTestDB(t)
	const reason = "urgent delivery approved by the client"
	uc := NewRecordDispatchOverrideUseCase(db, nil)

	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), RecordDispatchOverrideCommand{
		TenantID:     "tenant-1",
		TripID:       "trip-ovr",
		VehicleID:    "veh-1",
		DriverID:     "drv-1",
		BlockedBy:    "license_expiry",
		Reason:       reason,
		OverriddenBy: "user-9",
	}))

	var tenantID, tripID, vehicleID, driverID, blockedBy, gotReason, overriddenBy, createdAt string
	require.NoError(t, db.QueryRow(`SELECT tenant_id, trip_id, COALESCE(vehicle_id, ''), COALESCE(driver_id, ''),
		blocked_by, reason, overridden_by, created_at
		FROM dispatch_overrides WHERE trip_id = 'trip-ovr'`).
		Scan(&tenantID, &tripID, &vehicleID, &driverID, &blockedBy, &gotReason, &overriddenBy, &createdAt))
	assert.Equal(t, "tenant-1", tenantID, "override must be attributed to the trip's tenant")
	assert.Equal(t, "veh-1", vehicleID)
	assert.Equal(t, "drv-1", driverID)
	assert.Equal(t, "license_expiry", blockedBy)
	assert.Equal(t, reason, gotReason)
	assert.Equal(t, "user-9", overriddenBy)
	assert.NotEmpty(t, createdAt)

	// No audit service injected -> the raw audit_logs fallback must fire with
	// the request tenant and the acting user.
	var action, tableName, recordID, newValues, auditTenant, auditUser string
	require.NoError(t, db.QueryRow(`SELECT action, table_name, record_id, new_values, tenant_id, user_id
		FROM audit_logs WHERE action = 'dispatch_override'`).
		Scan(&action, &tableName, &recordID, &newValues, &auditTenant, &auditUser))
	assert.Equal(t, "trips", tableName)
	assert.Equal(t, "trip-ovr", recordID)
	assert.Equal(t, reason, newValues)
	assert.Equal(t, "tenant-1", auditTenant)
	assert.Equal(t, "user-9", auditUser)
}

func TestRecordDispatchOverride_PrefersAuditServiceOverRawFallback(t *testing.T) {
	db := newTripTestDB(t)
	fake := &fakeAuditLogger{}
	uc := NewRecordDispatchOverrideUseCase(db, fake)

	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), RecordDispatchOverrideCommand{
		TenantID:     "tenant-1",
		TripID:       "trip-ovr",
		BlockedBy:    "insurance_expiry",
		Reason:       "override with a long enough justification",
		OverriddenBy: "user-9",
	}))

	require.Len(t, fake.calls, 1)
	call := fake.calls[0]
	assert.Equal(t, "user-9", call.userID)
	assert.Equal(t, "dispatch_override", call.action)
	assert.Equal(t, "trips", call.tableName)
	assert.Equal(t, "trip-ovr", call.recordID)
	assert.Nil(t, call.oldValues)
	require.NotNil(t, call.newValues)
	assert.Equal(t, "override with a long enough justification", *call.newValues)

	// The audit service already recorded the entry — no duplicate raw row.
	var auditRows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'dispatch_override'`).Scan(&auditRows))
	assert.Zero(t, auditRows)

	// The override row itself is still persisted.
	var overrideRows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dispatch_overrides WHERE trip_id = 'trip-ovr'`).Scan(&overrideRows))
	assert.Equal(t, 1, overrideRows)
}

func TestRecordDispatchOverride_NilDependenciesAreSafe(t *testing.T) {
	uc := NewRecordDispatchOverrideUseCase(nil, nil)
	require.NoError(t, uc.Execute(context.Background(), RecordDispatchOverrideCommand{
		TenantID:     "tenant-1",
		TripID:       "trip-ovr",
		BlockedBy:    "blocked",
		Reason:       "no persistence wired",
		OverriddenBy: "user-9",
	}))
}

func TestRecordDispatchOverride_TenantScopedRows(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	uc := NewRecordDispatchOverrideUseCase(db, nil)

	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), RecordDispatchOverrideCommand{
		TenantID: "tenant-1", TripID: "shared-trip", BlockedBy: "puc_expiry", Reason: "first tenant reason", OverriddenBy: "u1",
	}))
	require.NoError(t, uc.Execute(phase2Ctx("tenant-2"), RecordDispatchOverrideCommand{
		TenantID: "tenant-2", TripID: "shared-trip", BlockedBy: "puc_expiry", Reason: "second tenant reason", OverriddenBy: "u2",
	}))

	count := func(tenantID string) int {
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM dispatch_overrides WHERE tenant_id = ? AND trip_id = 'shared-trip'`, tenantID).Scan(&n))
		return n
	}
	assert.Equal(t, 1, count("tenant-1"))
	assert.Equal(t, 1, count("tenant-2"))

	// Raw audit fallback rows are tenant-scoped from the request context too.
	var auditTenant1 int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'dispatch_override' AND tenant_id = 'tenant-1'`).Scan(&auditTenant1))
	assert.Equal(t, 1, auditTenant1)
}

func TestBlockVehicleForBreakdown_ResolvesTripVehicleAndBlocksIt(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-close", "TRIP-CLOSE", "tenant-1", "veh-close")
	seedPhase2Vehicle(t, db, "veh-close", "tenant-1", "running")

	uc := NewBlockVehicleForBreakdownUseCase(db)
	res := uc.Execute(phase2Ctx("tenant-1"), BlockVehicleForBreakdownCommand{
		TenantID: "tenant-1",
		TripID:   aggregate.TripID("trip-close"),
		Note:     "Engine overheated on highway",
	})

	assert.Equal(t, "veh-close", res.VehicleID)
	assert.Equal(t, "Breakdown reported at trip close: Engine overheated on highway (vehicle veh-close)", res.Description)

	status, blocked, reason := phase2VehicleState(t, db, "veh-close")
	assert.Equal(t, "maintenance", status)
	assert.Equal(t, 1, blocked)
	assert.True(t, reason.Valid)
	assert.Equal(t, res.Description, reason.String)
}

func TestBlockVehicleForBreakdown_ExplicitVehicleWithoutNote(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Vehicle(t, db, "veh-close", "tenant-1", "running")

	uc := NewBlockVehicleForBreakdownUseCase(db)
	res := uc.Execute(phase2Ctx("tenant-1"), BlockVehicleForBreakdownCommand{
		TenantID:  "tenant-1",
		TripID:    aggregate.TripID("trip-close"),
		VehicleID: "veh-close",
	})

	assert.Equal(t, "veh-close", res.VehicleID)
	assert.Equal(t, "Breakdown reported at trip close (vehicle veh-close)", res.Description)
	status, _, reason := phase2VehicleState(t, db, "veh-close")
	assert.Equal(t, "maintenance", status)
	assert.True(t, reason.Valid)
}

func TestBlockVehicleForBreakdown_NoVehicleKeepsAlertDescription(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-no-veh", "TRIP-NOVEH", "tenant-1", "")

	uc := NewBlockVehicleForBreakdownUseCase(db)
	res := uc.Execute(phase2Ctx("tenant-1"), BlockVehicleForBreakdownCommand{
		TenantID: "tenant-1",
		TripID:   aggregate.TripID("trip-no-veh"),
		Note:     "Flat tyre",
	})

	assert.Empty(t, res.VehicleID)
	assert.Equal(t, "Breakdown reported at trip close: Flat tyre", res.Description)
}

func TestBlockVehicleForBreakdown_TenantScoped(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-close", "TRIP-CLOSE", "tenant-1", "veh-close")
	seedPhase2Vehicle(t, db, "veh-close", "tenant-1", "running")

	uc := NewBlockVehicleForBreakdownUseCase(db)

	// Wrong tenant: the trip is invisible, so no vehicle is resolved and no
	// row is touched.
	res := uc.Execute(phase2Ctx("tenant-2"), BlockVehicleForBreakdownCommand{
		TenantID: "tenant-2",
		TripID:   aggregate.TripID("trip-close"),
	})
	assert.Empty(t, res.VehicleID)
	assert.Equal(t, "Breakdown reported at trip close", res.Description)

	// Even an explicit vehicle id must not cross tenants: the UPDATE is
	// tenant-scoped, so nothing changes.
	res = uc.Execute(phase2Ctx("tenant-2"), BlockVehicleForBreakdownCommand{
		TenantID:  "tenant-2",
		TripID:    aggregate.TripID("trip-close"),
		VehicleID: "veh-close",
	})
	assert.Equal(t, "veh-close", res.VehicleID, "description still names the vehicle the operator saw")

	status, blocked, _ := phase2VehicleState(t, db, "veh-close")
	assert.Equal(t, "running", status)
	assert.Equal(t, 0, blocked)
}

func TestMirrorPODArtifacts_UpdatesTripAndAttachments(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-pod", "TRIP-POD", "tenant-1", "")
	seedPhase2Stop(t, db, "stop-pod", "trip-pod", "tenant-1")

	uc := NewMirrorPODArtifactsUseCase(db)
	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), MirrorPODArtifactsCommand{
		TenantID:     "tenant-1",
		TripID:       "trip-pod",
		StopID:       "stop-pod",
		PODURL:       "/uploads/pod/a.jpg",
		SignatureURL: "/uploads/pod/sig.png",
		Notes:        "Signed by consignee",
	}))

	var podURL, podPhotoURL, podSignatureURL, podNotes string
	var capturedAt sql.NullString
	require.NoError(t, db.QueryRow(`SELECT COALESCE(pod_url, ''), COALESCE(pod_photo_url, ''),
		COALESCE(pod_signature_url, ''), COALESCE(pod_notes, ''), pod_captured_at
		FROM trips WHERE id = 'trip-pod'`).
		Scan(&podURL, &podPhotoURL, &podSignatureURL, &podNotes, &capturedAt))
	assert.Equal(t, "/uploads/pod/a.jpg", podURL)
	assert.Equal(t, "/uploads/pod/a.jpg", podPhotoURL)
	assert.Equal(t, "/uploads/pod/sig.png", podSignatureURL)
	assert.Equal(t, "Signed by consignee", podNotes)
	assert.True(t, capturedAt.Valid, "pod_captured_at must be stamped")

	var photoURL, stopID, tripID, tenantID string
	require.NoError(t, db.QueryRow(`SELECT file_url, stop_id, trip_id, tenant_id FROM stop_pod_attachments WHERE file_type = 'photo'`).
		Scan(&photoURL, &stopID, &tripID, &tenantID))
	assert.Equal(t, "/uploads/pod/a.jpg", photoURL)
	assert.Equal(t, "stop-pod", stopID)
	assert.Equal(t, "trip-pod", tripID)
	assert.Equal(t, "tenant-1", tenantID)

	var signatureAttachmentURL string
	require.NoError(t, db.QueryRow(`SELECT file_url FROM stop_pod_attachments WHERE file_type = 'signature'`).Scan(&signatureAttachmentURL))
	assert.Equal(t, "/uploads/pod/sig.png", signatureAttachmentURL)

	var attachments int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM stop_pod_attachments`).Scan(&attachments))
	assert.Equal(t, 2, attachments)
}

func TestMirrorPODArtifacts_PreservesExistingValuesWhenInputsEmpty(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-pod", "TRIP-POD", "tenant-1", "")
	_, err := db.Exec(`UPDATE trips SET pod_url = '/old.jpg', pod_notes = 'old note' WHERE id = 'trip-pod'`)
	require.NoError(t, err)

	uc := NewMirrorPODArtifactsUseCase(db)
	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), MirrorPODArtifactsCommand{
		TenantID: "tenant-1",
		TripID:   "trip-pod",
	}))

	var podURL, podNotes string
	var capturedAt sql.NullString
	require.NoError(t, db.QueryRow(`SELECT COALESCE(pod_url, ''), COALESCE(pod_notes, ''), pod_captured_at FROM trips WHERE id = 'trip-pod'`).
		Scan(&podURL, &podNotes, &capturedAt))
	assert.Equal(t, "/old.jpg", podURL, "COALESCE(NULLIF()) keeps the stored URL")
	assert.Equal(t, "old note", podNotes)

	var attachments int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM stop_pod_attachments`).Scan(&attachments))
	assert.Zero(t, attachments, "no attachments without a stop id and URLs")
}

func TestMirrorPODArtifacts_MatchesTripNumber(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-pod", "TRIP-POD", "tenant-1", "")

	uc := NewMirrorPODArtifactsUseCase(db)
	require.NoError(t, uc.Execute(phase2Ctx("tenant-1"), MirrorPODArtifactsCommand{
		TenantID: "tenant-1",
		TripID:   "TRIP-POD",
		PODURL:   "/uploads/pod/by-number.jpg",
	}))

	var podURL string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(pod_url, '') FROM trips WHERE id = 'trip-pod'`).Scan(&podURL))
	assert.Equal(t, "/uploads/pod/by-number.jpg", podURL, "mirror resolves the trip by id or trip_number")
}

func TestMirrorPODArtifacts_TenantScoped(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Trip(t, db, "trip-pod", "TRIP-POD", "tenant-1", "")
	seedPhase2Tenant(t, db, "tenant-2")

	uc := NewMirrorPODArtifactsUseCase(db)
	require.NoError(t, uc.Execute(phase2Ctx("tenant-2"), MirrorPODArtifactsCommand{
		TenantID: "tenant-2",
		TripID:   "trip-pod",
		PODURL:   "/uploads/pod/foreign.jpg",
	}))

	var podURL string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(pod_url, '') FROM trips WHERE id = 'trip-pod'`).Scan(&podURL))
	assert.Empty(t, podURL, "the mirror UPDATE is tenant-scoped")
}

func TestMirrorPODArtifacts_NilDBIsSafe(t *testing.T) {
	uc := NewMirrorPODArtifactsUseCase(nil)
	require.NoError(t, uc.Execute(context.Background(), MirrorPODArtifactsCommand{
		TenantID: "tenant-1",
		TripID:   "trip-pod",
		PODURL:   "/uploads/pod/a.jpg",
	}))
}
