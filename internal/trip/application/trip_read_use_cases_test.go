package application

// Characterization tests for the trip read-model queries extracted from
// internal/handlers/trips.go (handlers-end-state ADR, Phase 2). They pin
// tenant isolation, not-found and ordering behaviour, plus the exact legacy
// value mapping (nullable fields, POD URL precedence, best-effort companion
// lookups).

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTripReadQueries_PODConsigneePhone(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	seedPhase2Trip(t, db, "trip-phone", "TRIP-PHONE", "tenant-1", "")
	_, err := db.Exec(`UPDATE trips SET pod_consignee_phone = '+91 9000000001' WHERE id = 'trip-phone'`)
	require.NoError(t, err)

	reads := NewTripReadQueries(db)
	phone, err := reads.PODConsigneePhone(context.Background(), TripPODPhoneQuery{TripID: "trip-phone", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, "+91 9000000001", phone)

	// Cross-tenant reads must not see the row: the legacy lookup was
	// tenant-scoped. The handler ignores the error and flashes "no phone".
	phone, err = reads.PODConsigneePhone(context.Background(), TripPODPhoneQuery{TripID: "trip-phone", TenantID: "tenant-2"})
	assert.Empty(t, phone)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	phone, err = reads.PODConsigneePhone(context.Background(), TripPODPhoneQuery{TripID: "trip-missing", TenantID: "tenant-1"})
	assert.Empty(t, phone)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// nil DB degrades to empty, matching the legacy h.App.DB == nil path.
	phone, err = NewTripReadQueries(nil).PODConsigneePhone(context.Background(), TripPODPhoneQuery{TripID: "trip-phone", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, phone)
}

func TestTripReadQueries_DriverPreferredVehicles(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	seedTestVehicle(t, db, "veh-1", false)
	seedTestDriver(t, db, "drv-1")
	seedTestVehicle(t, db, "veh-3", false)
	seedTestDriver(t, db, "drv-3")

	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, tenant_id)
		VALUES ('veh-2', 'REG-veh-2', 'MH-02-veh-2', 'truck', 10, 'available', 'tenant-2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-2', 'DRV-drv-2', 'Suresh', 'Rao', '9876500002', 'DL-DRV-2', date('now','+1 year'), 'available', 'tenant-2')`)
	require.NoError(t, err)

	// One active assignment per tenant plus one released (unassigned_at set,
	// excluded by the active-only predicate).
	_, err = db.Exec(`INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, unassigned_at) VALUES
		('dpv-1', 'tenant-1', 'drv-1', 'veh-1', NULL),
		('dpv-2', 'tenant-2', 'drv-2', 'veh-2', NULL),
		('dpv-3', 'tenant-1', 'drv-3', 'veh-3', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	reads := NewTripReadQueries(db)
	preferred, err := reads.DriverPreferredVehicles(context.Background(), DriverPreferredVehiclesQuery{TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"drv-1": "veh-1"}, preferred, "released assignments must stay out of the prefill map")

	preferred, err = reads.DriverPreferredVehicles(context.Background(), DriverPreferredVehiclesQuery{TenantID: "tenant-2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"drv-2": "veh-2"}, preferred)

	preferred, err = reads.DriverPreferredVehicles(context.Background(), DriverPreferredVehiclesQuery{TenantID: "tenant-unknown"})
	require.NoError(t, err)
	assert.Empty(t, preferred)

	preferred, err = NewTripReadQueries(nil).DriverPreferredVehicles(context.Background(), DriverPreferredVehiclesQuery{TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, preferred)
}

func TestTripReadQueries_TripStops(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	seedPhase2Trip(t, db, "trip-stops", "TRIP-STOPS", "tenant-1", "")
	// Inserted out of order on purpose: the read model must return the
	// sequence order the template renders (and the legacy SQL produced).
	_, err := db.Exec(`INSERT INTO trip_stops
		(id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, status,
		 actual_arrival, actual_departure, otp_required, pod_required, pod_url, consignee_name, consignee_phone)
		VALUES
		('stop-2', 'tenant-1', 'trip-stops', 2, 'drop', 'Second Stop', 'Plot 2', 'completed',
		 '2026-09-02 14:30:00', '2026-09-02 14:45:00', 0, 1, '/uploads/pod/p2.jpg', 'Amit Patel', '+91 9123456780'),
		('stop-1', 'tenant-1', 'trip-stops', 1, 'pickup', 'First Stop', 'Plot 1', 'pending',
		 NULL, NULL, 1, 0, NULL, '', '')`)
	require.NoError(t, err)

	reads := NewTripReadQueries(db)
	stops, err := reads.TripStops(context.Background(), TripStopsQuery{TripID: "trip-stops", TenantID: "tenant-1"})
	require.NoError(t, err)
	require.Len(t, stops, 2)

	assert.Equal(t, "stop-1", stops[0].ID)
	assert.Equal(t, 1, stops[0].StopSequence)
	assert.Equal(t, "pickup", stops[0].StopType)
	assert.Equal(t, "First Stop", stops[0].LocationName)
	assert.Equal(t, "pending", stops[0].Status)
	assert.True(t, stops[0].RequiresOTP)
	assert.False(t, stops[0].RequiresPOD)
	assert.Nil(t, stops[0].ActualArrival)
	assert.Nil(t, stops[0].ActualDeparture)

	assert.Equal(t, "stop-2", stops[1].ID)
	assert.Equal(t, "drop", stops[1].StopType)
	assert.True(t, stops[1].RequiresPOD)
	assert.False(t, stops[1].RequiresOTP)
	assert.Equal(t, "/uploads/pod/p2.jpg", stops[1].PODURL)
	assert.Equal(t, "Amit Patel", stops[1].ConsigneeName)
	assert.Equal(t, "+91 9123456780", stops[1].ConsigneePhone)
	require.NotNil(t, stops[1].ActualArrival)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 30, 0, 0, time.UTC), *stops[1].ActualArrival)
	require.NotNil(t, stops[1].ActualDeparture)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 45, 0, 0, time.UTC), *stops[1].ActualDeparture)

	// Tenant mismatch yields no rows, exactly as the tenant-scoped legacy SQL.
	stops, err = reads.TripStops(context.Background(), TripStopsQuery{TripID: "trip-stops", TenantID: "tenant-2"})
	require.NoError(t, err)
	assert.Empty(t, stops)

	stops, err = reads.TripStops(context.Background(), TripStopsQuery{TripID: "trip-missing", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, stops)

	stops, err = NewTripReadQueries(nil).TripStops(context.Background(), TripStopsQuery{TripID: "trip-stops", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, stops)
}

func TestTripReadQueries_AssignmentRefs(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	seedPhase2Trip(t, db, "trip-refs", "TRIP-REFS", "tenant-1", "")
	_, err := db.Exec(`UPDATE trips SET vehicle_id = 'veh-refs', driver_id = 'drv-refs' WHERE id = 'trip-refs'`)
	require.NoError(t, err)

	reads := NewTripReadQueries(db)
	refs, err := reads.AssignmentRefs(context.Background(), TripAssignmentRefsQuery{TripID: "trip-refs", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, "veh-refs", refs.VehicleID)
	assert.Equal(t, "drv-refs", refs.DriverID)

	// Cross-tenant: no row. Compliance prechecks ignore the error and run with
	// nil ids, exactly as the legacy best-effort scan did.
	refs, err = reads.AssignmentRefs(context.Background(), TripAssignmentRefsQuery{TripID: "trip-refs", TenantID: "tenant-2"})
	assert.Empty(t, refs.VehicleID)
	assert.Empty(t, refs.DriverID)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	refs, err = reads.AssignmentRefs(context.Background(), TripAssignmentRefsQuery{TripID: "trip-missing", TenantID: "tenant-1"})
	assert.Empty(t, refs.VehicleID)
	assert.Empty(t, refs.DriverID)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	refs, err = NewTripReadQueries(nil).AssignmentRefs(context.Background(), TripAssignmentRefsQuery{TripID: "trip-refs", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, refs.VehicleID)
}

func TestTripReadQueries_LastStop(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	seedPhase2Trip(t, db, "trip-last", "TRIP-LAST", "tenant-1", "")
	seedPhase2Trip(t, db, "trip-nostops", "TRIP-NOSTOPS", "tenant-1", "")
	for i, stopID := range []string{"stop-last-1", "stop-last-3", "stop-last-2"} {
		_, err := db.Exec(`INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, status)
			VALUES (?, 'tenant-1', 'trip-last', ?, 'drop', 'Stop', 'Addr', 'pending')`, stopID, i+1)
		require.NoError(t, err)
	}
	// stop-last-2 has sequence 3: the auto-located "last stop" is the highest
	// sequence, not the last inserted row.
	reads := NewTripReadQueries(db)
	stopID, err := reads.LastStop(context.Background(), LastStopQuery{TripID: "trip-last", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, "stop-last-2", stopID, "highest stop_sequence must win")

	stopID, err = reads.LastStop(context.Background(), LastStopQuery{TripID: "trip-last", TenantID: "tenant-2"})
	require.NoError(t, err)
	assert.Empty(t, stopID)

	// A trip with no stops is not an error: the handler keeps the submitted id.
	stopID, err = reads.LastStop(context.Background(), LastStopQuery{TripID: "trip-nostops", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, stopID)

	stopID, err = NewTripReadQueries(nil).LastStop(context.Background(), LastStopQuery{TripID: "trip-last", TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Empty(t, stopID)
}

func TestTripReadQueries_PublicEPODCertificate(t *testing.T) {
	db := newTripTestDB(t)
	seedPhase2Tenant(t, db, "tenant-2")
	_, err := db.Exec(`UPDATE tenants SET name = 'Avandab Logistics Network' WHERE id = 'tenant-1'`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE tenants SET name = 'Other Tenant' WHERE id = 'tenant-2'`)
	require.NoError(t, err)
	seedTestVehicle(t, db, "veh-epod", false)
	seedTestDriver(t, db, "drv-epod")
	seedPhase2Trip(t, db, "trip-epod", "TRIP-EPOD-900", "tenant-1", "veh-epod")
	_, err = db.Exec(`UPDATE trips SET status = 'delivered', driver_id = 'drv-epod',
		departure_time = '2026-09-02 10:30:00', delivered_at = '2026-09-02 14:30:00',
		pod_url = '/uploads/pod/trip_pod.jpg', pod_photo_url = '/uploads/pod/trip_photo.jpg',
		pod_signature_url = '/uploads/pod/trip_sig.png',
		pod_consignee_name = 'Trip Consignee', pod_consignee_phone = '+91 9000000000',
		pod_otp_verified = 1, pod_captured_at = '2026-09-02 14:31:00', pod_notes = 'Trip notes'
		WHERE id = 'trip-epod'`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trip_stops
		(id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, status,
		 actual_arrival, actual_departure, otp_required, otp_verified_at,
		 pod_required, pod_url, pod_signature_url, pod_verified_at, pod_notes,
		 consignee_name, consignee_phone, consignee_email)
		VALUES
		('stop-epod-2', 'tenant-1', 'trip-epod', 2, 'drop', 'Bengaluru Terminal', 'Plot 100', 'completed',
		 '2026-09-02 14:20:00', '2026-09-02 14:25:00', 1, '2026-09-02 14:21:00',
		 1, '/uploads/pod/delivery.jpg', '/uploads/pod/sign.png', '2026-09-02 14:26:00', 'Received in good condition',
		 'Amit Patel', '+91 9123456780', 'amit@example.com'),
		('stop-epod-1', 'tenant-1', 'trip-epod', 1, 'pickup', 'Mumbai Depot', 'Plot 1', 'completed',
		 '2026-09-02 10:00:00', '2026-09-02 10:10:00', 1, NULL,
		 0, NULL, NULL, NULL, NULL,
		 '', '', '')`)
	require.NoError(t, err)

	reads := NewTripReadQueries(db)
	cert, err := reads.PublicEPODCertificate(context.Background(), PublicEPODCertificateQuery{TripRef: "trip-epod"})
	require.NoError(t, err)

	assert.Equal(t, "trip-epod", cert.Trip.ID)
	assert.Equal(t, "tenant-1", cert.Trip.TenantID)
	assert.Equal(t, "TRIP-EPOD-900", cert.Trip.TripNumber)
	assert.Equal(t, "delivered", cert.Trip.Status)
	assert.Equal(t, time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC), cert.Trip.DepartureTime)
	require.NotNil(t, cert.Trip.DeliveredAt)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 30, 0, 0, time.UTC), *cert.Trip.DeliveredAt)
	assert.Equal(t, "drv-epod", cert.Trip.DriverID)
	assert.Equal(t, "veh-epod", cert.Trip.VehicleID)
	assert.Equal(t, "/uploads/pod/trip_pod.jpg", cert.Trip.PODURL)
	assert.Equal(t, "/uploads/pod/trip_photo.jpg", cert.Trip.PODPhotoURL)
	assert.Equal(t, "/uploads/pod/trip_sig.png", cert.Trip.PODSignatureURL)
	assert.Equal(t, "Trip Consignee", cert.Trip.PODConsigneeName)
	assert.Equal(t, "+91 9000000000", cert.Trip.PODConsigneePhone)
	assert.True(t, cert.Trip.PODOTPVerified)
	require.NotNil(t, cert.Trip.PODCapturedAt)
	assert.Equal(t, "Trip notes", cert.Trip.PODNotes)

	assert.Equal(t, "REG-veh-epod", cert.VehicleRegistration)
	assert.Equal(t, "Rajesh Kumar", cert.DriverName)
	assert.Equal(t, "9876543210", cert.DriverPhone)
	assert.Equal(t, "Avandab Logistics Network", cert.TenantName, "branding must come from the trip's tenant only")

	require.Len(t, cert.Stops, 2)
	assert.Equal(t, "stop-epod-1", cert.Stops[0].ID)
	assert.Equal(t, 1, cert.Stops[0].StopSequence)
	assert.True(t, cert.Stops[0].OTPRequired)
	assert.False(t, cert.Stops[0].PODRequired)
	assert.Nil(t, cert.Stops[0].OTPVerifiedAt)

	assert.Equal(t, "stop-epod-2", cert.Stops[1].ID)
	assert.Equal(t, 2, cert.Stops[1].StopSequence)
	assert.Equal(t, "Bengaluru Terminal", cert.Stops[1].LocationName)
	assert.Equal(t, "completed", cert.Stops[1].Status)
	require.NotNil(t, cert.Stops[1].ActualArrival)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 20, 0, 0, time.UTC), *cert.Stops[1].ActualArrival)
	require.NotNil(t, cert.Stops[1].OTPVerifiedAt)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 21, 0, 0, time.UTC), *cert.Stops[1].OTPVerifiedAt)
	require.NotNil(t, cert.Stops[1].PODVerifiedAt)
	assert.Equal(t, time.Date(2026, 9, 2, 14, 26, 0, 0, time.UTC), *cert.Stops[1].PODVerifiedAt)
	assert.Equal(t, "/uploads/pod/delivery.jpg", cert.Stops[1].PODURL)
	assert.Equal(t, "/uploads/pod/sign.png", cert.Stops[1].SignatureURL)
	assert.Equal(t, "Received in good condition", cert.Stops[1].Notes)
	assert.Equal(t, "Amit Patel", cert.Stops[1].ConsigneeName)
	assert.Equal(t, "+91 9123456780", cert.Stops[1].ConsigneePhone)
	assert.Equal(t, "amit@example.com", cert.Stops[1].ConsigneeEmail)

	// The public URL also accepts the trip number, exactly as the legacy SQL did.
	byNumber, err := reads.PublicEPODCertificate(context.Background(), PublicEPODCertificateQuery{TripRef: "TRIP-EPOD-900"})
	require.NoError(t, err)
	assert.Equal(t, "trip-epod", byNumber.Trip.ID)

	// Unknown reference: sentinel drives the handler's 404.
	_, err = reads.PublicEPODCertificate(context.Background(), PublicEPODCertificateQuery{TripRef: "unknown-trip"})
	assert.True(t, errors.Is(err, ErrEPODCertificateNotFound))

	// Companion vehicle/driver lookups stay best-effort: missing rows leave
	// blank fields and never fail the public certificate.
	_, err = db.Exec(`UPDATE trips SET vehicle_id = 'missing-vehicle', driver_id = 'missing-driver' WHERE id = 'trip-epod'`)
	require.NoError(t, err)
	cert, err = reads.PublicEPODCertificate(context.Background(), PublicEPODCertificateQuery{TripRef: "trip-epod"})
	require.NoError(t, err)
	assert.Empty(t, cert.VehicleRegistration)
	assert.Empty(t, cert.DriverName)
	assert.Empty(t, cert.DriverPhone)
	assert.Equal(t, "Avandab Logistics Network", cert.TenantName)

	// nil DB degrades to a zero certificate (the handler guards DB first).
	cert, err = NewTripReadQueries(nil).PublicEPODCertificate(context.Background(), PublicEPODCertificateQuery{TripRef: "trip-epod"})
	require.NoError(t, err)
	assert.Empty(t, cert.Trip.ID)
	assert.Empty(t, cert.Stops)
}
