package application

// Characterization + extraction tests for the Command Center inline EWB
// extend use-case moved out of internal/handlers/console.go
// (handlers-end-state ADR, Phase 2): tenant-scoped lookup, local expiry shift
// vs the EWB_EXTEND_ENABLED provider seam, event + audit writes, and the
// audit tenant fallback.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/shared"
)

const ewbExtendTestSchema = `
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL
);
CREATE TABLE eway_bills (
    id TEXT PRIMARY KEY,
    trip_id TEXT,
    ewb_number TEXT NOT NULL,
    valid_until DATETIME,
    status TEXT DEFAULT 'active'
);
CREATE TABLE eway_bill_events (
    id TEXT PRIMARY KEY,
    ewb_number TEXT NOT NULL,
    trip_id TEXT,
    event_type TEXT NOT NULL,
    payload TEXT,
    created_by TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    action TEXT NOT NULL,
    table_name TEXT NOT NULL,
    record_id TEXT,
    new_values TEXT,
    tenant_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// fakeExtendClient implements the full provider Client; only Extend is
// exercised by the console inline action. Every method is a no-op.
type fakeExtendClient struct {
	calls     int
	validUpto time.Time
	err       error
	lastReq   intEWB.ExtendRequest
}

func (f *fakeExtendClient) Generate(context.Context, intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *fakeExtendClient) Get(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *fakeExtendClient) Cancel(context.Context, string, string) (intEWB.Cancellation, error) {
	return intEWB.Cancellation{}, nil
}
func (f *fakeExtendClient) GeneratePartA(context.Context, intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *fakeExtendClient) AttachPartB(context.Context, string, string, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *fakeExtendClient) Extend(_ context.Context, ewbNumber string, req intEWB.ExtendRequest) (intEWB.EWayBill, error) {
	f.calls++
	f.lastReq = req
	return intEWB.EWayBill{EwbNumber: ewbNumber, ValidUpto: f.validUpto}, f.err
}
func (f *fakeExtendClient) GetByNumber(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *fakeExtendClient) GetByTrip(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}

func newEWBExtendTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("ewb_extend_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(ewbExtendTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecEWBExtend(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func seedExtendEWB(t *testing.T, db *sql.DB, ewbID, tripID, tenantID, validUntil string) {
	t.Helper()
	mustExecEWBExtend(t, db, `INSERT OR IGNORE INTO trips (id, tenant_id) VALUES (?, ?)`, tripID, tenantID)
	mustExecEWBExtend(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, valid_until) VALUES (?, ?, ?, ?)`,
		ewbID, tripID, "EWB-"+ewbID, validUntil)
}

func countEWBExtend(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func readValidUntil(t *testing.T, db *sql.DB, ewbID string) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, db.QueryRow(`SELECT valid_until FROM eway_bills WHERE id = ?`, ewbID).Scan(&ts))
	return ts
}

func TestExtendEwayBillUseCase_TenantScopedAndAudit(t *testing.T) {
	db := newEWBExtendTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	seedExtendEWB(t, db, "e-1", "t-1", "tenant-1", "2026-09-02 10:00:00")

	uc := NewExtendEwayBillUseCase(db, nil, false)
	newExpiry, err := uc.Extend(ctx, ExtendEwayBillCommand{EWBID: "e-1", TenantID: "tenant-1", ActorID: "u-1", Hours: 4})
	require.NoError(t, err)
	want := time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)
	assert.Equal(t, want, newExpiry.UTC())
	assert.Equal(t, want, readValidUntil(t, db, "e-1").UTC(), "valid_until row updated")

	// EXTENDED lifecycle event (legacy payload/actor fields).
	assert.Equal(t, int64(1), countEWBExtend(t, db, `SELECT COUNT(*) FROM eway_bill_events WHERE ewb_number = 'EWB-e-1'`))
	var eventType, payload, createdBy, tripID string
	require.NoError(t, db.QueryRow(`SELECT event_type, payload, created_by, trip_id FROM eway_bill_events WHERE ewb_number = 'EWB-e-1'`).
		Scan(&eventType, &payload, &createdBy, &tripID))
	assert.Equal(t, "EXTENDED", eventType)
	assert.Equal(t, `{"hours":4}`, payload)
	assert.Equal(t, "u-1", createdBy)
	assert.Equal(t, "t-1", tripID)

	// Audit row (legacy action/table/record/new_values/tenant).
	assert.Equal(t, int64(1), countEWBExtend(t, db, `SELECT COUNT(*) FROM audit_logs WHERE action = 'ewaybill.extend'`))
	var userID, tableName, recordID, newValues, tenantID string
	require.NoError(t, db.QueryRow(`SELECT user_id, table_name, record_id, new_values, tenant_id FROM audit_logs WHERE action = 'ewaybill.extend'`).
		Scan(&userID, &tableName, &recordID, &newValues, &tenantID))
	assert.Equal(t, "u-1", userID)
	assert.Equal(t, "eway_bills", tableName)
	assert.Equal(t, "e-1", recordID)
	assert.Equal(t, "tenant-1", tenantID)
	var auditValues map[string]any
	require.NoError(t, json.Unmarshal([]byte(newValues), &auditValues))
	assert.Equal(t, "2026-09-02T14:00:00Z", auditValues["new_expiry"])
	assert.Equal(t, float64(4), auditValues["valid_upto_hours"])

	// Cross-tenant + unknown ids fail closed with ErrConsoleNotFound and
	// leave the row/event/audit untouched.
	_, err = uc.Extend(ctx, ExtendEwayBillCommand{EWBID: "e-1", TenantID: "tenant-2", ActorID: "u-2", Hours: 4})
	assert.ErrorIs(t, err, ErrConsoleNotFound)
	assert.Equal(t, want, readValidUntil(t, db, "e-1").UTC())
	assert.Equal(t, int64(1), countEWBExtend(t, db, `SELECT COUNT(*) FROM eway_bill_events`))
	assert.Equal(t, int64(1), countEWBExtend(t, db, `SELECT COUNT(*) FROM audit_logs`))

	_, err = uc.Extend(ctx, ExtendEwayBillCommand{EWBID: "e-ghost", TenantID: "tenant-1", ActorID: "u-1", Hours: 1})
	assert.ErrorIs(t, err, ErrConsoleNotFound)

	// nil DB degrades to not-found rather than panicking.
	_, err = NewExtendEwayBillUseCase(nil, nil, false).Extend(ctx, ExtendEwayBillCommand{EWBID: "e-1", TenantID: "tenant-1", Hours: 1})
	assert.ErrorIs(t, err, ErrConsoleNotFound)
}

func TestExtendEwayBillUseCase_ProviderSeam(t *testing.T) {
	db := newEWBExtendTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	providerExpiry := time.Date(2026, 9, 5, 18, 30, 0, 0, time.UTC)

	// Disabled seam: provider is never consulted, local shift wins.
	seedExtendEWB(t, db, "e-off", "t-off", "tenant-1", "2026-09-02 10:00:00")
	clientOff := &fakeExtendClient{validUpto: providerExpiry}
	expiry, err := NewExtendEwayBillUseCase(db, clientOff, false).Extend(ctx, ExtendEwayBillCommand{EWBID: "e-off", TenantID: "tenant-1", ActorID: "u-1", Hours: 2})
	require.NoError(t, err)
	assert.Equal(t, base.Add(2*time.Hour), expiry.UTC())
	assert.Zero(t, clientOff.calls, "EWB_EXTEND_ENABLED=false must not call the provider")

	// Enabled seam: the provider's returned validity wins.
	seedExtendEWB(t, db, "e-on", "t-on", "tenant-1", "2026-09-02 10:00:00")
	clientOn := &fakeExtendClient{validUpto: providerExpiry}
	expiry, err = NewExtendEwayBillUseCase(db, clientOn, true).Extend(ctx, ExtendEwayBillCommand{EWBID: "e-on", TenantID: "tenant-1", ActorID: "u-1", Hours: 2})
	require.NoError(t, err)
	assert.Equal(t, providerExpiry, expiry.UTC())
	assert.Equal(t, providerExpiry, readValidUntil(t, db, "e-on").UTC())
	assert.Equal(t, 1, clientOn.calls)
	assert.Equal(t, "console_inline_extend", clientOn.lastReq.Reason)
	assert.Equal(t, "EWB-e-on", clientOn.lastReq.EwbNumber)

	// Enabled but provider errors: best-effort fallback to the local shift.
	seedExtendEWB(t, db, "e-err", "t-err", "tenant-1", "2026-09-02 10:00:00")
	clientErr := &fakeExtendClient{validUpto: providerExpiry, err: fmt.Errorf("provider down")}
	expiry, err = NewExtendEwayBillUseCase(db, clientErr, true).Extend(ctx, ExtendEwayBillCommand{EWBID: "e-err", TenantID: "tenant-1", ActorID: "u-1", Hours: 3})
	require.NoError(t, err)
	assert.Equal(t, base.Add(3*time.Hour), expiry.UTC())
	assert.Equal(t, 1, clientErr.calls)

	// Enabled with nil client: local shift (legacy condition client != nil).
	seedExtendEWB(t, db, "e-nil", "t-nil", "tenant-1", "2026-09-02 10:00:00")
	expiry, err = NewExtendEwayBillUseCase(db, nil, true).Extend(ctx, ExtendEwayBillCommand{EWBID: "e-nil", TenantID: "tenant-1", ActorID: "u-1", Hours: 1})
	require.NoError(t, err)
	assert.Equal(t, base.Add(time.Hour), expiry.UTC())
}

func TestExtendEwayBillUseCase_AuditTenantFallback(t *testing.T) {
	db := newEWBExtendTestDB(t)
	seedExtendEWB(t, db, "e-1", "t-1", "tenant-1", "2026-09-02 10:00:00")

	// No tenant in context: the audit row still falls back to the platform
	// tenant via shared.TenantOrPlatform, while scoping uses the command tenant.
	_, err := NewExtendEwayBillUseCase(db, nil, false).Extend(context.Background(), ExtendEwayBillCommand{
		EWBID: "e-1", TenantID: "tenant-1", ActorID: "u-1", Hours: 2,
	})
	require.NoError(t, err)
	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM audit_logs WHERE action = 'ewaybill.extend'`).Scan(&tenant))
	assert.Equal(t, string(shared.DefaultTenant), tenant)
}

func TestWriteAuditLog_RowMapping(t *testing.T) {
	db := newEWBExtendTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-2")
	require.NoError(t, WriteAuditLog(ctx, db, "u-9", "tenant-2", "dispatch.run_create", "planner_runs", "run-9", map[string]any{"ok": true}))
	var userID, action, tableName, recordID, newValues, tenantID string
	require.NoError(t, db.QueryRow(`SELECT user_id, action, table_name, record_id, new_values, tenant_id FROM audit_logs`).
		Scan(&userID, &action, &tableName, &recordID, &newValues, &tenantID))
	assert.Equal(t, "u-9", userID)
	assert.Equal(t, "dispatch.run_create", action)
	assert.Equal(t, "planner_runs", tableName)
	assert.Equal(t, "run-9", recordID)
	assert.Equal(t, "tenant-2", tenantID)
	assert.JSONEq(t, `{"ok":true}`, newValues)

	// nil DB is a no-op (legacy helper would have panicked; guard only).
	assert.NoError(t, WriteAuditLog(ctx, nil, "u-9", "tenant-2", "a", "t", "r", nil))
}
