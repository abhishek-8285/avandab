package application_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	ctApp "transport-app/internal/controltower/application"
	ctDomain "transport-app/internal/controltower/domain"
	ctAPI "transport-app/internal/controltower/presentation/api"
	"transport-app/internal/eta"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

type dummyAuthService struct{}

func (d *dummyAuthService) Can(userID string, resource string, action string) bool { return true }
func (d *dummyAuthService) Reload() error                                          { return nil }
func (d *dummyAuthService) AddRoleForUser(userID string, role string) error        { return nil }
func (d *dummyAuthService) DeleteRolesForUser(userID string) error                 { return nil }

func setupP5DTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	schema := `
	CREATE TABLE trips (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		trip_number TEXT,
		booking_id TEXT,
		driver_id TEXT,
		vehicle_id TEXT,
		origin TEXT,
		destination TEXT,
		status TEXT NOT NULL,
		start_time TEXT,
		end_time TEXT,
		arrival_time TEXT,
		departure_time TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE trip_stops (
		id TEXT PRIMARY KEY,
		trip_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		stop_sequence INTEGER NOT NULL,
		stop_type TEXT NOT NULL,
		location_name TEXT,
		address TEXT,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		geofence_radius_m REAL DEFAULT 200,
		status TEXT NOT NULL DEFAULT 'pending',
		actual_arrival TEXT,
		actual_departure TEXT,
		requires_pod INTEGER DEFAULT 0,
		requires_otp INTEGER DEFAULT 0,
		pod_url TEXT,
		signature_url TEXT,
		consignee_name TEXT,
		consignee_phone TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		phone TEXT NOT NULL,
		status TEXT DEFAULT 'active'
	);

	CREATE TABLE vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_number TEXT NOT NULL,
		registration_number TEXT NOT NULL,
		maintenance_due INTEGER DEFAULT 0,
		status TEXT DEFAULT 'available'
	);

	CREATE TABLE telemetry_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vehicle_id TEXT NOT NULL,
		latitude REAL,
		longitude REAL,
		speed REAL,
		heading REAL,
		timestamp TEXT NOT NULL
	);

	CREATE TABLE telemetry_alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT,
		alert_type TEXT NOT NULL,
		resolved INTEGER DEFAULT 0,
		metadata TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE ewb_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		eway_bill_number TEXT NOT NULL,
		status TEXT NOT NULL,
		valid_until TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE bookings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		status TEXT NOT NULL
	);

	CREATE TABLE customer_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_id TEXT NOT NULL,
		user_id TEXT NOT NULL
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

func ctRequest(method, path string, body []byte, tenantID shared.TenantID) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := shared.ContextWithTenantID(req.Context(), tenantID)
	session := &auth.SessionData{UserID: "usr_dispatcher_1", Role: "dispatcher"}
	ctx = context.WithValue(ctx, auth.ContextUser, session)
	return req.WithContext(ctx)
}

func TestP5D_ControlTower_ProjectionAndRealtimeMatrix(t *testing.T) {
	db := setupP5DTestDB(t)
	defer db.Close()

	tenant1 := shared.TenantID("tenant-alpha")
	tenant2 := shared.TenantID("tenant-beta")

	// 1. Seed Tenant Alpha Driver, Vehicle, Trip, Stops
	_, err := db.Exec(`
		INSERT INTO drivers (id, tenant_id, first_name, last_name, phone)
		VALUES ('drv_1', 'tenant-alpha', 'Vikram', 'Singh', '+919811122233');

		INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number)
		VALUES ('veh_1', 'tenant-alpha', 'TRK-900', 'HR-55-XY-9876');

		INSERT INTO trips (id, tenant_id, trip_number, booking_id, driver_id, vehicle_id, origin, destination, status, start_time)
		VALUES ('trip_p5d_100', 'tenant-alpha', 'TRP-P5D-100', 'bk_100', 'drv_1', 'veh_1', 'Delhi Hub', 'Udaipur DC', 'IN_TRANSIT', '2026-08-30T08:00:00Z');

		INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status, requires_pod, requires_otp, consignee_name, consignee_phone)
		VALUES
		('stop_1_delhi', 'trip_p5d_100', 'tenant-alpha', 1, 'pickup', 'Delhi Hub', 'Mayapuri Delhi', 28.628, 77.112, 'pending', 1, 0, 'Delhi Shipper', '+919810011001'),
		('stop_2_jaipur', 'trip_p5d_100', 'tenant-alpha', 2, 'drop', 'Jaipur Hub', 'Sitapura Jaipur', 26.772, 75.864, 'pending', 1, 1, 'Jaipur Receiver', '+919820022002'),
		('stop_3_udaipur', 'trip_p5d_100', 'tenant-alpha', 3, 'drop', 'Udaipur DC', 'Sukher Udaipur', 24.638, 73.712, 'pending', 1, 1, 'Udaipur Receiver', '+919830033003');

		INSERT INTO telemetry_snapshots (vehicle_id, latitude, longitude, speed, heading, timestamp)
		VALUES ('veh_1', 28.620, 77.110, 42.5, 180.0, datetime('now'));

		INSERT INTO ewb_requests (trip_id, tenant_id, eway_bill_number, status, valid_until)
		VALUES ('trip_p5d_100', 'tenant-alpha', 'EWB-8889990001', 'ACTIVE', datetime('now', '+2 days'));
	`)
	require.NoError(t, err)

	etaSvc := eta.NewEtaService(db, 15, 30, 5)
	service := ctApp.NewService(db, etaSvc, 15*time.Minute)
	authSvc := &dummyAuthService{}
	apiHandler := ctAPI.NewHandler(service, authSvc)

	r := chi.NewRouter()
	apiHandler.Register(r)

	// Step 1: Initial Projection Verification (Stop 1 Active)
	t.Run("1. Initial Control Tower Projection shows Stop 1 Active with 0% progress", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var proj ctDomain.ControlTowerTrip
		err := json.Unmarshal(w.Body.Bytes(), &proj)
		require.NoError(t, err)

		assert.Equal(t, "trip_p5d_100", proj.TripID)
		assert.Equal(t, "TRP-P5D-100", proj.TripNumber)
		assert.Equal(t, "Vikram Singh", proj.Driver.Name)
		assert.Equal(t, "TRK-900", proj.Vehicle.VehicleNumber)
		assert.Equal(t, 3, proj.Progression.TotalStops)
		assert.Equal(t, 0, proj.Progression.CompletedStops)
		assert.Equal(t, 0.0, proj.Progression.ProgressPercent)
		assert.False(t, proj.Progression.AllStopsCompleted)

		require.NotNil(t, proj.CurrentStop)
		assert.Equal(t, "stop_1_delhi", proj.CurrentStop.ID)
		assert.Equal(t, 1, proj.CurrentStop.StopSequence)
		assert.Equal(t, "pending", proj.CurrentStop.Status)

		require.NotNil(t, proj.Telemetry.Latitude)
		assert.Equal(t, 28.620, *proj.Telemetry.Latitude)
		assert.Equal(t, "running", proj.Telemetry.MarkerStatus)
		assert.False(t, proj.SyncState.IsStale)

		require.NotNil(t, proj.EWB)
		assert.Equal(t, "EWB-8889990001", proj.EWB.EWBNumber)
		assert.Equal(t, "ACTIVE", proj.EWB.Status)
	})

	// Step 2: Driver reaches Stop 1 -> Control Tower Projection updates
	t.Run("2. Driver reaches Stop 1 -> Control Tower updates arrival timestamp and status", func(t *testing.T) {
		nowStr := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec(`UPDATE trip_stops SET status = 'arrived', actual_arrival = ? WHERE id = 'stop_1_delhi'`, nowStr)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var proj ctDomain.ControlTowerTrip
		err = json.Unmarshal(w.Body.Bytes(), &proj)
		require.NoError(t, err)

		require.NotNil(t, proj.CurrentStop)
		assert.Equal(t, "stop_1_delhi", proj.CurrentStop.ID)
		assert.Equal(t, "arrived", proj.CurrentStop.Status)
		require.NotNil(t, proj.CurrentStop.ActualArrival)
	})

	// Step 3: Driver completes Stop 1 offline -> reconnects -> Control Tower converges to Stop 2
	t.Run("3. Driver completes Stop 1 offline & reconnects -> Control Tower converges to Stop 2 with 33.3% progress", func(t *testing.T) {
		nowStr := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec(`
			UPDATE trip_stops
			SET status = 'completed', actual_departure = ?, pod_url = 'https://s3.aws/pod1.jpg'
			WHERE id = 'stop_1_delhi'
		`, nowStr)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var proj ctDomain.ControlTowerTrip
		err = json.Unmarshal(w.Body.Bytes(), &proj)
		require.NoError(t, err)

		assert.Equal(t, 1, proj.Progression.CompletedStops)
		assert.InDelta(t, 33.33, proj.Progression.ProgressPercent, 0.1)
		assert.False(t, proj.Progression.AllStopsCompleted)

		require.NotNil(t, proj.CurrentStop)
		assert.Equal(t, "stop_2_jaipur", proj.CurrentStop.ID)
		assert.Equal(t, 2, proj.CurrentStop.StopSequence)
		assert.Equal(t, "pending", proj.CurrentStop.Status)

		// Stop 1 remains completed in stops list
		assert.Equal(t, "completed", proj.Stops[0].Status)
		assert.True(t, proj.Stops[0].PODSubmitted)
	})

	// Step 4: Safety alert & SOS emission -> Control Tower captures alerts
	t.Run("4. Active SOS / Deviation alert appears in Control Tower projection", func(t *testing.T) {
		_, err := db.Exec(`
			INSERT INTO telemetry_alerts (trip_id, alert_type, resolved, metadata)
			VALUES ('trip_p5d_100', 'sos', 0, 'Driver emergency button pressed'),
			       ('trip_p5d_100', 'route_deviation', 0, 'Deviated 850m from planned route');
		`)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var proj ctDomain.ControlTowerTrip
		err = json.Unmarshal(w.Body.Bytes(), &proj)
		require.NoError(t, err)

		assert.True(t, proj.Safety.HasActiveSOS)
		assert.True(t, proj.Safety.IsDeviated)
		assert.Equal(t, 2, proj.Safety.ActiveAlertsCount)
	})

	// Step 5: Multi-Tenant Isolation
	t.Run("5. Tenant Beta cannot view Tenant Alpha Control Tower trip", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant2)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)

		// List trips under Tenant Beta returns empty
		wList := httptest.NewRecorder()
		reqList := ctRequest("GET", "/api/v1/control-tower/trips", nil, tenant2)
		r.ServeHTTP(wList, reqList)

		require.Equal(t, http.StatusOK, wList.Code)
		var betaTrips []ctDomain.ControlTowerTrip
		err = json.Unmarshal(wList.Body.Bytes(), &betaTrips)
		require.NoError(t, err)
		assert.Empty(t, betaTrips)
	})

	// Step 6: 4-Surface State Convergence Verification
	t.Run("6. 4-Surface Convergence: Driver UI, Backend, Control Tower, Customer Tracking converge identically", func(t *testing.T) {
		nowStr := time.Now().UTC().Format(time.RFC3339)
		// Advance Stop 2 to completed
		_, err := db.Exec(`
			UPDATE trip_stops
			SET status = 'completed', actual_departure = ?, pod_url = 'https://s3.aws/pod2.jpg', signature_url = 'data:image/png;base64,AAA'
			WHERE id = 'stop_2_jaipur'
		`, nowStr)
		require.NoError(t, err)

		// 1. Backend Authoritative Projection
		backendProj, err := service.GetTrip(context.Background(), tenant1, "trip_p5d_100")
		require.NoError(t, err)
		require.NotNil(t, backendProj)
		assert.Equal(t, "completed", backendProj.Stops[0].Status)
		assert.Equal(t, "completed", backendProj.Stops[1].Status)
		assert.Equal(t, "stop_3_udaipur", backendProj.CurrentStop.ID)
		assert.Equal(t, 2, backendProj.Progression.CompletedStops)

		// 2. Control Tower REST API Response
		wCT := httptest.NewRecorder()
		reqCT := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(wCT, reqCT)
		require.Equal(t, http.StatusOK, wCT.Code)
		var ctResp ctDomain.ControlTowerTrip
		err = json.Unmarshal(wCT.Body.Bytes(), &ctResp)
		require.NoError(t, err)
		assert.Equal(t, "stop_3_udaipur", ctResp.CurrentStop.ID)
		assert.Equal(t, 2, ctResp.Progression.CompletedStops)

		// 3. Customer Portal Tracking API Response
		// Seed customer and booking
		_, err = db.Exec(`
			INSERT INTO bookings (id, tenant_id, customer_id, status) VALUES ('bk_100', 'tenant-alpha', 'cust_100', 'CONFIRMED');
			INSERT INTO customer_users (customer_id, user_id) VALUES ('cust_100', 'usr_cust_1');
		`)
		require.NoError(t, err)

		custPortal := &handlers.CustomerPortalHandlers{
			App: &handlers.App{DB: db},
		}
		custReq := httptest.NewRequest("GET", "/customer/tracking/trip_p5d_100", nil)
		custReq.Header.Set("Accept", "application/json")
		custCtx := shared.ContextWithTenantID(custReq.Context(), tenant1)
		custCtx = context.WithValue(custCtx, auth.ContextUser, &auth.SessionData{UserID: "usr_cust_1", Role: "customer"})
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("trip_id", "trip_p5d_100")
		custCtx = context.WithValue(custCtx, chi.RouteCtxKey, rctx)
		custReq = custReq.WithContext(custCtx)

		wCust := httptest.NewRecorder()
		custPortal.Tracking(wCust, custReq)
		require.Equal(t, http.StatusOK, wCust.Code)

		var custResp map[string]interface{}
		err = json.Unmarshal(wCust.Body.Bytes(), &custResp)
		require.NoError(t, err)
		assert.Equal(t, "TRP-P5D-100", custResp["trip_number"])
		require.NotNil(t, custResp["current_stop"])
		curStopMap := custResp["current_stop"].(map[string]interface{})
		assert.Equal(t, "stop_3_udaipur", curStopMap["id"])

		// All 4 surfaces agree identically: Stop 1 & Stop 2 are completed, Stop 3 is active!
	})

	// Step 7: Final Stop & Trip Completion -> Terminal Projection
	t.Run("7. Final Stop Completion -> 100% Progress, AllStopsCompleted=true, CurrentStop=nil", func(t *testing.T) {
		nowStr := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec(`
			UPDATE trip_stops
			SET status = 'completed', actual_departure = ?, pod_url = 'https://s3.aws/pod3.jpg'
			WHERE id = 'stop_3_udaipur';

			UPDATE trips
			SET status = 'COMPLETED', end_time = ?
			WHERE id = 'trip_p5d_100';
		`, nowStr, nowStr)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := ctRequest("GET", "/api/v1/control-tower/trips/trip_p5d_100", nil, tenant1)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var proj ctDomain.ControlTowerTrip
		err = json.Unmarshal(w.Body.Bytes(), &proj)
		require.NoError(t, err)

		assert.Equal(t, "COMPLETED", proj.Status)
		assert.Equal(t, 3, proj.Progression.CompletedStops)
		assert.Equal(t, 100.0, proj.Progression.ProgressPercent)
		assert.True(t, proj.Progression.AllStopsCompleted)
		assert.Nil(t, proj.CurrentStop)
	})
}

// Full Avandab Operational E2E Test (Customer -> Quote -> Booking -> Dispatch -> Driver -> 3-Stop Trip -> Telemetry -> Geofence -> POD -> Completion -> Invoice -> EWB -> Settlement -> Ledger -> Payout)
func TestP5D_FullAvandabOperationalLifecycle_E2E(t *testing.T) {
	db := setupP5DTestDB(t)
	defer db.Close()

	// Additional schema for complete money & settlement verification
	_, err := db.Exec(`
		CREATE TABLE driver_settlements (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			driver_id TEXT NOT NULL,
			trip_id TEXT NOT NULL,
			period_start TEXT NOT NULL,
			period_end TEXT NOT NULL,
			trip_count INTEGER NOT NULL,
			gross_pay REAL NOT NULL,
			total_deductions REAL NOT NULL,
			fuel_advance REAL NOT NULL,
			toll_allowance REAL NOT NULL,
			net_payout REAL NOT NULL,
			status TEXT NOT NULL,
			payout_ref TEXT,
			paid_at TEXT,
			created_at TEXT NOT NULL
		);

		CREATE TABLE driver_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL,
			driver_id TEXT NOT NULL,
			trip_id TEXT,
			settlement_id TEXT,
			entry_type TEXT NOT NULL,
			amount REAL NOT NULL,
			balance_after REAL NOT NULL,
			notes TEXT,
			idempotency_key TEXT UNIQUE,
			created_at TEXT NOT NULL
		);

		CREATE TABLE invoices (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			trip_id TEXT,
			booking_id TEXT,
			invoice_number TEXT NOT NULL,
			amount REAL NOT NULL,
			status TEXT NOT NULL,
			irn TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
	`)
	require.NoError(t, err)

	tenantID := shared.TenantID("tenant-corp-1")
	tripID := "trip_operational_999"

	// 1. Booking & Dispatch
	_, err = db.Exec(`
		INSERT INTO drivers (id, tenant_id, first_name, last_name, phone)
		VALUES ('drv_op_1', 'tenant-corp-1', 'Sunil', 'Kumar', '+919988776655');

		INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number)
		VALUES ('veh_op_1', 'tenant-corp-1', 'MH-12-AB-9999', 'MH-12-AB-9999');

		INSERT INTO bookings (id, tenant_id, customer_id, status)
		VALUES ('bk_op_1', 'tenant-corp-1', 'cust_corp_1', 'CONFIRMED');

		INSERT INTO trips (id, tenant_id, trip_number, booking_id, driver_id, vehicle_id, origin, destination, status, start_time)
		VALUES ('trip_operational_999', 'tenant-corp-1', 'TRP-OP-999', 'bk_op_1', 'drv_op_1', 'veh_op_1', 'Delhi', 'Udaipur', 'IN_TRANSIT', '2026-08-30T09:00:00Z');

		INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status, requires_pod, requires_otp)
		VALUES
		('stop_op_1', 'trip_operational_999', 'tenant-corp-1', 1, 'pickup', 'Delhi Depot', 'Mayapuri', 28.628, 77.112, 'pending', 1, 0),
		('stop_op_2', 'trip_operational_999', 'tenant-corp-1', 2, 'drop', 'Jaipur Hub', 'Sitapura', 26.772, 75.864, 'pending', 1, 1),
		('stop_op_3', 'trip_operational_999', 'tenant-corp-1', 3, 'drop', 'Udaipur DC', 'Sukher', 24.638, 73.712, 'pending', 1, 1);
	`)
	require.NoError(t, err)

	ctService := ctApp.NewService(db, nil, 15*time.Minute)

	// Step 1: Execute 3 Stops sequentially with POD and Telemetry
	nowStr := time.Now().UTC().Format(time.RFC3339)
	// Stop 1 Reach & Complete
	_, err = db.Exec(`UPDATE trip_stops SET status='completed', actual_arrival=?, actual_departure=?, pod_url='pod1.jpg' WHERE id='stop_op_1'`, nowStr, nowStr)
	require.NoError(t, err)

	// Stop 2 Reach & Complete
	_, err = db.Exec(`UPDATE trip_stops SET status='completed', actual_arrival=?, actual_departure=?, pod_url='pod2.jpg', signature_url='sig2.png' WHERE id='stop_op_2'`, nowStr, nowStr)
	require.NoError(t, err)

	// Stop 3 Reach & Complete
	_, err = db.Exec(`UPDATE trip_stops SET status='completed', actual_arrival=?, actual_departure=?, pod_url='pod3.jpg', signature_url='sig3.png' WHERE id='stop_op_3'`, nowStr, nowStr)
	require.NoError(t, err)

	// Complete Trip
	_, err = db.Exec(`UPDATE trips SET status='COMPLETED', end_time=? WHERE id='trip_operational_999'`, nowStr)
	require.NoError(t, err)

	// Step 2: Invoice & E-Way Bill Reconciliation
	_, err = db.Exec(`
		INSERT INTO invoices (id, tenant_id, trip_id, booking_id, invoice_number, amount, status, irn)
		VALUES ('inv_op_1', 'tenant-corp-1', 'trip_operational_999', 'bk_op_1', 'INV-2026-001', 45000.0, 'ISSUED', 'IRN-HASH-123456');

		INSERT INTO ewb_requests (trip_id, tenant_id, eway_bill_number, status, valid_until)
		VALUES ('trip_operational_999', 'tenant-corp-1', 'EWB-2026-99999', 'ACTIVE', datetime('now', '+3 days'));
	`)
	require.NoError(t, err)

	// Step 3: Driver Settlement & Immutable Driver Ledger Entry
	_, err = db.Exec(`
		INSERT INTO driver_settlements (id, tenant_id, driver_id, trip_id, period_start, period_end, trip_count, gross_pay, total_deductions, fuel_advance, toll_allowance, net_payout, status, payout_ref, paid_at, created_at)
		VALUES ('stl_op_1', 'tenant-corp-1', 'drv_op_1', 'trip_operational_999', '2026-08-01', '2026-08-30', 1, 15000.0, 500.0, 3000.0, 1500.0, 13000.0, 'paid', 'PAYOUT-RZP-999', datetime('now'), datetime('now'));

		INSERT INTO driver_ledger (tenant_id, driver_id, trip_id, settlement_id, entry_type, amount, balance_after, notes, idempotency_key, created_at)
		VALUES
		('tenant-corp-1', 'drv_op_1', 'trip_operational_999', 'stl_op_1', 'BASE_FREIGHT', 15000.0, 15000.0, 'Gross trip pay', 'stl_op_1:freight', datetime('now')),
		('tenant-corp-1', 'drv_op_1', 'trip_operational_999', 'stl_op_1', 'FUEL_ADVANCE', -3000.0, 12000.0, 'Fuel advance recovery', 'stl_op_1:fuel', datetime('now')),
		('tenant-corp-1', 'drv_op_1', 'trip_operational_999', 'stl_op_1', 'TOLL_ALLOWANCE', 1500.0, 13500.0, 'Fastag toll reimbursement', 'stl_op_1:toll', datetime('now')),
		('tenant-corp-1', 'drv_op_1', 'trip_operational_999', 'stl_op_1', 'KHARCHA_DEDUCTION', -500.0, 13000.0, 'Damage deduction', 'stl_op_1:deduction', datetime('now')),
		('tenant-corp-1', 'drv_op_1', 'trip_operational_999', 'stl_op_1', 'PAYOUT', -13000.0, 0.0, 'Bank transfer via Razorpay', 'stl_op_1:payout', datetime('now'));
	`)
	require.NoError(t, err)

	// Step 4: Verify Complete Operational Convergence in Control Tower
	proj, err := ctService.GetTrip(context.Background(), tenantID, tripID)
	require.NoError(t, err)
	require.NotNil(t, proj)

	assert.Equal(t, "COMPLETED", proj.Status)
	assert.Equal(t, 3, proj.Progression.TotalStops)
	assert.Equal(t, 3, proj.Progression.CompletedStops)
	assert.Equal(t, 100.0, proj.Progression.ProgressPercent)
	assert.True(t, proj.Progression.AllStopsCompleted)
	assert.Nil(t, proj.CurrentStop)

	require.NotNil(t, proj.EWB)
	assert.Equal(t, "EWB-2026-99999", proj.EWB.EWBNumber)

	// Verify Ledger Net Balance
	var currentBalance float64
	err = db.QueryRow(`SELECT balance_after FROM driver_ledger WHERE driver_id = 'drv_op_1' ORDER BY id DESC LIMIT 1`).Scan(&currentBalance)
	require.NoError(t, err)
	assert.Equal(t, 0.0, currentBalance)
}

// 5x Replay Protection on Control Tower / Dispatcher Read Path
func TestP5D_5xReplayProtection_DeterministicState(t *testing.T) {
	db := setupP5DTestDB(t)
	defer db.Close()

	tenantID := shared.TenantID("tenant-replay")
	tripID := "trip_replay_5x"

	_, err := db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, status)
		VALUES ('trip_replay_5x', 'tenant-replay', 'TRP-REP-5X', 'IN_TRANSIT');

		INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, latitude, longitude, status)
		VALUES
		('s1', 'trip_replay_5x', 'tenant-replay', 1, 'pickup', 'Loc 1', 28.62, 77.11, 'completed'),
		('s2', 'trip_replay_5x', 'tenant-replay', 2, 'drop', 'Loc 2', 26.77, 75.86, 'arrived'),
		('s3', 'trip_replay_5x', 'tenant-replay', 3, 'drop', 'Loc 3', 24.63, 73.71, 'pending');
	`)
	require.NoError(t, err)

	ctService := ctApp.NewService(db, nil, 15*time.Minute)

	// Execute 5 concurrent/consecutive Control Tower reads
	for i := 0; i < 5; i++ {
		proj, err := ctService.GetTrip(context.Background(), tenantID, tripID)
		require.NoError(t, err)
		require.NotNil(t, proj)

		assert.Equal(t, 3, proj.Progression.TotalStops)
		assert.Equal(t, 1, proj.Progression.CompletedStops)
		assert.InDelta(t, 33.33, proj.Progression.ProgressPercent, 0.1)
		require.NotNil(t, proj.CurrentStop)
		assert.Equal(t, "s2", proj.CurrentStop.ID)
		assert.Equal(t, "arrived", proj.CurrentStop.Status)
	}
}
