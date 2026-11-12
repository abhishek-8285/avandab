package application

// Characterization + extraction tests for the kharcha/driver-money helpers
// moved out of internal/handlers/{kharcha,driver_money,kharcha_verify}.go
// (handlers-end-state ADR, Phase 2): driver identity rules, e-POD artifact
// coalesce semantics, settlement row mapping/ordering and tenant-scoped OCR
// field reads.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const kharchaTestSchema = `
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT,
    email TEXT,
    tenant_id TEXT NOT NULL
);
CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    pod_signature_data TEXT,
    pod_quantity_short REAL,
    pod_damage_qty REAL,
    pod_refusal_reason TEXT,
    pod_consignee_name TEXT,
    pod_consignee_phone TEXT,
    pod_scan_value TEXT
);
CREATE TABLE driver_settlements (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL,
    trip_id TEXT,
    gross_fare REAL,
    deductions REAL,
    net_payout REAL,
    status TEXT,
    tds_amount REAL,
    paid_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE driver_expenses (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    ocr_amount REAL,
    ocr_confidence REAL
);
`

func newKharchaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("kharcha_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(kharchaTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecKharcha(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestLookupUserDriver_IdentityRules(t *testing.T) {
	db := newKharchaTestDB(t)
	ctx := context.Background()
	mustExecKharcha(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@x.in'), ('u-2', 'two@x.in')`)
	mustExecKharcha(t, db, `INSERT INTO drivers (id, driver_id, email, tenant_id) VALUES
		('d-1', 'DRV-1', 'one@x.in', 'tenant-1'),
		('d-2', 'DRV-2', 'two@x.in', 'tenant-2')`)

	// Email-linked identity.
	ref, err := LookupUserDriver(ctx, db, "u-1")
	require.NoError(t, err)
	require.NotNil(t, ref)
	assert.Equal(t, "d-1", ref.ID)
	assert.Equal(t, "DRV-1", ref.DriverCode)

	// drivers.id identity rule (user id equals the driver row id).
	ref, err = LookupUserDriver(ctx, db, "d-2")
	require.NoError(t, err)
	require.NotNil(t, ref)
	assert.Equal(t, "d-2", ref.ID)

	// FROZEN LEGACY SCOPE: the identity lookup has no tenant filter - the
	// mobile/driver endpoints map the auth user to their own org's driver row.
	ref, err = LookupUserDriver(ctx, db, "u-2")
	require.NoError(t, err)
	require.NotNil(t, ref)
	assert.Equal(t, "d-2", ref.ID)

	// No driver row is a valid state.
	ref, err = LookupUserDriver(ctx, db, "u-missing")
	require.NoError(t, err)
	assert.Nil(t, ref)

	// Real lookup failures are surfaced (callers log them).
	mustExecKharcha(t, db, `DROP TABLE drivers`)
	_, err = LookupUserDriver(ctx, db, "u-1")
	require.Error(t, err)

	ref, err = LookupUserDriver(ctx, nil, "u-1")
	require.NoError(t, err)
	assert.Nil(t, ref)
}

func TestUpdateTripPODArtifacts_CoalesceSemantics(t *testing.T) {
	db := newKharchaTestDB(t)
	ctx := context.Background()
	mustExecKharcha(t, db, `INSERT INTO trips (id, pod_signature_data, pod_quantity_short, pod_damage_qty,
		 pod_refusal_reason, pod_consignee_name, pod_consignee_phone, pod_scan_value)
		VALUES ('t-1', 'old-sig', 1, 2, 'old-refusal', 'Old Name', '111', 'old-scan')`)

	read := func() (sig, refusal, name, phone, scan string, qty, damage float64) {
		t.Helper()
		require.NoError(t, db.QueryRow(`SELECT pod_signature_data, pod_quantity_short, pod_damage_qty,
			pod_refusal_reason, pod_consignee_name, pod_consignee_phone, pod_scan_value FROM trips WHERE id = 't-1'`).
			Scan(&sig, &qty, &damage, &refusal, &name, &phone, &scan))
		return
	}

	// Empty strings / zero quantities keep the stored values (legacy guards).
	require.NoError(t, UpdateTripPODArtifacts(ctx, db, "t-1", PODArtifacts{SignatureData: "new-sig"}))
	sig, refusal, name, phone, scan, qty, damage := read()
	assert.Equal(t, "new-sig", sig)
	assert.Equal(t, "old-refusal", refusal)
	assert.Equal(t, "Old Name", name)
	assert.Equal(t, "111", phone)
	assert.Equal(t, "old-scan", scan)
	assert.Equal(t, 1.0, qty)
	assert.Equal(t, 2.0, damage)

	// Non-zero/non-empty values overwrite.
	require.NoError(t, UpdateTripPODArtifacts(ctx, db, "t-1", PODArtifacts{
		QuantityShort: 5, DamageQty: 3, RefusalReason: "r", ConsigneeName: "New Name",
		ConsigneePhone: "999", ScanValue: "scan2",
	}))
	sig, refusal, name, phone, scan, qty, damage = read()
	assert.Equal(t, "new-sig", sig, "empty signature keeps the stored value")
	assert.Equal(t, "r", refusal)
	assert.Equal(t, "New Name", name)
	assert.Equal(t, "999", phone)
	assert.Equal(t, "scan2", scan)
	assert.Equal(t, 5.0, qty)
	assert.Equal(t, 3.0, damage)

	// Best-effort contract: nil DB and unknown trips do not panic/error hard.
	assert.NoError(t, UpdateTripPODArtifacts(ctx, nil, "t-1", PODArtifacts{SignatureData: "x"}))
	assert.NoError(t, UpdateTripPODArtifacts(ctx, db, "t-missing", PODArtifacts{SignatureData: "x"}))
}

func TestDriverSettlements_DriverScopedOrderingMappingAndLimit(t *testing.T) {
	db := newKharchaTestDB(t)
	ctx := context.Background()
	// 51 rows for the driver (one over the 50-row cap) plus another driver.
	for i := 1; i <= 51; i++ {
		mustExecKharcha(t, db, `INSERT INTO driver_settlements (id, driver_id, trip_id, gross_fare, deductions, net_payout, status, tds_amount, created_at)
			VALUES (?, 'd-1', ?, 1000, 100, 900, 'pending', 5, ?)`,
			fmt.Sprintf("s-%02d", i), fmt.Sprintf("trip-%02d", i), fmt.Sprintf("2026-02-01 00:%02d:%02d", (i-1)/60, (i-1)%60))
	}
	mustExecKharcha(t, db, `INSERT INTO driver_settlements (id, driver_id, trip_id, gross_fare, deductions, net_payout, status, created_at)
		VALUES ('s-other', 'd-2', 'trip-other', 500, 50, 450, 'paid', '2026-02-01 00:00:00')`)
	mustExecKharcha(t, db, `UPDATE driver_settlements SET paid_at = '2026-02-01 01:00:00' WHERE id = 's-51'`)

	settlements, err := DriverSettlements(ctx, db, "d-1")
	require.NoError(t, err)
	require.Len(t, settlements, 50, "legacy limit is 50")
	assert.Equal(t, "s-51", settlements[0].ID, "newest first")
	assert.Equal(t, "s-02", settlements[49].ID)
	assert.Equal(t, "trip-51", settlements[0].Period, "legacy maps trip_id to the period field")
	assert.Equal(t, 1000.0, settlements[0].Gross)
	assert.Equal(t, 100.0, settlements[0].Deductions)
	assert.Equal(t, 900.0, settlements[0].Net)
	assert.Equal(t, "pending", settlements[0].Status)
	assert.Equal(t, 5.0, settlements[0].TDS)
	require.NotNil(t, settlements[0].PaidAt)
	assert.Nil(t, settlements[1].PaidAt, "NULL paid_at stays nil (handler omits the key)")

	for _, s := range settlements {
		assert.NotEqual(t, "s-other", s.ID, "other drivers' settlements never appear")
	}

	other, err := DriverSettlements(ctx, db, "d-2")
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, "s-other", other[0].ID)

	settlements, err = DriverSettlements(ctx, nil, "d-1")
	require.NoError(t, err)
	assert.Nil(t, settlements)
}

func TestExpenseOCRFields_TenantScoped(t *testing.T) {
	db := newKharchaTestDB(t)
	ctx := context.Background()
	mustExecKharcha(t, db, `INSERT INTO driver_expenses (id, tenant_id, ocr_amount, ocr_confidence) VALUES
		('e-1', 'tenant-1', 120.5, 0.93),
		('e-2', 'tenant-2', 999.0, 0.5),
		('e-null', 'tenant-1', NULL, NULL)`)

	amt, conf, err := ExpenseOCRFields(ctx, db, "tenant-1", "e-1")
	require.NoError(t, err)
	assert.Equal(t, 120.5, amt)
	assert.Equal(t, 0.93, conf)

	// Cross-tenant id reads as absent (NULLs), no error.
	amt, conf, err = ExpenseOCRFields(ctx, db, "tenant-1", "e-2")
	require.NoError(t, err)
	assert.Nil(t, amt)
	assert.Nil(t, conf)

	// NULL OCR columns come back as JSON nulls.
	amt, conf, err = ExpenseOCRFields(ctx, db, "tenant-1", "e-null")
	require.NoError(t, err)
	assert.Nil(t, amt)
	assert.Nil(t, conf)

	// Missing row is not an error (legacy tolerated sql.ErrNoRows).
	amt, conf, err = ExpenseOCRFields(ctx, db, "tenant-1", "e-missing")
	require.NoError(t, err)
	assert.Nil(t, amt)
	assert.Nil(t, conf)

	// Real query failures surface.
	mustExecKharcha(t, db, `DROP TABLE driver_expenses`)
	_, _, err = ExpenseOCRFields(ctx, db, "tenant-1", "e-1")
	require.Error(t, err)

	amt, conf, err = ExpenseOCRFields(ctx, nil, "tenant-1", "e-1")
	require.NoError(t, err)
	assert.Nil(t, amt)
	assert.Nil(t, conf)
}
