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

// Retried creates with the same key return the original trip, one row.
func TestCreateTrip_IdempotentRetry(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	ctx := context.Background()
	tenantID := shared.TenantID("tenant-1")

	_, _ = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt_idem_1', 'tenant-1', 'Delhi', 'Jaipur', 280, 5, 12000)`)

	uc := NewCreateTripUseCase(unitOfWork, idGen, clk)
	cmd := CreateTripCommand{
		TenantID: tenantID, RouteID: "rt_idem_1",
		DepartureTime: time.Now().Add(2 * time.Hour), IdempotencyKey: "idem-tr-1",
	}
	id1, err := uc.Execute(ctx, cmd)
	require.NoError(t, err)
	id2, err := uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM trips WHERE tenant_id = 'tenant-1'`).Scan(&count))
	assert.Equal(t, 1, count)
}
