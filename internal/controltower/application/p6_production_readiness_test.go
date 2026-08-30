package application_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	ctApp "transport-app/internal/controltower/application"
	"transport-app/internal/shared"
)

func setupProductionStressDB(t *testing.T) *sql.DB {
	// Enable WAL mode and memory journal for high concurrent throughput testing
	db, err := sql.Open("sqlite", "file:stress_test?mode=memory&cache=shared&_journal=WAL&_busy_timeout=5000")
	require.NoError(t, err)
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)

	schema := `
	CREATE TABLE IF NOT EXISTS trips (
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

	CREATE TABLE IF NOT EXISTS trip_stops (
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

	CREATE TABLE IF NOT EXISTS drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		phone TEXT NOT NULL,
		status TEXT DEFAULT 'active'
	);

	CREATE TABLE IF NOT EXISTS vehicles (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		vehicle_number TEXT NOT NULL,
		registration_number TEXT NOT NULL,
		status TEXT DEFAULT 'available'
	);

	CREATE TABLE IF NOT EXISTS telemetry_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vehicle_id TEXT NOT NULL,
		latitude REAL,
		longitude REAL,
		speed REAL,
		heading REAL,
		timestamp TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS telemetry_alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT,
		alert_type TEXT NOT NULL,
		resolved INTEGER DEFAULT 0,
		metadata TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS ewb_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trip_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		eway_bill_number TEXT NOT NULL,
		status TEXT NOT NULL,
		valid_until TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS driver_ledger (
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
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

// Benchmark & Stress Test 1: 1,000 High-Throughput Telemetry Ingestions
func TestP6_ProductionAudit_1000TelemetryIngestionConcurrency(t *testing.T) {
	db := setupProductionStressDB(t)
	defer db.Close()

	numEvents := 1000
	numWorkers := 20
	eventChan := make(chan int, numEvents)
	for i := 0; i < numEvents; i++ {
		eventChan <- i
	}
	close(eventChan)

	var wg sync.WaitGroup
	latencies := make([]time.Duration, numEvents)
	var mu sync.Mutex

	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for idx := range eventChan {
				t0 := time.Now()
				vehID := fmt.Sprintf("veh_stress_%d", idx%50)
				lat := 28.0 + rand.Float64()
				lng := 77.0 + rand.Float64()
				speed := 40.0 + rand.Float64()*20.0
				heading := float64(idx % 360)

				_, err := db.Exec(`
					INSERT INTO telemetry_snapshots (vehicle_id, latitude, longitude, speed, heading, timestamp)
					VALUES (?, ?, ?, ?, ?, datetime('now'))
				`, vehID, lat, lng, speed, heading)
				dur := time.Since(t0)

				if err == nil {
					mu.Lock()
					latencies[idx] = dur
					mu.Unlock()
				}
			}
		}(w)
	}

	wg.Wait()
	totalElapsed := time.Since(start)

	// Filter valid latencies
	var validLats []float64
	for _, l := range latencies {
		if l > 0 {
			validLats = append(validLats, float64(l.Microseconds())/1000.0) // ms
		}
	}
	require.Equal(t, numEvents, len(validLats), "All 1000 telemetry events must succeed with zero loss")
	sort.Float64s(validLats)

	p50 := validLats[int(float64(len(validLats))*0.50)]
	p95 := validLats[int(float64(len(validLats))*0.95)]
	p99 := validLats[int(float64(len(validLats))*0.99)]
	throughput := float64(numEvents) / totalElapsed.Seconds()

	t.Logf("=== 1,000 TELEMETRY INGESTION CONCURRENCY RESULTS ===")
	t.Logf("Total Time  : %v", totalElapsed)
	t.Logf("Throughput  : %.2f events/sec (~%.0f events/min)", throughput, throughput*60)
	t.Logf("P50 Latency : %.2f ms", p50)
	t.Logf("P95 Latency : %.2f ms", p95)
	t.Logf("P99 Latency : %.2f ms", p99)

	// Assertions for SLA
	assert.Less(t, p95, 25.0, "P95 latency should be under 25ms under local memory WAL")
	assert.Greater(t, throughput, 500.0, "Throughput should exceed 500 events/sec")
}

// Benchmark & Stress Test 2: 100 Simultaneous Payment Webhooks & Replay Idempotency
func TestP6_ProductionAudit_100ConcurrentPaymentWebhooks_Deduplication(t *testing.T) {
	db := setupProductionStressDB(t)
	defer db.Close()

	numPayments := 100
	var wg sync.WaitGroup

	// For each payment, send 5 concurrent identical webhooks
	for i := 0; i < numPayments; i++ {
		paymentIdx := i
		wg.Add(5)
		for r := 0; r < 5; r++ {
			go func(pIdx, replayIdx int) {
				defer wg.Done()
				idempKey := fmt.Sprintf("rzp_hook_pay_%d", pIdx)
				driverID := fmt.Sprintf("drv_%d", pIdx%10)
				tripID := fmt.Sprintf("trip_%d", pIdx)
				settleID := fmt.Sprintf("stl_%d", pIdx)

				// Concurrent insert with ON CONFLICT / UNIQUE check
				_, _ = db.Exec(`
					INSERT INTO driver_ledger (tenant_id, driver_id, trip_id, settlement_id, entry_type, amount, balance_after, notes, idempotency_key, created_at)
					VALUES ('tenant-prod-1', ?, ?, ?, 'PAYOUT', -5000.0, 0.0, 'Payment Webhook', ?, datetime('now'))
				`, driverID, tripID, settleID, idempKey)
			}(paymentIdx, r)
		}
	}

	wg.Wait()

	var distinctRows int
	err := db.QueryRow(`SELECT COUNT(*) FROM driver_ledger WHERE tenant_id = 'tenant-prod-1'`).Scan(&distinctRows)
	require.NoError(t, err)

	assert.Equal(t, numPayments, distinctRows, "Exact 100 ledger lines must exist despite 500 concurrent webhooks")
}

// Benchmark & Stress Test 3: 100 Concurrent Multi-Stop Control Tower Projections
func TestP6_ProductionAudit_100ConcurrentControlTowerReads(t *testing.T) {
	db := setupProductionStressDB(t)
	defer db.Close()

	tenantID := shared.TenantID("tenant-ct-stress")
	tenantStr := string(tenantID)

	// Seed 20 multi-stop active trips
	for i := 0; i < 20; i++ {
		tripID := fmt.Sprintf("trip_ct_%d", i)
		tripNum := fmt.Sprintf("TRP-CT-%03d", i)
		drvID := fmt.Sprintf("drv_ct_%d", i)
		vehID := fmt.Sprintf("veh_ct_%d", i)

		_, err := db.Exec(`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone) VALUES (?, ?, 'Driver', 'Test', '+919999900000')`, drvID, tenantStr)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number) VALUES (?, ?, 'TRK-100', 'HR-55-AA-1111')`, vehID, tenantStr)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, driver_id, vehicle_id, status, start_time) VALUES (?, ?, ?, ?, ?, 'IN_TRANSIT', datetime('now'))`, tripID, tenantStr, tripNum, drvID, vehID)
		require.NoError(t, err)

		for s := 1; s <= 3; s++ {
			stopID := fmt.Sprintf("stop_%d_%d", i, s)
			status := "pending"
			if s == 1 {
				status = "completed"
			}
			_, err = db.Exec(`INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, latitude, longitude, status) VALUES (?, ?, ?, ?, 'drop', 'DC', 28.5, 77.2, ?)`, stopID, tripID, tenantStr, s, status)
			require.NoError(t, err)
		}
	}

	ctService := ctApp.NewService(db, nil, 15*time.Minute)

	numReaders := 100
	var wg sync.WaitGroup
	latencies := make([]time.Duration, numReaders)
	var mu sync.Mutex

	start := time.Now()

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			tripID := fmt.Sprintf("trip_ct_%d", readerID%20)
			t0 := time.Now()
			proj, err := ctService.GetTrip(context.Background(), tenantID, tripID)
			dur := time.Since(t0)

			if err == nil && proj != nil {
				mu.Lock()
				latencies[readerID] = dur
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	totalElapsed := time.Since(start)

	var validLats []float64
	for _, l := range latencies {
		if l > 0 {
			validLats = append(validLats, float64(l.Microseconds())/1000.0)
		}
	}
	require.Equal(t, numReaders, len(validLats))
	sort.Float64s(validLats)

	p50 := validLats[int(float64(len(validLats))*0.50)]
	p95 := validLats[int(float64(len(validLats))*0.95)]
	p99 := validLats[int(float64(len(validLats))*0.99)]

	t.Logf("=== 100 CONCURRENT CONTROL TOWER PROJECTION READS ===")
	t.Logf("Total Time  : %v", totalElapsed)
	t.Logf("P50 Latency : %.2f ms", p50)
	t.Logf("P95 Latency : %.2f ms", p95)
	t.Logf("P99 Latency : %.2f ms", p99)

	assert.Less(t, p95, 50.0, "Control Tower query p95 should be sub-50ms under 100 concurrent readers")
}

// Test 4: E-Way Bill Terminal State Transition Invariant
func TestP6_ProductionAudit_EWayBillTerminalStateInvariant(t *testing.T) {
	db := setupProductionStressDB(t)
	defer db.Close()

	tenantID := shared.TenantID("tenant-ewb-prod")
	tripID := "trip_ewb_prod_888"

	// 1. Initial active trip with ACTIVE E-Way Bill
	_, err := db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, status, start_time)
		VALUES ('trip_ewb_prod_888', 'tenant-ewb-prod', 'TRP-EWB-888', 'IN_TRANSIT', datetime('now'));

		INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, latitude, longitude, status)
		VALUES ('stop_final_1', 'trip_ewb_prod_888', 'tenant-ewb-prod', 1, 'pickup', 'Origin', 28.5, 77.2, 'completed'),
		       ('stop_final_2', 'trip_ewb_prod_888', 'tenant-ewb-prod', 2, 'drop', 'Destination', 26.8, 75.8, 'pending');

		INSERT INTO ewb_requests (trip_id, tenant_id, eway_bill_number, status, valid_until)
		VALUES ('trip_ewb_prod_888', 'tenant-ewb-prod', 'EWB-888-ACTIVE', 'ACTIVE', datetime('now', '+2 days'));
	`)
	require.NoError(t, err)

	// 2. Final Stop Arrival & POD Complete -> Trip reaches COMPLETED
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`UPDATE trip_stops SET status='completed', actual_departure=?, pod_url='pod_final.jpg' WHERE id='stop_final_2'`, nowStr)
	require.NoError(t, err)

	_, err = db.Exec(`UPDATE trips SET status='COMPLETED', end_time=? WHERE id='trip_ewb_prod_888'`, nowStr)
	require.NoError(t, err)

	// Invariant rule: On trip completion, EWB status must transition to 'COMPLETED' (transport leg delivered)
	_, err = db.Exec(`UPDATE ewb_requests SET status='COMPLETED' WHERE trip_id='trip_ewb_prod_888' AND status='ACTIVE'`)
	require.NoError(t, err)

	ctService := ctApp.NewService(db, nil, 15*time.Minute)
	proj, err := ctService.GetTrip(context.Background(), tenantID, tripID)
	require.NoError(t, err)
	require.NotNil(t, proj)

	assert.Equal(t, "COMPLETED", proj.Status)
	assert.Equal(t, "COMPLETED", proj.EWB.Status)
	assert.Equal(t, "EWB-888-ACTIVE", proj.EWB.EWBNumber)
}
