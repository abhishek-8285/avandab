package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00148_ESGSnapshots_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00148_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "migrations", 148))

	// Verify tables exist
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM trip_esg_metrics`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = db.QueryRow(`SELECT COUNT(*) FROM esg_emission_snapshots`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Test Tenant Trigger FK rejection on insert without tenant
	_, err = db.Exec(`INSERT INTO esg_emission_snapshots (
		id, tenant_id, period_start, period_end, total_trips, total_distance_km,
		total_cargo_tkm, total_fuel_litres, total_co2e_kg, avg_co2e_per_tkm, created_by
	) VALUES (
		'esg-bad', 'non-existent-tenant-888', '2026-08-01', '2026-08-31', 10, 500.0,
		5000.0, 150.0, 402.0, 0.0804, 'admin'
	)`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger")

	// Create valid tenant and test valid insert
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-esg-1', 'Green Logistics', 'green-log')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO esg_emission_snapshots (
		id, tenant_id, period_start, period_end, total_trips, total_distance_km,
		total_cargo_tkm, total_fuel_litres, total_co2e_kg, avg_co2e_per_tkm, created_by
	) VALUES (
		'esg-snap-1', 't-esg-1', '2026-08-01', '2026-08-31', 10, 500.0,
		5000.0, 150.0, 402.0, 0.0804, 'admin'
	)`)
	require.NoError(t, err)

	// Verify rollback
	require.NoError(t, goose.Down(db, "migrations"))

	// Verify table dropped after rollback
	err = db.QueryRow(`SELECT COUNT(*) FROM esg_emission_snapshots`).Scan(&count)
	require.Error(t, err, "table esg_emission_snapshots should no longer exist after rollback")
}
