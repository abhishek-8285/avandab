package sql_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/driver/domain"
	driversql "transport-app/internal/driver/infrastructure/persistence/sql"
)

func setupLifecycleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	migrationBytes, err := os.ReadFile("../../../../../db/migrations/00108_driver_lifecycle_refactor.sql")
	require.NoError(t, err)

	raw := string(migrationBytes)
	// Only run the Up portion
	if idx := strings.Index(raw, "-- +goose Down"); idx != -1 {
		raw = raw[:idx]
	}

	for _, stmt := range strings.Split(raw, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" {
			_, err := db.Exec(trimmed)
			require.NoError(t, err, "failed executing statement: %s", trimmed)
		}
	}

	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestDriverLifecycle_TenantIsolation(t *testing.T) {
	db := setupLifecycleTestDB(t)
	repo := driversql.NewDriverLifecycleRepository(db)
	ctx := context.Background()

	tenantA := "tenant-a"
	tenantB := "tenant-b"
	driverID := "driver-123"

	// Save license under Tenant A
	licA := domain.DriverLicenseRecord{
		ID:                 "lic-a",
		TenantID:           tenantA,
		DriverID:           driverID,
		LicenseNumber:      "DL-TN-A",
		ExpiresOn:          time.Now().Add(365 * 24 * time.Hour),
		VerificationStatus: "verified",
	}
	err := repo.SaveLicense(ctx, tenantA, driverID, licA, []string{"HMV", "TRANS"})
	require.NoError(t, err)

	// Tenant A can read license
	resA, classesA, err := repo.GetCurrentLicense(ctx, tenantA, driverID)
	require.NoError(t, err)
	require.NotNil(t, resA)
	assert.Equal(t, "DL-TN-A", resA.LicenseNumber)
	assert.Equal(t, []string{"HMV", "TRANS"}, classesA)

	// Tenant B CANNOT read Tenant A's license
	resB, classesB, err := repo.GetCurrentLicense(ctx, tenantB, driverID)
	require.NoError(t, err)
	assert.Nil(t, resB)
	assert.Nil(t, classesB)
}

func TestDriverLifecycle_ConflictingAssignmentsPrevented(t *testing.T) {
	db := setupLifecycleTestDB(t)
	repo := driversql.NewDriverLifecycleRepository(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driver1 := "driver-1"
	driver2 := "driver-2"
	vehicle1 := "vehicle-1"

	now := time.Now()
	asg1 := domain.DriverVehicleAssignmentRecord{
		ID:             "asg-1",
		TenantID:       tenantID,
		DriverID:       driver1,
		VehicleID:      vehicle1,
		AssignmentType: "company_assigned",
		Status:         "active",
		StartedAt:      &now,
	}

	// First assignment succeeds
	err := repo.CreateAssignment(ctx, tenantID, asg1)
	require.NoError(t, err)

	// Second driver trying to claim same vehicle concurrently fails
	asg2 := domain.DriverVehicleAssignmentRecord{
		ID:             "asg-2",
		TenantID:       tenantID,
		DriverID:       driver2,
		VehicleID:      vehicle1,
		AssignmentType: "company_assigned",
		Status:         "active",
		StartedAt:      &now,
	}
	err = repo.CreateAssignment(ctx, tenantID, asg2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting active assignment exists")

	// Same driver trying to claim another vehicle fails
	asg3 := domain.DriverVehicleAssignmentRecord{
		ID:             "asg-3",
		TenantID:       tenantID,
		DriverID:       driver1,
		VehicleID:      "vehicle-2",
		AssignmentType: "company_assigned",
		Status:         "active",
		StartedAt:      &now,
	}
	err = repo.CreateAssignment(ctx, tenantID, asg3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting active assignment exists")

	// Ending assignment 1 releases lock
	err = repo.EndAssignment(ctx, tenantID, asg1.ID, time.Now())
	require.NoError(t, err)

	// Now driver 2 can be assigned to vehicle 1
	err = repo.CreateAssignment(ctx, tenantID, asg2)
	require.NoError(t, err)
}

func TestDriverLifecycle_TelemetryIdempotency(t *testing.T) {
	db := setupLifecycleTestDB(t)
	repo := driversql.NewDriverLifecycleRepository(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	sessionID := "sess-1"
	clientEventID := "evt-unique-123"

	// Register installation & session first
	inst := domain.TelemetryInstallationRecord{
		ID:                "inst-1",
		TenantID:          tenantID,
		AppInstallationID: "app-inst-1",
		Platform:          "android",
		AppVersion:        "1.0.0",
		DeviceModel:       "Realme 8",
		OSVersion:         "Android 13",
		Status:            "active",
	}
	err := repo.RegisterInstallation(ctx, tenantID, inst)
	require.NoError(t, err)

	sess := domain.TelemetrySessionRecord{
		ID:             sessionID,
		TenantID:       tenantID,
		InstallationID: inst.ID,
		DriverID:       "driver-1",
		SessionType:    "on_duty",
		Status:         "active",
		StartReason:    "APP_AVAILABLE",
		StartedAt:      time.Now(),
	}
	err = repo.StartSession(ctx, tenantID, sess)
	require.NoError(t, err)

	evt := domain.TelemetryEventRecord{
		ID:            "evt-row-1",
		TenantID:      tenantID,
		SessionID:     sessionID,
		ClientEventID: clientEventID,
		OccurredAt:    time.Now(),
		Latitude:      28.6139,
		Longitude:     77.2090,
		Speed:         42.5,
	}

	// First ingestion succeeds
	err = repo.IngestEvent(ctx, tenantID, evt)
	require.NoError(t, err)

	// Duplicate ingestion with same clientEventID is silently ignored (idempotent)
	evtDuplicate := evt
	evtDuplicate.ID = "evt-row-2"
	err = repo.IngestEvent(ctx, tenantID, evtDuplicate)
	require.NoError(t, err)

	// Verify only 1 event exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM telemetry_events WHERE session_id = ?", sessionID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestDriverLifecycle_VehicleLatestPositionProjection(t *testing.T) {
	db := setupLifecycleTestDB(t)
	repo := driversql.NewDriverLifecycleRepository(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	vehicleID := "vehicle-101"

	pos1 := domain.VehicleLatestPositionRecord{
		TenantID:   tenantID,
		VehicleID:  vehicleID,
		SessionID:  "sess-1",
		DriverID:   "driver-1",
		Latitude:   28.5000,
		Longitude:  77.1000,
		Speed:      30.0,
		OccurredAt: time.Now().Add(-5 * time.Minute),
		Source:     "mobile_session",
	}
	err := repo.UpsertLatestPosition(ctx, tenantID, pos1)
	require.NoError(t, err)

	pos2 := domain.VehicleLatestPositionRecord{
		TenantID:   tenantID,
		VehicleID:  vehicleID,
		SessionID:  "sess-1",
		DriverID:   "driver-1",
		Latitude:   28.5500,
		Longitude:  77.1500,
		Speed:      55.0,
		OccurredAt: time.Now(),
		Source:     "mobile_session",
	}
	err = repo.UpsertLatestPosition(ctx, tenantID, pos2)
	require.NoError(t, err)

	// Fetch projected position
	fetched, err := repo.GetLatestPosition(ctx, tenantID, vehicleID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, 28.5500, fetched.Latitude)
	assert.Equal(t, 55.0, fetched.Speed)

	// Verify Tenant B cannot view Tenant A's latest position
	fetchedB, err := repo.GetLatestPosition(ctx, "tenant-2", vehicleID)
	require.NoError(t, err)
	assert.Nil(t, fetchedB)
}

func TestDriverLifecycle_AuditAndVerificationLog(t *testing.T) {
	db := setupLifecycleTestDB(t)
	repo := driversql.NewDriverLifecycleRepository(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "driver-1"
	actorID := "admin-user-1"
	reason := "Approved after Vahan RC check"
	oldState := "pending"
	newState := "verified"

	auditEvt := domain.AuditEventRecord{
		ID:          "audit-1",
		TenantID:    tenantID,
		ActorUserID: &actorID,
		EntityType:  "driver_license",
		EntityID:    driverID,
		Action:      "verify",
		OldState:    &oldState,
		NewState:    &newState,
		Reason:      &reason,
	}
	err := repo.RecordAuditEvent(ctx, tenantID, auditEvt)
	require.NoError(t, err)

	ref := "VAHAN-REF-9921"
	attempt := domain.VerificationAttemptRecord{
		ID:                "att-1",
		TenantID:          tenantID,
		EntityType:        "driver_license",
		EntityID:          driverID,
		Provider:          "vahan_api",
		ProviderReference: &ref,
		Status:            "success",
		RequestedAt:       time.Now().Add(-10 * time.Second),
	}
	err = repo.RecordVerificationAttempt(ctx, tenantID, attempt)
	require.NoError(t, err)

	var auditCount, attemptCount int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE tenant_id = ?", tenantID).Scan(&auditCount)
	require.NoError(t, err)
	assert.Equal(t, 1, auditCount)

	err = db.QueryRow("SELECT COUNT(*) FROM verification_attempts WHERE tenant_id = ?", tenantID).Scan(&attemptCount)
	require.NoError(t, err)
	assert.Equal(t, 1, attemptCount)
}
