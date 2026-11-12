package application

// Read-model queries behind the trip pages, extracted from
// internal/handlers/trips.go (handlers-end-state ADR, Phase 2). The exact
// legacy SQL, tenant scoping and best-effort error semantics are preserved;
// handlers keep HTTP, formatting, ETA and rendering concerns.

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/shared"
)

// ErrEPODCertificateNotFound reports that no trip matched the public e-POD
// certificate reference (trip id or trip number).
var ErrEPODCertificateNotFound = errors.New("epod certificate not found")

// TripReadQueries is the read-model facade for the trip pages. It is stateless
// and cheap to construct per request.
type TripReadQueries struct {
	db *sql.DB
}

// NewTripReadQueries wires the read model against the database. A nil db
// degrades to empty results, matching the legacy handlers' nil-DB rendering.
func NewTripReadQueries(db *sql.DB) *TripReadQueries {
	return &TripReadQueries{db: db}
}

// TripPODPhoneQuery identifies the trip whose POD consignee phone is needed.
type TripPODPhoneQuery struct {
	TripID   string
	TenantID shared.TenantID
}

// PODConsigneePhone returns the consignee phone captured on the trip's POD
// (empty when unset). Legacy semantics: NULL or a lookup error yields "".
func (q *TripReadQueries) PODConsigneePhone(ctx context.Context, query TripPODPhoneQuery) (string, error) {
	if q.db == nil {
		return "", nil
	}
	var phone sql.NullString
	if err := q.db.QueryRowContext(ctx,
		`SELECT COALESCE(pod_consignee_phone,'') FROM trips WHERE id = $1 AND tenant_id = $2`,
		query.TripID, string(query.TenantID)).Scan(&phone); err != nil {
		return "", err
	}
	return phone.String, nil
}

// DriverPreferredVehiclesQuery selects the tenant whose active driver-vehicle
// master assignments are needed.
type DriverPreferredVehiclesQuery struct {
	TenantID shared.TenantID
}

// DriverPreferredVehicles returns the map of driver_id to vehicle_id active
// preferred assignments (unassigned_at IS NULL) for the tenant. Legacy
// semantics: row scan failures were skipped and a query error left the map
// empty; callers ignore the error and render whatever was collected.
func (q *TripReadQueries) DriverPreferredVehicles(ctx context.Context, query DriverPreferredVehiclesQuery) (map[string]string, error) {
	preferred := map[string]string{}
	if q.db == nil {
		return preferred, nil
	}
	rows, err := q.db.QueryContext(ctx,
		`SELECT driver_id, vehicle_id FROM driver_preferred_vehicles WHERE tenant_id = $1 AND unassigned_at IS NULL`,
		string(query.TenantID))
	if err != nil {
		return preferred, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var driverID, vehicleID string
		if rows.Scan(&driverID, &vehicleID) == nil {
			preferred[driverID] = vehicleID
		}
	}
	return preferred, nil
}

// TripStopsQuery identifies the trip whose stop list is needed.
type TripStopsQuery struct {
	TripID   string
	TenantID shared.TenantID
}

// TripStopRecord is one tenant-scoped trip_stops row for the trip detail page.
type TripStopRecord struct {
	ID              string
	StopSequence    int
	StopType        string
	LocationName    string
	Address         string
	Status          string
	ActualArrival   *time.Time
	ActualDeparture *time.Time
	RequiresPOD     bool
	PODURL          string
	RequiresOTP     bool
	ConsigneeName   string
	ConsigneePhone  string
}

// TripStops returns the trip's stops ordered by stop_sequence. Timestamps use
// the portable text form (YYYY-MM-DD HH:MM:SS) on both engines, exactly as the
// legacy handler read them; unparseable values stay nil.
func (q *TripReadQueries) TripStops(ctx context.Context, query TripStopsQuery) ([]TripStopRecord, error) {
	if q.db == nil {
		return nil, nil
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), COALESCE(address, ''),
		       status, substr(CAST(actual_arrival AS TEXT), 1, 19), substr(CAST(actual_departure AS TEXT), 1, 19),
		       COALESCE(pod_required, 0), COALESCE(pod_url, ''),
		       COALESCE(otp_required, 0), COALESCE(consignee_name, ''), COALESCE(consignee_phone, '')
		FROM trip_stops
		WHERE trip_id = $1 AND tenant_id = $2
		ORDER BY stop_sequence ASC
	`, query.TripID, string(query.TenantID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var stops []TripStopRecord
	for rows.Next() {
		var stop TripStopRecord
		var arrivalText, departureText sql.NullString
		var podRequired, otpRequired int
		if err := rows.Scan(&stop.ID, &stop.StopSequence, &stop.StopType, &stop.LocationName, &stop.Address,
			&stop.Status, &arrivalText, &departureText, &podRequired, &stop.PODURL,
			&otpRequired, &stop.ConsigneeName, &stop.ConsigneePhone); err != nil {
			continue
		}
		stop.RequiresPOD = podRequired == 1
		stop.RequiresOTP = otpRequired == 1
		// Portable datetime text (YYYY-MM-DD HH:MM:SS) on both engines.
		if arrivalText.Valid && arrivalText.String != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", arrivalText.String); err == nil {
				stop.ActualArrival = &parsed
			}
		}
		if departureText.Valid && departureText.String != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", departureText.String); err == nil {
				stop.ActualDeparture = &parsed
			}
		}
		stops = append(stops, stop)
	}
	return stops, nil
}

// TripAssignmentRefsQuery identifies the trip whose current assignment is
// needed for dispatch-compliance prechecks.
type TripAssignmentRefsQuery struct {
	TripID   string
	TenantID shared.TenantID
}

// TripAssignmentRefs is the trip's current driver/vehicle assignment. Both are
// empty when unassigned (legacy NULL semantics).
type TripAssignmentRefs struct {
	DriverID  string
	VehicleID string
}

// AssignmentRefs returns the trip's current driver and vehicle ids, scoped to
// the tenant. The compliance prechecks ignore the error exactly as before.
func (q *TripReadQueries) AssignmentRefs(ctx context.Context, query TripAssignmentRefsQuery) (TripAssignmentRefs, error) {
	var refs TripAssignmentRefs
	if q.db == nil {
		return refs, nil
	}
	var driverID, vehicleID sql.NullString
	if err := q.db.QueryRowContext(ctx,
		`SELECT vehicle_id, driver_id FROM trips WHERE id = $1 AND tenant_id = $2`,
		query.TripID, string(query.TenantID)).Scan(&vehicleID, &driverID); err != nil {
		return refs, err
	}
	refs.DriverID = driverID.String
	refs.VehicleID = vehicleID.String
	return refs, nil
}

// LastStopQuery identifies the trip whose last stop should be auto-located.
type LastStopQuery struct {
	TripID   string
	TenantID shared.TenantID
}

// LastStop returns the id of the trip's highest-sequence stop. Legacy
// semantics: no stop (or a lookup error) yields "" and callers keep the
// submitted stop id; ErrNoRows is therefore not an error here.
func (q *TripReadQueries) LastStop(ctx context.Context, query LastStopQuery) (string, error) {
	if q.db == nil {
		return "", nil
	}
	var stopID string
	if err := q.db.QueryRowContext(ctx,
		`SELECT id FROM trip_stops WHERE trip_id = $1 AND tenant_id = $2 ORDER BY stop_sequence DESC LIMIT 1`,
		query.TripID, string(query.TenantID)).Scan(&stopID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return stopID, nil
}

// PublicEPODCertificateQuery is the public certificate reference: the trip id
// or the trip number, as carried by the login-free /epod/ URL.
type PublicEPODCertificateQuery struct {
	TripRef string
}

// EPODCertificateTrip is the trip header read model for the public e-POD
// certificate (same columns the legacy SQL selected).
type EPODCertificateTrip struct {
	ID                string
	TenantID          string
	TripNumber        string
	Status            string
	DepartureTime     time.Time
	ArrivalTime       *time.Time
	StartedAt         *time.Time
	ReachedPickupAt   *time.Time
	InTransitAt       *time.Time
	DeliveredAt       *time.Time
	CompletedAt       *time.Time
	DriverID          string
	VehicleID         string
	PODURL            string
	PODPhotoURL       string
	PODSignatureURL   string
	PODConsigneeName  string
	PODConsigneePhone string
	PODOTPVerified    bool
	PODCapturedAt     *time.Time
	PODNotes          string
}

// EPODCertificateStop is one trip_stops row for the public certificate.
type EPODCertificateStop struct {
	ID              string
	StopSequence    int
	StopType        string
	LocationName    string
	Address         string
	Status          string
	ActualArrival   *time.Time
	ActualDeparture *time.Time
	OTPRequired     bool
	OTPVerifiedAt   *time.Time
	PODRequired     bool
	PODURL          string
	SignatureURL    string
	PODVerifiedAt   *time.Time
	Notes           string
	ConsigneeName   string
	ConsigneePhone  string
	ConsigneeEmail  string
}

// EPODCertificate is the complete public certificate read model: the trip, its
// tenant-safe branding name, vehicle/driver display data and the stop list.
type EPODCertificate struct {
	Trip                EPODCertificateTrip
	VehicleRegistration string
	DriverName          string
	DriverPhone         string
	TenantName          string
	Stops               []EPODCertificateStop
}

// PublicEPODCertificate loads the public e-POD certificate data. The trip
// lookup is by id or trip number and (like the legacy public page) not
// tenant-filtered; tenant scoping is derived from the trip row. Companion
// vehicle/driver/tenant lookups remain best-effort (errors leave blank
// fields), and a stop-list error leaves the stop list empty - exactly the
// legacy behaviour. Only a missing trip or a failed trip query is returned.
func (q *TripReadQueries) PublicEPODCertificate(ctx context.Context, query PublicEPODCertificateQuery) (EPODCertificate, error) {
	var cert EPODCertificate
	if q.db == nil {
		return cert, nil
	}
	var (
		resolvedTripID, tenantID, tripNumber, status string
		departureTime                                time.Time
		arrivalTime                                  sql.NullTime
		startedAt, reachedPickupAt                   sql.NullTime
		inTransitAt, deliveredAt                     sql.NullTime
		completedAt                                  sql.NullTime
		driverID, vehicleID                          sql.NullString
		podURL, podPhotoURL                          sql.NullString
		podSigURL, podConsName                       sql.NullString
		podConsPhone                                 sql.NullString
		podOTPVerified                               int
		podCapturedAt                                sql.NullTime
		podNotes                                     sql.NullString
	)

	err := q.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, trip_number, status, departure_time, arrival_time,
		       started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at,
		       COALESCE(driver_id, ''), COALESCE(vehicle_id, ''),
		       COALESCE(pod_url, ''), COALESCE(pod_photo_url, ''), COALESCE(pod_signature_url, ''),
		       COALESCE(pod_consignee_name, ''), COALESCE(pod_consignee_phone, ''),
		       COALESCE(pod_otp_verified, 0), pod_captured_at, COALESCE(pod_notes, '')
		FROM trips
		WHERE id = $1 OR trip_number = $2
		LIMIT 1
	`, query.TripRef, query.TripRef).Scan(
		&resolvedTripID, &tenantID, &tripNumber, &status, &departureTime, &arrivalTime,
		&startedAt, &reachedPickupAt, &inTransitAt, &deliveredAt, &completedAt,
		&driverID, &vehicleID,
		&podURL, &podPhotoURL, &podSigURL,
		&podConsName, &podConsPhone,
		&podOTPVerified, &podCapturedAt, &podNotes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cert, ErrEPODCertificateNotFound
		}
		return cert, err
	}

	cert.Trip = EPODCertificateTrip{
		ID:                resolvedTripID,
		TenantID:          tenantID,
		TripNumber:        tripNumber,
		Status:            status,
		DepartureTime:     departureTime,
		ArrivalTime:       nullTimePtr(arrivalTime),
		StartedAt:         nullTimePtr(startedAt),
		ReachedPickupAt:   nullTimePtr(reachedPickupAt),
		InTransitAt:       nullTimePtr(inTransitAt),
		DeliveredAt:       nullTimePtr(deliveredAt),
		CompletedAt:       nullTimePtr(completedAt),
		DriverID:          driverID.String,
		VehicleID:         vehicleID.String,
		PODURL:            podURL.String,
		PODPhotoURL:       podPhotoURL.String,
		PODSignatureURL:   podSigURL.String,
		PODConsigneeName:  podConsName.String,
		PODConsigneePhone: podConsPhone.String,
		PODOTPVerified:    podOTPVerified == 1,
		PODCapturedAt:     nullTimePtr(podCapturedAt),
		PODNotes:          podNotes.String,
	}

	// Lookup vehicle (best-effort, as legacy).
	if vehicleID.Valid && vehicleID.String != "" {
		_ = q.db.QueryRowContext(ctx, `
			SELECT COALESCE(registration_number, vehicle_number, '') FROM vehicles WHERE id = $1
		`, vehicleID.String).Scan(&cert.VehicleRegistration)
	}

	// Lookup driver (best-effort, as legacy).
	if driverID.Valid && driverID.String != "" {
		_ = q.db.QueryRowContext(ctx, `
			SELECT COALESCE(first_name || ' ' || last_name, ''), COALESCE(phone, '')
			FROM drivers WHERE id = $1 OR driver_id = $2
		`, driverID.String, driverID.String).Scan(&cert.DriverName, &cert.DriverPhone)
	}

	// Tenant branding: the trip's own tenant only (company_settings is global
	// and would leak another tenant's branding onto a public page). Unknown
	// tenants keep the caller's neutral default.
	if tenantID != "" {
		var tenantName sql.NullString
		if err := q.db.QueryRowContext(ctx, `SELECT COALESCE(name, '') FROM tenants WHERE id = $1`, tenantID).Scan(&tenantName); err == nil && tenantName.Valid && tenantName.String != "" {
			cert.TenantName = tenantName.String
		}
	}

	// Stop list: a query error leaves it empty (legacy behaviour).
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), COALESCE(address, ''),
		       status, actual_arrival, actual_departure,
		       COALESCE(otp_required, 0), otp_verified_at,
		       COALESCE(pod_required, 0), COALESCE(pod_url, ''), COALESCE(pod_signature_url, ''),
		       pod_verified_at, COALESCE(pod_notes, ''),
		       COALESCE(consignee_name, ''), COALESCE(consignee_phone, ''), COALESCE(consignee_email, '')
		FROM trip_stops
		WHERE trip_id = $1
		ORDER BY stop_sequence ASC
	`, resolvedTripID)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var stop EPODCertificateStop
			var arrival, departure sql.NullTime
			var otpVerifiedAt, podVerifiedAt sql.NullTime
			var otpRequired, podRequired int
			if err := rows.Scan(
				&stop.ID, &stop.StopSequence, &stop.StopType, &stop.LocationName, &stop.Address,
				&stop.Status, &arrival, &departure,
				&otpRequired, &otpVerifiedAt,
				&podRequired, &stop.PODURL, &stop.SignatureURL,
				&podVerifiedAt, &stop.Notes,
				&stop.ConsigneeName, &stop.ConsigneePhone, &stop.ConsigneeEmail,
			); err != nil {
				continue
			}
			stop.OTPRequired = otpRequired == 1
			stop.PODRequired = podRequired == 1
			stop.ActualArrival = nullTimePtr(arrival)
			stop.ActualDeparture = nullTimePtr(departure)
			stop.OTPVerifiedAt = nullTimePtr(otpVerifiedAt)
			stop.PODVerifiedAt = nullTimePtr(podVerifiedAt)
			cert.Stops = append(cert.Stops, stop)
		}
	}
	return cert, nil
}

// nullTimePtr converts a nullable SQL timestamp into a typed pointer so the
// read model never leaks database/sql types into the handler.
func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := value.Time
	return &parsed
}
