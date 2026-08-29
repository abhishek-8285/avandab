package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

	"transport-app/internal/shared"
	"transport-app/internal/telemetry"
)

func newLoadTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_load_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLiveEndpoint_500Vehicles(t *testing.T) {
	db := newLoadTestDB(t)

	// Seed 500 vehicles with latest telemetry snapshots
	tx, err := db.Begin()
	require.NoError(t, err)

	stmtVeh, err := tx.Prepare(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES (?, ?, ?, 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), ?, '1')`)
	require.NoError(t, err)
	defer stmtVeh.Close()

	stmtSnap, err := tx.Prepare(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, timestamp, latitude, longitude, speed, odometer, ignition)
		VALUES (?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, 1)`)
	require.NoError(t, err)
	defer stmtSnap.Close()

	for i := 1; i <= 500; i++ {
		vehID := fmt.Sprintf("veh-scale-%03d", i)
		regNum := fmt.Sprintf("MH-01-SC-%04d", i)

		// Every 50th vehicle is maintenance due
		var dueVal *string
		if i%50 == 0 {
			today := time.Now().UTC().Format("2006-01-02")
			dueVal = &today
		}

		_, err = stmtVeh.Exec(vehID, regNum, regNum, dueVal)
		require.NoError(t, err)

		speed := 45.0
		if i%2 == 0 {
			speed = 0.0 // stopped
		}
		snapID := fmt.Sprintf("snap-scale-%03d", i)
		lat := 18.5 + float64(i)*0.001
		lng := 73.8 + float64(i)*0.001
		_, err = stmtSnap.Exec(snapID, vehID, lat, lng, speed, float64(i*100))
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	liveHandler := telemetry.LiveHandler(db, 15*time.Minute)

	// Measure response time
	start := time.Now()
	req := httptest.NewRequest("GET", "/api/v1/telemetry/live", nil)
	ctx := shared.ContextWithTenantID(req.Context(), "1")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	liveHandler.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Less(t, elapsed, 2*time.Second, "500 vehicles should respond in < 2s (actual: %v)", elapsed)

	var vehicles []telemetry.LiveVehicle
	err = json.Unmarshal(rr.Body.Bytes(), &vehicles)
	require.NoError(t, err)
	assert.Equal(t, 500, len(vehicles), "must return all 500 vehicles")

	// Verify states represented
	runningCount := 0
	stoppedCount := 0
	maintCount := 0
	for _, v := range vehicles {
		switch v.Status {
		case "running":
			runningCount++
		case "stopped":
			stoppedCount++
		case "maintenance_due":
			maintCount++
		}
	}

	assert.Equal(t, 10, maintCount, "10 vehicles should be maintenance_due (override priority)")
	assert.Greater(t, runningCount, 200)
	assert.Greater(t, stoppedCount, 200)
}
