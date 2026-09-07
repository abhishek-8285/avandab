package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/repository"
)

// DeviceStatus constants matching the CHECK constraint in migration 00040.
const (
	DeviceStatusInventory   = "inventory"
	DeviceStatusAssigned    = "assigned"
	DeviceStatusActive      = "active"
	DeviceStatusRetired     = "retired"
	DeviceStatusQuarantined = "quarantined"
)

// DeviceType constants matching the CHECK constraint in migration 00040.
const (
	DeviceTypeHardware  = "hardware"
	DeviceTypeMobileApp = "mobile_app"
	DeviceTypeOBDDongle = "obd_dongle"
)

// Device represents a row in telemetry_devices.
type Device struct {
	ID               string
	TenantID         string
	IMEI             string
	SerialNumber     *string
	FirmwareVersion  *string
	SimNumber        *string
	ICCID            *string
	WarrantyUntil    *time.Time
	DeviceType       string
	Status           string
	VehicleID        *string
	CustomerID       *string
	ActivatedAt      *time.Time
	LastSeenAt       *time.Time
	DeviceSecretHash *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DeviceStore provides raw-SQL persistence for telemetry_devices,
// device_quarantine, and provider_poll_state. All queries pick up the active
// transaction from context via repository.TxFromContext when present.
type DeviceStore struct {
	db *sql.DB
}

// NewDeviceStore constructs a DeviceStore backed by the given DB.
func NewDeviceStore(db *sql.DB) *DeviceStore {
	return &DeviceStore{db: db}
}

// dbFromContext returns the transactional DB handle if one is in the context,
// otherwise falls back to the stored *sql.DB.
func (s *DeviceStore) dbFromContext(ctx context.Context) interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.db
}

// GetByIMEI looks up a device by IMEI. Returns (nil, nil) if not found.
func (s *DeviceStore) GetByIMEI(ctx context.Context, imei string) (*Device, error) {
	db := s.dbFromContext(ctx)
	row := db.QueryRowContext(ctx,
		`SELECT id, tenant_id, imei, serial_number, firmware_version,
		        sim_number, iccid, warranty_until, device_type, status,
		        vehicle_id, customer_id, activated_at, last_seen_at,
		        device_secret_hash, created_at, updated_at
		 FROM telemetry_devices WHERE imei = $1`, imei)

	var d Device
	var serial, firmware, sim, iccid, secretHash sql.NullString
	err := row.Scan(
		&d.ID, &d.TenantID, &d.IMEI, &serial, &firmware,
		&sim, &iccid, &d.WarrantyUntil, &d.DeviceType, &d.Status,
		&d.VehicleID, &d.CustomerID, &d.ActivatedAt, &d.LastSeenAt,
		&secretHash, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("device lookup: %w", err)
	}
	if serial.Valid {
		d.SerialNumber = &serial.String
	}
	if firmware.Valid {
		d.FirmwareVersion = &firmware.String
	}
	if sim.Valid {
		d.SimNumber = &sim.String
	}
	if iccid.Valid {
		d.ICCID = &iccid.String
	}
	if secretHash.Valid {
		d.DeviceSecretHash = &secretHash.String
	}
	return &d, nil
}

// UpdateLastSeen sets last_seen_at and updated_at to now for the given IMEI.
func (s *DeviceStore) UpdateLastSeen(ctx context.Context, imei string) error {
	db := s.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE telemetry_devices SET last_seen_at = CURRENT_TIMESTAMP,
		        updated_at = CURRENT_TIMESTAMP WHERE imei = $1`, imei)
	return err
}

// GetLastOdometer returns the last known odometer for a device's vehicle.
// Returns (0, false, nil) if no position exists.
func (s *DeviceStore) GetLastOdometer(ctx context.Context, imei string) (float64, bool, error) {
	var odometer float64
	err := s.db.QueryRowContext(ctx,
		`SELECT odometer FROM telemetry_positions
		 WHERE imei = $1 AND odometer IS NOT NULL
		 ORDER BY device_time DESC LIMIT 1`, imei).Scan(&odometer)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return odometer, true, nil
}

// GetLastFuelLevel returns the last known fuel level for a device's vehicle.
// Returns (0, false, nil) if no position exists.
func (s *DeviceStore) GetLastFuelLevel(ctx context.Context, imei string) (float64, bool, error) {
	var fuelLevel float64
	err := s.db.QueryRowContext(ctx,
		`SELECT fuel_level FROM telemetry_positions
		 WHERE imei = $1 AND fuel_level IS NOT NULL
		 ORDER BY device_time DESC LIMIT 1`, imei).Scan(&fuelLevel)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return fuelLevel, true, nil
}

// GetLastPositionTime returns the device_time of the most recent position for
// a device, to support monotonic-time checks in the latest-position upsert.
func (s *DeviceStore) GetLastPositionTime(ctx context.Context, imei string) (*time.Time, error) {
	var t time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT device_time FROM telemetry_positions
		 WHERE imei = $1 ORDER BY device_time DESC LIMIT 1`, imei).Scan(&t)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetByVehicleID looks up the active device assigned to a vehicle, if any.
func (s *DeviceStore) GetByVehicleID(ctx context.Context, vehicleID string) (*Device, error) {
	db := s.dbFromContext(ctx)
	row := db.QueryRowContext(ctx,
		`SELECT id, tenant_id, imei, serial_number, firmware_version,
		        sim_number, iccid, warranty_until, device_type, status,
		        vehicle_id, customer_id, activated_at, last_seen_at,
		        device_secret_hash, created_at, updated_at
		 FROM telemetry_devices WHERE vehicle_id = $1 LIMIT 1`, vehicleID)

	var d Device
	var serial, firmware, sim, iccid, secretHash sql.NullString
	err := row.Scan(
		&d.ID, &d.TenantID, &d.IMEI, &serial, &firmware,
		&sim, &iccid, &d.WarrantyUntil, &d.DeviceType, &d.Status,
		&d.VehicleID, &d.CustomerID, &d.ActivatedAt, &d.LastSeenAt,
		&secretHash, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("device by vehicle lookup: %w", err)
	}
	if serial.Valid {
		d.SerialNumber = &serial.String
	}
	if firmware.Valid {
		d.FirmwareVersion = &firmware.String
	}
	if sim.Valid {
		d.SimNumber = &sim.String
	}
	if iccid.Valid {
		d.ICCID = &iccid.String
	}
	if secretHash.Valid {
		d.DeviceSecretHash = &secretHash.String
	}
	return &d, nil
}
