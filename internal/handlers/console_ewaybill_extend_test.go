package handlers

// Focused HTTP tests for the Command Center inline EWB extend action
// (handlers-end-state ADR, Phase 2): body validation + status codes stay in
// the handler while the migration, provider seam, event and audit writes live
// in internal/controltower/application (covered by ewaybill_extend_test.go).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/shared"
)

const consoleExtendTestSchema = `
CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL);
CREATE TABLE eway_bills (id TEXT PRIMARY KEY, trip_id TEXT, ewb_number TEXT NOT NULL, valid_until DATETIME);
CREATE TABLE eway_bill_events (id TEXT PRIMARY KEY, ewb_number TEXT NOT NULL, trip_id TEXT, event_type TEXT NOT NULL, payload TEXT, created_by TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE audit_logs (id TEXT PRIMARY KEY, user_id TEXT, action TEXT NOT NULL, table_name TEXT NOT NULL, record_id TEXT, new_values TEXT, tenant_id TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
`

type consoleExtendFakeClient struct {
	calls     int
	validUpto time.Time
}

func (f *consoleExtendFakeClient) Generate(context.Context, intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *consoleExtendFakeClient) Get(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *consoleExtendFakeClient) Cancel(context.Context, string, string) (intEWB.Cancellation, error) {
	return intEWB.Cancellation{}, nil
}
func (f *consoleExtendFakeClient) GeneratePartA(context.Context, intEWB.GenerateRequest) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *consoleExtendFakeClient) AttachPartB(context.Context, string, string, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *consoleExtendFakeClient) Extend(_ context.Context, ewbNumber string, _ intEWB.ExtendRequest) (intEWB.EWayBill, error) {
	f.calls++
	return intEWB.EWayBill{EwbNumber: ewbNumber, ValidUpto: f.validUpto}, nil
}
func (f *consoleExtendFakeClient) GetByNumber(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}
func (f *consoleExtendFakeClient) GetByTrip(context.Context, string) (intEWB.EWayBill, error) {
	return intEWB.EWayBill{}, nil
}

func newConsoleExtendTestEnv(t *testing.T) (*sql.DB, *chi.Mux, *ConsoleHandlers) {
	t.Helper()
	name := fmt.Sprintf("console_extend_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(consoleExtendTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('t-1', 'tenant-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO eway_bills (id, trip_id, ewb_number, valid_until) VALUES ('e-1', 't-1', 'EWB-1', '2026-09-02 10:00:00')`)
	require.NoError(t, err)

	h := &ConsoleHandlers{db: db}
	r := chi.NewRouter()
	r.Post("/api/ewaybill/{id}/extend", h.ExtendEwayBill)
	return db, r, h
}

func consoleExtendRequest(t *testing.T, path, tenant, userID, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
	if userID != "" {
		ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: userID})
	}
	return req.WithContext(ctx)
}

func TestConsoleExtendEwayBill_ValidationStatusCodes(t *testing.T) {
	_, r, _ := newConsoleExtendTestEnv(t)

	for _, body := range []string{`{}`, `{"valid_upto_hours":0}`, `{"valid_upto_hours":25}`, `{"valid_upto_hours":-2}`, `not-json`} {
		req := consoleExtendRequest(t, "/api/ewaybill/e-1/extend", "tenant-1", "u-1", body)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, body)
		assert.Contains(t, w.Body.String(), "valid_upto_hours must be between 1 and 24")
	}

	// Unknown id -> 404 (no row touched).
	req := consoleExtendRequest(t, "/api/ewaybill/e-ghost/extend", "tenant-1", "u-1", `{"valid_upto_hours":4}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Cross-tenant id -> 404 (tenant scope preserved).
	req = consoleExtendRequest(t, "/api/ewaybill/e-1/extend", "tenant-2", "u-2", `{"valid_upto_hours":4}`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestConsoleExtendEwayBill_SuccessAndProviderSeam(t *testing.T) {
	db, r, h := newConsoleExtendTestEnv(t)

	// Local shift path (EWB_EXTEND_ENABLED=false).
	req := consoleExtendRequest(t, "/api/ewaybill/e-1/extend", "tenant-1", "u-1", `{"valid_upto_hours":4}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, "2026-09-02T14:00:00Z", resp["new_expiry"])

	var validUntil time.Time
	require.NoError(t, db.QueryRow(`SELECT valid_until FROM eway_bills WHERE id = 'e-1'`).Scan(&validUntil))
	assert.Equal(t, time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC), validUntil.UTC())
	var events, audits int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM eway_bill_events WHERE ewb_number = 'EWB-1'`).Scan(&events))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'ewaybill.extend'`).Scan(&audits))
	assert.Equal(t, 1, events)
	assert.Equal(t, 1, audits)
	var auditUser, auditTenant string
	require.NoError(t, db.QueryRow(`SELECT user_id, tenant_id FROM audit_logs WHERE action = 'ewaybill.extend'`).Scan(&auditUser, &auditTenant))
	assert.Equal(t, "u-1", auditUser)
	assert.Equal(t, "tenant-1", auditTenant)

	// Provider-enabled path: the adapter's returned validity wins.
	providerExpiry := time.Date(2026, 9, 6, 9, 30, 0, 0, time.UTC)
	fake := &consoleExtendFakeClient{validUpto: providerExpiry}
	h.WithEwayBillAdapter(fake, true)
	req = consoleExtendRequest(t, "/api/ewaybill/e-1/extend", "tenant-1", "u-1", `{"valid_upto_hours":2}`)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "2026-09-06T09:30:00Z", resp["new_expiry"])
	assert.Equal(t, 1, fake.calls)
}
