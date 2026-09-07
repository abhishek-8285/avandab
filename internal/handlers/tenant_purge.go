package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	appdb "transport-app/internal/database"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/httpx"
	"transport-app/internal/shared"
)

// ── Per-tenant operational purge (GDPR/offboarding) ─────────────────────
// Destructive: deletes every operational row of one tenant in FK-safe order
// inside a single transaction. Protected tables (tenants, users, roles,
// billing seeds, migration bookkeeping) are never touched — a fail-closed
// guard aborts if any statement targets them. The bootstrap tenant ("1",
// the platform owner's own org) cannot be purged, mirroring the suspend
// rule: wiping it would nuke seed data and lock everyone out.

// purgeProtected tables must never appear in a purge statement.
var purgeProtected = map[string]bool{
	"tenants": true, "users": true, "user_roles": true, "roles": true,
	"permissions": true, "role_permissions": true,
	"goose_db_version": true, "schema_migrations": true, "migrations": true,
	"company_config": true, "company_settings": true,
}

// purgeGlobalPreds deletes from tables without their own tenant_id by
// scoping through parent trips/drivers/vehicles/bookings/etc. Children first.
var purgeGlobalPreds = []struct{ table, pred string }{
	{"eway_bill_events", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR ewb_number IN (SELECT ewb_number FROM eway_bills WHERE trip_id IN (SELECT id FROM trips WHERE tenant_id = $2))"},
	{"settlement_lines", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR settlement_id IN (SELECT id FROM driver_settlements WHERE tenant_id = $2)"},
	{"share_links", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1)"},
	{"telemetry_snapshots", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $2)"},
	{"fuel_claim_audits", "expense_id IN (SELECT id FROM driver_expenses WHERE tenant_id = $1) OR trip_id IN (SELECT id FROM trips WHERE tenant_id = $2) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $3) OR driver_id IN (SELECT id FROM drivers WHERE tenant_id = $4)"},
	{"fuel_events", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $2) OR driver_id IN (SELECT id FROM drivers WHERE tenant_id = $3)"},
	{"driver_behaviour_events", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $2) OR driver_id IN (SELECT id FROM drivers WHERE tenant_id = $3)"},
	{"driver_scores", "driver_id IN (SELECT id FROM drivers WHERE tenant_id = $1)"},
	{"driver_documents", "driver_id IN (SELECT id FROM drivers WHERE tenant_id = $1)"},
	{"vehicle_documents", "vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $1)"},
	{"dtc_events", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $2)"},
	{"telemetry_alerts", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $2) OR driver_id IN (SELECT id FROM drivers WHERE tenant_id = $3)"},
	{"customer_users", "customer_id IN (SELECT id FROM customers WHERE tenant_id = $1)"},
	{"route_locations", "route_id IN (SELECT id FROM routes WHERE tenant_id = $1)"},
	{"route_constraints", "job_id IN (SELECT id FROM route_optimization_jobs WHERE tenant_id = $1)"},
	{"files", "uploadable_type IN ('trip_pod','expense_receipt') AND (uploadable_id IN (SELECT id FROM trips WHERE tenant_id = $1) OR uploadable_id IN (SELECT id FROM driver_expenses WHERE tenant_id = $2))"},
	{"dispatches", "tenant_id IS NULL AND booking_id IN (SELECT id FROM bookings WHERE tenant_id = $1)"},
	{"eway_bills", "trip_id IN (SELECT id FROM trips WHERE tenant_id = $1)"},
	{"maintenance_schedules", "vehicle_id IN (SELECT id FROM vehicles WHERE tenant_id = $1)"},
}

// purgeTenantTables deletes by direct tenant_id, children before parents.
var purgeTenantTables = []string{
	"stop_pod_attachments", "trip_stops", "trip_feedback", "trip_detentions",
	"eta_history", "eta_history_monthly",
	"invoice_line_items", "payments", "credit_debit_notes", "money_ledger",
	"fastag_transactions",
	"driver_license_classes", "driver_licenses", "driver_ledger_entries",
	"payout_instructions", "driver_payout_accounts", "driver_advance_requests",
	"invoices", "driver_expenses", "driver_settlements",
	"dispatches", "dispatch_offers", "dispatch_overrides",
	"telemetry_positions", "telemetry_raw_events", "telemetry_devices",
	"telemetry_events", "telemetry_sessions", "telemetry_installations",
	"vehicle_latest_position", "vehicle_latest_positions", "engine_state",
	"geofence_events", "vehicle_geofences",
	"trips", "customer_booking_details", "bookings", "customer_quotes",
	"fastag_tags", "maintenance_records",
	"driver_vehicle_assignments", "driver_onboarding", "driver_identities",
	"driver_compliance_documents", "driver_issues", "driver_commands",
	"vehicle_ownership", "vehicle_claims", "vehicle_compliance_documents",
	"drivers", "vehicles", "customers", "routes", "geofences",
	"route_optimization_jobs", "device_quarantine",
	"provider_poll_state", "provider_events", "subscription_webhook_events",
	"tenant_subscriptions", "tenant_entitlement_overrides", "tenant_usage_meters",
	"tenant_usage_events", "comm_outbox", "email_send_log",
	"alerts", "ops_alerts", "audit_events", "incidents", "error_reports",
	"offline_sync_log", "fuel_prices", "pnl_daily",
	"invoice_sequences", "note_sequences", "feature_flags",
	"experiment_assignments", "experiment_events", "experiments_spec16",
	"founder_audit", "founder_signals", "verification_attempts",
}

// purgeTableExists probes for a table portably (sqlite_master is
// sqlite-only). Missing-table errors on either engine mean "absent".
func purgeTableExists(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, table string) bool {
	var one int
	if err := q.QueryRowContext(ctx, `SELECT 1 FROM `+table+` LIMIT 0`).Scan(&one); err != nil {
		// LIMIT 0 yields zero rows on success (sql.ErrNoRows = table exists).
		if errors.Is(err, sql.ErrNoRows) {
			return true
		}
		msg := err.Error()
		if strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist") {
			return false
		}
		return false
	}
	return true
}

func purgeCountArgs(pred, tenantID string) []any {
	// Predicates use sequential $N markers (PG/sqlite portable); every
	// marker binds the same tenant, so arg count = highest marker index.
	n := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(pred, -1) {
		var v int
		if _, err := fmt.Sscanf(m[1], "%d", &v); err == nil && v > n {
			n = v
		}
	}
	// Legacy bare-? predicates (none remain, kept for safety).
	if n == 0 {
		n = strings.Count(pred, "?")
	}
	args := make([]any, n)
	for i := range args {
		args[i] = tenantID
	}
	return args
}

// purgeTenantPreview counts rows that a purge would delete, deleting nothing.
func purgeTenantPreview(ctx context.Context, db *sql.DB, tenantID string) (map[string]int64, error) {
	out := map[string]int64{}
	count := func(table, query string, args ...any) error {
		rebound, rerr := appdb.Rebind(query)
		if rerr != nil {
			return rerr
		}
		var n int64
		if err := db.QueryRowContext(ctx, rebound, args...).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			out[table] = n
		}
		return nil
	}
	if !purgeTableExists(ctx, db, "tenants") {
		return nil, fmt.Errorf("tenants table missing")
	}
	for _, g := range purgeGlobalPreds {
		if !purgeTableExists(ctx, db, g.table) {
			continue
		}
		if err := count(g.table, `SELECT COUNT(*) FROM `+g.table+` WHERE `+g.pred, purgeCountArgs(g.pred, tenantID)...); err != nil {
			return nil, err
		}
	}
	for _, table := range purgeTenantTables {
		if purgeProtected[table] {
			return nil, fmt.Errorf("refusing: %s is protected", table)
		}
		if !purgeTableExists(ctx, db, table) {
			continue
		}
		if err := count(table, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = ?`, tenantID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// purgeTenantOps deletes every operational row of tenantID in one
// transaction. Returns per-table deleted counts.
func purgeTenantOps(ctx context.Context, db *sql.DB, tenantID string) (map[string]int64, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant id required")
	}
	if tenantID == string(shared.DefaultTenant) { //nolint:tenant-default // safety guard: bootstrap org must never be purgeable
		return nil, fmt.Errorf("bootstrap tenant cannot be purged")
	}
	var found int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id = $1`, tenantID).Scan(&found); err != nil || found != 1 {
		return nil, fmt.Errorf("tenant %q not found", tenantID)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Postgres enforces FKs unconditionally; the PRAGMA is sqlite-only.
	if !appdb.IsPostgres(db) {
		if _, err := tx.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
			return nil, err
		}
	}

	deleted := map[string]int64{}
	execDel := func(table, query string, args ...any) error {
		if purgeProtected[table] {
			return fmt.Errorf("refusing: %s is protected", table)
		}
		if !purgeTableExists(ctx, tx, table) {
			return nil
		}
		rebound, rerr := appdb.Rebind(query)
		if rerr != nil {
			return fmt.Errorf("purge %s: %w", table, rerr)
		}
		res, err := tx.ExecContext(ctx, rebound, args...)
		if err != nil {
			return fmt.Errorf("purge %s: %w", table, err)
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			deleted[table] = n
		}
		return nil
	}

	for _, g := range purgeGlobalPreds {
		if err := execDel(g.table, `DELETE FROM `+g.table+` WHERE `+g.pred, purgeCountArgs(g.pred, tenantID)...); err != nil {
			return nil, err
		}
	}
	for _, table := range purgeTenantTables {
		if err := execDel(table, `DELETE FROM `+table+` WHERE tenant_id = ?`, tenantID); err != nil {
			return nil, err
		}
	}
	// Post-purge leftover check: no operational row may reference the tenant.
	for _, table := range purgeTenantTables {
		if !purgeTableExists(ctx, tx, table) {
			continue
		}
		var n int64
		reboundLeft, rerr := appdb.Rebind(`SELECT COUNT(*) FROM ` + table + ` WHERE tenant_id = ?`)
		if rerr != nil {
			return nil, rerr
		}
		if err := tx.QueryRowContext(ctx, reboundLeft, tenantID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("leftover rows in %s after purge", table)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deleted, nil
}

// PurgePreview returns dry-run counts for the confirm screen (admin only via route gate).
func (h *TenantsHandlers) PurgePreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "missing tenant id"})
		return
	}
	counts, err := purgeTenantPreview(r.Context(), h.DB, id)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "tenant_id": id, "would_delete": counts})
}

// Purge executes the operational purge after typed confirmation.
func (h *TenantsHandlers) Purge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "invalid form"})
		return
	}
	if id == "" || r.PostFormValue("confirm") != id {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "type the tenant id to confirm purge"})
		return
	}
	deleted, err := purgeTenantOps(r.Context(), h.DB, id)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
		return
	}
	h.auditTenant(r, "tenant.purge", id, map[string]string{"tables": fmt.Sprintf("%d", len(deleted))})
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "tenant_id": id, "deleted": deleted})
}
