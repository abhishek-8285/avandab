package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"

	"transport-app/internal/repository"
)

// AuditLogger abstracts the audit log service for testability. The guard passes
// structured old/new values; the adapter serializes them to JSON for audit_logs.
type AuditLogger interface {
	LogAction(ctx context.Context, action, tableName, recordID string, oldValues, newValues map[string]interface{}) error
}

// OdometerGuard enforces monotonic odometer values and clamps fuel level.
type OdometerGuard struct {
	store             *DeviceStore
	maxRegressionKM   float64 // TELEMETRY_ODOMETER_MAX_REGRESSION_KM
	fuelClampDeltaPct float64 // TELEMETRY_FUEL_CLAMP_DELTA_PCT
	audit             AuditLogger
}

// NewOdometerGuard constructs an OdometerGuard.
func NewOdometerGuard(ds *DeviceStore, maxRegressionKM, fuelClampDeltaPct float64, audit AuditLogger) *OdometerGuard {
	return &OdometerGuard{
		store:             ds,
		maxRegressionKM:   maxRegressionKM,
		fuelClampDeltaPct: fuelClampDeltaPct,
		audit:             audit,
	}
}

// CheckOdometer validates the odometer value against the stored last value.
// Returns the adjusted odometer (original if valid, last-known if regression
// exceeds the threshold) and whether the guard fired.
func (g *OdometerGuard) CheckOdometer(ctx context.Context, imei string, odometer float64) (float64, bool, error) {
	if odometer <= 0 {
		return odometer, false, nil // no odometer data, skip guard
	}

	lastOdometer, exists, err := g.store.GetLastOdometer(ctx, imei)
	if err != nil {
		return odometer, false, fmt.Errorf("odometer guard lookup: %w", err)
	}
	if !exists {
		return odometer, false, nil // first frame, no guard needed
	}

	regression := lastOdometer - odometer
	if regression > g.maxRegressionKM {
		// Guard fired: keep last known value, audit-log the rollback.
		if g.audit != nil {
			_ = g.audit.LogAction(ctx, "odometer_rollback_guard", "telemetry_devices", imei,
				map[string]interface{}{"last_odometer": lastOdometer},
				map[string]interface{}{
					"frame_odometer": odometer,
					"regression_km":  regression,
					"kept_odometer":  lastOdometer,
				})
		}
		return lastOdometer, true, nil
	}

	return odometer, false, nil
}

// ClampFuelLevel validates the fuel level against the stored last value.
// Returns the clamped fuel level (percent 0-100) and whether the clamp fired.
func (g *OdometerGuard) ClampFuelLevel(ctx context.Context, imei string, fuelLevel float64) (float64, bool, error) {
	if fuelLevel <= 0 {
		return fuelLevel, false, nil // no fuel data, skip clamp
	}

	lastFuel, exists, err := g.store.GetLastFuelLevel(ctx, imei)
	if err != nil {
		return fuelLevel, false, fmt.Errorf("fuel clamp lookup: %w", err)
	}
	if !exists {
		return fuelLevel, false, nil // first frame, no clamp needed
	}

	delta := math.Abs(fuelLevel - lastFuel)
	if delta > g.fuelClampDeltaPct {
		// Clamp fired: adjust to last ± clamp delta.
		var clamped float64
		if fuelLevel > lastFuel {
			clamped = lastFuel + g.fuelClampDeltaPct
		} else {
			clamped = lastFuel - g.fuelClampDeltaPct
		}
		if clamped < 0 {
			clamped = 0
		}
		if clamped > 100 {
			clamped = 100
		}

		if g.audit != nil {
			_ = g.audit.LogAction(ctx, "fuel_level_clamp", "telemetry_devices", imei,
				map[string]interface{}{"last_fuel": lastFuel},
				map[string]interface{}{
					"frame_fuel": fuelLevel,
					"delta":      delta,
					"clamped_to": clamped,
				})
		}
		return clamped, true, nil
	}

	return fuelLevel, false, nil
}

// auditLogAdapter implements AuditLogger by writing to audit_logs. It is
// transaction-aware via repository.TxFromContext.
type auditLogAdapter struct {
	db *sql.DB
}

// NewAuditLogger returns an AuditLogger that writes to the audit_logs table.
func NewAuditLogger(db *sql.DB) AuditLogger {
	return &auditLogAdapter{db: db}
}

func (a *auditLogAdapter) LogAction(ctx context.Context, action, tableName, recordID string, oldValues, newValues map[string]interface{}) error {
	var oldJSON, newJSON *string
	if oldValues != nil {
		if b, err := json.Marshal(oldValues); err == nil {
			s := string(b)
			oldJSON = &s
		}
	}
	if newValues != nil {
		if b, err := json.Marshal(newValues); err == nil {
			s := string(b)
			newJSON = &s
		}
	}

	execDB := a.txOrDB(ctx)
	_, err := execDB.ExecContext(ctx,
		`INSERT INTO audit_logs (id, action, table_name, record_id, old_values, new_values, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.NewString(), action, tableName, recordID, oldJSON, newJSON, "",
	)
	return err
}

// txOrDB returns the active transaction from context, or the stored DB.
func (a *auditLogAdapter) txOrDB(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return a.db
}
