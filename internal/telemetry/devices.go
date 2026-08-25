package telemetry

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// Sentinel errors for the device lifecycle state machine (Spec 01 §3, §4.1).
var (
	ErrDuplicateDevice   = errors.New("device already exists")
	ErrDeviceNotFound    = errors.New("device not found")
	ErrInvalidTransition = errors.New("invalid device state transition")
	ErrVehicleAssigned   = errors.New("vehicle already has a device assigned")
	ErrBatchTooLarge     = errors.New("bulk registration batch exceeds 500 rows")
	ErrInvalidAction     = errors.New("invalid quarantine resolution action")
)

// RegisterDeviceCommand is the intent to register a new device.
type RegisterDeviceCommand struct {
	IMEI            string
	SerialNumber    *string
	DeviceType      string
	FirmwareVersion *string
	SimNumber       *string
	ICCID           *string
	WarrantyUntil   *time.Time
	VehicleID       *string
	CustomerID      *string
}

// BulkRegisterResult reports the outcome for a single row in a bulk batch.
type BulkRegisterResult struct {
	IMEI     string
	Success  bool
	DeviceID string
	Error    string
}

// ActivateResult carries the device plus the raw secret generated on activation.
// The raw secret is returned exactly once so an admin can flash it to the
// hardware; it is never persisted in plaintext (Spec 01 §4.1).
type ActivateResult struct {
	Device    *Device
	RawSecret string
}

// ResolveQuarantineCommand is the intent to resolve an open quarantine entry.
type ResolveQuarantineCommand struct {
	EntryID   string // device_quarantine.id
	Action    string // register_new | assign_existing | reject
	VehicleID *string
	UserID    string // acting admin (resolved_by)
}

// ── DeviceStore write methods ──────────────────────────────────────────

// InsertDevice persists a new device row in `inventory` state. It does NOT
// provision the device secret; that happens during ActivateDevice (§4.1).
func (s *DeviceStore) InsertDevice(ctx context.Context, d Device) error {
	db := s.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO telemetry_devices
            (id, tenant_id, imei, serial_number, firmware_version, sim_number, iccid,
             warranty_until, device_type, status, vehicle_id, customer_id, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		d.ID, d.TenantID, d.IMEI, d.SerialNumber, d.FirmwareVersion, d.SimNumber, d.ICCID,
		d.WarrantyUntil, d.DeviceType, d.Status, d.VehicleID, d.CustomerID)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrDuplicateDevice
		}
		return fmt.Errorf("insert device: %w", err)
	}
	return nil
}

// CountByIMEI reports how many devices exist for the IMEI in the tenant.
func (s *DeviceStore) CountByIMEI(ctx context.Context, tenantID, imei string) (int, error) {
	db := s.dbFromContext(ctx)
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM telemetry_devices WHERE tenant_id = ? AND imei = ?`,
		tenantID, imei).Scan(&n)
	return n, err
}

// updateStatus transitions a device from one status to another, scoped to the
// tenant. Returns ErrDeviceNotFound when no row matches the IMEI+status.
func (s *DeviceStore) updateStatus(ctx context.Context, tenantID, imei, from, to string) error {
	db := s.dbFromContext(ctx)
	res, err := db.ExecContext(ctx,
		`UPDATE telemetry_devices
         SET status = ?, updated_at = CURRENT_TIMESTAMP
         WHERE imei = ? AND tenant_id = ? AND status = ?`,
		to, imei, tenantID, from)
	if err != nil {
		return fmt.Errorf("status update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// SetVehicle binds a device to a vehicle and transitions it to `assigned`.
// The partial UNIQUE(vehicle_id) index enforces one device per vehicle.
func (s *DeviceStore) SetVehicle(ctx context.Context, tenantID, imei, vehicleID string) error {
	db := s.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE telemetry_devices
         SET vehicle_id = ?, status = ?, updated_at = CURRENT_TIMESTAMP
         WHERE imei = ? AND tenant_id = ?`,
		vehicleID, DeviceStatusAssigned, imei, tenantID)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrVehicleAssigned
		}
		return fmt.Errorf("assign vehicle: %w", err)
	}
	return nil
}

// SetDeviceSecret stores the device_secret_hash and activates the device.
func (s *DeviceStore) SetDeviceSecret(ctx context.Context, tenantID, imei, secretHash string, activatedAt time.Time) error {
	db := s.dbFromContext(ctx)
	res, err := db.ExecContext(ctx,
		`UPDATE telemetry_devices
         SET device_secret_hash = ?, status = ?, activated_at = COALESCE(activated_at, ?), updated_at = CURRENT_TIMESTAMP
         WHERE imei = ? AND tenant_id = ? AND status = ?`,
		secretHash, DeviceStatusActive, activatedAt, imei, tenantID, DeviceStatusAssigned)
	if err != nil {
		return fmt.Errorf("set device secret: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// ListByTenant returns devices for a tenant with pagination.
func (s *DeviceStore) ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, imei, serial_number, firmware_version,
		        sim_number, iccid, warranty_until, device_type, status,
		        vehicle_id, customer_id, activated_at, last_seen_at,
		        device_secret_hash, created_at, updated_at
		 FROM telemetry_devices
		 WHERE tenant_id = ?
		 ORDER BY last_seen_at IS NULL, last_seen_at DESC, imei
		 LIMIT ? OFFSET ?`,
		tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDevices(rows)
}

// CountByTenant returns the total device count for a tenant.
func (s *DeviceStore) CountByTenant(ctx context.Context, tenantID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM telemetry_devices WHERE tenant_id = ?`, tenantID).Scan(&n)
	return n, err
}

// deviceDateClause filters on created_at using date(substr(...)) because
// SQLite stores timestamps as text in mixed formats (RFC3339 from Go,
// 'YYYY-MM-DD HH:MM:SS' from CURRENT_TIMESTAMP) — only the prefix is stable.
const deviceDateClause = `
		 AND (? = '' OR date(substr(created_at,1,10)) >= date(?))
		 AND (? = '' OR date(substr(created_at,1,10)) <= date(?))`

// ListByTenantFiltered returns devices for a tenant filtered by free-text
// query (imei/serial/vehicle), status and a created_at window, with
// pagination. Used by the devices list page filter bar.
func (s *DeviceStore) ListByTenantFiltered(ctx context.Context, tenantID string, query, status, from, to string, limit, offset int) ([]Device, error) {
	qPattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, imei, serial_number, firmware_version,
		        sim_number, iccid, warranty_until, device_type, status,
		        vehicle_id, customer_id, activated_at, last_seen_at,
		        device_secret_hash, created_at, updated_at
		 FROM telemetry_devices
		 WHERE tenant_id = ?
		   AND (? = '' OR imei LIKE ? OR serial_number LIKE ? OR vehicle_id LIKE ?)
		   AND (? = '' OR status = ?)`+deviceDateClause+`
		 ORDER BY last_seen_at IS NULL, last_seen_at DESC, imei
		 LIMIT ? OFFSET ?`,
		tenantID,
		query, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to,
		limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list devices filtered: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanDevices(rows)
}

// CountByTenantFiltered counts devices matching the same filters as
// ListByTenantFiltered.
func (s *DeviceStore) CountByTenantFiltered(ctx context.Context, tenantID string, query, status, from, to string) (int64, error) {
	qPattern := "%" + query + "%"
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM telemetry_devices
		 WHERE tenant_id = ?
		   AND (? = '' OR imei LIKE ? OR serial_number LIKE ? OR vehicle_id LIKE ?)
		   AND (? = '' OR status = ?)`+deviceDateClause,
		tenantID,
		query, qPattern, qPattern, qPattern,
		status, status,
		from, from, to, to).Scan(&n)
	return n, err
}

// scanDevices maps rows into Device values, handling nullable columns safely.
func scanDevices(rows *sql.Rows) ([]Device, error) {
	var out []Device
	for rows.Next() {
		var d Device
		var serial, firmware, sim, iccid, vID, custID, secretHash sql.NullString
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.IMEI, &serial, &firmware,
			&sim, &iccid, &d.WarrantyUntil, &d.DeviceType, &d.Status,
			&vID, &custID, &d.ActivatedAt, &d.LastSeenAt,
			&secretHash, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
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
		if vID.Valid {
			d.VehicleID = &vID.String
		}
		if custID.Valid {
			d.CustomerID = &custID.String
		}
		if secretHash.Valid {
			d.DeviceSecretHash = &secretHash.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetByID returns a quarantine entry by primary key.
func (s *QuarantineStore) GetByID(ctx context.Context, id string) (*QuarantineEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, imei, source, raw_payload, reason, status,
		        resolved_by, resolved_at, created_at
		 FROM device_quarantine WHERE id = ?`, id)
	var e QuarantineEntry
	if err := row.Scan(&e.ID, &e.TenantID, &e.IMEI, &e.Source,
		&e.RawPayload, &e.Reason, &e.Status, &e.ResolvedBy, &e.ResolvedAt, &e.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("quarantine lookup: %w", err)
	}
	return &e, nil
}

// isUniqueConstraint reports whether err originates from a SQLite UNIQUE
// constraint violation (driver-agnostic string match).
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") && (strings.Contains(msg, "constraint") || strings.Contains(msg, "already"))
}

// ── DeviceService ─────────────────────────────────────────────────────

// DeviceService owns the device lifecycle state machine (Spec 01 §3, §4, §7).
type DeviceService struct {
	store      *DeviceStore
	quarantine *QuarantineStore
	uow        ports.UnitOfWork
	pepper     string // TELEMETRY_DEVICE_SECRET_PEPPER
	idGen      ports.IDGenerator
	audit      AuditLogger
}

// NewDeviceService constructs a DeviceService.
func NewDeviceService(store *DeviceStore, quarantine *QuarantineStore, uow ports.UnitOfWork,
	pepper string, idGen ports.IDGenerator, audit AuditLogger) *DeviceService {
	if audit == nil {
		audit = noopAudit{}
	}
	return &DeviceService{
		store:      store,
		quarantine: quarantine,
		uow:        uow,
		pepper:     pepper,
		idGen:      idGen,
		audit:      audit,
	}
}

// noopAudit is a no-op AuditLogger for callers that don't need auditing.
type noopAudit struct{}

func (noopAudit) LogAction(ctx context.Context, action, tableName, recordID string, oldValues, newValues map[string]interface{}) error {
	return nil
}

func tenantFromContext(ctx context.Context) (string, error) {
	t := shared.TenantIDFromContext(ctx)
	if t == "" {
		return "", errors.New("tenant not set in context")
	}
	return string(t), nil
}

// RegisterDevice inserts a device in `inventory` state (Spec 01 §3).
func (s *DeviceService) RegisterDevice(ctx context.Context, cmd RegisterDeviceCommand) (string, error) {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return "", err
	}
	if cmd.IMEI == "" {
		return "", errors.New("imei is required")
	}
	if cmd.DeviceType == "" {
		cmd.DeviceType = DeviceTypeHardware
	}

	d := Device{
		ID:              s.idGen.GenerateUUID(),
		TenantID:        tenantID,
		IMEI:            cmd.IMEI,
		SerialNumber:    cmd.SerialNumber,
		FirmwareVersion: cmd.FirmwareVersion,
		SimNumber:       cmd.SimNumber,
		ICCID:           cmd.ICCID,
		WarrantyUntil:   cmd.WarrantyUntil,
		DeviceType:      cmd.DeviceType,
		Status:          DeviceStatusInventory,
		VehicleID:       cmd.VehicleID,
		CustomerID:      cmd.CustomerID,
	}

	if err := s.store.InsertDevice(ctx, d); err != nil {
		return "", err
	}
	_ = s.audit.LogAction(ctx, "device_register", "telemetry_devices", d.ID, nil, map[string]interface{}{
		"imei": d.IMEI, "status": d.Status,
	})
	return d.ID, nil
}

// BulkRegister registers up to 500 devices atomically (Spec 01 §3). If any row
// conflicts (existing IMEI or a duplicate within the batch) the entire batch is
// rolled back.
func (s *DeviceService) BulkRegister(ctx context.Context, cmds []RegisterDeviceCommand) ([]BulkRegisterResult, error) {
	if len(cmds) > 500 {
		return nil, ErrBatchTooLarge
	}

	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]BulkRegisterResult, 0, len(cmds))
	for _, c := range cmds {
		results = append(results, BulkRegisterResult{IMEI: c.IMEI})
	}

	err = s.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		// Pre-check: duplicates against existing rows and within the batch.
		seen := make(map[string]bool, len(cmds))
		for _, c := range cmds {
			if c.IMEI == "" {
				return errors.New("empty imei in batch")
			}
			if seen[c.IMEI] {
				return ErrDuplicateDevice
			}
			seen[c.IMEI] = true
			if n, err := s.store.CountByIMEI(txCtx, tenantID, c.IMEI); err != nil {
				return err
			} else if n > 0 {
				return ErrDuplicateDevice
			}
		}

		// All clear — insert inside the same transaction.
		for i, c := range cmds {
			if c.DeviceType == "" {
				c.DeviceType = DeviceTypeHardware
			}
			d := Device{
				ID:              s.idGen.GenerateUUID(),
				TenantID:        tenantID,
				IMEI:            c.IMEI,
				SerialNumber:    c.SerialNumber,
				FirmwareVersion: c.FirmwareVersion,
				SimNumber:       c.SimNumber,
				ICCID:           c.ICCID,
				WarrantyUntil:   c.WarrantyUntil,
				DeviceType:      c.DeviceType,
				Status:          DeviceStatusInventory,
				VehicleID:       c.VehicleID,
				CustomerID:      c.CustomerID,
			}
			if err := s.store.InsertDevice(txCtx, d); err != nil {
				results[i].Error = err.Error()
				results[i].Success = false
				return err
			}
			results[i].Success = true
			results[i].DeviceID = d.ID
		}
		return nil
	})
	if err != nil {
		for i := range results {
			if !results[i].Success {
				results[i].Error = err.Error()
			}
		}
		return results, err
	}
	return results, nil
}

// AssignDevice moves a device from `inventory` to `assigned` and binds it to a
// vehicle (Spec 01 §3). Enforces the one-device-per-vehicle constraint.
func (s *DeviceService) AssignDevice(ctx context.Context, imei, vehicleID string) error {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return err
	}
	if imei == "" || vehicleID == "" {
		return errors.New("imei and vehicle_id are required")
	}

	dev, err := s.store.GetByIMEI(ctx, imei)
	if err != nil {
		return err
	}
	if dev == nil || dev.TenantID != tenantID {
		return ErrDeviceNotFound
	}
	if dev.Status != DeviceStatusInventory {
		return fmt.Errorf("%w: device is %s, assign requires inventory", ErrInvalidTransition, dev.Status)
	}

	if err := s.store.SetVehicle(ctx, tenantID, imei, vehicleID); err != nil {
		return err
	}
	_ = s.audit.LogAction(ctx, "device_assign", "telemetry_devices", dev.ID,
		map[string]interface{}{"status": dev.Status},
		map[string]interface{}{"vehicle_id": vehicleID, "status": DeviceStatusAssigned})
	return nil
}

// ActivateDevice moves a device from `assigned` to `active`, generates a random
// raw device secret, stores only the HMAC-SHA256 hash (with the server pepper),
// and returns the raw secret exactly once (Spec 01 §4.1).
func (s *DeviceService) ActivateDevice(ctx context.Context, imei string) (ActivateResult, error) {
	var result ActivateResult
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return result, err
	}

	dev, err := s.store.GetByIMEI(ctx, imei)
	if err != nil {
		return result, err
	}
	if dev == nil || dev.TenantID != tenantID {
		return result, ErrDeviceNotFound
	}
	if dev.Status != DeviceStatusAssigned {
		return result, fmt.Errorf("%w: device is %s, activate requires assigned", ErrInvalidTransition, dev.Status)
	}

	rawSecret, err := generateDeviceSecret()
	if err != nil {
		return result, fmt.Errorf("generate secret: %w", err)
	}
	secretHash := hmacSHA256(s.pepper, rawSecret)

	if err := s.store.SetDeviceSecret(ctx, tenantID, imei, secretHash, time.Now().UTC()); err != nil {
		return result, fmt.Errorf("persist device secret: %w", err)
	}
	_ = s.store.UpdateLastSeen(ctx, imei)

	_ = s.audit.LogAction(ctx, "device_activate", "telemetry_devices", dev.ID,
		map[string]interface{}{"status": dev.Status},
		map[string]interface{}{"status": DeviceStatusActive})

	result.Device, _ = s.store.GetByIMEI(ctx, imei)
	result.RawSecret = rawSecret
	return result, nil
}

// RetireDevice moves a device from `active` or `assigned` to `retired`
// (Spec 01 §3). Retired devices are quarantined by the ingestion pipeline.
func (s *DeviceService) RetireDevice(ctx context.Context, imei string) error {
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return err
	}

	dev, err := s.store.GetByIMEI(ctx, imei)
	if err != nil {
		return err
	}
	if dev == nil || dev.TenantID != tenantID {
		return ErrDeviceNotFound
	}

	var from string
	switch dev.Status {
	case DeviceStatusActive, DeviceStatusAssigned:
		from = dev.Status
	case DeviceStatusRetired:
		return fmt.Errorf("%w: device is already retired", ErrInvalidTransition)
	default:
		return fmt.Errorf("%w: device is %s", ErrInvalidTransition, dev.Status)
	}

	if err := s.store.updateStatus(ctx, tenantID, imei, from, DeviceStatusRetired); err != nil {
		return err
	}
	_ = s.audit.LogAction(ctx, "device_retire", "telemetry_devices", dev.ID,
		map[string]interface{}{"status": from},
		map[string]interface{}{"status": DeviceStatusRetired})
	return nil
}

// ResolveQuarantine applies an admin decision to an open quarantine entry
// (Spec 01 §7 / §10). Multi-write paths use uow.Execute for atomicity.
func (s *DeviceService) ResolveQuarantine(ctx context.Context, cmd ResolveQuarantineCommand) error {
	if cmd.Action == "" {
		return ErrInvalidAction
	}
	tenantID, err := tenantFromContext(ctx)
	if err != nil {
		return err
	}

	entry, err := s.quarantine.GetByID(ctx, cmd.EntryID)
	if err != nil {
		return err
	}
	if entry == nil {
		return ErrDeviceNotFound
	}
	if entry.TenantID != tenantID {
		return ErrDeviceNotFound
	}
	if entry.Status != QuarantineStatusOpen {
		return fmt.Errorf("%w: entry is %s, not open", ErrInvalidTransition, entry.Status)
	}

	switch cmd.Action {
	case "reject":
		return s.uow.Execute(ctx, func(txCtx ports.TxContext) error {
			return s.quarantine.Resolve(txCtx, cmd.EntryID, QuarantineStatusRejected, cmd.UserID)
		})

	case "register_new":
		return s.uow.Execute(ctx, func(txCtx ports.TxContext) error {
			var frame struct {
				IMEI string `json:"imei"`
			}
			if err := json.Unmarshal([]byte(entry.RawPayload), &frame); err != nil {
				return fmt.Errorf("parse quarantine payload: %w", err)
			}
			if frame.IMEI == "" {
				frame.IMEI = entry.IMEI
			}
			d := Device{
				ID:         s.idGen.GenerateUUID(),
				TenantID:   tenantID,
				IMEI:       frame.IMEI,
				DeviceType: DeviceTypeHardware,
				Status:     DeviceStatusInventory,
			}
			if err := s.store.InsertDevice(txCtx, d); err != nil {
				return err
			}
			if err := s.quarantine.Resolve(txCtx, cmd.EntryID, QuarantineStatusResolved, cmd.UserID); err != nil {
				return err
			}
			return nil
		})

	case "assign_existing":
		return s.uow.Execute(ctx, func(txCtx ports.TxContext) error {
			dev, err := s.store.GetByIMEI(txCtx, entry.IMEI)
			if err != nil {
				return err
			}
			if dev == nil || dev.TenantID != tenantID {
				return ErrDeviceNotFound
			}
			vehicleID := ""
			if cmd.VehicleID != nil {
				vehicleID = *cmd.VehicleID
			}
			switch dev.Status {
			case DeviceStatusRetired, DeviceStatusQuarantined:
				if vehicleID != "" {
					if err := s.store.SetVehicle(txCtx, tenantID, entry.IMEI, vehicleID); err != nil {
						return err
					}
				} else {
					if err := s.store.updateStatus(txCtx, tenantID, entry.IMEI, dev.Status, DeviceStatusAssigned); err != nil {
						return err
					}
				}
			case DeviceStatusInventory:
				if vehicleID != "" {
					if err := s.store.SetVehicle(txCtx, tenantID, entry.IMEI, vehicleID); err != nil {
						return err
					}
				}
			case DeviceStatusActive:
				// already active; just resolve the stale quarantine entry
			}
			return s.quarantine.Resolve(txCtx, cmd.EntryID, QuarantineStatusResolved, cmd.UserID)
		})

	default:
		return fmt.Errorf("%w: %s", ErrInvalidAction, cmd.Action)
	}
}

// generateDeviceSecret returns a 32-byte hex raw secret suitable for flashing
// to GPS hardware. Only its HMAC hash (with the server pepper) is persisted.
func generateDeviceSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
