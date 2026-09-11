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

func TestMigration00147_FuelCards_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00147_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "migrations"))

	// Verify tables created
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM fuel_cards`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = db.QueryRow(`SELECT COUNT(*) FROM fuel_card_transactions`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Test Tenant Trigger FK rejection on insert without tenant
	_, err = db.Exec(`INSERT INTO fuel_cards (id, tenant_id, card_number_masked, card_token_hash, provider)
		VALUES ('fc-bad', 'non-existent-tenant-999', '**** 1234', 'hash123', 'IOCL')`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger")

	// Create valid tenant and test valid insert
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-fc-1', 'Test Tenant', 'test-tenant')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO fuel_cards (id, tenant_id, card_number_masked, card_token_hash, provider, daily_spend_limit, status)
		VALUES ('fc-1', 't-fc-1', '**** **** **** 4321', 'hash4321', 'BPCL', 25000, 'ACTIVE')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO fuel_card_transactions (id, tenant_id, fuel_card_id, external_txn_id, txn_time,
		fuel_station_name, fuel_station_city, fuel_type, volume_litres, rate_per_litre, total_amount)
		VALUES ('fctx-1', 't-fc-1', 'fc-1', 'EXT-TXN-001', datetime('now'), 'BPCL Station Pune', 'Pune', 'DIESEL', 50.0, 90.0, 4500.0)`)
	require.NoError(t, err)

	// Verify rollback
	require.NoError(t, goose.Down(db, "migrations"))

	// Verify table dropped after rollback
	err = db.QueryRow(`SELECT COUNT(*) FROM fuel_cards`).Scan(&count)
	require.Error(t, err, "table fuel_cards should no longer exist after rollback")
}
