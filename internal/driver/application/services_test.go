package application_test

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

	"transport-app/internal/driver/application"
)

const baseSchema = `
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL UNIQUE,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    email TEXT,
    license_number TEXT NOT NULL,
    license_expiry DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    notes TEXT,
    tenant_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL UNIQUE,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL DEFAULT 'truck',
    capacity REAL NOT NULL DEFAULT 5000,
    fuel_type TEXT NOT NULL DEFAULT 'diesel',
    insurance_expiry DATE NOT NULL,
    fitness_expiry DATE NOT NULL,
    permit_expiry DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    tenant_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS trips (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    booking_id TEXT NOT NULL,
    driver_id TEXT,
    vehicle_id TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    started_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func setupAppServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(baseSchema)
	require.NoError(t, err)

	migrationBytes, err := os.ReadFile("../../../db/migrations/00108_driver_lifecycle_refactor.sql")
	require.NoError(t, err)

	raw := string(migrationBytes)
	if idx := strings.Index(raw, "-- +goose Down"); idx != -1 {
		raw = raw[:idx]
	}

	for _, stmt := range strings.Split(raw, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" {
			_, err := db.Exec(trimmed)
			require.NoError(t, err, "failed executing: %s", trimmed)
		}
	}

	mig109Bytes, err := os.ReadFile("../../../db/migrations/00109_dispatch_offers.sql")
	require.NoError(t, err)

	raw109 := string(mig109Bytes)
	if idx := strings.Index(raw109, "-- +goose Down"); idx != -1 {
		raw109 = raw109[:idx]
	}

	for _, stmt := range strings.Split(raw109, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" {
			_, err := db.Exec(trimmed)
			require.NoError(t, err, "failed executing 109: %s", trimmed)
		}
	}

	mig116Bytes, err := os.ReadFile("../../../db/migrations/00116_driver_push_tokens.sql")
	require.NoError(t, err)

	raw116 := string(mig116Bytes)
	if idx := strings.Index(raw116, "-- +goose Down"); idx != -1 {
		raw116 = raw116[:idx]
	}

	for _, stmt := range strings.Split(raw116, ";") {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" {
			_, err := db.Exec(trimmed)
			require.NoError(t, err, "failed executing 116: %s", trimmed)
		}
	}

	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestDriverAppService_LifecycleE2E(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-101"
	reviewerID := "admin-user"

	// 1. Register Driver -> Onboarding state created
	err := svc.RegisterDriver(ctx, tenantID, driverID, "Rajesh Kumar", "rajesh@example.com", "9876543210")
	require.NoError(t, err)

	onb, err := svc.GetOnboardingState(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, "profile", onb.CurrentStep)
	assert.False(t, onb.IsEligible)

	// 2. Submit License -> Verification pending
	err = svc.SubmitLicense(ctx, tenantID, driverID, "DL-DELHI-9988", "Delhi RTO", time.Now().Add(-365*24*time.Hour), time.Now().Add(365*24*time.Hour), []string{"HMV", "TRANS"})
	require.NoError(t, err)

	// 3. Submit Document
	docID, err := svc.SubmitDocument(ctx, tenantID, driverID, "dl_front", "s3://docs/dl.pdf", "application/pdf", 1024, "hash123")
	require.NoError(t, err)
	assert.NotEmpty(t, docID)

	// 4. Submit Payout Account
	accID, err := svc.SubmitPayoutAccount(ctx, tenantID, driverID, "Rajesh Kumar", "123456789012", "SBIN0001234", "State Bank of India")
	require.NoError(t, err)
	assert.NotEmpty(t, accID)

	// 5. Owner-Operator Claims Vehicle
	claimID, err := svc.ClaimVehicle(ctx, tenantID, driverID, "DL1LN9999", &docID)
	require.NoError(t, err)
	assert.NotEmpty(t, claimID)

	// 6. Submit for verification
	err = svc.SubmitForVerification(ctx, tenantID, driverID)
	require.NoError(t, err)

	onbSubmitted, err := svc.GetOnboardingState(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, "submitted", onbSubmitted.OverallStatus)
	assert.False(t, onbSubmitted.IsEligible) // Not eligible until verified

	// 7. Reviewer Approves Claim & License
	var licID string
	err = db.QueryRow("SELECT id FROM driver_licenses WHERE tenant_id = ? AND driver_id = ?", tenantID, driverID).Scan(&licID)
	require.NoError(t, err)

	err = svc.ReviewDriverLicense(ctx, tenantID, licID, reviewerID, true, "Verified with Parivahan")
	require.NoError(t, err)

	err = svc.ReviewVehicleClaim(ctx, tenantID, claimID, reviewerID, true, "RC Matches Owner Name")
	require.NoError(t, err)

	// 8. Add verified compliance documents for vehicle
	var vehID string
	err = db.QueryRow("SELECT id FROM vehicles WHERE tenant_id = ? AND registration_number = 'DL1LN9999'", tenantID).Scan(&vehID)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vehicle_compliance_documents (id, tenant_id, vehicle_id, document_type, document_number, expires_on, verification_status, created_at)
		VALUES
			('vcd-1', 'tenant-1', ?, 'insurance', 'INS-101', date('now', '+1 year'), 'verified', CURRENT_TIMESTAMP),
			('vcd-2', 'tenant-1', ?, 'fitness', 'FIT-101', date('now', '+1 year'), 'verified', CURRENT_TIMESTAMP),
			('vcd-3', 'tenant-1', ?, 'permit', 'PER-101', date('now', '+1 year'), 'verified', CURRENT_TIMESTAMP),
			('vcd-4', 'tenant-1', ?, 'puc', 'PUC-101', date('now', '+1 year'), 'verified', CURRENT_TIMESTAMP)`,
		vehID, vehID, vehID, vehID)
	require.NoError(t, err)

	// 9. Driver now becomes operationally eligible for dispatch!
	el, err := svc.EvaluateDispatchEligibility(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.True(t, el.IsEligible, "expected driver to be dispatch eligible after all approvals; blockers: %+v", el.Blockers)
}

func TestDriverAppService_VehicleReassignment(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-102"
	assignerID := "dispatcher-1"

	_, err := db.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, license_number, license_expiry, tenant_id)
		VALUES (?, ?, 'Suresh', 'Singh', '9988776655', 'available', 'DL-123', date('now', '+1 year'), ?);
		INSERT INTO vehicles (id, registration_number, vehicle_number, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES
			('veh-101', 'DL1AB1111', 'DL1AB1111', date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'), ?),
			('veh-102', 'DL1AB2222', 'DL1AB2222', date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'), ?)`,
		driverID, driverID, tenantID, tenantID, tenantID)
	require.NoError(t, err)

	// Assign vehicle 101
	asg1, err := svc.AssignVehicleToDriver(ctx, tenantID, driverID, "veh-101", assignerID, "company_assigned")
	require.NoError(t, err)
	assert.NotEmpty(t, asg1)

	// Reassign to vehicle 102 (automatically closes assignment 101)
	asg2, err := svc.AssignVehicleToDriver(ctx, tenantID, driverID, "veh-102", assignerID, "company_assigned")
	require.NoError(t, err)
	assert.NotEmpty(t, asg2)

	// Verify assignment 101 is ended and 102 is active
	var status1, status2 string
	err = db.QueryRow("SELECT status FROM driver_vehicle_assignments WHERE id = ?", asg1).Scan(&status1)
	require.NoError(t, err)
	assert.Equal(t, "ended", status1)

	err = db.QueryRow("SELECT status FROM driver_vehicle_assignments WHERE id = ?", asg2).Scan(&status2)
	require.NoError(t, err)
	assert.Equal(t, "active", status2)
}

func TestDriverAppService_TenantIsolation(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantA := "tenant-a"
	tenantB := "tenant-b"
	driverA := "drv-a"

	err := svc.RegisterDriver(ctx, tenantA, driverA, "Driver A", "a@test.com", "9000000001")
	require.NoError(t, err)

	// Tenant B cannot evaluate or see Tenant A's driver
	_, err = svc.EvaluateDispatchEligibility(ctx, tenantB, driverA)
	assert.Error(t, err, "expected error when querying driver across tenant boundary")
}

func TestDriverAppService_DispatchOffersAndConcurrency(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverA := "drv-alpha"
	driverB := "drv-beta"
	bookingID := "booking-101"

	// Register drivers
	require.NoError(t, svc.RegisterDriver(ctx, tenantID, driverA, "Alpha Driver", "alpha@test.com", "9876543210"))
	require.NoError(t, svc.RegisterDriver(ctx, tenantID, driverB, "Beta Driver", "beta@test.com", "9876543211"))

	// Create dispatch offers for same booking
	offerA, err := svc.CreateDispatchOffer(ctx, tenantID, bookingID, driverA, "veh-001", 15)
	require.NoError(t, err)
	assert.Equal(t, "offered", offerA.Status)

	offerB, err := svc.CreateDispatchOffer(ctx, tenantID, bookingID, driverB, "veh-002", 15)
	require.NoError(t, err)
	assert.Equal(t, "offered", offerB.Status)

	// Driver A accepts offer
	cmdA := application.DriverCommandRequest{
		CommandID: "cmd-accept-1",
		Type:      "ACCEPT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offerA.ID},
	}
	respA, err := svc.ProcessDriverCommand(ctx, tenantID, driverA, cmdA)
	require.NoError(t, err)
	assert.True(t, respA.Success)
	assert.Equal(t, "ACCEPTED", respA.Status)
	assert.NotEmpty(t, respA.TripID)

	// Idempotency: Driver A sending same command ID returns identical success without error
	respAIdempotent, err := svc.ProcessDriverCommand(ctx, tenantID, driverA, cmdA)
	require.NoError(t, err)
	assert.True(t, respAIdempotent.Success)
	assert.Equal(t, "ACCEPTED", respAIdempotent.Status)
	assert.Equal(t, respA.TripID, respAIdempotent.TripID)

	// Concurrency Invariant: Driver B tries to accept same booking -> MUST BE REJECTED ("ONLY ONE WINS")
	cmdB := application.DriverCommandRequest{
		CommandID: "cmd-accept-2",
		Type:      "ACCEPT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offerB.ID},
	}
	respB, err := svc.ProcessDriverCommand(ctx, tenantID, driverB, cmdB)
	assert.Error(t, err)
	assert.False(t, respB.Success)
	assert.True(t, strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "cancelled"))

	// Trip State Progression: START_TRIP -> ARRIVE_PICKUP -> COMPLETE_LOADING -> COMPLETE_TRIP
	tripID := respA.TripID
	startCmd := application.DriverCommandRequest{
		CommandID: "cmd-start-1",
		Type:      "START_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID},
	}
	respStart, err := svc.ProcessDriverCommand(ctx, tenantID, driverA, startCmd)
	require.NoError(t, err)
	assert.True(t, respStart.Success)
	assert.Equal(t, "en_route_pickup", respStart.TripState)

	// Complete trip
	completeCmd := application.DriverCommandRequest{
		CommandID: "cmd-complete-1",
		Type:      "COMPLETE_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID},
	}
	respComplete, err := svc.ProcessDriverCommand(ctx, tenantID, driverA, completeCmd)
	require.NoError(t, err)
	assert.True(t, respComplete.Success)
	assert.Equal(t, "completed", respComplete.TripState)
}

func TestDriverAppService_OfferExpirationAndRejection(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-expire-test"
	bookingID := "booking-expire-1"

	require.NoError(t, svc.RegisterDriver(ctx, tenantID, driverID, "Expire Test", "expire@test.com", "9876543212"))

	// 1. Create offer with -1 minute TTL (already expired)
	offerExp, err := svc.CreateDispatchOffer(ctx, tenantID, bookingID, driverID, "veh-exp", -1)
	require.NoError(t, err)

	// Accept expired offer -> fails
	cmdExp := application.DriverCommandRequest{
		CommandID: "cmd-exp-1",
		Type:      "ACCEPT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offerExp.ID},
	}
	respExp, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmdExp)
	assert.Error(t, err)
	assert.False(t, respExp.Success)
	assert.Contains(t, err.Error(), "expired")

	// 2. Reject offer
	offerRej, err := svc.CreateDispatchOffer(ctx, tenantID, "booking-rej-1", driverID, "veh-rej", 15)
	require.NoError(t, err)

	cmdRej := application.DriverCommandRequest{
		CommandID: "cmd-rej-1",
		Type:      "REJECT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offerRej.ID},
	}
	respRej, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmdRej)
	require.NoError(t, err)
	assert.True(t, respRej.Success)
	assert.Equal(t, "REJECTED", respRej.Status)
}

func TestDriverAppService_FullTripStateMachineAndPODEnforcement(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-state-test"
	unauthDriverID := "drv-unauth"
	bookingID := "booking-full-flow"

	require.NoError(t, svc.RegisterDriver(ctx, tenantID, driverID, "Flow Driver", "flow@test.com", "9876543213"))
	require.NoError(t, svc.RegisterDriver(ctx, tenantID, unauthDriverID, "Unauth Driver", "unauth@test.com", "9876543214"))

	// Offer and Accept
	offer, err := svc.CreateDispatchOffer(ctx, tenantID, bookingID, driverID, "veh-flow", 15)
	require.NoError(t, err)

	cmdAccept := application.DriverCommandRequest{
		CommandID: "cmd-flow-accept",
		Type:      "ACCEPT_OFFER",
		Payload:   map[string]interface{}{"offer_id": offer.ID},
	}
	respAccept, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmdAccept)
	require.NoError(t, err)
	tripID := respAccept.TripID

	// Unauthorized driver cannot command trip
	cmdUnauth := application.DriverCommandRequest{
		CommandID: "cmd-flow-unauth",
		Type:      "START_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID},
	}
	respUnauth, err := svc.ProcessDriverCommand(ctx, tenantID, unauthDriverID, cmdUnauth)
	assert.Error(t, err)
	assert.False(t, respUnauth.Success)

	// Step-by-step state progression
	steps := []struct {
		cmdType   string
		wantState string
	}{
		{"START_TRIP", "en_route_pickup"},
		{"ARRIVE_PICKUP", "arrived_pickup"},
		{"START_LOADING", "loading"},
		{"COMPLETE_LOADING", "loaded"},
		{"START_DELIVERY", "in_transit"},
		{"ARRIVE_DELIVERY", "arrived_delivery"},
		{"START_UNLOADING", "unloading"},
	}

	for _, s := range steps {
		cmd := application.DriverCommandRequest{
			CommandID: "cmd-" + s.cmdType,
			Type:      s.cmdType,
			Payload:   map[string]interface{}{"trip_id": tripID},
		}
		resp, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmd)
		require.NoError(t, err, "step %s failed", s.cmdType)
		assert.Equal(t, s.wantState, resp.TripState)
	}

	// POD enforcement test: COMPLETE_TRIP with pod_required = true fails if no POD document is uploaded
	cmdCompleteBlocked := application.DriverCommandRequest{
		CommandID: "cmd-complete-block",
		Type:      "COMPLETE_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID, "pod_required": true},
	}
	respBlocked, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmdCompleteBlocked)
	assert.Error(t, err)
	assert.False(t, respBlocked.Success)
	assert.Contains(t, err.Error(), "POD")

	// Complete trip with POD doc ID attached
	cmdCompleteOK := application.DriverCommandRequest{
		CommandID: "cmd-complete-ok",
		Type:      "COMPLETE_TRIP",
		Payload:   map[string]interface{}{"trip_id": tripID, "pod_required": true, "pod_document_id": "doc-pod-999"},
	}
	respOK, err := svc.ProcessDriverCommand(ctx, tenantID, driverID, cmdCompleteOK)
	require.NoError(t, err)
	assert.True(t, respOK.Success)
	assert.Equal(t, "completed", respOK.TripState)
}

func TestRegisterPushToken(t *testing.T) {
	db := setupAppServiceTestDB(t)
	svc := application.NewDriverAppService(db)
	ctx := context.Background()

	tenantID := "tenant-test-1"
	_, err := db.Exec("INSERT INTO tenants (id, name, slug) VALUES (?, ?, ?)", tenantID, "Test Fleet", "test-fleet")
	require.NoError(t, err)

	driverID := "drv-push-1"
	userID := "usr-push-1"
	deviceID := "device-rmx-8i"
	pushToken := "ExponentPushToken[xxxxxxxxxxxxxx]"

	// Register token
	err = svc.RegisterPushToken(ctx, tenantID, driverID, userID, deviceID, pushToken, "android")
	require.NoError(t, err)

	// Verify persistence
	var count int
	var storedToken string
	err = db.QueryRow("SELECT count(*), push_token FROM driver_push_tokens WHERE tenant_id = ? AND driver_id = ? AND device_id = ?", tenantID, driverID, deviceID).Scan(&count, &storedToken)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, pushToken, storedToken)

	// Idempotent update on same device
	updatedToken := "ExponentPushToken[yyyyyyyyyyyyyy]"
	err = svc.RegisterPushToken(ctx, tenantID, driverID, userID, deviceID, updatedToken, "android")
	require.NoError(t, err)

	err = db.QueryRow("SELECT count(*), push_token FROM driver_push_tokens WHERE tenant_id = ? AND driver_id = ? AND device_id = ?", tenantID, driverID, deviceID).Scan(&count, &storedToken)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, updatedToken, storedToken)
}
