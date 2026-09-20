package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// driverVehicleAssignment holds one active preferred assignment (nil = none).
type driverVehicleAssignment struct {
	ID        string
	DriverID  string
	VehicleID string
	IsPrimary bool
}

// getActiveAssignmentByDriver returns the active (unassigned_at IS NULL) assignment for a driver.
func getActiveAssignmentByDriver(ctx context.Context, db *sql.DB, driverID, tenantID string) (*driverVehicleAssignment, error) {
	var a driverVehicleAssignment
	var isPrimary int
	err := db.QueryRowContext(ctx,
		`SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		 WHERE driver_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`, driverID, tenantID).
		Scan(&a.ID, &a.DriverID, &a.VehicleID, &isPrimary)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.IsPrimary = isPrimary == 1
	return &a, nil
}

func getActiveAssignmentByVehicle(ctx context.Context, db *sql.DB, vehicleID, tenantID string) (*driverVehicleAssignment, error) {
	var a driverVehicleAssignment
	var isPrimary int
	err := db.QueryRowContext(ctx,
		`SELECT id, driver_id, vehicle_id, is_primary FROM driver_preferred_vehicles
		 WHERE vehicle_id = $1 AND tenant_id = $2 AND unassigned_at IS NULL LIMIT 1`, vehicleID, tenantID).
		Scan(&a.ID, &a.DriverID, &a.VehicleID, &isPrimary)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.IsPrimary = isPrimary == 1
	return &a, nil
}

// assignVehicleToDriverTx closes any conflicting active rows and inserts a new active assignment.
// Caller must hold a transaction if atomicity is required; this helper uses the provided DB handle
// (which may be a *sql.Tx via TxFromContext in the future, but for now plain DB with sequential ops is fine
// because UNIQUE partial indexes reject races loudly).
func assignVehicleToDriver(ctx context.Context, db *sql.DB, tenantID, driverID, vehicleID, assignedBy string) error {
	if driverID == "" || vehicleID == "" {
		return sql.ErrNoRows
	}
	now := "datetime('now')"
	// Verify driver and vehicle exist in this tenant (fail fast with clear message).
	var cnt int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drivers WHERE id = $1 AND tenant_id = $2`, driverID, tenantID).Scan(&cnt); err != nil {
		return err
	}
	if cnt == 0 {
		return sql.ErrNoRows
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vehicles WHERE id = $1 AND tenant_id = $2`, vehicleID, tenantID).Scan(&cnt); err != nil {
		return err
	}
	if cnt == 0 {
		return sql.ErrNoRows
	}
	// Close any active assignment for this driver and for this vehicle (1:1 active).
	_, _ = db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND driver_id = $2 AND unassigned_at IS NULL`, tenantID, driverID)
	_, _ = db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND vehicle_id = $2 AND unassigned_at IS NULL`, tenantID, vehicleID)
	id := "dva-" + uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, assigned_by) VALUES ($1,$2,$3,$4,1,$5)`,
		id, tenantID, driverID, vehicleID, assignedBy)
	return err
}

func unassignDriver(ctx context.Context, db *sql.DB, tenantID, driverID string) error {
	now := "datetime('now')"
	_, err := db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND driver_id = $2 AND unassigned_at IS NULL`, tenantID, driverID)
	return err
}

func unassignVehicle(ctx context.Context, db *sql.DB, tenantID, vehicleID string) error {
	now := "datetime('now')"
	_, err := db.ExecContext(ctx, `UPDATE driver_preferred_vehicles SET unassigned_at = `+now+`, updated_at = `+now+` WHERE tenant_id = $1 AND vehicle_id = $2 AND unassigned_at IS NULL`, tenantID, vehicleID)
	return err
}

func (h *DriverHandlers) AssignVehicleToDriver(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "id")
	vehicleID := r.PostFormValue("vehicle_id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if vehicleID == "" {
		http.Error(w, "vehicle_id is required", http.StatusBadRequest)
		return
	}
	assignedBy := ""
	if s, _ := h.getUserFromContext(r); s != nil {
		assignedBy = s.UserID
	}
	if err := assignVehicleToDriver(r.Context(), h.DB, tenantID, driverID, vehicleID, assignedBy); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Assignment Failed")
		return
	}
	http.Redirect(w, r, "/drivers/"+driverID, http.StatusSeeOther)
}

func (h *DriverHandlers) UnassignVehicleFromDriver(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	_ = unassignDriver(r.Context(), h.DB, tenantID, driverID)
	http.Redirect(w, r, "/drivers/"+driverID, http.StatusSeeOther)
}

func (h *VehicleHandlers) AssignDriverToVehicle(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "id")
	driverID := r.PostFormValue("driver_id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if driverID == "" {
		http.Error(w, "driver_id is required", http.StatusBadRequest)
		return
	}
	assignedBy := ""
	if s, _ := h.getUserFromContext(r); s != nil {
		assignedBy = s.UserID
	}
	if err := assignVehicleToDriver(r.Context(), h.DB, tenantID, driverID, vehicleID, assignedBy); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Assignment Failed")
		return
	}
	http.Redirect(w, r, "/vehicles/"+vehicleID, http.StatusSeeOther)
}

func (h *VehicleHandlers) UnassignDriverFromVehicle(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	_ = unassignVehicle(r.Context(), h.DB, tenantID, vehicleID)
	http.Redirect(w, r, "/vehicles/"+vehicleID, http.StatusSeeOther)
}
