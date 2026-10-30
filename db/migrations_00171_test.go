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

func TestMigration00171_TyreManagement_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00171_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 170))

	// Verify tyres table does not exist prior to 171
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tyres'`).Scan(&count))
	assert.Equal(t, 0, count, "tyres table must not exist before v171")

	// Apply migration 00171
	require.NoError(t, goose.UpTo(db, "migrations", 171))

	// Verify tables created
	for _, tbl := range []string{"tyres", "tyre_activities", "tyre_inspections"} {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v171", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('tyres:read', 'tyres:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "tyres:read and tyres:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('tyres:read', 'tyres:write') AND rp.role_id IN (1, 2, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 6, rolePermCount, "each role (1, 2, 6) must receive 2 tyre permissions")

	// Seed tenant and vehicle to verify tyre insert and constraints
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-171', 'Tenant 171', 't-171')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v-mig-171', 'tenant-171', 'MH-01-TYRE-01', 'REG-TYRE-171', 'truck', 20, date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'))`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO tyres (id, tenant_id, serial_number, brand, size, status, vehicle_id, axle_position, mount_odometer_km)
		VALUES ('tyre-1', 'tenant-171', 'SN-TYRE-001', 'Apollo', '295/80R22.5', 'MOUNTED', 'v-mig-171', 'FL', 50000.0)`)
	require.NoError(t, err)

	// Unique constraint: Cannot mount another tyre at same axle position 'FL' on vehicle 'v-mig-171'
	_, err = db.Exec(`INSERT INTO tyres (id, tenant_id, serial_number, brand, size, status, vehicle_id, axle_position, mount_odometer_km)
		VALUES ('tyre-2', 'tenant-171', 'SN-TYRE-002', 'MRF', '295/80R22.5', 'MOUNTED', 'v-mig-171', 'FL', 50000.0)`)
	assert.Error(t, err, "mounting two tyres at the same axle position must fail unique index")

	// Rollback migration 00171 down
	require.NoError(t, goose.DownTo(db, "migrations", 170))

	for _, tbl := range []string{"tyres", "tyre_activities", "tyre_inspections"} {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down", tbl)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('tyres:read', 'tyres:write')`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "tyre permissions must be removed after down")
}
