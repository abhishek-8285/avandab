package maintenance

// Handler-facing queries for the maintenance, tyre and inventory web pages,
// extracted from internal/handlers/maintenance.go,
// internal/handlers/maintenance_plans.go and
// internal/handlers/work_orders_web.go (handlers-end-state ADR, Phase 2).
// The handlers keep rendering and the legacy map-shape adaptation; every
// query keeps its exact tenant scoping, ordering and best-effort semantics.

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// VehicleOption is one tenant vehicle offered in the mounting/issue dropdowns.
type VehicleOption struct {
	ID                 string
	VehicleNumber      string
	RegistrationNumber string
}

// FacilityOption is one tenant facility offered in the stock operations.
type FacilityOption struct {
	ID   string
	Name string
	Code string
}

// VehicleLabels returns the tenant's vehicle-id -> display-label map used by
// the job-card panel (registration number, else vehicle number, else id).
// Best-effort: a query/scan failure renders an empty map, never an error.
func VehicleLabels(ctx context.Context, db *sql.DB, tenantID string) map[string]string {
	labels := map[string]string{}
	if db == nil {
		return labels
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(NULLIF(registration_number,''), vehicle_number, id) FROM vehicles WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return labels
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vid, label string
		if rows.Scan(&vid, &label) == nil {
			labels[vid] = label
		}
	}
	return labels
}

// VehicleOptions returns the tenant's vehicles ordered by vehicle_number for
// the tyre-mounting and inventory issue/return dropdowns. Best-effort: a
// query error or row scan failure is skipped (legacy handler behavior).
func VehicleOptions(ctx context.Context, db *sql.DB, tenantID string) []VehicleOption {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var vehicles []VehicleOption
	for rows.Next() {
		var v VehicleOption
		if err := rows.Scan(&v.ID, &v.VehicleNumber, &v.RegistrationNumber); err == nil {
			vehicles = append(vehicles, v)
		}
	}
	return vehicles
}

// Facilities returns the tenant's facilities ordered by name for the stock
// operations forms. Best-effort like the legacy inline query.
func Facilities(ctx context.Context, db *sql.DB, tenantID string) []FacilityOption {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, name, facility_code FROM facilities WHERE tenant_id = $1 ORDER BY name ASC`, tenantID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var facilities []FacilityOption
	for rows.Next() {
		var f FacilityOption
		if err := rows.Scan(&f.ID, &f.Name, &f.Code); err == nil {
			facilities = append(facilities, f)
		}
	}
	return facilities
}

// VehicleLabel returns the display label for one vehicle id (registration
// number, else vehicle number, else id). The legacy lookup was NOT
// tenant-filtered - it runs only after the tenant-scoped work-order read - and
// refresh/scan errors leave the label empty (best-effort), both preserved.
func VehicleLabel(ctx context.Context, db *sql.DB, vehicleID string) string {
	if db == nil {
		return ""
	}
	var label string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(registration_number,''), vehicle_number, id) FROM vehicles WHERE id = $1`,
		vehicleID).Scan(&label)
	return label
}

// WriteOverrideAudit appends the maintenance_override audit row after a
// successful dispatch-blocker override. Payload bytes/action/table/tenant
// fallback are preserved verbatim; failures stay best-effort at the call site.
func WriteOverrideAudit(ctx context.Context, db *sql.DB, actorID, vehicleID, reason string) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, tenant_id, created_at)
		VALUES ($1, $2, 'maintenance_override', 'vehicles', $3, $4, $5, CURRENT_TIMESTAMP)`,
		uuid.NewString(), actorID, vehicleID, fmt.Sprintf(`{"reason":%q,"override_by":%q}`, reason, actorID), string(shared.TenantOrPlatform(ctx)))
	return err
}
