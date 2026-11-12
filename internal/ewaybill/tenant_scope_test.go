package ewaybill_test

// Unit tests for the tenant-scoped record lookups and the write-path tenant
// gate added by the 2026-11-11 E-Way Bill security fix: a caller carrying a
// tenant can only read/mutate EWBs whose owning trip belongs to that tenant;
// unknown and cross-tenant ids both surface ErrEWBNotFound (no oracle).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/shared"
)

const ewbTenantScopeSchema = `
CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, vehicle_id TEXT, status TEXT);
CREATE TABLE eway_bills (
    id TEXT PRIMARY KEY, trip_id TEXT, ewb_number TEXT NOT NULL, irn TEXT, status TEXT NOT NULL,
    generation_date DATETIME NOT NULL, valid_until DATETIME NOT NULL, vehicle_number TEXT,
    transporter_id TEXT, qr_code TEXT, from_place TEXT, from_state_code TEXT, to_place TEXT,
    to_state_code TEXT, goods_value REAL NOT NULL DEFAULT 0, distance REAL NOT NULL DEFAULT 0,
    doc_type TEXT, doc_no TEXT, doc_date TEXT, transporter_doc_no TEXT,
    extension_count INTEGER NOT NULL DEFAULT 0, cancel_reason TEXT, cancelled_at DATETIME, part_b_json TEXT,
    gen_mode TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE eway_bill_events (id TEXT PRIMARY KEY, ewb_number TEXT, trip_id TEXT,
    event_type TEXT, payload TEXT, created_by TEXT, created_at DATETIME);
`

// scopedFakeProvider is a non-stub NIC client that records mutation calls.
type scopedFakeProvider struct {
	attachCalls int
	extendCalls int
	cancelCalls int
}

func (f *scopedFakeProvider) GeneratePartA(context.Context, intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *scopedFakeProvider) Generate(ctx context.Context, req intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return f.GeneratePartA(ctx, req)
}
func (f *scopedFakeProvider) Get(_ context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "ACTIVE"}, nil
}
func (f *scopedFakeProvider) Cancel(_ context.Context, ewbNumber, reason string) (intEWB.Cancellation, error) {
	f.cancelCalls++
	return intEWB.Cancellation{EwbNumber: ewbNumber, Reason: reason, Status: "CANCELLED"}, nil
}
func (f *scopedFakeProvider) AttachPartB(_ context.Context, ewbNumber, _, _ string) (intEWB.EWayBill, error) {
	f.attachCalls++
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "ACTIVE"}, nil
}
func (f *scopedFakeProvider) Extend(_ context.Context, ewbNumber string, _ intEWB.ExtendRequest) (intEWB.EWayBill, error) {
	f.extendCalls++
	return intEWB.EWayBill{EwbNumber: ewbNumber, Status: "EXTENDED", ValidUpto: time.Now().Add(24 * time.Hour)}, nil
}
func (f *scopedFakeProvider) GetByNumber(ctx context.Context, ewbNumber string) (intEWB.EWayBill, error) {
	return f.Get(ctx, ewbNumber)
}
func (f *scopedFakeProvider) GetByTrip(_ context.Context, tripID string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{EwbNumber: "EWB-" + tripID, Status: "ACTIVE"}, nil
}

func newEWBTenantScopeFixture(t *testing.T) (*sql.DB, *scopedFakeProvider, *ewaybill.EWayBillService) {
	t.Helper()
	name := fmt.Sprintf("ewb_tenant_scope_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(ewbTenantScopeSchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, status) VALUES
		('trip-a', 'tenant-a', 'in_transit'), ('trip-b', 'tenant-b', 'in_transit')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO eway_bills (id, trip_id, ewb_number, status, generation_date, valid_until, created_at) VALUES
		('eb-a', 'trip-a', 'EWB-A', 'active', '2026-09-01 00:00:00', '2026-12-31 00:00:00', '2026-09-01 00:00:00'),
		('eb-b', 'trip-b', 'EWB-B', 'active', '2026-09-02 00:00:00', '2026-12-31 00:00:00', '2026-09-02 00:00:00')`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	provider := &scopedFakeProvider{}
	svc := ewaybill.NewEWayBillService(db, nil, provider, nil, ewaybill.Config{})
	return db, provider, svc
}

func tenantCtx(tenantID string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(tenantID))
}

func TestEWBLookups_TenantScoped(t *testing.T) {
	_, _, svc := newEWBTenantScopeFixture(t)

	rec, err := svc.GetByNumberForTenant(context.Background(), "EWB-A", "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, "EWB-A", rec.EwbNumber)
	assert.Equal(t, "trip-a", rec.TripID)

	for _, tc := range []struct {
		name              string
		ewb, trip, tenant string
	}{
		{"cross-tenant number", "EWB-B", "", "tenant-a"},
		{"unknown number", "EWB-NOPE", "", "tenant-a"},
		{"empty tenant number", "EWB-A", "", ""},
		{"cross-tenant trip", "", "trip-b", "tenant-a"},
		{"unknown trip", "", "trip-nope", "tenant-a"},
		{"empty tenant trip", "", "trip-a", ""},
	} {
		if tc.ewb != "" {
			_, err := svc.GetByNumberForTenant(context.Background(), tc.ewb, tc.tenant)
			assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound, tc.name)
			continue
		}
		_, err := svc.GetByTripForTenant(context.Background(), tc.trip, tc.tenant)
		assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound, tc.name)
	}

	rec, err = svc.GetByTripForTenant(context.Background(), "trip-a", "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, "EWB-A", rec.EwbNumber)
}

func TestEWBMutations_TenantGate(t *testing.T) {
	db, provider, svc := newEWBTenantScopeFixture(t)
	ctxA := tenantCtx("tenant-a")

	// Cross-tenant calls fail closed BEFORE the provider is touched.
	_, err := svc.AttachPartB(ctxA, "EWB-B", "MH12AB1234", "")
	assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound)
	_, err = svc.Extend(ctxA, "EWB-B", ewaybill.ExtendRequest{EwbNumber: "EWB-B"})
	assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound)
	_, err = svc.Cancel(ctxA, "EWB-B", "test")
	assert.ErrorIs(t, err, ewaybill.ErrEWBNotFound)
	assert.Zero(t, provider.attachCalls)
	assert.Zero(t, provider.extendCalls)
	assert.Zero(t, provider.cancelCalls)
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM eway_bills WHERE ewb_number = 'EWB-B'`).Scan(&status))
	assert.Equal(t, "active", status, "cross-tenant write must not mutate the row")

	// Own-tenant mutations succeed and return the scoped record.
	rec, err := svc.AttachPartB(ctxA, "EWB-A", "MH12AB1234", "")
	require.NoError(t, err)
	assert.Equal(t, "EWB-A", rec.EwbNumber)
	assert.Equal(t, 1, provider.attachCalls)

	rec, err = svc.Cancel(ctxA, "EWB-A", "test cancel")
	require.NoError(t, err)
	assert.Equal(t, "EWB-A", rec.EwbNumber)
	assert.Equal(t, 1, provider.cancelCalls)
}

func TestEWBMutations_NoTenantContextKeepsTrustedPath(t *testing.T) {
	// Background bus/integration callers without a tenant context keep the
	// legacy by-number path (documented trusted callers only).
	_, provider, svc := newEWBTenantScopeFixture(t)
	rec, err := svc.AttachPartB(context.Background(), "EWB-B", "KA01XY1234", "")
	require.NoError(t, err)
	assert.Equal(t, "EWB-B", rec.EwbNumber)
	assert.Equal(t, 1, provider.attachCalls)
}
