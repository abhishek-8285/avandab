package maintenance

// Focused tests for the work-order application facade extracted from the
// agent work-order tools (handlers-end-state ADR, Phase 3): tenant isolation,
// lifecycle transitions (incl the done side effects) and error mapping.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/shared"
)

func facadeTenantCtx(tenant string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(tenant))
}

func seedFacadeVehicle(t *testing.T, db *sql.DB, id, tenant string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, status,
		 insurance_expiry, fitness_expiry, permit_expiry)
		VALUES (?, ?, ?, ?, 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`,
		id, tenant, "REG-"+id, "MH-01-"+id)
	require.NoError(t, err)
}

func newFacadeFixture(t *testing.T) (*WorkOrderFacade, *sql.DB) {
	t.Helper()
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-b', 'Tenant B', 'tenant-b')`)
	require.NoError(t, err)
	seedFacadeVehicle(t, db, "veh-f1", "1")
	seedFacadeVehicle(t, db, "veh-f2", "tenant-b")
	return NewWorkOrderFacade(db), db
}

func TestWorkOrderFacade_TenantIsolation(t *testing.T) {
	f, _ := newFacadeFixture(t)
	ctxOwn := facadeTenantCtx("1")
	ctxOther := facadeTenantCtx("tenant-b")

	created, err := f.Create(ctxOwn, CreateWorkOrderInput{VehicleID: "veh-f1", Title: "Brake job"})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID, "facade assigns the uuid")
	assert.Equal(t, "1", created.TenantID, "tenant derived from ctx")
	assert.Equal(t, domain.WorkOrderOpen, created.Status)

	list, err := f.List(ctxOwn, "", 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, created.ID, list[0].ID)

	// Other tenant sees nothing and cannot resolve the card.
	otherList, err := f.List(ctxOther, "", 0)
	require.NoError(t, err)
	assert.Empty(t, otherList)
	foreign, err := f.Get(ctxOther, created.ID)
	require.NoError(t, err)
	assert.Nil(t, foreign, "foreign card must be invisible")

	unknown, err := f.Get(ctxOwn, "wo-nope")
	require.NoError(t, err)
	assert.Nil(t, unknown, "not-found is (nil, nil) for the tool's legacy error")

	got, err := f.Get(ctxOwn, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Brake job", got.Title)

	// Missing store fails closed with the legacy model-facing string.
	var unavailable *WorkOrderFacade
	_, err = unavailable.List(ctxOwn, "", 0)
	assert.ErrorIs(t, err, ErrStoreUnavailable)
	assert.Equal(t, "maintenance store unavailable", err.Error())
	_, err = unavailable.Get(ctxOwn, "x")
	assert.ErrorIs(t, err, ErrStoreUnavailable)
	_, err = unavailable.Create(ctxOwn, CreateWorkOrderInput{VehicleID: "veh-f1", Title: "x"})
	assert.ErrorIs(t, err, ErrStoreUnavailable)
	_, err = unavailable.Transition(ctxOwn, TransitionWorkOrderInput{ID: "x", Status: "open"})
	assert.ErrorIs(t, err, ErrStoreUnavailable)
	assert.Nil(t, NewWorkOrderFacade(nil))
}

func TestWorkOrderFacade_TransitionLifecycle(t *testing.T) {
	f, _ := newFacadeFixture(t)
	ctx := facadeTenantCtx("1")
	created, err := f.Create(ctx, CreateWorkOrderInput{VehicleID: "veh-f1", Title: "Clutch job"})
	require.NoError(t, err)

	// Assignment rides along with the status move and auto-assigns open -> assigned.
	res, err := f.Transition(ctx, TransitionWorkOrderInput{ID: created.ID, Status: "in_progress", Assignee: "Ramesh", Vendor: "Garage"})
	require.NoError(t, err)
	assert.False(t, res.Completed)
	assert.Equal(t, "in_progress", res.Status)
	got, err := f.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "in_progress", got.Status)
	assert.Equal(t, "Ramesh", got.Assignee)
	assert.Equal(t, "Garage", got.Vendor)

	// Done closes the card, writes the service record and resolves the due flag.
	res, err = f.Transition(ctx, TransitionWorkOrderInput{ID: created.ID, Status: "done", Actor: "admin-1"})
	require.NoError(t, err)
	assert.True(t, res.Completed)
	assert.Equal(t, created.ID, res.ID)
	doneRow, err := f.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, doneRow.Terminal())
	assert.NotNil(t, doneRow.ClosedAt)
	var serviceRecords int
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM maintenance_records WHERE vehicle_id = ?`, "veh-f1").Scan(&serviceRecords))
	assert.Equal(t, 1, serviceRecords, "done writes the service record")

	// Re-closing is rejected by the repository guard.
	_, err = f.Transition(ctx, TransitionWorkOrderInput{ID: created.ID, Status: "done", Actor: "admin-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already terminal")

	// Missing card surfaces the repository not-found error.
	_, err = f.Transition(ctx, TransitionWorkOrderInput{ID: "wo-nope", Status: "in_progress"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Repo-level required-field validation is passed through unchanged.
	_, err = f.Create(ctx, CreateWorkOrderInput{VehicleID: "veh-f1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}
