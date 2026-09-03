package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

func setupTestAppForConsolidation(t *testing.T) (*App, *sql.DB) {
	db := newCustomersSelectedDB(t)
	_, _ = db.Exec(`UPDATE company_settings SET gst_enabled = 1, gst_rate = 18.0, state_code = '27', gst_number = '27AABCU9603R1ZX' WHERE id = 1`)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	app.Invoices = &InvoiceHandlers{App: app}
	app.Invoices.init()
	return app, db
}

func TestUnbilledTrips_API(t *testing.T) {
	app, db := setupTestAppForConsolidation(t)

	// Seed customer
	custID := "cust-corp-1"
	_, err := db.Exec(`
		INSERT INTO customers (id, customer_code, name, company, phone, payment_terms_days, tenant_id)
		VALUES (?, 'CUST-001', 'Reliance Logistics', 'Reliance Industries', '9820098200', 30, '1')
	`, custID)
	require.NoError(t, err)

	// Seed route
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('rt-1', 'Mumbai', 'Pune', 150.0, 4.0, 8000.0, '1')`)
	require.NoError(t, err)

	// Seed 2 bookings and 2 completed trips
	_, err = db.Exec(`
		INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, tenant_id)
		VALUES ('bk-1', 'BK-001', 'cust-corp-1', 'rt-1', datetime('now', '-3 days'), 'truck', 1, 12000, 'completed', '1'),
		       ('bk-2', 'BK-002', 'cust-corp-1', 'rt-1', datetime('now', '-2 days'), 'truck', 1, 15000, 'completed', '1')
	`)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, delivered_at, status, toll_costs, tenant_id)
		VALUES ('trp-1', 'TRP-001', 'bk-1', 'rt-1', ?, ?, 'delivered', 800, '1'),
		       ('trp-2', 'TRP-002', 'bk-2', 'rt-1', ?, ?, 'delivered', 1200, '1')
	`, now.Add(-72*time.Hour), now.Add(-48*time.Hour), now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	require.NoError(t, err)

	// Add detention to trip 1
	_, err = db.Exec(`
		INSERT INTO trip_detentions (id, tenant_id, trip_id, zone_kind, entered_at, billable_seconds, rate_per_hour, amount, status)
		VALUES ('det-1', '1', 'trp-1', 'drop', datetime('now', '-50 hours'), 7200, 500, 1000, 'open')
	`)
	require.NoError(t, err)

	// Setup Router
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// Request unbilled trips
	req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/cust-corp-1/unbilled-trips?format=json", nil), "1", "user-1", "admin")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var res map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &res)
	require.NoError(t, err)

	assert.Equal(t, float64(2), res["count"])
	unbilledList, ok := res["unbilled_trips"].([]interface{})
	require.True(t, ok)
	assert.Len(t, unbilledList, 2)

	firstTrip := unbilledList[0].(map[string]interface{})
	assert.Equal(t, "TRP-001", firstTrip["trip_number"])
	assert.Equal(t, float64(12000), firstTrip["freight"])
	assert.Equal(t, float64(800), firstTrip["tolls"])
	assert.Equal(t, float64(1000), firstTrip["detention"])
	assert.Equal(t, float64(13800), firstTrip["total"])
}

func TestConsolidateInvoices_CreationAndTerms(t *testing.T) {
	app, db := setupTestAppForConsolidation(t)

	// Seed customer with 30-day payment terms
	custID := "cust-tata-1"
	_, err := db.Exec(`
		INSERT INTO customers (id, customer_code, name, company, phone, payment_terms_days, tenant_id, state_code)
		VALUES (?, 'CUST-TATA', 'Tata Motors', 'Tata Group', '9811198111', 30, '1', '27')
	`, custID)
	require.NoError(t, err)

	// Seed route
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('rt-tata', 'Pune', 'Nagpur', 700.0, 14.0, 15000.0, '1')`)
	require.NoError(t, err)

	// Seed 2 bookings and 2 delivered trips
	_, err = db.Exec(`
		INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, tenant_id)
		VALUES ('bk-t1', 'BK-T01', 'cust-tata-1', 'rt-tata', datetime('now', '-5 days'), 'truck', 1, 20000, 'completed', '1'),
		       ('bk-t2', 'BK-T02', 'cust-tata-1', 'rt-tata', datetime('now', '-4 days'), 'truck', 1, 25000, 'completed', '1')
	`)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, delivered_at, status, toll_costs, tenant_id)
		VALUES ('trp-t1', 'TRP-T01', 'bk-t1', 'rt-tata', ?, ?, 'delivered', 1500, '1'),
		       ('trp-t2', 'TRP-T02', 'bk-t2', 'rt-tata', ?, ?, 'delivered', 2000, '1')
	`, now.Add(-96*time.Hour), now.Add(-72*time.Hour), now.Add(-72*time.Hour), now.Add(-48*time.Hour))
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	// Test POST consolidate with JSON
	payload := `{"trip_ids": ["trp-t1", "trp-t2"], "notes": "Monthly Consolidated Freight Billing - August"}`
	req := withTenantSession(httptest.NewRequest(http.MethodPost, "/customers/cust-tata-1/invoices/consolidate", strings.NewReader(payload)), "1", "user-1", "admin")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var res map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &res)
	require.NoError(t, err)

	invoiceID, ok := res["invoice_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, invoiceID)

	// Verification in Database
	var subtotal, tax, total, cgst, sgst float64
	var dueDateStr, status, paymentStatus string
	err = db.QueryRow(`
		SELECT subtotal, tax, total, cgst, sgst, due_date, status, payment_status
		FROM invoices WHERE id = ?
	`, invoiceID).Scan(&subtotal, &tax, &total, &cgst, &sgst, &dueDateStr, &status, &paymentStatus)
	require.NoError(t, err)

	// Subtotal = (20000 + 1500) + (25000 + 2000) = 48500
	assert.InDelta(t, 48500.0, subtotal, 0.01)
	// Intra-state 18% GST = 8730 (CGST 4365, SGST 4365)
	assert.InDelta(t, 8730.0, tax, 0.01)
	assert.InDelta(t, 4365.0, cgst, 0.01)
	assert.InDelta(t, 4365.0, sgst, 0.01)
	assert.InDelta(t, 57230.0, total, 0.01)
	assert.Equal(t, "outstanding", status)
	assert.Equal(t, "pending", paymentStatus)

	// Verify Payment Terms: DueDate should be ~30 days in the future
	var parsedDue time.Time
	if tParsed, err := time.Parse("2006-01-02 15:04:05", dueDateStr); err == nil {
		parsedDue = tParsed
	} else if tParsed, err := time.Parse(time.RFC3339, dueDateStr); err == nil {
		parsedDue = tParsed
	} else if tParsed, err := time.Parse("2006-01-02T15:04:05Z", dueDateStr); err == nil {
		parsedDue = tParsed
	} else if len(dueDateStr) >= 10 {
		parsedDue, _ = time.Parse("2006-01-02", dueDateStr[:10])
	}
	assert.False(t, parsedDue.IsZero())
	expectedDueMin := now.AddDate(0, 0, 29)
	expectedDueMax := now.AddDate(0, 0, 31)
	assert.True(t, parsedDue.After(expectedDueMin) && parsedDue.Before(expectedDueMax))

	// Verify Line Items in DB (Freight and Toll items generated for both trips)
	var lineCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items WHERE invoice_id = ?`, invoiceID).Scan(&lineCount)
	require.NoError(t, err)
	assert.Equal(t, 4, lineCount) // 2 freight + 2 toll lines

	// Verify that unbilled trips query now returns 0 trips
	unbilledCheck, err := app.Customers.fetchUnbilledTrips(context.Background(), shared.TenantID("1"), "cust-tata-1", nil, "", "")
	require.NoError(t, err)
	assert.Empty(t, unbilledCheck, "Delivered trips must no longer show as unbilled after consolidation")
}

func TestStatementOfAccount_LedgerAndAging(t *testing.T) {
	app, db := setupTestAppForConsolidation(t)

	custID := "cust-mahindra-1"
	_, err := db.Exec(`
		INSERT INTO customers (id, customer_code, name, company, phone, payment_terms_days, tenant_id, state_code)
		VALUES (?, 'CUST-MHD', 'Mahindra Logistics', 'Mahindra Group', '9833398333', 15, '1', '27')
	`, custID)
	require.NoError(t, err)

	now := time.Now()

	// 1. Invoices:
	// Invoice 1: Overdue (Due 45 days ago, Total 30000, Paid 10000, Balance 20000) -> 31-60 days bucket
	// Invoice 2: Overdue (Due 20 days ago, Total 15000, Paid 0, Balance 15000) -> 16-30 days bucket
	// Invoice 3: Current (Due in 10 days, Total 25000, Paid 0, Balance 25000) -> 0-15 days bucket
	inv1Date := now.Add(-60 * 24 * time.Hour)
	inv1Due := now.Add(-45 * 24 * time.Hour)
	inv2Date := now.Add(-35 * 24 * time.Hour)
	inv2Due := now.Add(-20 * 24 * time.Hour)
	inv3Date := now.Add(-5 * 24 * time.Hour)
	inv3Due := now.Add(10 * 24 * time.Hour)

	_, err = db.Exec(`
		INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, paid_amount, payment_status, status, due_date, created_at, tenant_id)
		VALUES ('inv-m1', 'INV-M01', 'bk-m1', 'cust-mahindra-1', 25423.73, 4576.27, 30000, 10000, 'partially_paid', 'outstanding', ?, ?, '1'),
		       ('inv-m2', 'INV-M02', 'bk-m2', 'cust-mahindra-1', 12711.86, 2288.14, 15000, 0, 'pending', 'outstanding', ?, ?, '1'),
		       ('inv-m3', 'INV-M03', 'bk-m3', 'cust-mahindra-1', 21186.44, 3813.56, 25000, 0, 'pending', 'outstanding', ?, ?, '1')
	`, inv1Due, inv1Date, inv2Due, inv2Date, inv3Due, inv3Date)
	require.NoError(t, err)

	// 2. Payments:
	payDate := now.Add(-30 * 24 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, tenant_id)
		VALUES ('pay-m1', 'inv-m1', ?, 10000, 'bank_transfer', 'NEFT-889900', '1')
	`, payDate)
	require.NoError(t, err)

	// 3. Unbilled Delivered Trip (WIP):
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES ('rt-m', 'Chakan', 'Zaheerabad', 500.0, 10.0, 18000.0, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, tenant_id)
		VALUES ('bk-m-wip', 'BK-MWIP', 'cust-mahindra-1', 'rt-m', ?, 'truck', 1, 18000, 'completed', '1')
	`, now.Add(-2*24*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, delivered_at, status, toll_costs, tenant_id)
		VALUES ('trp-m-wip', 'TRP-MWIP', 'bk-m-wip', 'rt-m', ?, ?, 'delivered', 1500, '1')
	`, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/cust-mahindra-1/statement?format=json", nil), "1", "user-1", "admin")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var st CustomerStatementData
	err = json.Unmarshal(w.Body.Bytes(), &st)
	require.NoError(t, err)

	// Verification of Summary Totals
	// Total Invoiced = 30000 + 15000 + 25000 = 70000
	assert.InDelta(t, 70000.0, st.TotalInvoiced, 0.01)
	// Total Paid = 10000
	assert.InDelta(t, 10000.0, st.TotalPaid, 0.01)
	// Outstanding Invoices = (30000-10000) + 15000 + 25000 = 60000
	assert.InDelta(t, 60000.0, st.OutstandingInvoices, 0.01)
	// Unbilled WIP Freight = 18000 + 1500 = 19500
	assert.InDelta(t, 19500.0, st.TotalUnbilledAmount, 0.01)
	// Net Balance Due = 60000 + 19500 = 79500
	assert.InDelta(t, 79500.0, st.NetBalanceDue, 0.01)

	// Verification of Credit Aging Buckets
	// CurrentOr15 = 25000 (Inv 3)
	assert.InDelta(t, 25000.0, st.Aging.CurrentOr15, 0.01)
	// Days16To30 = 15000 (Inv 2)
	assert.InDelta(t, 15000.0, st.Aging.Days16To30, 0.01)
	// Days31To60 = 20000 (Inv 1)
	assert.InDelta(t, 20000.0, st.Aging.Days31To60, 0.01)
	// Days60Plus = 0
	assert.InDelta(t, 0.0, st.Aging.Days60Plus, 0.01)
	// Total Overdue = 15000 + 20000 = 35000
	assert.InDelta(t, 35000.0, st.Aging.TotalOverdue, 0.01)

	// Verification of Chronological Ledger Transactions
	require.NotEmpty(t, st.Transactions)
	assert.Equal(t, 4, len(st.Transactions)) // Inv 1, Inv 2, Pay 1, Inv 3

	// First tx: Inv 1 (+30000 -> Bal 30000)
	assert.Equal(t, "Invoice", st.Transactions[0].Type)
	assert.InDelta(t, 30000.0, st.Transactions[0].Debit, 0.01)
	assert.InDelta(t, 30000.0, st.Transactions[0].Balance, 0.01)

	// Second tx: Inv 2 (+15000 -> Bal 45000)
	assert.Equal(t, "Invoice", st.Transactions[1].Type)
	assert.InDelta(t, 15000.0, st.Transactions[1].Debit, 0.01)
	assert.InDelta(t, 45000.0, st.Transactions[1].Balance, 0.01)

	// Third tx: Payment 1 (-10000 -> Bal 35000)
	assert.Equal(t, "Payment", st.Transactions[2].Type)
	assert.InDelta(t, 10000.0, st.Transactions[2].Credit, 0.01)
	assert.InDelta(t, 35000.0, st.Transactions[2].Balance, 0.01)

	// Fourth tx: Inv 3 (+25000 -> Bal 60000)
	assert.Equal(t, "Invoice", st.Transactions[3].Type)
	assert.InDelta(t, 25000.0, st.Transactions[3].Debit, 0.01)
	assert.InDelta(t, 60000.0, st.Transactions[3].Balance, 0.01)

	// Verify WhatsApp sharing link
	assert.NotEmpty(t, st.WhatsAppShareURL)
	assert.Contains(t, st.WhatsAppShareURL, "wa.me")
	assert.Contains(t, st.WhatsAppShareURL, "919833398333")
	assert.Contains(t, st.WhatsAppShareURL, url.QueryEscape("Mahindra Logistics"))
}
