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

func TestMigration00172_PartsInventory_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00172_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 171))

	// Verify parts table does not exist prior to 172
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='parts'`).Scan(&count))
	assert.Equal(t, 0, count, "parts table must not exist before v172")

	// Apply migration 00172
	require.NoError(t, goose.UpTo(db, "migrations", 172))

	// Verify tables created
	expectedTables := []string{
		"parts",
		"part_stocks",
		"part_purchase_bills",
		"part_purchase_items",
		"part_transactions",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v172", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('inventory:read', 'inventory:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "inventory:read and inventory:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('inventory:read', 'inventory:write') AND rp.role_id IN (1, 2, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 6, rolePermCount, "each role (1, 2, 6) must receive 2 inventory permissions")

	// Seed tenant, facility, and part to verify insert and constraints
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-172', 'Tenant 172', 't-172')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type)
		VALUES ('fac-172', 'tenant-172', 'FAC-STORE-01', 'Central Spares Depot', 'workshop')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO parts (id, tenant_id, part_number, name, category, uom, min_stock_level, reorder_quantity, cost_price)
		VALUES ('part-1', 'tenant-172', 'FLT-OIL-001', 'Heavy Duty Oil Filter', 'FILTERS', 'PIECES', 5.0, 20.0, 450.0)`)
	require.NoError(t, err)

	// Unique constraint: Cannot add duplicate part_number in same tenant
	_, err = db.Exec(`INSERT INTO parts (id, tenant_id, part_number, name, category)
		VALUES ('part-2', 'tenant-172', 'FLT-OIL-001', 'Duplicate Filter', 'FILTERS')`)
	assert.Error(t, err, "duplicate part_number in same tenant must fail unique index")

	// Stock level unique constraint: one record per (tenant_id, part_id, facility_id)
	_, err = db.Exec(`INSERT INTO part_stocks (id, tenant_id, part_id, facility_id, quantity_on_hand, bin_location)
		VALUES ('stk-1', 'tenant-172', 'part-1', 'fac-172', 15.0, 'Rack-A1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO part_stocks (id, tenant_id, part_id, facility_id, quantity_on_hand, bin_location)
		VALUES ('stk-2', 'tenant-172', 'part-1', 'fac-172', 10.0, 'Rack-A2')`)
	assert.Error(t, err, "duplicate part_stock for same part and facility must fail unique index")

	// Rollback migration 00172 down
	require.NoError(t, goose.DownTo(db, "migrations", 171))

	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down", tbl)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('inventory:read', 'inventory:write')`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "inventory permissions must be removed after down")
}
