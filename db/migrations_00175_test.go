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

func TestMigration00175_RFQAndCounterOffer_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00175_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 174))

	// Verify rfqs does not exist prior to 175
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rfqs'`).Scan(&count))
	assert.Equal(t, 0, count, "rfqs table must not exist before v175")

	// Apply migration 00175
	require.NoError(t, goose.UpTo(db, "migrations", 175))

	// Verify tables created
	expectedTables := []string{
		"rfqs",
		"rfq_bids",
		"counter_offers",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v175", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('rfq:read', 'rfq:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "rfq:read and rfq:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('rfq:read', 'rfq:write') AND rp.role_id IN (1, 2, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 6, rolePermCount, "each role (1, 2, 6) must receive rfq:read and rfq:write (6 total)")

	// Verify inserting and updating records with tenant trigger
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-175', 'Tenant 175', 't-175')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO rfqs (
		id, tenant_id, rfq_number, title, origin_city, destination_city,
		vehicle_type_required, target_rate, max_acceptable_rate,
		pickup_date, delivery_deadline, bid_deadline, status
	) VALUES (
		'rfq-1', 'tenant-175', 'RFQ-2026-0001', 'Delhi to Mumbai Lane RFQ', 'Delhi', 'Mumbai',
		'32FT_MXL', 45000.0, 52000.0,
		'2026-11-01 10:00:00', '2026-11-04 18:00:00', '2026-10-31 23:59:59', 'OPEN'
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO rfq_bids (
		id, tenant_id, rfq_id, carrier_id, carrier_name, bid_amount, transit_days, vehicles_available
	) VALUES (
		'bid-1', 'tenant-175', 'rfq-1', 'carrier-1', 'Apex Logistics', 48000.0, 3, 2
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO counter_offers (
		id, tenant_id, entity_type, rfq_id, bid_id, round_number, sender_role,
		sender_id, sender_name, offered_amount, remarks, status
	) VALUES (
		'co-1', 'tenant-175', 'RFQ', 'rfq-1', 'bid-1', 1, 'SHIPPER',
		'user-1', 'Dispatcher John', 46000.0, 'Target budget is 46k for immediate award', 'PENDING'
	)`)
	require.NoError(t, err)

	// Roll back migration 00175
	require.NoError(t, goose.DownTo(db, "migrations", 174))

	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down migration", tbl)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('rfq:read', 'rfq:write')`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "permissions must be deleted on rollback")
}
