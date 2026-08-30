package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/alerts/channels"
	alertpipeline "transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/handlers"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

type mockChannel struct {
	name string
	sent []channels.Message
}

func (m *mockChannel) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockChannel) Send(ctx context.Context, msg channels.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

func setupSOSTestEnvironment(t *testing.T) (*sql.DB, *handlers.App, *events.InMemoryBus, *alertpipeline.Engine, *mockChannel) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, migrationsDir))

	// Seed tenant
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1', 'Test Tenant 1', 'tenant-1')`)

	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{}
	svcs := service.NewServices(repo, cfg, nil, bus)

	app := handlers.NewApp(svcs, cfg, nil, db, nil, nil)

	// Set up alert engine with mock channel provider
	alertRepo := alertsqlite.NewAlertRepository(db)
	mockInApp := &mockChannel{}
	chanMap := map[string]channels.Provider{
		"in_app":   mockInApp,
		"telegram": mockInApp,
	}
	engine := alertpipeline.NewEngine(alertRepo, chanMap, nil)

	bus.Subscribe(events.SOSEvent, func(ctx context.Context, e events.Event) error {
		return engine.ProcessEvent(ctx, e)
	})

	return db, app, bus, engine, mockInApp
}

func TestDriverSOS_EndToEndLifecycle(t *testing.T) {
	db, app, _, _, mockInApp := setupSOSTestEnvironment(t)
	defer db.Close()

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")

	// 1. Seed Driver, Vehicle, Route, Booking, Trip
	driverID := "drv-sos-1"
	vehicleID := "veh-sos-1"
	tripID := "trip-sos-1"

	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, tenant_id, first_name, last_name, phone, license_number, license_expiry, pan, status)
		VALUES (?, ?, 'tenant-1', 'Suresh', 'Singh', '9811122233', 'DL-SOS-001', '2029-01-01', 'ABCDE9999F', 'available')`,
		driverID, driverID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, tenant_id, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES (?, 'DL01SOS01', 'tenant-1', 'DL01SOS01', 'truck', 10000, '2028-01-01', '2028-01-01', '2028-01-01', 'running')`,
		vehicleID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, trip_number, tenant_id, driver_id, vehicle_id, route_id, departure_time, status)
		VALUES (?, 'TRIP-SOS-001', 'tenant-1', ?, ?, 'route-1', datetime('now'), 'in_transit')`,
		tripID, driverID, vehicleID)
	require.NoError(t, err)

	// 2. Trigger SOS HTTP Request from Driver Mobile App
	sosPayload := handlers.SOSRequest{
		TripID:       tripID,
		VehicleID:    vehicleID,
		Latitude:     28.5355,
		Longitude:    77.3910,
		BatteryLevel: 82.5,
		Reason:       "Vehicle breakdown / suspicious activity",
		TriggeredAt:  time.Now().UTC(),
	}
	body, err := json.Marshal(sosPayload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sos", bytes.NewReader(body))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Driver-ID", driverID)

	rec := httptest.NewRecorder()
	app.SOS.TriggerSOS(rec, req)

	// Assert HTTP response
	require.Equal(t, http.StatusCreated, rec.Code)
	var resp handlers.SOSResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "acknowledged", resp.Status)
	assert.Equal(t, driverID, resp.DriverID)
	assert.NotEmpty(t, resp.SOSID)

	// 3. Assert Outbox Event Persisted
	var outboxCount int
	var outboxPayload string
	err = db.QueryRow(`SELECT COUNT(*), payload FROM outbox_events WHERE event_type = ?`, events.SOSEvent).Scan(&outboxCount, &outboxPayload)
	require.NoError(t, err)
	assert.Equal(t, 1, outboxCount)
	assert.Contains(t, outboxPayload, "drv-sos-1")
	assert.Contains(t, outboxPayload, "veh-sos-1")

	// 4. Assert Canonical Blocker Alert Created by Alert Engine
	var alertCount int
	var alertTitle, alertSeverity, alertSource string
	err = db.QueryRow(`SELECT COUNT(*), title, severity, source FROM alerts WHERE alert_type = 'sos'`).Scan(&alertCount, &alertTitle, &alertSeverity, &alertSource)
	require.NoError(t, err)
	assert.Equal(t, 1, alertCount)
	assert.Equal(t, "blocker", alertSeverity)
	assert.Equal(t, "sos", alertSource)
	assert.Contains(t, alertTitle, "veh-sos-1")

	// 5. Assert Channels Dispatched
	assert.GreaterOrEqual(t, len(mockInApp.sent), 1)
	assert.Contains(t, mockInApp.sent[0].Title, "SOS")

	// 6. Assert Cooldown / Dedup: Rapid consecutive trigger within 60s increments occurrences, does not duplicate alert
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/sos", bytes.NewReader(body))
	req2 = req2.WithContext(ctx)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Driver-ID", driverID)

	app.SOS.TriggerSOS(rec2, req2)
	assert.Equal(t, http.StatusCreated, rec2.Code)

	var alertCountAfter int
	var occurrences int
	err = db.QueryRow(`SELECT COUNT(*), occurrences FROM alerts WHERE alert_type = 'sos'`).Scan(&alertCountAfter, &occurrences)
	require.NoError(t, err)
	assert.Equal(t, 1, alertCountAfter, "duplicate SOS alert must not be created within cooldown window")
	assert.Equal(t, 2, occurrences, "occurrences counter must increment on dedup")
}
