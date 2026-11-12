package application

// Characterization + extraction tests for the consolidated-invoice and
// customer-statement paths moved out of
// internal/handlers/invoices_consolidate.go (handlers-end-state ADR, Phase 2):
// tenant isolation, GST consolidation math, detention annexure + attachment,
// credit-policy due-date precedence, financial-year numbering (with fallback),
// the consolidation transaction boundary and the statement aging/ledger reads
// (including credit/debit notes).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/domain/customer"
	"transport-app/internal/domain/types"
)

func newBillingTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_billing_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, "../../../db/migrations"))

	mustExec(t, db, `INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-1', 'Tenant One', 'tenant-one'), ('t-2', 'Tenant Two', 'tenant-two')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func billingStrPtr(s string) *string { return &s }

func billingIntPtr(n int) *int { return &n }

func seedBillingCompany(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `UPDATE company_settings SET gst_enabled = 1, gst_rate = 18.0, state_code = '27',
		gst_number = '27AABCU9603R1ZX', invoice_prefix = 'INV' WHERE id = 1`)
}

func seedBillingCustomer(t *testing.T, db *sql.DB, id, tenantID, gst, stateCode string, terms int) customer.Customer {
	t.Helper()
	mustExec(t, db, `INSERT INTO customers (id, customer_code, name, company, phone, payment_terms_days, tenant_id, state_code, gst)
		VALUES (?, ?, ?, ?, '9820098200', ?, ?, ?, ?)`,
		id, "C-"+id, "Customer "+id, "Company "+id, terms, tenantID, stateCode, gst)
	return customer.Customer{
		ID:               types.CustomerID(id),
		CustomerCode:     "C-" + id,
		Name:             "Customer " + id,
		Phone:            "9820098200",
		TenantID:         tenantID,
		PaymentTermsDays: terms,
		GST:              billingStrPtr(gst),
		StateCode:        billingStrPtr(stateCode),
	}
}

// seedBillingTrip creates the route/booking/trip chain of one delivered trip.
func seedBillingTrip(t *testing.T, db *sql.DB, tenantID, customerID, suffix string, price, tolls float64, deliveredAt time.Time) string {
	t.Helper()
	routeID := "route-" + suffix + "-" + tenantID
	bookingID := "book-" + suffix + "-" + tenantID
	tripID := "trip-" + suffix + "-" + tenantID
	mustExec(t, db, `INSERT OR IGNORE INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES (?, 'Mumbai', 'Pune', 150.0, 4.0, ?, ?)`, routeID, price, tenantID)
	mustExec(t, db, `INSERT INTO bookings (id, booking_number, customer_id, route_id, pickup_date, vehicle_type, passengers, price, status, tenant_id)
		VALUES (?, ?, ?, ?, ?, 'truck', 1, ?, 'completed', ?)`,
		bookingID, "BK-"+suffix+"-"+tenantID, customerID, routeID, deliveredAt.Add(-24*time.Hour), price, tenantID)
	mustExec(t, db, `INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, delivered_at, status, toll_costs, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, 'delivered', ?, ?)`,
		tripID, "TRP-"+suffix+"-"+tenantID, bookingID, routeID, deliveredAt.Add(-12*time.Hour), deliveredAt, tolls, tenantID)
	return tripID
}

func seedBillingDetention(t *testing.T, db *sql.DB, tenantID, tripID, id string, seconds int64, rate, amount float64) {
	t.Helper()
	mustExec(t, db, `INSERT INTO trip_detentions (id, tenant_id, trip_id, zone_kind, entered_at, billable_seconds, rate_per_hour, amount, status)
		VALUES (?, ?, ?, 'drop', datetime('now', '-50 hours'), ?, ?, ?, 'open')`,
		id, tenantID, tripID, seconds, rate, amount)
}

func billingSettings() ConsolidationSettings {
	return ConsolidationSettings{
		GSTEnabled:    true,
		GSTRate:       18.0,
		StateCode:     "27",
		GSTNumber:     billingStrPtr("27AABCU9603R1ZX"),
		InvoicePrefix: "INV",
	}
}

func TestUnbilledTrips_TenantScopedReadModel(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	now := time.Now()

	seedBillingCustomer(t, db, "cust-a", "t-1", "27ABCDE1234F1Z5", "27", 30)
	seedBillingCustomer(t, db, "cust-b", "t-2", "29ABCDE1234F1Z1", "29", 15)
	tripA := seedBillingTrip(t, db, "t-1", "cust-a", "a", 12000, 800, now.Add(-48*time.Hour))
	seedBillingTrip(t, db, "t-2", "cust-b", "b", 9999, 0, now.Add(-24*time.Hour))
	seedBillingDetention(t, db, "t-1", tripA, "det-a", 7200, 500, 1000)

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-a"})
	require.NoError(t, err)
	require.Len(t, trips, 1)
	assert.Equal(t, tripA, trips[0].ID)
	assert.Equal(t, "TRP-a-t-1", trips[0].TripNumber)
	assert.InDelta(t, 12000.0, trips[0].Freight, 0.001)
	assert.InDelta(t, 800.0, trips[0].Tolls, 0.001)
	assert.InDelta(t, 1000.0, trips[0].Detention, 0.001)
	assert.InDelta(t, 13800.0, trips[0].Total, 0.001)
	require.NotNil(t, trips[0].DeliveredAt)

	// Cross-tenant reads never see the other tenant's bookings.
	crossTenant, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-2", CustomerID: "cust-a"})
	require.NoError(t, err)
	assert.Empty(t, crossTenant, "a tenant must never see another tenant's billing candidates")

	// Trip-ID filter is applied after the tenant scoping.
	filtered, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-a", TripIDs: []string{"does-not-exist"}})
	require.NoError(t, err)
	assert.Empty(t, filtered)

	// Once a live invoice references the trip it stops being unbilled.
	mustExec(t, db, `INSERT INTO invoices (id, invoice_number, booking_id, customer_id, trip_id, subtotal, tax, total, status, tenant_id)
		VALUES ('inv-a', 'INV-A', 'book-a-t-1', 'cust-a', ?, 0, 0, 0, 'outstanding', 't-1')`, tripA)
	afterInvoice, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-a"})
	require.NoError(t, err)
	assert.Empty(t, afterInvoice, "invoiced trips must not show up as unbilled")
}

func TestConsolidateInvoices_IntraStateGSTAndStatusTransitions(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	now := time.Now()
	cust := seedBillingCustomer(t, db, "cust-intra", "t-1", "27ABCDE1234F1Z5", "27", 30)
	seedBillingTrip(t, db, "t-1", "cust-intra", "a", 20000, 1500, now.Add(-72*time.Hour))
	seedBillingTrip(t, db, "t-1", "cust-intra", "b", 25000, 2000, now.Add(-48*time.Hour))
	seedBillingDetention(t, db, "t-1", "trip-a-t-1", "det-a", 7200, 500, 1000)

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-intra"})
	require.NoError(t, err)
	require.Len(t, trips, 2)
	assert.InDelta(t, 22500.0, trips[0].Total, 0.001, "freight + toll + detention")
	assert.InDelta(t, 27000.0, trips[1].Total, 0.001)

	uc := NewConsolidateInvoicesUseCase(db)
	res, err := uc.Execute(ctx, ConsolidateInvoicesCommand{
		TenantID:   "t-1",
		CustomerID: "cust-intra",
		Customer:   &cust,
		Settings:   billingSettings(),
		Trips:      trips,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.InvoiceID)
	assert.True(t, strings.HasPrefix(res.InvoiceNumber, "INV/"), "sequential GST invoice number: %s", res.InvoiceNumber)

	// Intra-state 18% GST (9% CGST + 9% SGST) over the annexure lines:
	// subtotal = 20000+1500+1000+25000+2000, each line rounded to paise.
	assert.InDelta(t, 49500.0, res.Subtotal, 0.001)
	assert.InDelta(t, 8910.0, res.Tax, 0.001)
	assert.InDelta(t, 4455.0, res.CGST, 0.001)
	assert.InDelta(t, 4455.0, res.SGST, 0.001)
	assert.InDelta(t, 0.0, res.IGST, 0.001)
	assert.InDelta(t, 58410.0, res.Total, 0.001)
	assert.WithinDuration(t, now.AddDate(0, 0, 30), res.DueDate, 2*time.Minute)

	var subtotal, tax, cgst, sgst, igst, total float64
	var status, paymentStatus, tenantID, invoiceNumber, dueDate string
	err = db.QueryRow(`SELECT subtotal, tax, cgst, sgst, igst, total, status, payment_status, tenant_id, invoice_number, due_date
		FROM invoices WHERE id = ?`, res.InvoiceID).
		Scan(&subtotal, &tax, &cgst, &sgst, &igst, &total, &status, &paymentStatus, &tenantID, &invoiceNumber, &dueDate)
	require.NoError(t, err)
	assert.InDelta(t, 49500.0, subtotal, 0.001)
	assert.InDelta(t, 8910.0, tax, 0.001)
	assert.InDelta(t, 4455.0, cgst, 0.001)
	assert.InDelta(t, 4455.0, sgst, 0.001)
	assert.InDelta(t, 0.0, igst, 0.001)
	assert.InDelta(t, 58410.0, total, 0.001)
	assert.Equal(t, "outstanding", status)
	assert.Equal(t, "pending", paymentStatus)
	assert.Equal(t, "t-1", tenantID)
	assert.Equal(t, res.InvoiceNumber, invoiceNumber)
	assert.Contains(t, dueDate, now.AddDate(0, 0, 30).Format("2006-01-02"))

	// 2 freight + 2 toll + 1 detention annexure line.
	var lineCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_line_items WHERE invoice_id = ?`, res.InvoiceID).Scan(&lineCount))
	assert.Equal(t, 5, lineCount)

	var qty, rate, amount float64
	var refID, lineType string
	require.NoError(t, db.QueryRow(`SELECT quantity, rate, amount, ref_id, line_type FROM invoice_line_items
		WHERE invoice_id = ? AND line_type = 'detention'`, res.InvoiceID).Scan(&qty, &rate, &amount, &refID, &lineType))
	assert.InDelta(t, 2.0, qty, 0.001, "7200 billable seconds = 2 hours")
	assert.InDelta(t, 500.0, rate, 0.001)
	assert.InDelta(t, 1000.0, amount, 0.001)
	assert.Equal(t, "det-a", refID)

	var detentionStatus string
	require.NoError(t, db.QueryRow(`SELECT status FROM trip_detentions WHERE id = 'det-a'`).Scan(&detentionStatus))
	assert.Equal(t, "attached", detentionStatus, "consolidated detention rows are marked attached in-tx")

	var lastNumber int64
	require.NoError(t, db.QueryRow(`SELECT last_number FROM invoice_sequences WHERE tenant_id = 't-1'`).Scan(&lastNumber))
	assert.Equal(t, int64(1), lastNumber)

	remaining, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-intra"})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	_, err = uc.Execute(ctx, ConsolidateInvoicesCommand{TenantID: "t-1", CustomerID: "cust-intra", Customer: &cust, Settings: billingSettings()})
	assert.ErrorIs(t, err, ErrNoUnbilledTrips)

	// Tenant isolation: another tenant's trips are never pulled into this
	// consolidation and the write stays on the acting tenant.
	seedBillingCustomer(t, db, "cust-other", "t-2", "29ABCDE1234F1Z1", "29", 15)
	seedBillingTrip(t, db, "t-2", "cust-other", "o", 5000, 0, now.Add(-24*time.Hour))
	otherTrips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-2", CustomerID: "cust-other"})
	require.NoError(t, err)
	require.Len(t, otherTrips, 1, "the other tenant's trip stays unbilled and untouched")

	var t1Invoices, t2Invoices int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE tenant_id = 't-1'`).Scan(&t1Invoices))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE tenant_id = 't-2'`).Scan(&t2Invoices))
	assert.Equal(t, 1, t1Invoices)
	assert.Zero(t, t2Invoices, "no cross-tenant invoice rows")
}

func TestConsolidateInvoices_InterStateIGSTAndPaiseRounding(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	cust := seedBillingCustomer(t, db, "cust-inter", "t-1", "29ABCDE1234F1Z1", "29", 15)
	seedBillingTrip(t, db, "t-1", "cust-inter", "a", 1234.56, 99.99, time.Now().Add(-24*time.Hour))

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-inter"})
	require.NoError(t, err)
	require.Len(t, trips, 1)

	res, err := NewConsolidateInvoicesUseCase(db).Execute(ctx, ConsolidateInvoicesCommand{
		TenantID:   "t-1",
		CustomerID: "cust-inter",
		Customer:   &cust,
		Settings:   billingSettings(),
		Trips:      trips,
	})
	require.NoError(t, err)

	// Inter-state: full 18% IGST, no CGST/SGST; line tax rounds to paise.
	assert.InDelta(t, 1334.55, res.Subtotal, 0.001)
	assert.InDelta(t, 240.22, res.IGST, 0.001)
	assert.InDelta(t, 240.22, res.Tax, 0.001)
	assert.InDelta(t, 0.0, res.CGST, 0.001)
	assert.InDelta(t, 0.0, res.SGST, 0.001)
	assert.InDelta(t, 1574.77, res.Total, 0.001)

	var igstSum, cgstSum, sgstSum float64
	require.NoError(t, db.QueryRow(`SELECT COALESCE(SUM(igst_amount),0), COALESCE(SUM(cgst_amount),0), COALESCE(SUM(sgst_amount),0)
		FROM invoice_line_items WHERE invoice_id = ?`, res.InvoiceID).Scan(&igstSum, &cgstSum, &sgstSum))
	assert.InDelta(t, 240.22, igstSum, 0.001)
	assert.InDelta(t, 0.0, cgstSum, 0.001)
	assert.InDelta(t, 0.0, sgstSum, 0.001)
}

func TestConsolidateInvoices_IntraStatePaiseRounding(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	cust := seedBillingCustomer(t, db, "cust-round", "t-1", "27ABCDE1234F1Z5", "27", 15)
	seedBillingTrip(t, db, "t-1", "cust-round", "a", 333.33, 0, time.Now().Add(-24*time.Hour))

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-round"})
	require.NoError(t, err)
	require.Len(t, trips, 1)

	res, err := NewConsolidateInvoicesUseCase(db).Execute(ctx, ConsolidateInvoicesCommand{
		TenantID: "t-1", CustomerID: "cust-round", Customer: &cust, Settings: billingSettings(), Trips: trips,
	})
	require.NoError(t, err)
	// 333.33 * 9% = 29.9997 -> 30.00 per side (legacy roundMoney).
	assert.InDelta(t, 333.33, res.Subtotal, 0.001)
	assert.InDelta(t, 30.0, res.CGST, 0.001)
	assert.InDelta(t, 30.0, res.SGST, 0.001)
	assert.InDelta(t, 60.0, res.Tax, 0.001)
	assert.InDelta(t, 393.33, res.Total, 0.001)
}

func TestConsolidateInvoices_DueDatePrecedence(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	now := time.Now()
	cust := seedBillingCustomer(t, db, "cust-due", "t-1", "27ABCDE1234F1Z5", "27", 30)
	seedBillingTrip(t, db, "t-1", "cust-due", "a", 10000, 0, now.Add(-72*time.Hour))
	seedBillingTrip(t, db, "t-1", "cust-due", "b", 10000, 0, now.Add(-60*time.Hour))
	seedBillingTrip(t, db, "t-1", "cust-due", "c", 10000, 0, now.Add(-48*time.Hour))
	seedBillingTrip(t, db, "t-1", "cust-due", "d", 10000, 0, now.Add(-24*time.Hour))

	q := NewCustomerBillingQueries(db)
	all, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-due"})
	require.NoError(t, err)
	require.Len(t, all, 4)

	uc := NewConsolidateInvoicesUseCase(db)
	base := ConsolidateInvoicesCommand{TenantID: "t-1", CustomerID: "cust-due", Customer: &cust, Settings: billingSettings()}

	// Priority 1: direct due-date override.
	cmd := base
	cmd.Trips = all[0:1]
	cmd.DueDateOverride = "2026-12-31"
	res, err := uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.Equal(t, "2026-12-31", res.DueDate.Format("2006-01-02"))
	var storedDue string
	require.NoError(t, db.QueryRow(`SELECT due_date FROM invoices WHERE id = ?`, res.InvoiceID).Scan(&storedDue))
	assert.Contains(t, storedDue, "2026-12-31")

	// An unparseable override falls through to the terms override.
	cmd = base
	cmd.Trips = all[1:2]
	cmd.DueDateOverride = "31/12/2026"
	cmd.PaymentTermsOverride = billingIntPtr(10)
	res, err = uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.WithinDuration(t, now.AddDate(0, 0, 10), res.DueDate, 2*time.Minute)

	// Priority 3: per-customer configured terms.
	cmd = base
	cmd.Trips = all[2:3]
	res, err = uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.WithinDuration(t, now.AddDate(0, 0, 30), res.DueDate, 2*time.Minute)

	// Priority 4: company default when the customer has no terms.
	mustExec(t, db, `INSERT INTO company_config (tenant_id, key, value) VALUES ('t-1', 'billing.default_payment_terms_days', '45')`)
	zeroTerms := cust
	zeroTerms.PaymentTermsDays = 0
	cmd = base
	cmd.Customer = &zeroTerms
	cmd.Trips = all[3:4]
	res, err = uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.WithinDuration(t, now.AddDate(0, 0, 45), res.DueDate, 2*time.Minute)

	var lastNumber int64
	require.NoError(t, db.QueryRow(`SELECT last_number FROM invoice_sequences WHERE tenant_id = 't-1'`).Scan(&lastNumber))
	assert.Equal(t, int64(4), lastNumber, "every consolidation allocates one invoice sequence number")
}

func TestConsolidateInvoices_TransactionRollbackOnStageError(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	cust := seedBillingCustomer(t, db, "cust-rb", "t-1", "27ABCDE1234F1Z5", "27", 30)
	seedBillingTrip(t, db, "t-1", "cust-rb", "a", 10000, 500, time.Now().Add(-24*time.Hour))

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-rb"})
	require.NoError(t, err)
	require.Len(t, trips, 1)

	// Break the line-item insert to prove the header insert rolls back.
	mustExec(t, db, `DROP TABLE invoice_line_items`)
	_, err = NewConsolidateInvoicesUseCase(db).Execute(ctx, ConsolidateInvoicesCommand{
		TenantID: "t-1", CustomerID: "cust-rb", Customer: &cust, Settings: billingSettings(), Trips: trips,
	})
	require.Error(t, err)
	var stageErr *ConsolidationError
	require.ErrorAs(t, err, &stageErr)
	assert.Equal(t, ConsolidationStageInsertFreight, stageErr.Stage)

	var invoiceRows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&invoiceRows))
	assert.Zero(t, invoiceRows, "a failed annexure insert must roll back the invoice header")
}

func TestConsolidateInvoices_InvoiceNumberFallback(t *testing.T) {
	db := newBillingTestDB(t)
	seedBillingCompany(t, db)
	ctx := context.Background()
	cust := seedBillingCustomer(t, db, "cust-fb", "t-1", "27ABCDE1234F1Z5", "27", 30)
	seedBillingTrip(t, db, "t-1", "cust-fb", "a", 10000, 0, time.Now().Add(-24*time.Hour))

	q := NewCustomerBillingQueries(db)
	trips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: "t-1", CustomerID: "cust-fb"})
	require.NoError(t, err)
	require.Len(t, trips, 1)

	// Without the sequence table the legacy path degrades to a random suffix
	// instead of failing the consolidation.
	mustExec(t, db, `DROP TABLE invoice_sequences`)
	res, err := NewConsolidateInvoicesUseCase(db).Execute(ctx, ConsolidateInvoicesCommand{
		TenantID: "t-1", CustomerID: "cust-fb", Customer: &cust, Settings: billingSettings(), Trips: trips,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(res.InvoiceNumber, "INV-"), "fallback number: %s", res.InvoiceNumber)
	assert.Len(t, res.InvoiceNumber, len("INV-")+8)
}

func TestCustomerStatement_AgingLedgerAndTenantIsolation(t *testing.T) {
	db := newBillingTestDB(t)
	ctx := context.Background()
	now := time.Now()
	cust := seedBillingCustomer(t, db, "cust-stmt", "t-1", "27AAACP0000M1Z9", "27", 15)
	seedBillingCustomer(t, db, "cust-other", "t-2", "29AAACP0000M1Z9", "29", 15)

	inv1Due := now.Add(-45 * 24 * time.Hour)
	inv1Created := now.Add(-60 * 24 * time.Hour)
	inv2Due := now.Add(-20 * 24 * time.Hour)
	inv2Created := now.Add(-35 * 24 * time.Hour)
	inv3Due := now.Add(10 * 24 * time.Hour)
	inv3Created := now.Add(-5 * 24 * time.Hour)

	mustExec(t, db, `INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, paid_amount, payment_status, status, due_date, created_at, tenant_id)
		VALUES ('inv-s1', 'INV-S01', 'bk-s1', 'cust-stmt', 25423.73, 4576.27, 30000, 10000, 'partially_paid', 'outstanding', ?, ?, 't-1'),
		       ('inv-s2', 'INV-S02', 'bk-s2', 'cust-stmt', 12711.86, 2288.14, 15000, 0, 'pending', 'outstanding', ?, ?, 't-1'),
		       ('inv-s3', 'INV-S03', 'bk-s3', 'cust-stmt', 21186.44, 3813.56, 25000, 0, 'pending', 'outstanding', ?, ?, 't-1')`,
		inv1Due, inv1Created, inv2Due, inv2Created, inv3Due, inv3Created)
	mustExec(t, db, `INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, total, status, tenant_id)
		VALUES ('inv-other', 'INV-OTHER', 'bk-other', 'cust-other', 1000, 180, 1180, 'outstanding', 't-2')`)

	mustExec(t, db, `INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, tenant_id)
		VALUES ('pay-s1', 'inv-s1', ?, 10000, 'bank_transfer', 'NEFT-889900', 't-1')`, now.Add(-30*24*time.Hour))

	mustExec(t, db, `INSERT INTO credit_debit_notes (id, tenant_id, note_number, note_type, invoice_id, reason, taxable_value, igst, cgst, sgst, total, created_at)
		VALUES ('cn-s1', 't-1', 'CN/2026/0001', 'credit', 'inv-s1', 'rate correction', 500, 0, 45, 45, 500, ?)`, now.Add(-25*24*time.Hour))
	mustExec(t, db, `INSERT INTO credit_debit_notes (id, tenant_id, note_number, note_type, invoice_id, reason, taxable_value, igst, cgst, sgst, total, created_at)
		VALUES ('dn-s1', 't-1', 'DN/2026/0001', 'debit', 'inv-s1', 'additional toll', 300, 0, 27, 27, 300, ?)`, now.Add(-20*24*time.Hour))

	seedBillingTrip(t, db, "t-1", "cust-stmt", "wip", 18000, 1500, now.Add(-24*time.Hour))

	st, err := NewCustomerBillingQueries(db).CustomerStatement(ctx, "t-1", &cust)
	require.NoError(t, err)

	// Other tenants' invoices never leak into the statement.
	require.Len(t, st.Invoices, 3)
	for _, inv := range st.Invoices {
		assert.NotEqual(t, "inv-other", inv.ID)
	}
	require.Len(t, st.Payments, 1)

	assert.InDelta(t, 70000.0, st.TotalInvoiced, 0.001)
	assert.InDelta(t, 10000.0, st.TotalPaid, 0.001)
	assert.InDelta(t, 60000.0, st.OutstandingInvoices, 0.001)
	assert.InDelta(t, 19500.0, st.TotalUnbilledAmount, 0.001)
	assert.InDelta(t, 79500.0, st.NetBalanceDue, 0.001)

	assert.InDelta(t, 25000.0, st.Aging.CurrentOr15, 0.001)
	assert.InDelta(t, 15000.0, st.Aging.Days16To30, 0.001)
	assert.InDelta(t, 20000.0, st.Aging.Days31To60, 0.001)
	assert.InDelta(t, 0.0, st.Aging.Days60Plus, 0.001)
	assert.InDelta(t, 35000.0, st.Aging.TotalOverdue, 0.001)
	assert.InDelta(t, 60000.0, st.Aging.TotalOutstanding, 0.001)

	// Ledger: inv1, inv2, payment, credit note, debit note, final invoice.
	require.Len(t, st.Transactions, 6)
	assert.Equal(t, "Invoice", st.Transactions[0].Type)
	assert.InDelta(t, 30000.0, st.Transactions[0].Debit, 0.001)
	assert.InDelta(t, 30000.0, st.Transactions[0].Balance, 0.001)
	assert.Equal(t, "Invoice", st.Transactions[1].Type)
	assert.InDelta(t, 45000.0, st.Transactions[1].Balance, 0.001)
	assert.Equal(t, "Payment", st.Transactions[2].Type)
	assert.InDelta(t, 35000.0, st.Transactions[2].Balance, 0.001)
	assert.Equal(t, "Credit Note", st.Transactions[3].Type)
	assert.InDelta(t, 500.0, st.Transactions[3].Credit, 0.001)
	assert.InDelta(t, 34500.0, st.Transactions[3].Balance, 0.001)
	assert.Equal(t, "Debit Note", st.Transactions[4].Type)
	assert.InDelta(t, 300.0, st.Transactions[4].Debit, 0.001)
	assert.InDelta(t, 34800.0, st.Transactions[4].Balance, 0.001)
	assert.Equal(t, "Invoice", st.Transactions[5].Type)
	assert.InDelta(t, 59800.0, st.Transactions[5].Balance, 0.001)

	assert.Contains(t, st.WhatsAppShareURL, "wa.me/919820098200")
	assert.Contains(t, st.WhatsAppSummaryText, "Net Balance Due")
	assert.Equal(t, "919820098200", st.CleanPhone)
	assert.False(t, st.StatementDate.IsZero())
}

func TestCleanPhoneForWhatsApp(t *testing.T) {
	assert.Equal(t, "919820098200", CleanPhoneForWhatsApp("98200 98200"))
	assert.Equal(t, "919820098200", CleanPhoneForWhatsApp("+91 98200-98200"))
	assert.Equal(t, "", CleanPhoneForWhatsApp(""))
}
