package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/agent"
	alertchannels "transport-app/internal/alerts/channels"
	alertdomain "transport-app/internal/alerts/domain"
	alertpipeline "transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

type recordingChannel struct {
	name string
	sent []alertchannels.Message
}

func (r *recordingChannel) Name() string { return r.name }
func (r *recordingChannel) Send(ctx context.Context, msg alertchannels.Message) error {
	r.sent = append(r.sent, msg)
	return nil
}

func TestSubTask4D_MasterIntegrationSuite(t *testing.T) {
	dbConn, svcs, eventBus, unitOfWork, _ := setupComplianceTestEnv(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	now := time.Now().UTC()

	alertRepo := alertsqlite.NewAlertRepository(dbConn)
	inAppMock := &recordingChannel{name: "in_app"}
	tgMock := &recordingChannel{name: "telegram"}
	chMap := map[string]alertchannels.Provider{
		"in_app":   inAppMock,
		"telegram": tgMock,
	}

	engine := alertpipeline.NewEngine(alertRepo, chMap, nil)
	eventBus.Subscribe("SOSEvent", func(ctx context.Context, e events.Event) error {
		return engine.ProcessEvent(ctx, e)
	})

	// -------------------------------------------------------------
	// 1. SOS Flow: SOSEvent creates blocker alert & fans out
	// -------------------------------------------------------------
	sosEv := events.Event{
		Type: "SOSEvent",
		Payload: map[string]interface{}{
			"VehicleID":  "veh-sos-e2e",
			"DriverID":   "drv-sos-e2e",
			"Latitude":   19.0760,
			"Longitude":  72.8777,
			"OccurredAt": now.Format(time.RFC3339),
		},
	}
	eventBus.Publish(ctx, sosEv)

	var sosAlertID, sosSeverity, sosStatus string
	err := dbConn.QueryRow(`
		SELECT id, severity, status FROM alerts 
		WHERE dedup_key = 'sos:veh-sos-e2e'`).Scan(&sosAlertID, &sosSeverity, &sosStatus)
	require.NoError(t, err)
	assert.Equal(t, alertdomain.SeverityBlocker, sosSeverity)
	assert.Equal(t, alertdomain.StatusOpen, sosStatus)

	assert.NotEmpty(t, inAppMock.sent)
	assert.NotEmpty(t, tgMock.sent)
	assert.Equal(t, alertdomain.SeverityBlocker, inAppMock.sent[0].Severity)

	// -------------------------------------------------------------
	// 2. Agent Tools Integration: get_open_alerts & extend_ewaybill
	// -------------------------------------------------------------
	mutating := agent.MutatingTools()
	assert.Contains(t, mutating, "extend_ewaybill")

	toolEnv := &agent.ToolEnv{
		Services: svcs,
		UserID:   "admin-101",
		UserName: "Admin",
	}
	tools := agent.RegisterTools(toolEnv)

	var getOpenAlertsTool, extendEwbTool *agent.RegisteredTool
	for _, tool := range tools {
		if tool.Name == "get_open_alerts" {
			getOpenAlertsTool = tool
		}
		if tool.Name == "extend_ewaybill" {
			extendEwbTool = tool
		}
	}
	require.NotNil(t, getOpenAlertsTool)
	require.NotNil(t, extendEwbTool)

	// Test get_open_alerts
	res, err := getOpenAlertsTool.Handler(ctx, json.RawMessage(`{"severity":"blocker"}`))
	require.NoError(t, err)
	assert.Contains(t, res, "veh-sos-e2e")
	assert.Contains(t, res, "sos")

	// -------------------------------------------------------------
	// 3. Keyword Routing to Ops sub-agent
	// -------------------------------------------------------------
	orch := agent.NewOrchestrator(nil, toolEnv, nil, 5)
	r1, _ := orch.Route(ctx, "Show me open alerts for vehicle 99")
	assert.Equal(t, "ops", r1)
	r2, _ := orch.Route(ctx, "Need to extend eway bill for trip 101")
	assert.Equal(t, "ops", r2)
	r3, _ := orch.Route(ctx, "Incoming SOS from driver")
	assert.Equal(t, "ops", r3)

	// -------------------------------------------------------------
	// 4. E-Way Bill Service: Generate Part-A & lookup (canonical path)
	// -------------------------------------------------------------
	ewbSvc := ewaybill.NewEWayBillService(dbConn, eventBus, intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true}), nil, ewaybill.Config{Enabled: true})

	// Seed Route, Vehicle, Trip
	routeID := "rt-ewb-1"
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES (?, 'Mumbai', 'Goa', 600, 12, 18000, 'tenant-1')`, routeID)
	require.NoError(t, err)

	tripID := "trp-ewb-1"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-EWB-01', ?, datetime('now'), 'started', 'tenant-1')`, tripID, routeID)
	require.NoError(t, err)

	// Step 1: Service generates EWB Part-A for the started trip
	rec, err := ewbSvc.GeneratePartA(ctx, ewaybill.GeneratePartARequest{
		TripID:        tripID,
		DocType:       "INV",
		DocNo:         "INV-EWB-01",
		DocDate:       now.Format("2006-01-02"),
		FromGSTIN:     "27AAAAA0000A1Z5",
		ToGSTIN:       "24BBBBB0000B1Z5",
		FromPlace:     "Mumbai",
		FromStateCode: "27",
		ToPlace:       "Goa",
		ToStateCode:   "30",
		GoodsValue:    180000,
		Distance:      600,
		GenMode:       "MANUAL",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, rec.EwbNumber)
	assert.Equal(t, "active", rec.Status)

	fetched, err := ewbSvc.GetByTrip(ctx, tripID)
	require.NoError(t, err)
	assert.Equal(t, rec.EwbNumber, fetched.EwbNumber)

	// -------------------------------------------------------------
	// 5. TripAssignedEvent: Published from Path A & Path B
	// -------------------------------------------------------------
	// Path A: Vertical slice AssignVehicle
	vehID := "veh-ewb-1"
	futureDate := now.AddDate(1, 0, 0).Format("2006-01-02")
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRK-999', 'MH04AB9999', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, vehID, futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)

	drvID := "drv-ewb-1"
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES (?, 'DRV-999', 'Ramesh', 'Kumar', '+919999999999', 'DL-999', ?, 'available', 'tenant-1')`, drvID, futureDate)
	require.NoError(t, err)

	trip2ID := "trp-ewb-2"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-EWB-02', ?, datetime('now'), 'scheduled', 'tenant-1')`, trip2ID, routeID)
	require.NoError(t, err)

	clk := &realClock{}
	assignDriverUC := tripapp.NewAssignDriverUseCase(unitOfWork, clk)
	err = assignDriverUC.Execute(ctx, tripapp.AssignDriverCommand{
		TripID:   tripagg.TripID(trip2ID),
		DriverID: drvID,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	assignVehUC := tripapp.NewAssignVehicleUseCase(unitOfWork, clk)
	err = assignVehUC.Execute(ctx, tripapp.AssignVehicleCommand{
		TripID:    tripagg.TripID(trip2ID),
		VehicleID: vehID,
		TenantID:  "tenant-1",
	})
	require.NoError(t, err)

	// Verify outbox recorded TripAssignedEvent
	var outboxCount int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_type = 'Trip' AND event_type = 'TripAssignedEvent'`).Scan(&outboxCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, outboxCount, 1)

	// Path B: Legacy trip_service AssignVehicle & AssignDriver publishes TripAssignedEvent
	var receivedBusEvents []string
	eventBus.Subscribe("TripAssignedEvent", func(ctx context.Context, e events.Event) error {
		receivedBusEvents = append(receivedBusEvents, e.Type)
		return nil
	})

	drv2ID := "drv-ewb-2"
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES (?, 'DRV-998', 'Suresh', 'Kumar', '+919999999998', 'DL-998', ?, 'available', 'tenant-1')`, drv2ID, futureDate)
	require.NoError(t, err)

	veh2ID := "veh-ewb-2"
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRK-998', 'MH04AB9998', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, veh2ID, futureDate, futureDate, futureDate, futureDate)
	require.NoError(t, err)

	trip3ID := "trp-ewb-3"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-EWB-03', ?, datetime('now'), 'scheduled', 'tenant-1')`, trip3ID, routeID)
	require.NoError(t, err)

	_, err = svcs.Trips.AssignDriver(ctx, domain.TripID(trip3ID), domain.DriverID(drv2ID))
	require.NoError(t, err)

	_, err = svcs.Trips.AssignVehicle(ctx, domain.TripID(trip3ID), domain.VehicleID(veh2ID))
	require.NoError(t, err)
	assert.NotEmpty(t, receivedBusEvents)

	// -------------------------------------------------------------
	// 6. Telemetry Alerts Rebuild & Service Persistence (00059)
	// -------------------------------------------------------------
	// Verify telemetry_alerts accepts 13 types and maps fuel_theft -> theft_suspicion
	alertPoints, err := svcs.Telemetry.ProcessTelemetryStream(ctx, service.TelemetryDataPoint{
		VehicleID:       domain.VehicleID(vehID),
		TripID:          ptrTo(domain.TripID(tripID)),
		DriverID:        ptrTo(domain.DriverID(drvID)),
		Latitude:        19.0760,
		Longitude:       72.8777,
		Speed:           0,
		FuelLevel:       40.0,
		IgnitionOn:      false,
		Odometer:        1500.0,
		Timestamp:       now,
		PlannedRouteLat: 20.0, // >5km deviation
		PlannedRouteLng: 74.0,
	}, 65.0 /* 25L drop */)
	require.NoError(t, err)
	assert.Len(t, alertPoints, 2)

	var persistedCount int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM telemetry_alerts WHERE vehicle_id = ? AND alert_type IN ('gps_deviation', 'theft_suspicion')`, vehID).Scan(&persistedCount)
	require.NoError(t, err)
	assert.Equal(t, 2, persistedCount)
}

func ptrTo[T any](v T) *T {
	return &v
}
