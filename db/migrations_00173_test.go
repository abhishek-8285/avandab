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

func TestMigration00173_FreightAudit_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00173_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 172))

	// Verify freight_audits table does not exist prior to 173
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='freight_audits'`).Scan(&count))
	assert.Equal(t, 0, count, "freight_audits table must not exist before v173")

	// Apply migration 00173
	require.NoError(t, goose.UpTo(db, "migrations", 173))

	// Verify tables created
	expectedTables := []string{
		"freight_rate_cards",
		"freight_audits",
		"freight_audit_deductions",
	}
	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 1, count, "table %s must exist after v173", tbl)
	}

	// Verify permissions seeded
	var permCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write')`).Scan(&permCount))
	assert.Equal(t, 2, permCount, "freight_audit:read and freight_audit:write must be seeded in permissions")

	// Verify role_permissions seeded for roles 1, 2, 6
	var rolePermCount int
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE p.name IN ('freight_audit:read', 'freight_audit:write') AND rp.role_id IN (1, 2, 6)
	`).Scan(&rolePermCount))
	assert.Equal(t, 6, rolePermCount, "each role (1, 2, 6) must receive 2 freight audit permissions")

	// Verify inserting and updating records with tenant trigger
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-173', 'Tenant 173', 't-173')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO freight_rate_cards (
		id, tenant_id, contract_number, name, billing_basis, rate, min_charge
	) VALUES (
		'frc-1', 'tenant-173', 'RATE-DEL-MUM-01', 'Delhi-Mumbai Standard Rate', 'PER_KM', 42.5, 5000.0
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO freight_audits (
		id, tenant_id, audit_number, rate_card_id, vendor_name, vehicle_number, status, billing_basis,
		billed_distance_km, billed_rate, billed_base_freight, billed_total,
		gps_distance_km, audited_rate, audited_base_freight, audited_total,
		distance_variance_km, overbilling_amount, deduction_amount, payable_amount
	) VALUES (
		'fa-1', 'tenant-173', 'AUD-2026-0001', 'frc-1', 'Express Logistics', 'MH12AB1234', 'DISCREPANCY_FLAGGED', 'PER_KM',
		650.0, 45.0, 29250.0, 29250.0,
		580.0, 42.5, 24650.0, 24650.0,
		70.0, 4600.0, 4600.0, 24650.0
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO freight_audit_deductions (
		id, tenant_id, audit_id, category, description, claimed_value, verified_value, unit_rate, deduction_amount
	) VALUES (
		'fad-1', 'tenant-173', 'fa-1', 'DISTANCE_INFLATION', 'Billed 650 km vs GPS 580 km (70 km excess)', 650.0, 580.0, 42.5, 2975.0
	)`)
	require.NoError(t, err)

	// Roll back migration 00173
	require.NoError(t, goose.DownTo(db, "migrations", 172))

	for _, tbl := range expectedTables {
		require.NoError(t, db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`, tbl)).Scan(&count))
		assert.Equal(t, 0, count, "table %s must be dropped after down migration", tbl)
	}

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name IN ('freight_audit:read', 'freight_audit:write')`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "permissions must be deleted on rollback")
}
