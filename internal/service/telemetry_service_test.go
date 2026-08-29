package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/domain"
	"transport-app/internal/events"
	sqliterepo "transport-app/internal/repository/sqlite"
)

func newTelemetryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_telem_svc_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
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

func TestTelemetryService_ProcessTelemetryStream_PersistsAlerts(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	bus := events.NewInMemoryBus()

	var receivedAlertEvents []events.Event
	bus.Subscribe("AlertEvent", func(ctx context.Context, e events.Event) error {
		receivedAlertEvents = append(receivedAlertEvents, e)
		return nil
	})

	svc := NewServices(repo, nil, nil, bus)

	ctx := context.Background()
	dp := TelemetryDataPoint{
		VehicleID:       domain.VehicleID("veh-telem-1"),
		Latitude:        19.0,
		Longitude:       74.0,
		PlannedRouteLat: 18.5,
		PlannedRouteLng: 73.8, // > 5 km deviation
		FuelLevel:       40.0,
		IgnitionOn:      false,
		Timestamp:       time.Now(),
	}

	// Last fuel level 60 -> drop 20L while ignition is OFF (theft_suspicion)
	alerts, err := svc.Telemetry.ProcessTelemetryStream(ctx, dp, 60.0)
	require.NoError(t, err)
	assert.Len(t, alerts, 2, "must generate both gps_deviation and theft_suspicion alerts")

	// Verify alert types
	alertTypes := []string{alerts[0].AlertType, alerts[1].AlertType}
	assert.Contains(t, alertTypes, "gps_deviation")
	assert.Contains(t, alertTypes, "theft_suspicion")

	// Verify persistence in telemetry_alerts table
	rows, err := db.Query("SELECT alert_type, severity FROM telemetry_alerts WHERE vehicle_id = 'veh-telem-1' ORDER BY created_at ASC")
	require.NoError(t, err)
	defer rows.Close()

	var savedTypes []string
	for rows.Next() {
		var at, sev string
		require.NoError(t, rows.Scan(&at, &sev))
		savedTypes = append(savedTypes, at)
	}
	assert.Len(t, savedTypes, 2)
	assert.Contains(t, savedTypes, "gps_deviation")
	assert.Contains(t, savedTypes, "theft_suspicion")

	// Verify AlertEvent published on bus
	assert.Len(t, receivedAlertEvents, 2)
}
