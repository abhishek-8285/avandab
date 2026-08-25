package test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/integration"
	"transport-app/internal/integration/gstn"
)

func seedIRNCancelInvoice(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO invoices (id, tenant_id, invoice_number, booking_id, customer_id, subtotal, tax, discount, total, payment_status, status, created_at, updated_at)
		VALUES (?, '1', ?, 'book-x', 'cust-x', 1000.0, 180.0, 0.0, 1180.0, 'pending', 'outstanding', datetime('now'), datetime('now'))`, id, "INV-"+id)
	require.NoError(t, err)
}

func setInvoiceIRN(t *testing.T, db *sql.DB, id, irn, ackDate string) {
	t.Helper()
	_, err := db.Exec(`UPDATE invoices SET irn = ?, irn_ack_no = 'ACK0001', irn_ack_date = ? WHERE id = ?`, irn, ackDate, id)
	require.NoError(t, err)
}

func postIRNCancel(r chi.Router, invoiceID string, reason int, remark string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]interface{}{"cancel_reason": reason, "cancel_remark": remark})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/integrations/gstn/einvoice/%s/cancel", invoiceID), bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIRN_Cancel_Handler(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := integration.Config{GSTN: gstn.Config{Enabled: true, UseMock: true}}
	h := integration.NewHandler(cfg, &stubAuthSvc{}, db)
	r := chi.NewRouter()
	r.Use(authInjectMiddleware)
	h.Register(r)

	oldAck := time.Now().UTC().Add(-25 * time.Hour).Format("2006-01-02 15:04:05")
	freshAck := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")

	for _, id := range []string{"inv-noirn", "inv-old", "inv-ok", "inv-fallback"} {
		seedIRNCancelInvoice(t, db, id)
	}
	// invoices.irn has a UNIQUE partial index (00048) — each seed needs its own IRN.
	setInvoiceIRN(t, db, "inv-old", strings.Repeat("a", 64), oldAck)
	setInvoiceIRN(t, db, "inv-ok", strings.Repeat("b", 64), freshAck)
	setInvoiceIRN(t, db, "inv-fallback", strings.Repeat("c", 64), "") // no ack date -> created_at fallback

	// A) Invoice without IRN -> 400
	w := postIRNCancel(r, "inv-noirn", 1, "dup")
	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())

	// B) Ack date older than 24h -> 409
	w = postIRNCancel(r, "inv-old", 2, "order cancelled")
	assert.Equal(t, http.StatusConflict, w.Code, "body: %s", w.Body.String())

	// C) Unknown invoice -> 404
	w = postIRNCancel(r, "inv-missing", 1, "x")
	assert.Equal(t, http.StatusNotFound, w.Code)

	// D) Invalid cancel_reason -> 400
	w = postIRNCancel(r, "inv-ok", 9, "bad reason")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// E) Happy path within 24h -> 200 + irn_cancelled_at set
	var resp gstn.CancelIRNResponse
	{
		w = postIRNCancel(r, "inv-ok", 3, "data entry error")
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Cancelled)
		assert.Equal(t, strings.Repeat("b", 64), resp.IRN)
		assert.Equal(t, "data entry error", resp.Remark)

		var cancelledAt sql.NullString
		err = db.QueryRow(`SELECT irn_cancelled_at FROM invoices WHERE id='inv-ok'`).Scan(&cancelledAt)
		require.NoError(t, err)
		require.True(t, cancelledAt.Valid && cancelledAt.String != "", "irn_cancelled_at must be recorded")

		// Audit trail written
		var audits int
		err = db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='irn_cancelled' AND record_id='inv-ok'`).Scan(&audits)
		require.NoError(t, err)
		assert.Equal(t, 1, audits)
	}

	// F) Double cancel -> 409
	w = postIRNCancel(r, "inv-ok", 3, "again")
	assert.Equal(t, http.StatusConflict, w.Code)

	// G) Missing/unparseable ack date falls back to created_at -> 200
	{
		w = postIRNCancel(r, "inv-fallback", 1, "duplicate")
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		var cancelledAt sql.NullString
		err := db.QueryRow(`SELECT irn_cancelled_at FROM invoices WHERE id='inv-fallback'`).Scan(&cancelledAt)
		require.NoError(t, err)
		assert.True(t, cancelledAt.Valid && cancelledAt.String != "")
	}
}

func TestIRN_Cancel_MigrationRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	columnExists := func() int {
		var n int
		err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('invoices') WHERE name='irn_cancelled_at'`).Scan(&n)
		require.NoError(t, err)
		return n
	}
	require.Equal(t, 1, columnExists(), "irn_cancelled_at must exist after goose up")

	require.NoError(t, goose.DownTo(db, "../db/migrations", 98), "down to 98 failed")
	assert.Equal(t, 0, columnExists(), "irn_cancelled_at must be dropped after rollback")
}

func TestIRN_Cancel_GSTNDisabled_Returns503(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := integration.Config{GSTN: gstn.Config{Enabled: false, UseMock: true}}
	h := integration.NewHandler(cfg, &stubAuthSvc{}, db)
	r := chi.NewRouter()
	r.Use(authInjectMiddleware)
	h.Register(r)

	seedIRNCancelInvoice(t, db, "inv-dis")
	setInvoiceIRN(t, db, "inv-dis", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2",
		time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05"))

	w := postIRNCancel(r, "inv-dis", 1, "dup")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// Nothing persisted on failure
	var cancelledAt sql.NullString
	err := db.QueryRow(`SELECT irn_cancelled_at FROM invoices WHERE id='inv-dis'`).Scan(&cancelledAt)
	require.NoError(t, err)
	assert.False(t, cancelledAt.Valid && cancelledAt.String != "", "must not mark cancelled when GSTN rejects")
}
