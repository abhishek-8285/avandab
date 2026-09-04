package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
	sqlrepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/uow"
)

// Fixes are attributed to their vehicle's org; gated-off orgs and unknown
// vehicles are skipped instead of processed as the default tenant.
func TestDwellWorker_PerTenantSweep(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','A','a'),('tenant-B','B','b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('va','RA','RA','truck',10,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-A'),
		       ('vb','RB','RB','truck',10,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-B')`)
	require.NoError(t, err)

	now := time.Now().UTC()
	fixes := []domain.Fix{
		{VehicleID: "va", Timestamp: now, Latitude: 12.97, Longitude: 77.59},
		{VehicleID: "vb", Timestamp: now, Latitude: 12.97, Longitude: 77.59},
		{VehicleID: "ghost", Timestamp: now, Latitude: 12.97, Longitude: 77.59},
	}

	logger := slog.New(slog.DiscardHandler)
	bus := events.NewInMemoryBus()
	w := &DwellWorker{
		uow:       uow.NewSQLUnitOfWork(db),
		db:        db,
		config:    application.NewConfigReader(db),
		bus:       bus,
		outbox:    outbox.NewOutboxWriter(db),
		idGen:     id.NewUUIDGenerator(),
		log:       logger,
		tenantID:  "1",
		fixes:     &mockFixRepo{fixes: fixes, db: db},
		geofences: sqlrepo.NewGeofenceRepository(db),
		states:    sqlrepo.NewEngineStateRepository(db),
		logs:      sqlrepo.NewEventLogRepository(db),
	}
	// Only tenant-A has the feature.
	w.WithFeatureGate(func(tenantID string) bool { return tenantID == "tenant-A" })

	handled, err := w.Tick(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)

	states := sqlrepo.NewEngineStateRepository(db)
	_, err = states.GetByVehicle(context.Background(), "tenant-A", "va")
	require.NoError(t, err, "tenant-A vehicle must have state")

	_, err = states.GetByVehicle(context.Background(), "tenant-B", "vb")
	require.Error(t, err, "gated-off tenant-B must have no state")

	_, err = states.GetByVehicle(context.Background(), "1", "ghost")
	require.Error(t, err, "unknown vehicle must have no state")
}
