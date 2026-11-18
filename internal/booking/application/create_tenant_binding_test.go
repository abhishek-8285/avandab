package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// TestCreateBooking_RejectsCrossTenantReferences: without a check, a caller
// can book against another org's customer/route and inherit its pricing.
func TestCreateBooking_RejectsCrossTenantReferences(t *testing.T) {
	db := newCascadeTestDB(t)
	seedCascadeLane(t, db)
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-foreign', 'Foreign', 'tenant-foreign')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone) VALUES ('cust_foreign', 'tenant-foreign', 'Foreign Buyer', 'f@example.com', '9999999991')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_foreign', 'tenant-foreign', 'A', 'B', 10, 1, 999999)`)
	require.NoError(t, err)

	uc := NewCreateBookingUseCase(uow.NewSQLUnitOfWork(db), id.NewUUIDGenerator(), clock.NewRealClock())
	for _, tc := range []struct {
		name, customer, route string
	}{
		{"foreign_customer", "cust_foreign", "rt_cas_1"},
		{"foreign_route", "cust_cas_1", "rt_foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := gateTestCmd(shared.TenantID("tenant-1"))
			cmd.CustomerID, cmd.RouteID = tc.customer, tc.route
			_, err := uc.Execute(context.Background(), cmd)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "does not belong to this tenant")
		})
	}
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bookings WHERE tenant_id = 'tenant-1'`).Scan(&n))
	assert.Zero(t, n)
}
