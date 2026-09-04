package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

func newIdemBookingCmd(tenant shared.TenantID, key string) CreateBookingCommand {
	return CreateBookingCommand{
		TenantID: tenant, CustomerID: "cust_cas_1", RouteID: "rt_cas_1",
		PickupDate:  time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		VehicleType: "truck", Passengers: 1, Price: 12000,
		IdempotencyKey: key,
	}
}

// Retried creates with the same key return the original booking, one row.
func TestCreateBooking_IdempotentRetry(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")
	seedCascadeLane(t, db)

	uc := NewCreateBookingUseCase(unitOfWork, idGen, clk)
	id1, err := uc.Execute(ctx, newIdemBookingCmd(tenantID, "idem-bk-1"))
	require.NoError(t, err)
	id2, err := uc.Execute(ctx, newIdemBookingCmd(tenantID, "idem-bk-1"))
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bookings WHERE tenant_id = 'tenant-1'`).Scan(&count))
	assert.Equal(t, 1, count)
}

// Same key in different tenants creates separate rows (tenant-scoped keys).
func TestCreateBooking_IdempotencyKeyTenantScoped(t *testing.T) {
	db := newCascadeTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	seedCascadeLane(t, db)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-idem-2','Idem Tenant 2','tenant-idem-2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone) VALUES ('cust_cas_2', 'tenant-idem-2', 'Other Buyer', 'o@example.com', '8888888888')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_cas_2', 'tenant-idem-2', 'Delhi', 'Jaipur', 280, 5, 12000)`)
	require.NoError(t, err)

	uc := NewCreateBookingUseCase(unitOfWork, idGen, clk)
	cmd1 := newIdemBookingCmd(shared.TenantID("tenant-1"), "idem-shared")
	id1, err := uc.Execute(ctx, cmd1)
	require.NoError(t, err)

	cmd2 := newIdemBookingCmd(shared.TenantID("tenant-idem-2"), "idem-shared")
	cmd2.CustomerID = "cust_cas_2"
	cmd2.RouteID = "rt_cas_2"
	id2, err := uc.Execute(ctx, cmd2)
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}
