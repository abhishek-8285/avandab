package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00146STOAndLoadBoard(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig_00146.db")
	database, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	migFS, err := fs.Sub(Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migFS)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 146)
	require.NoError(t, err)

	assertForeignKeyCheckClean(t, database, "after 00146 up")

	// Insert origin and destination facilities
	_, err = database.Exec(`
		INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type)
		VALUES 
		('fac-org-01', '1', 'FAC-PLANT-01', 'Pune Plant', 'depot'),
		('fac-dst-01', '1', 'FAC-RDC-01', 'Mumbai RDC', 'hub');
	`)
	require.NoError(t, err)

	// Insert STO
	_, err = database.Exec(`
		INSERT INTO stock_transfer_orders (
			id, tenant_id, sto_number, origin_facility_id, destination_facility_id,
			material_code, material_description, quantity, uom, required_delivery_date,
			status, created_by
		) VALUES (
			'sto-001', '1', 'STO-2026-0001', 'fac-org-01', 'fac-dst-01',
			'MAT-AUTO-01', 'Transmission Assemblies', 15.5, 'MT', '2026-09-15',
			'DRAFT', 'user-01'
		)
	`)
	require.NoError(t, err)

	// Insert load board listing
	_, err = database.Exec(`
		INSERT INTO load_board_listings (
			id, tenant_id, sto_id, origin_city, destination_city,
			vehicle_type_required, target_rate, max_rate, visibility, status, expires_at
		) VALUES (
			'lb-001', '1', 'sto-001', 'Pune', 'Mumbai',
			'truck', 18000, 22000, 'PRIVATE', 'OPEN', datetime('now', '+2 days')
		)
	`)
	require.NoError(t, err)

	// Insert load board bid
	_, err = database.Exec(`
		INSERT INTO load_board_bids (
			id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount, status
		) VALUES (
			'bid-001', '1', 'lb-001', 'carrier-99', 'Western Roadways', 19500, 'SUBMITTED'
		)
	`)
	require.NoError(t, err)

	// Verify records
	var stoNum, matCode, status string
	err = database.QueryRowContext(ctx, `SELECT sto_number, material_code, status FROM stock_transfer_orders WHERE id = 'sto-001'`).
		Scan(&stoNum, &matCode, &status)
	require.NoError(t, err)
	require.Equal(t, "STO-2026-0001", stoNum)
	require.Equal(t, "MAT-AUTO-01", matCode)
	require.Equal(t, "DRAFT", status)

	var origCity, destCity, lbStatus string
	err = database.QueryRowContext(ctx, `SELECT origin_city, destination_city, status FROM load_board_listings WHERE id = 'lb-001'`).
		Scan(&origCity, &destCity, &lbStatus)
	require.NoError(t, err)
	require.Equal(t, "Pune", origCity)
	require.Equal(t, "Mumbai", destCity)
	require.Equal(t, "OPEN", lbStatus)

	var bidCarrier string
	var bidAmount float64
	err = database.QueryRowContext(ctx, `SELECT carrier_name, bid_amount FROM load_board_bids WHERE id = 'bid-001'`).
		Scan(&bidCarrier, &bidAmount)
	require.NoError(t, err)
	require.Equal(t, "Western Roadways", bidCarrier)
	require.Equal(t, 19500.0, bidAmount)

	// Verify permissions seeded
	var permCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM permissions
		WHERE name IN ('sto:read', 'sto:write', 'loadboard:read', 'loadboard:write')
	`).Scan(&permCount)
	require.NoError(t, err)
	require.Equal(t, 4, permCount)

	// Verify Down rollback
	_, err = provider.Down(ctx)
	require.NoError(t, err)

	var tableCount int
	err = database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name IN ('stock_transfer_orders', 'load_board_listings', 'load_board_bids')
	`).Scan(&tableCount)
	require.NoError(t, err)
	require.Equal(t, 0, tableCount)

	assertForeignKeyCheckClean(t, database, "after 00146 down")
}
