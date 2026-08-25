package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// mountNoteRoutes wires a bare chi router with the tenant context middleware
// and the note handlers, mirroring the immutability-guard test rig (the
// permission middleware itself is exercised by auth_flow tests).
func mountNoteRoutes(app *App) *chi.Mux {
	notes := &CreditNoteHandlers{App: app}
	r := chi.NewRouter()
	tenantMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
	r.With(tenantMW).Post("/invoices/{id}/credit-note", notes.CreateCreditNote)
	r.With(tenantMW).Post("/invoices/{id}/debit-note", notes.CreateDebitNote)
	r.With(tenantMW).Get("/invoices/{id}/notes", notes.ListForInvoice)
	return r
}

// postNote builds a form POST against the note endpoints. Empty accept means
// browser-style form flow; "application/json" selects the JSON 201 path.
func postNote(t *testing.T, router *chi.Mux, method, path string, form string, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req = withSession(req, "user-1", "admin")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestCreditNoteHandlers_CreateAndList — happy paths: form POST → 303 with a
// persisted CN/DN row; JSON POST → 201 with the created note; GET list → JSON.
func TestCreditNoteHandlers_CreateAndList(t *testing.T) {
	db := newInvoiceGuardHandlerDB(t)
	app := newInvoiceGuardHandlerApp(t, db)
	router := mountNoteRoutes(app)
	insertGuardHandlerInvoice(t, db, "inv-happy")

	fy := sqliterepo.FinancialYear(time.Now())

	w := postNote(t, router, http.MethodPost, "/invoices/inv-happy/credit-note",
		"reason=rate+correction&taxable_value=100&cgst=9&sgst=9&place_of_supply=27", "")
	require.Equal(t, http.StatusSeeOther, w.Code, "form credit note must redirect back to the invoice")

	var (
		noteNumber, noteType string
		total                float64
		createdBy            sql.NullString
	)
	require.NoError(t, db.QueryRow(
		`SELECT note_number, note_type, total, created_by FROM credit_debit_notes WHERE invoice_id='inv-happy'`,
	).Scan(&noteNumber, &noteType, &total, &createdBy))
	assert.Equal(t, fmt.Sprintf("CN/%s/0001", fy), noteNumber)
	assert.Equal(t, "credit", noteType)
	assert.InDelta(t, 118.0, total, 0.001, "total = taxable + cgst + sgst")
	assert.Equal(t, "user-1", createdBy.String, "created_by must come from the session user")

	// Debit note: independent counter starting at 0001, no upper bound.
	w = postNote(t, router, http.MethodPost, "/invoices/inv-happy/debit-note",
		"reason=extra+stop&taxable_value=99999", "")
	require.Equal(t, http.StatusSeeOther, w.Code, "debit above invoice total must pass")
	var dnNumber string
	require.NoError(t, db.QueryRow(
		`SELECT note_number FROM credit_debit_notes WHERE invoice_id='inv-happy' AND note_type='debit'`,
	).Scan(&dnNumber))
	assert.Equal(t, fmt.Sprintf("DN/%s/0001", fy), dnNumber)

	// JSON create → 201 Created with the note payload.
	w = postNote(t, router, http.MethodPost, "/invoices/inv-happy/credit-note",
		"reason=post-supply+discount&taxable_value=50", "application/json")
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), fmt.Sprintf("CN/%s/0002", fy))
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// List endpoint returns both types as JSON.
	w = postNote(t, router, http.MethodGet, "/invoices/inv-happy/notes", "", "application/json")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, fmt.Sprintf("CN/%s/0001", fy))
	assert.Contains(t, body, fmt.Sprintf("DN/%s/0001", fy))

	// Ledger hook: one adjustment debit for each credit note.
	var ledgerRows int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM money_ledger WHERE ref_table='credit_notes' AND direction='debit'`).Scan(&ledgerRows))
	assert.Equal(t, 2, ledgerRows, "each credit note must append an adjustment debit entry")
}

// TestCreditNoteHandlers_ErrorPaths covers validation and guard failures:
// 400 malformed input / missing reason, 409 over-total credit, 404 foreign or
// bogus invoice.
func TestCreditNoteHandlers_ErrorPaths(t *testing.T) {
	cases := []struct {
		name     string
		invID    string
		setup    func(t *testing.T, db *sql.DB)
		form     string
		wantCode int
		wantBody string
	}{
		{
			name:  "missing_reason_400",
			invID: "inv-err-1",
			setup: func(t *testing.T, db *sql.DB) {
				insertGuardHandlerInvoice(t, db, "inv-err-1")
			},
			form:     "taxable_value=100",
			wantCode: http.StatusBadRequest,
			wantBody: "reason is required",
		},
		{
			name:  "bad_amount_400",
			invID: "inv-err-2",
			setup: func(t *testing.T, db *sql.DB) {
				insertGuardHandlerInvoice(t, db, "inv-err-2")
			},
			form:     "reason=why&taxable_value=abc",
			wantCode: http.StatusBadRequest,
			wantBody: "taxable_value must be a number",
		},
		{
			name:  "zero_total_400",
			invID: "inv-err-3",
			setup: func(t *testing.T, db *sql.DB) {
				insertGuardHandlerInvoice(t, db, "inv-err-3")
			},
			form:     "reason=why&taxable_value=0",
			wantCode: http.StatusBadRequest,
			wantBody: "note total must be greater than zero",
		},
		{
			name:  "credit_over_total_409",
			invID: "inv-err-4",
			setup: func(t *testing.T, db *sql.DB) {
				insertGuardHandlerInvoice(t, db, "inv-err-4")
			},
			form:     "reason=too+much&taxable_value=999999",
			wantCode: http.StatusConflict,
			wantBody: "exceeds invoice total",
		},
		{
			name:     "bogus_invoice_404",
			invID:    "inv-err-missing",
			setup:    func(t *testing.T, db *sql.DB) {},
			form:     "reason=why&taxable_value=10",
			wantCode: http.StatusNotFound,
			wantBody: "Invoice not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newInvoiceGuardHandlerDB(t)
			app := newInvoiceGuardHandlerApp(t, db)
			router := mountNoteRoutes(app)
			tc.setup(t, db)

			w := postNote(t, router, http.MethodPost, "/invoices/"+tc.invID+"/credit-note", tc.form, "")
			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantBody != "" {
				assert.Contains(t, w.Body.String(), tc.wantBody)
			}

			var rows int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM credit_debit_notes`).Scan(&rows))
			assert.Equal(t, 0, rows, "failed requests must not persist notes")
		})
	}
}

// TestCreditNoteHandlers_CreditCapBoundary — successive credits up to exactly
// the invoice total succeed; the next paisa is rejected with 409.
func TestCreditNoteHandlers_CreditCapBoundary(t *testing.T) {
	db := newInvoiceGuardHandlerDB(t)
	app := newInvoiceGuardHandlerApp(t, db)
	router := mountNoteRoutes(app)
	insertGuardHandlerInvoice(t, db, "inv-boundary") // total 1180

	w := postNote(t, router, http.MethodPost, "/invoices/inv-boundary/credit-note",
		"reason=first&taxable_value=1062", "")
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = postNote(t, router, http.MethodPost, "/invoices/inv-boundary/credit-note",
		"reason=fills+the+headroom&taxable_value=118", "")
	require.Equal(t, http.StatusSeeOther, w.Code, "credits summing exactly to the invoice total are legal")

	w = postNote(t, router, http.MethodPost, "/invoices/inv-boundary/credit-note",
		"reason=one+paisa+over&taxable_value=0.02", "")
	assert.Equal(t, http.StatusConflict, w.Code)
}
