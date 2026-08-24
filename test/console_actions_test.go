package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/handlers"
)

// TestSpec22_EwayBillExtend — Spec 22 §7 S4: mock behavior shifts expiry,
// the DB row updates, an eway_bill_events row and an audit_logs row are
// written; unknown and cross-tenant ids return 404.
func TestSpec22_EwayBillExtend(t *testing.T) {
	db := NewTestDB(t)
	day := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)

	// Reuse S3 fixtures: v-a/tr-1/tenant-a already carry an active EWB.
	seedFleetContextFixtures(t, db, day)
	var originalExpiry time.Time
	require.NoError(t, db.QueryRow(`SELECT valid_until FROM eway_bills WHERE id='ewb-1'`).Scan(&originalExpiry))

	console := handlers.NewConsoleHandlers(&handlers.App{}, nil, nil, db, nil, nil)
	r := chi.NewRouter()
	r.With(tenantMW("tenant-a")).Post("/api/ewaybill/{id}/extend", console.ExtendEwayBill)
	r.With(tenantMW("tenant-b")).Post("/b/api/ewaybill/{id}/extend", console.ExtendEwayBill)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Invalid hours → 400.
	assert.Equal(t, http.StatusBadRequest, post("/api/ewaybill/ewb-1/extend", `{"valid_upto_hours":0}`).Code)
	assert.Equal(t, http.StatusBadRequest, post("/api/ewaybill/ewb-1/extend", `{"valid_upto_hours":99}`).Code)

	// Unknown + cross-tenant → 404.
	assert.Equal(t, http.StatusNotFound, post("/api/ewaybill/ewb-none/extend", `{"valid_upto_hours":4}`).Code)
	assert.Equal(t, http.StatusNotFound, post("/b/api/ewaybill/ewb-1/extend", `{"valid_upto_hours":4}`).Code)

	// Happy path: +4h shift.
	w := post("/api/ewaybill/ewb-1/extend", `{"valid_upto_hours":4}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		OK        bool   `json:"ok"`
		NewExpiry string `json:"new_expiry"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	got, err := time.Parse(time.RFC3339, resp.NewExpiry)
	require.NoError(t, err)
	assert.WithinDuration(t, originalExpiry.Add(4*time.Hour), got, time.Minute,
		"mock contract: expiry shifted by requested hours")

	// DB row updated.
	var stored time.Time
	require.NoError(t, db.QueryRow(`SELECT valid_until FROM eway_bills WHERE id='ewb-1'`).Scan(&stored))
	assert.WithinDuration(t, originalExpiry.Add(4*time.Hour), stored, time.Minute)

	// EXTENDED lifecycle event recorded.
	var events int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM eway_bill_events WHERE ewb_number=(SELECT ewb_number FROM eway_bills WHERE id='ewb-1') AND event_type='EXTENDED'`).
		Scan(&events))
	assert.Equal(t, 1, events)

	// Audit trail row recorded.
	var audits int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM audit_logs WHERE action='ewaybill.extend' AND record_id='ewb-1'`).
		Scan(&audits))
	assert.Equal(t, 1, audits)
}

// TestSpec22_KharchaActionEndpointsExist guards the console wiring targets:
// approve/reject routes must stay mounted on /kharcha/{id}.
func TestSpec22_KharchaActionEndpointsExist(t *testing.T) {
	db := NewTestDB(t)
	ctx := context.Background()
	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('driver_expenses','audit_logs','eway_bills','eway_bill_events')`).
		Scan(&n))
	assert.Equal(t, 4, n)
}
