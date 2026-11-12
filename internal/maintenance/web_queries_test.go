package maintenance

// Characterization + extraction tests for the handler-facing maintenance
// queries moved out of internal/handlers/{maintenance,maintenance_plans,
// work_orders_web}.go (handlers-end-state ADR, Phase 2): tenant isolation of
// the vehicle/facility dropdowns, display-label fallbacks, best-effort
// semantics and the maintenance_override audit row.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

const webQueriesTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    vehicle_number TEXT,
    registration_number TEXT
);
CREATE TABLE facilities (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    name TEXT,
    facility_code TEXT
);
CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    action TEXT NOT NULL,
    table_name TEXT NOT NULL,
    record_id TEXT,
    new_values TEXT,
    tenant_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newWebQueriesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("maint_web_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(webQueriesTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecWebQueries(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestVehicleLabels_TenantScopedAndFallback(t *testing.T) {
	db := newWebQueriesTestDB(t)
	mustExecWebQueries(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number) VALUES
		('v-reg', 'tenant-1', 'VN-1', 'MH12AB1234'),
		('v-null-reg', 'tenant-1', 'VN-2', NULL),
		('v-empty-reg', 'tenant-1', 'VN-3', ''),
		('v-tenant-2', 'tenant-2', 'VN-9', 'KA01XY9999')`)

	labels := VehicleLabels(context.Background(), db, "tenant-1")
	require.NotNil(t, labels)
	assert.Equal(t, "MH12AB1234", labels["v-reg"])
	assert.Equal(t, "VN-2", labels["v-null-reg"], "NULL registration falls back to vehicle number")
	assert.Equal(t, "VN-3", labels["v-empty-reg"], "empty registration falls back to vehicle number")
	assert.NotContains(t, labels, "v-tenant-2", "other tenants never leak into the job-card panel")

	// Best-effort: nil DB and a dead table render an empty map, never an error.
	assert.Empty(t, VehicleLabels(context.Background(), nil, "tenant-1"))
	mustExecWebQueries(t, db, `DROP TABLE vehicles`)
	assert.Empty(t, VehicleLabels(context.Background(), db, "tenant-1"))
}

func TestVehicleOptions_TenantScopedOrdering(t *testing.T) {
	db := newWebQueriesTestDB(t)
	mustExecWebQueries(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number) VALUES
		('v-b', 'tenant-1', 'VN-B', 'MH12B'),
		('v-a', 'tenant-1', 'VN-A', 'MH12A'),
		('v-t2', 'tenant-2', 'VN-0', 'KA01')`)

	vehicles := VehicleOptions(context.Background(), db, "tenant-1")
	require.Len(t, vehicles, 2)
	assert.Equal(t, "v-a", vehicles[0].ID, "ordered by vehicle_number ASC")
	assert.Equal(t, "VN-A", vehicles[0].VehicleNumber)
	assert.Equal(t, "MH12A", vehicles[0].RegistrationNumber)
	assert.Equal(t, "v-b", vehicles[1].ID)

	assert.Nil(t, VehicleOptions(context.Background(), db, "tenant-unknown"))
	assert.Nil(t, VehicleOptions(context.Background(), nil, "tenant-1"))
}

func TestFacilities_TenantScopedOrdering(t *testing.T) {
	db := newWebQueriesTestDB(t)
	mustExecWebQueries(t, db, `INSERT INTO facilities (id, tenant_id, name, facility_code) VALUES
		('f-hub', 'tenant-1', 'Zonal Hub', 'ZH'),
		('f-depot', 'tenant-1', 'Central Depot', 'CD'),
		('f-t2', 'tenant-2', 'Other Depot', 'OD')`)

	facilities := Facilities(context.Background(), db, "tenant-1")
	require.Len(t, facilities, 2)
	assert.Equal(t, "f-depot", facilities[0].ID, "ordered by name ASC")
	assert.Equal(t, "Central Depot", facilities[0].Name)
	assert.Equal(t, "CD", facilities[0].Code)
	assert.Equal(t, "f-hub", facilities[1].ID)

	assert.Nil(t, Facilities(context.Background(), db, "tenant-unknown"))
	assert.Nil(t, Facilities(context.Background(), nil, "tenant-1"))
}

func TestVehicleLabel_BestEffort(t *testing.T) {
	db := newWebQueriesTestDB(t)
	mustExecWebQueries(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number) VALUES
		('v-reg', 'tenant-1', 'VN-1', 'MH12AB1234'),
		('v-empty-reg', 'tenant-1', 'VN-3', '')`)

	ctx := context.Background()
	assert.Equal(t, "MH12AB1234", VehicleLabel(ctx, db, "v-reg"))
	assert.Equal(t, "VN-3", VehicleLabel(ctx, db, "v-empty-reg"))
	assert.Empty(t, VehicleLabel(ctx, db, "v-missing"), "best-effort: unknown ids render no label")
	assert.Empty(t, VehicleLabel(ctx, nil, "v-reg"))

	// FROZEN LEGACY SCOPE: the label lookup is id-only (no tenant filter); it
	// runs only after a tenant-scoped work-order read, so the id is trusted.
	mustExecWebQueries(t, db, `INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number) VALUES ('v-other', 'tenant-2', 'VN-O', 'KA01')`)
	assert.Equal(t, "KA01", VehicleLabel(ctx, db, "v-other"))
}

func TestWriteOverrideAudit_RowShape(t *testing.T) {
	db := newWebQueriesTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-2")
	reason := `blocked sensor says "no"`
	require.NoError(t, WriteOverrideAudit(ctx, db, "u-admin", "veh-1", reason))

	var userID, action, tableName, recordID, newValues, tenantID string
	require.NoError(t, db.QueryRow(`SELECT user_id, action, table_name, record_id, new_values, tenant_id FROM audit_logs`).
		Scan(&userID, &action, &tableName, &recordID, &newValues, &tenantID))
	assert.Equal(t, "u-admin", userID)
	assert.Equal(t, "maintenance_override", action)
	assert.Equal(t, "vehicles", tableName)
	assert.Equal(t, "veh-1", recordID)
	assert.Equal(t, "tenant-2", tenantID)
	assert.Equal(t, fmt.Sprintf(`{"reason":%q,"override_by":%q}`, reason, "u-admin"), newValues, "legacy fmt.Sprintf payload bytes preserved")

	// No tenant in context -> platform-tenant fallback (legacy helper behavior).
	require.NoError(t, WriteOverrideAudit(context.Background(), db, "u-admin", "veh-2", "late override"))
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM audit_logs WHERE record_id = 'veh-2'`).Scan(&tenantID))
	assert.Equal(t, string(shared.DefaultTenant), tenantID)

	assert.NoError(t, WriteOverrideAudit(ctx, nil, "u", "v", "r"))
}
