package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	entitlementApp "transport-app/internal/entitlement/application"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

type fakeOpGate struct {
	ok  bool
	err error
}

func (f *fakeOpGate) CanCreateBooking(_ context.Context, _ shared.TenantID) (bool, error) {
	return f.ok, f.err
}

func gateTestCmd(tenant shared.TenantID) CreateBookingCommand {
	return CreateBookingCommand{
		TenantID: tenant, CustomerID: "cust_cas_1", RouteID: "rt_cas_1",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck", Passengers: 1, Price: 12000,
	}
}

// Blocked orgs fail fast with a billing message, creating nothing.
func TestCreateBooking_BlockedOrgRejected(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	uc := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithOperationGate(&fakeOpGate{ok: false})
	_, err := uc.Execute(ctx, gateTestCmd(tenantID))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bookings WHERE tenant_id = 'tenant-1'`).Scan(&count))
	assert.Equal(t, 0, count)
}

// Allowed orgs pass through untouched.
func TestCreateBooking_AllowedOrgProceeds(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	uc := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithOperationGate(&fakeOpGate{ok: true})
	id, err := uc.Execute(ctx, gateTestCmd(tenantID))
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// Real service: READ_ONLY subscription blocks; missing subscription allows.
func TestCreateBooking_RealGateReadOnlyVsLegacy(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	seedCascadeLane(t, db)
	gate := entitlementApp.NewService(db)

	_, err := db.Exec(`INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end)
		VALUES ('sub_ro_1', 'tenant-1', 'GROWTH', 'READ_ONLY', '2026-01-01 00:00:00', '2026-12-31 00:00:00')`)
	require.NoError(t, err)

	uc := NewCreateBookingUseCase(unitOfWork, idGen, clk).WithOperationGate(gate)
	_, err = uc.Execute(ctx, gateTestCmd(shared.TenantID("tenant-1")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked")

	// Tenant with no subscription row (legacy/bootstrap) is never blocked.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-norow','No Row','tenant-norow')`)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone) VALUES ('cust_nr_1', 'tenant-norow', 'NoRow', 'nr@example.com', '7777777777')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_nr_1', 'tenant-norow', 'A', 'B', 10, 1, 100)`)
	require.NoError(t, err)
	cmd := gateTestCmd(shared.TenantID("tenant-norow"))
	cmd.CustomerID = "cust_nr_1"
	cmd.RouteID = "rt_nr_1"
	id, err := uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}
