package worker

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/geofence/application"
	"transport-app/internal/geofence/domain"
	sqlrepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/shared/uow"
)

func newThresholdFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE company_config (tenant_id TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY (tenant_id, key));
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL);
		CREATE TABLE geofences (
			id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL,
			shape TEXT NOT NULL, center_lat REAL, center_lng REAL, radius_m REAL, polygon TEXT,
			route_name TEXT, priority INTEGER NOT NULL, is_active INTEGER NOT NULL, created_by TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE vehicle_geofences (vehicle_id TEXT, geofence_id TEXT);
		CREATE TABLE engine_state (
			vehicle_id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, state TEXT NOT NULL, trip_id TEXT,
			geofence_id TEXT, zone_kind TEXT, zone_entered_at DATETIME, confirmed_at DATETIME,
			exit_started_at DATETIME, last_fix_at DATETIME, last_lat REAL, last_lng REAL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE geofence_events (
			id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT, trip_id TEXT, geofence_id TEXT,
			zone_kind TEXT, event_type TEXT, alert_type TEXT, severity TEXT, latitude REAL,
			longitude REAL, details TEXT, created_at DATETIME);`)
	require.NoError(t, err)
	return db
}

func TestDwellWorker_TenantThresholds(t *testing.T) {
	for _, tc := range []struct {
		name, key, initial, global, a, b, wantA, wantB string
		metres                                         float64
	}{
		{"debounce", application.ConfigDwellDebounceSeconds, domain.StateEntering, "60", "2", "90", domain.StateInside, domain.StateEntering, 30},
		{"buffer", application.ConfigBufferMetres, domain.StateOutside, "20", "10", "40", domain.StateOutside, domain.StateEntering, 130},
		{"hysteresis", application.ConfigHysteresisMetres, domain.StateInside, "25", "5", "35", domain.StateInside, domain.StateLeaving, 80},
	} {
		for _, order := range [][]string{{"tenant-a", "tenant-b"}, {"tenant-b", "tenant-a"}} {
			t.Run(tc.name+"/"+order[0], func(t *testing.T) {
				db := newThresholdFixture(t)
				ctx := context.Background()
				w := NewDwellWorker(db, uow.NewSQLUnitOfWork(db), application.NewConfigReader(db), nil, slog.New(slog.DiscardHandler))
				w.tenantID = "tenant-poll"
				_, err := db.ExecContext(ctx, `INSERT INTO company_config (tenant_id, key, value) VALUES (?, ?, ?)`, w.tenantID, tc.key, tc.global)
				require.NoError(t, err)
				t0 := time.Date(2026, 9, 18, 12, 0, 0, 123456000, time.UTC)
				zones := sqlrepo.NewGeofenceRepository(db)
				states := sqlrepo.NewEngineStateRepository(db)
				values := map[string]string{"tenant-a": tc.a, "tenant-b": tc.b}
				want := map[string]string{"tenant-a": tc.wantA, "tenant-b": tc.wantB}
				var fixes []domain.Fix
				for _, tenant := range order {
					vehicle := "vehicle-" + tenant
					_, err = db.ExecContext(ctx, `INSERT INTO vehicles (id, tenant_id) VALUES (?, ?)`, vehicle, tenant)
					require.NoError(t, err)
					_, err = db.ExecContext(ctx, `INSERT INTO company_config (tenant_id, key, value) VALUES (?, ?, ?)`, tenant, tc.key, values[tenant])
					require.NoError(t, err)
					zone := domain.Geofence{
						ID: "zone-" + tenant, TenantID: tenant, Name: "depot", Kind: domain.KindDepot,
						Shape: domain.ShapeCircle, CenterLat: 12.97, CenterLng: 77.59, RadiusM: 100, IsActive: true,
					}
					require.NoError(t, zones.Insert(ctx, zone))
					state := domain.EngineState{
						VehicleID: vehicle, TenantID: tenant, State: tc.initial,
						GeofenceID: &zone.ID, ZoneKind: &zone.Kind, ZoneEnteredAt: &t0,
						LastFixAt: t0, LastLat: zone.CenterLat, LastLng: zone.CenterLng,
					}
					require.NoError(t, states.Upsert(ctx, state))
					stored, err := states.GetByVehicle(ctx, tenant, vehicle)
					require.NoError(t, err)
					require.True(t, stored.LastFixAt.Equal(t0))
					lat, lng := northOf(zone.CenterLat, zone.CenterLng, tc.metres)
					fixes = append(fixes, domain.Fix{VehicleID: vehicle, Timestamp: t0.Add(10 * time.Second), Latitude: lat, Longitude: lng})
				}
				w.fixes = &mockFixRepo{fixes: fixes, db: db}
				handled, err := w.Tick(ctx)
				require.NoError(t, err)
				require.Equal(t, 2, handled)
				for _, tenant := range order {
					state, err := states.GetByVehicle(ctx, tenant, "vehicle-"+tenant)
					require.NoError(t, err)
					assert.Equal(t, want[tenant], state.State, "tenant %s", tenant)
					assert.Equal(t, tenant, state.TenantID)
					assert.True(t, state.LastFixAt.Equal(t0.Add(10*time.Second)))
				}
			})
		}
	}
}
