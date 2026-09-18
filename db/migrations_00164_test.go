package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00164_SubscriberIdempotencyBackstops_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00164_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 163))
	require.NoError(t, goose.UpTo(db, "migrations", 164))

	var invIdx int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_invoices_tenant_trip_unique'`).Scan(&invIdx))
	require.Equal(t, 1, invIdx, "invoices backstop index must exist at v164")

	require.NoError(t, goose.DownTo(db, "migrations", 163))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_invoices_tenant_trip_unique'`).Scan(&invIdx))
	require.Equal(t, 0, invIdx, "rollback must drop invoices backstop index")
	require.NoError(t, goose.UpTo(db, "migrations", 164))
}

func TestMigration00164_BackstopsRejectDuplicates(t *testing.T) {
	name := fmt.Sprintf("test_mig_00164_dup_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 164))

	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('dup-audit', 'Dup Audit', 'dup-audit')
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('other-tenant', 'Other Tenant', 'other-tenant')
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('dup-route', 'dup-audit', 'Delhi', 'Jaipur', 250, 5, 5000)
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id)
		VALUES ('dup-trip-1', 'TR-DUP-1', 'dup-booking', 'dup-route', datetime('now'), 'draft', 'dup-audit')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id)
		VALUES ('dup-trip-2', 'TR-DUP-2', 'dup-booking', 'dup-route', datetime('now'), 'draft', 'dup-audit')`)
	require.NoError(t, err, "trips stay 1:N per booking (settlement seeds rely on this); no trips constraint")

	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status)
		VALUES ('dup-booking-row', 'dup-audit', 'BK-DUP', 'dup-cust', datetime('now'), 'dup-route', 'truck', 5000, 'confirmed')
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('dup-cust', 'dup-audit', 'Dup Buyer', '9999999999')
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, trip_id, subtotal, total, tenant_id)
		VALUES ('dup-inv-1', 'INV-DUP-1', 'dup-booking-row', 'dup-cust', 'dup-trip-1', 5000, 5900, 'dup-audit')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, trip_id, subtotal, total, tenant_id)
		VALUES ('dup-inv-2', 'INV-DUP-2', 'dup-booking-row', 'dup-cust', 'dup-trip-1', 5000, 5900, 'dup-audit')`)
	require.Error(t, err, "second invoice for same (tenant, trip) must be rejected")
}
