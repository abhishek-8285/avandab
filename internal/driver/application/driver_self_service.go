package application

// Driver-app self-service use-cases behind the mobile driver endpoints,
// extracted from internal/handlers/drivers.go (handlers-end-state ADR,
// Phase 2). The handler keeps HTTP parsing/validation/JSON rendering; this
// file keeps the legacy identity rules, KYC/status write semantics and the
// exact SQL. Two legacy identity scopes exist and are preserved verbatim:
// GetMe/UpdateMe are tenant-scoped (Spec 13 §2.2), while the older
// status/issue endpoints resolve the driver without a tenant filter.

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// ErrDriverNotFound reports that no driver row matched the acting user under
// the legacy identity rules (drivers.id = user id, or drivers.email equal to
// the user's email).
var ErrDriverNotFound = errors.New("driver not found")

// DriverSelfService exposes the driver-app self-service operations. It is
// stateless and cheap to construct per request.
type DriverSelfService struct {
	db *sql.DB
}

// NewDriverSelfService wires the self-service operations against the database.
func NewDriverSelfService(db *sql.DB) *DriverSelfService {
	return &DriverSelfService{db: db}
}

// ResolveActorDriverID resolves the acting user's driver row with the legacy
// tenant-unscoped identity rule used by the mobile status/issue endpoints
// (they predate tenant scoping). The behavior is preserved as-is and pinned
// by TestDriverSelfService_LegacyUnscopedIdentity.
func (s *DriverSelfService) ResolveActorDriverID(ctx context.Context, userID string) (string, error) {
	if s.db == nil {
		return "", ErrDriverNotFound
	}
	var driverID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM drivers
		WHERE id = $1 OR email = (SELECT email FROM users WHERE id = $2)
		LIMIT 1`, userID, userID).Scan(&driverID)
	if err != nil {
		return "", ErrDriverNotFound
	}
	return driverID, nil
}

// resolveTenantDriverID resolves the acting user's driver row scoped to the
// acting tenant (Spec 13 §2.2): a cross-tenant user id/email match must never
// resolve.
func (s *DriverSelfService) resolveTenantDriverID(ctx context.Context, userID string, tenantID shared.TenantID) (string, error) {
	if s.db == nil {
		return "", ErrDriverNotFound
	}
	var driverID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM drivers
		WHERE (id = $1 OR email = (SELECT email FROM users WHERE id = $2))
		  AND tenant_id = $3
		LIMIT 1`, userID, userID, string(tenantID)).Scan(&driverID)
	if err != nil {
		return "", ErrDriverNotFound
	}
	return driverID, nil
}

// DriverSelfProfile is the drivers row rendered by GET /api/v1/drivers/me.
type DriverSelfProfile struct {
	ID            string
	DriverID      string
	FirstName     string
	LastName      string
	Phone         string
	Status        string
	LicenseNumber string
	LicenseExpiry string
	BankDetails   string
	Aadhaar       string
	Pan           string
	Notes         string
}

// DriverLocation is a last-known GPS position.
type DriverLocation struct {
	Latitude  float64
	Longitude float64
}

// DriverMe is the read model behind GET /api/v1/drivers/me.
type DriverMe struct {
	DriverCode      string
	Profile         DriverSelfProfile
	VehiclePlate    string
	CurrentLocation *DriverLocation
}

// GetMe loads the authenticated driver profile plus the active-trip vehicle
// plate and last known location. Identity is tenant-scoped; the companion
// trip/location lookups keep their legacy best-effort semantics (errors leave
// the value empty).
func (s *DriverSelfService) GetMe(ctx context.Context, userID string, tenantID shared.TenantID) (DriverMe, error) {
	var me DriverMe
	if s.db == nil {
		return me, ErrDriverNotFound
	}
	profile := &me.Profile
	err := s.db.QueryRowContext(ctx, `
		SELECT id, driver_id, first_name, last_name, phone, status,
		       COALESCE(license_number, ''), COALESCE(CAST(license_expiry AS TEXT), ''),
		       COALESCE(bank_details, ''), COALESCE(aadhaar, ''), COALESCE(pan, ''),
		       COALESCE(notes, '')
		FROM drivers
		WHERE (id = $1 OR email = (SELECT email FROM users WHERE id = $2))
		  AND tenant_id = $3
		LIMIT 1
	`, userID, userID, string(tenantID)).Scan(
		&profile.ID, &profile.DriverID, &profile.FirstName, &profile.LastName, &profile.Phone, &profile.Status,
		&profile.LicenseNumber, &profile.LicenseExpiry, &profile.BankDetails, &profile.Aadhaar, &profile.Pan,
		&profile.Notes,
	)
	if err != nil {
		return me, ErrDriverNotFound
	}
	// Identity rule: driver_code is the display driver_id, falling back to the
	// row id when the display code is empty.
	me.DriverCode = profile.DriverID
	if me.DriverCode == "" {
		me.DriverCode = profile.ID
	}

	// Active trip + vehicle plate; legacy query is best-effort.
	var vehicleID string
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(v.registration_number, ''), COALESCE(t.vehicle_id, '')
		FROM trips t
		LEFT JOIN vehicles v ON t.vehicle_id = v.id
		WHERE (t.driver_id = $1 OR t.driver_id = $2 OR t.driver_id = $3)
		  AND t.status IN ('assigned', 'started', 'reached_pickup', 'in_transit')
		  AND t.tenant_id = $4
		ORDER BY t.departure_time DESC, t.created_at DESC
		LIMIT 1
	`, profile.ID, profile.DriverID, userID, string(tenantID)).Scan(&me.VehiclePlate, &vehicleID)

	// No notes-to-plate fallback on purpose: the drivers.notes vehicle
	// convention was removed pipeline-wide (the onboarding vehicle_binding
	// step owns the binding), so an empty plate simply renders as unassigned.

	if vehicleID != "" {
		var lat, lon float64
		if errLoc := s.db.QueryRowContext(ctx, `
			SELECT latitude, longitude
			FROM vehicle_latest_position
			WHERE vehicle_id = $1
		`, vehicleID).Scan(&lat, &lon); errLoc == nil {
			me.CurrentLocation = &DriverLocation{Latitude: lat, Longitude: lon}
		}
	}
	if me.CurrentLocation == nil {
		var lat, lon float64
		if errLoc := s.db.QueryRowContext(ctx, `
			SELECT latitude, longitude
			FROM telemetry_snapshots
			WHERE (vehicle_id = $1 AND vehicle_id != '') OR driver_id = $2 OR driver_id = $3
			ORDER BY timestamp DESC
			LIMIT 1
		`, vehicleID, profile.ID, profile.DriverID).Scan(&lat, &lon); errLoc == nil {
			me.CurrentLocation = &DriverLocation{Latitude: lat, Longitude: lon}
		}
	}
	return me, nil
}

// UpdateMeCommand is the driver-app self-service update payload.
type UpdateMeCommand struct {
	UserID        string
	TenantID      shared.TenantID
	LicenseNumber string
	LicenseExpiry string
	BankDetails   string
	Aadhaar       string
	Pan           string
	Status        string
	VehicleNumber string
	VehiclePlate  string
}

// UpdateMeResult reports the driver row and normalised vehicle number.
type UpdateMeResult struct {
	DriverID     string
	VehiclePlate string
}

// UpdateMe updates license/bank/KYC fields and the duty status for the acting
// driver, preserving the legacy semantics exactly:
//   - tenant-scoped identity (a cross-tenant match does not resolve),
//   - empty request values keep the stored column value,
//   - status only moves when it is one of available/leave/inactive; any other
//     value leaves the stored status untouched,
//   - status inactive stamps deactivated_at once and reactivation clears it
//     (migration 00185 KYC post-tenure clock); kyc_purged_at is never written
//     here (the retention worker owns the purge),
//   - license_number/license_expiry are written verbatim without an expiry
//     guard, as before,
//   - the vehicle upsert, telemetry-device link and notes mirror are
//     best-effort (errors are ignored).
func (s *DriverSelfService) UpdateMe(ctx context.Context, cmd UpdateMeCommand) (UpdateMeResult, error) {
	result := UpdateMeResult{}
	if s.db == nil {
		return result, ErrDriverNotFound
	}
	driverID, err := s.resolveTenantDriverID(ctx, cmd.UserID, cmd.TenantID)
	if err != nil {
		return result, err
	}
	tenantID := string(cmd.TenantID)

	vehicleNumber := strings.ToUpper(strings.TrimSpace(cmd.VehicleNumber))
	if vehicleNumber == "" {
		vehicleNumber = strings.ToUpper(strings.TrimSpace(cmd.VehiclePlate))
	}
	if vehicleNumber != "" {
		var existingVehicleID string
		errV := s.db.QueryRowContext(ctx,
			`SELECT id FROM vehicles WHERE registration_number = $1 AND tenant_id = $2`,
			vehicleNumber, tenantID).Scan(&existingVehicleID)
		if errV != nil {
			existingVehicleID = uuid.New().String()
			_, _ = s.db.ExecContext(ctx, `
				INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
				VALUES ($1, $2, $3, 'truck', 5000, 'diesel', NULL, NULL, NULL, 'available', $4)`,
				existingVehicleID, vehicleNumber, vehicleNumber, tenantID)
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE drivers SET notes = $1 WHERE id = $2 AND tenant_id = $3`, vehicleNumber, driverID, tenantID)

		// Link telemetry synthetic device to this vehicle.
		_, _ = s.db.ExecContext(ctx, `
			UPDATE telemetry_devices
			SET vehicle_id = $1, status = 'active', updated_at = CURRENT_TIMESTAMP
			WHERE (imei = $2 OR imei = $3) AND tenant_id = $4`,
			existingVehicleID, driverID, cmd.UserID, tenantID)
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE drivers
		SET updated_at = CURRENT_TIMESTAMP,
		    license_number = CASE WHEN $1 != '' THEN $2 ELSE license_number END,
		    license_expiry = CASE WHEN $3 != '' THEN $4 ELSE license_expiry END,
		    bank_details = CASE WHEN $5 != '' THEN $6 ELSE bank_details END,
		    aadhaar = CASE WHEN $7 != '' THEN $8 ELSE aadhaar END,
		    pan = CASE WHEN $9 != '' THEN $10 ELSE pan END,
		    status = CASE WHEN $11 IN ('available', 'leave', 'inactive') THEN $12 ELSE status END,
		    deactivated_at = CASE
		        WHEN $11 IN ('available', 'leave', 'inactive') AND $12 = 'inactive' THEN COALESCE(deactivated_at, CURRENT_TIMESTAMP)
		        WHEN $11 IN ('available', 'leave', 'inactive') AND $12 IN ('available', 'leave') THEN NULL
		        ELSE deactivated_at END
		WHERE id = $13 AND tenant_id = $14`,
		cmd.LicenseNumber, cmd.LicenseNumber,
		cmd.LicenseExpiry, cmd.LicenseExpiry,
		cmd.BankDetails, cmd.BankDetails,
		cmd.Aadhaar, cmd.Aadhaar,
		cmd.Pan, cmd.Pan,
		cmd.Status, cmd.Status,
		driverID, tenantID)
	if err != nil {
		return result, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return result, ErrDriverNotFound
	}
	result.DriverID = driverID
	result.VehiclePlate = vehicleNumber
	return result, nil
}
