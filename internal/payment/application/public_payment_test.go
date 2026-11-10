package application

// Characterization + extraction tests for the public invoice payment portal
// read model moved out of internal/handlers/public_payment.go
// (handlers-end-state ADR, Phase 2): login-free invoice id/number resolution,
// tenant derivation + branding, KYC-safe customer masking, amount/outstanding
// math, paid-link detection, payment-history ordering and the legacy
// best-effort fallbacks (company defaults, fallback freight line item).

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

const publicPaymentTestSchema = `
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    name TEXT
);
CREATE TABLE company_settings (
    id INTEGER PRIMARY KEY,
    company_name TEXT,
    gst_number TEXT,
    address TEXT,
    phone TEXT,
    email TEXT,
    logo_path TEXT,
    state_code TEXT
);
CREATE TABLE customers (
    id TEXT PRIMARY KEY,
    name TEXT,
    company TEXT,
    phone TEXT,
    email TEXT,
    gst TEXT,
    address TEXT,
    billing_address TEXT
);
CREATE TABLE routes (
    id TEXT PRIMARY KEY,
    source TEXT,
    destination TEXT
);
CREATE TABLE bookings (
    id TEXT PRIMARY KEY,
    booking_number TEXT,
    route_id TEXT
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT
);
CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    invoice_number TEXT UNIQUE,
    booking_id TEXT,
    customer_id TEXT,
    trip_id TEXT,
    subtotal REAL DEFAULT 0,
    tax REAL DEFAULT 0,
    cgst REAL DEFAULT 0,
    sgst REAL DEFAULT 0,
    igst REAL DEFAULT 0,
    discount REAL DEFAULT 0,
    total REAL DEFAULT 0,
    paid_amount REAL DEFAULT 0,
    payment_status TEXT,
    status TEXT,
    due_date DATETIME,
    created_at DATETIME,
    tenant_id TEXT,
    irn TEXT,
    signed_qr TEXT
);
CREATE TABLE invoice_line_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id TEXT,
    description TEXT,
    hsn_sac_code TEXT,
    quantity REAL NOT NULL DEFAULT 1,
    unit_price REAL NOT NULL DEFAULT 0,
    taxable_value REAL NOT NULL DEFAULT 0,
    cgst_rate REAL NOT NULL DEFAULT 0,
    cgst_amount REAL NOT NULL DEFAULT 0,
    sgst_rate REAL NOT NULL DEFAULT 0,
    sgst_amount REAL NOT NULL DEFAULT 0,
    igst_rate REAL NOT NULL DEFAULT 0,
    igst_amount REAL NOT NULL DEFAULT 0,
    total REAL NOT NULL DEFAULT 0,
    created_at DATETIME
);
CREATE TABLE payments (
    id TEXT PRIMARY KEY,
    invoice_id TEXT,
    payment_date DATETIME,
    amount REAL,
    method TEXT,
    reference TEXT,
    razorpay_payment_id TEXT,
    razorpay_order_id TEXT,
    created_at DATETIME
);
`

func newPublicPaymentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("public_payment_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(publicPaymentTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecPublicPay(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

type publicPayInvoiceFixture struct {
	tenantID      string
	tenantName    string
	invoiceID     string
	invoiceNumber string
	total         float64
	paid          float64
	paymentStatus string
	status        string
	createdAt     string
	paidAmount    *float64
}

// seedPublicPayInvoice seeds one tenant/customer/route/booking/trip/invoice
// chain shaped like the legacy public-page query expects.
func seedPublicPayInvoice(t *testing.T, db *sql.DB, f publicPayInvoiceFixture) {
	t.Helper()
	mustExecPublicPay(t, db, `INSERT OR IGNORE INTO tenants (id, name) VALUES ($1, $2)`, f.tenantID, f.tenantName)
	mustExecPublicPay(t, db, `INSERT OR IGNORE INTO customers (id, name, company, phone, email, gst, address, billing_address)
		VALUES ($1, 'Rajesh Logistics', 'Rajesh Enterprises Ltd', '+919876543210', 'rajesh@enterprises.com', '27AABCR1234M1Z2', 'Plot 45, MIDC, Navi Mumbai', '')`,
		"cust-"+f.invoiceID)
	mustExecPublicPay(t, db, `INSERT INTO routes (id, source, destination) VALUES ($1, 'Mumbai JNPT', 'Pune Bhosari')`, "route-"+f.invoiceID)
	mustExecPublicPay(t, db, `INSERT INTO bookings (id, booking_number, route_id) VALUES ($1, 'BK-2026-001', $2)`,
		"booking-"+f.invoiceID, "route-"+f.invoiceID)
	mustExecPublicPay(t, db, `INSERT INTO trips (id, trip_number) VALUES ($1, 'TRP-2026-001')`, "trip-"+f.invoiceID)
	paidAmount := f.paid
	if f.paidAmount != nil {
		paidAmount = *f.paidAmount
	}
	mustExecPublicPay(t, db, `INSERT INTO invoices
		(id, invoice_number, booking_id, customer_id, trip_id, subtotal, tax, cgst, sgst, total, paid_amount,
		 payment_status, status, due_date, created_at, tenant_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10, $11, $12, '2026-09-15 00:00:00', $13, $14)`,
		f.invoiceID, f.invoiceNumber, "booking-"+f.invoiceID, "cust-"+f.invoiceID, "trip-"+f.invoiceID,
		f.total-1800, 1800.0, 900.0, f.total, paidAmount, f.paymentStatus, f.status, f.createdAt, f.tenantID)
}

func seedPublicPayCompanySettings(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecPublicPay(t, db, `INSERT OR REPLACE INTO company_settings
		(id, company_name, gst_number, address, phone, email, logo_path, state_code)
		VALUES (1, 'Avandab Logistics Network', '27AAACA1234A1Z5', '101 Freight Terminal, Mumbai',
		 '+91 9876543210', 'billing@avandab.com', '/uploads/logo.png', '27')`)
}

func TestPublicPaymentQueries_ResolvePublicInvoice(t *testing.T) {
	db := newPublicPaymentTestDB(t)
	ctx := context.Background()
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-1", invoiceNumber: "INV-2026-001",
		total: 11800, paid: 0, paymentStatus: "pending", status: "issued", createdAt: "2026-09-02 09:00:00",
	})
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "2", tenantName: "Second Tenant Carriers",
		invoiceID: "inv-2", invoiceNumber: "INV-2026-002",
		total: 5900, paid: 5900, paymentStatus: "paid", status: "paid", createdAt: "2026-09-03 09:00:00",
	})

	reads := NewPublicPaymentQueries(db)
	canonical, tenant, err := reads.ResolvePublicInvoice(ctx, "inv-1")
	require.NoError(t, err)
	assert.Equal(t, "inv-1", canonical)
	assert.Equal(t, "1", tenant)

	// The link may carry the invoice NUMBER instead of the id.
	canonical, tenant, err = reads.ResolvePublicInvoice(ctx, "INV-2026-002")
	require.NoError(t, err)
	assert.Equal(t, "inv-2", canonical)
	assert.Equal(t, "2", tenant, "tenant is derived from the invoice row (the link is the capability)")

	// Unknown reference: handler maps sql.ErrNoRows to 404.
	_, _, err = reads.ResolvePublicInvoice(ctx, "inv-ghost")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	_, _, err = NewPublicPaymentQueries(nil).ResolvePublicInvoice(ctx, "inv-1")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestPublicPaymentQueries_LoadPublicPay_UnpaidLink(t *testing.T) {
	db := newPublicPaymentTestDB(t)
	ctx := context.Background()
	seedPublicPayCompanySettings(t, db)
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-unpaid", invoiceNumber: "INV-2026-001",
		total: 11800, paid: 0, paymentStatus: "pending", status: "issued", createdAt: "2026-09-02 09:00:00",
	})
	// Line items inserted out of order: legacy orders by created_at ASC.
	mustExecPublicPay(t, db, `INSERT INTO invoice_line_items
		(invoice_id, description, hsn_sac_code, quantity, unit_price, taxable_value,
		 cgst_rate, cgst_amount, sgst_rate, sgst_amount, igst_rate, igst_amount, total, created_at)
		VALUES ('inv-unpaid', 'Second line', '996511', 1, 2000, 2000, 9, 180, 9, 180, 0, 0, 2360, '2026-09-02 02:00:00')`)
	mustExecPublicPay(t, db, `INSERT INTO invoice_line_items
		(invoice_id, description, hsn_sac_code, quantity, unit_price, taxable_value,
		 cgst_rate, cgst_amount, sgst_rate, sgst_amount, igst_rate, igst_amount, total, created_at)
		VALUES ('inv-unpaid', 'Primary Freight Transportation JNPT to Bhosari', '996511', 1, 10000, 10000, 9, 900, 9, 900, 0, 0, 11800, '2026-09-02 01:00:00')`)

	reads := NewPublicPaymentQueries(db)
	data, err := reads.LoadPublicPay(ctx, "inv-unpaid")
	require.NoError(t, err)

	assert.Equal(t, "Pay Invoice INV-2026-001 · Avandab Logistics Network", data.Title)
	assert.Equal(t, "inv-unpaid", data.Invoice.ID)
	assert.Equal(t, "INV-2026-001", data.Invoice.InvoiceNumber)
	assert.Equal(t, "BK-2026-001", data.Invoice.BookingNumber)
	assert.Equal(t, "TRP-2026-001", data.Invoice.TripNumber)
	assert.Equal(t, "Mumbai JNPT", data.Invoice.Origin)
	assert.Equal(t, "Pune Bhosari", data.Invoice.Destination)
	assert.Equal(t, "Rajesh Logistics", data.Invoice.CustomerName)
	assert.Equal(t, "Rajesh Enterprises Ltd", data.Invoice.CustomerCompany)
	assert.Equal(t, "27AABCR1234M1Z2", data.Invoice.CustomerGSTIN)
	// billing_address is empty, so the legacy billing->address fallback applies.
	assert.Equal(t, "Plot 45, MIDC, Navi Mumbai", data.Invoice.CustomerAddress)
	assert.Equal(t, "••••••3210", data.Invoice.CustomerPhone, "public page must mask the customer phone (audit M2)")
	assert.Equal(t, "r***@enterprises.com", data.Invoice.CustomerEmail, "public page must mask the customer email (audit M2)")
	assert.Equal(t, 10000.0, data.Invoice.Subtotal)
	assert.Equal(t, 1800.0, data.Invoice.Tax)
	assert.Equal(t, 900.0, data.Invoice.CGST)
	assert.Equal(t, 900.0, data.Invoice.SGST)
	assert.Equal(t, 11800.0, data.Invoice.Total)
	assert.Equal(t, 0.0, data.Invoice.PaidAmount)
	assert.Equal(t, 11800.0, data.Invoice.OutstandingBalance)
	assert.Equal(t, "pending", data.Invoice.PaymentStatus)
	assert.Equal(t, "issued", data.Invoice.Status)
	assert.Equal(t, "02 Sep 2026", data.Invoice.CreatedAt)
	assert.Equal(t, "15 Sep 2026", data.Invoice.DueDate)
	assert.Empty(t, data.Invoice.TenantID, "the internal tenant id must never leave the perimeter")
	assert.False(t, data.IsPaid)
	assert.Empty(t, data.RazorpayKeyID, "the handler owns the config-sourced checkout key")

	require.Len(t, data.LineItems, 2)
	assert.Equal(t, "Primary Freight Transportation JNPT to Bhosari", data.LineItems[0].Description)
	assert.Equal(t, "996511", data.LineItems[0].HSNSAC)
	assert.Equal(t, 10000.0, data.LineItems[0].UnitPrice)
	assert.Equal(t, 11800.0, data.LineItems[0].Total)
	assert.Equal(t, "Second line", data.LineItems[1].Description)

	assert.Equal(t, "Avandab Logistics Network", data.Company.Name)
	assert.Equal(t, "27AAACA1234A1Z5", data.Company.GSTIN)
	assert.Equal(t, "101 Freight Terminal, Mumbai", data.Company.Address)
	assert.Equal(t, "+91 9876543210", data.Company.Phone)
	assert.Equal(t, "billing@avandab.com", data.Company.Email)
	assert.Equal(t, "/uploads/logo.png", data.Company.LogoURL)
	assert.Equal(t, "27", data.Company.StateCode)

	assert.Empty(t, data.Payments)
	assert.Nil(t, data.LatestPayment)

	// The link may carry the invoice number too.
	byNumber, err := reads.LoadPublicPay(ctx, "INV-2026-001")
	require.NoError(t, err)
	assert.Equal(t, "inv-unpaid", byNumber.Invoice.ID)

	// Unknown link -> sql.ErrNoRows (handler maps to 404).
	_, err = reads.LoadPublicPay(ctx, "inv-ghost")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestPublicPaymentQueries_LoadPublicPay_FallbackLineItemAndCompanyDefaults(t *testing.T) {
	db := newPublicPaymentTestDB(t)
	ctx := context.Background()
	// No company_settings row and no tenants row: the legacy hardcoded
	// carrier defaults must be used.
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "tenant-unknown", tenantName: "",
		invoiceID: "inv-bare", invoiceNumber: "INV-BARE-001",
		total: 11800, paid: 0, paymentStatus: "pending", status: "issued", createdAt: "2026-09-02 09:00:00",
	})

	data, err := NewPublicPaymentQueries(db).LoadPublicPay(ctx, "inv-bare")
	require.NoError(t, err)

	assert.Equal(t, "Pay Invoice INV-BARE-001 · Avandab Logistics Network", data.Title)
	assert.Equal(t, "Avandab Logistics Network", data.Company.Name)
	assert.Equal(t, "27AAACA1234A1Z5", data.Company.GSTIN)
	assert.Equal(t, "101 Freight Terminal Hub, Mumbai, Maharashtra", data.Company.Address)
	assert.Equal(t, "27", data.Company.StateCode)
	assert.Empty(t, data.Company.Phone)
	assert.Empty(t, data.Company.Email)
	assert.Empty(t, data.Company.LogoURL)

	require.Len(t, data.LineItems, 1, "empty invoice_line_items gets the legacy freight fallback row")
	fallback := data.LineItems[0]
	assert.Equal(t, "Freight & Transportation Services", fallback.Description)
	assert.Equal(t, "996511", fallback.HSNSAC)
	assert.Equal(t, 1.0, fallback.Quantity)
	assert.Equal(t, 10000.0, fallback.UnitPrice)
	assert.Equal(t, 10000.0, fallback.TaxableValue)
	assert.Equal(t, 900.0, fallback.CGSTAmount)
	assert.Equal(t, 900.0, fallback.SGSTAmount)
	assert.Equal(t, 11800.0, fallback.Total)
}

func TestPublicPaymentQueries_LoadPublicPay_PaidAndUnpaidStates(t *testing.T) {
	db := newPublicPaymentTestDB(t)
	ctx := context.Background()
	seedPublicPayCompanySettings(t, db)
	// Fully paid invoice + settled payment rows (inserted out of order).
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-paid", invoiceNumber: "INV-2026-002",
		total: 5900, paid: 5900, paymentStatus: "paid", status: "paid", createdAt: "2026-09-01 09:00:00",
	})
	mustExecPublicPay(t, db, `INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, razorpay_payment_id, razorpay_order_id, created_at)
		VALUES ('pay-1', 'inv-paid', '2026-09-02 10:00:00', 2900, 'razorpay', 'rzp_ref_1', 'pay_rzp_1', 'order_rzp_1', '2026-09-02 10:00:00')`)
	mustExecPublicPay(t, db, `INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, razorpay_payment_id, razorpay_order_id, created_at)
		VALUES ('pay-2', 'inv-paid', '2026-09-03 11:00:00', 3000, 'razorpay', 'rzp_ref_2', 'pay_rzp_2', 'order_rzp_2', '2026-09-03 11:00:00')`)

	reads := NewPublicPaymentQueries(db)
	paid, err := reads.LoadPublicPay(ctx, "inv-paid")
	require.NoError(t, err)
	assert.True(t, paid.IsPaid)
	assert.Equal(t, 0.0, paid.Invoice.OutstandingBalance)
	require.Len(t, paid.Payments, 2)
	assert.Equal(t, "pay-2", paid.Payments[0].ID, "payment history is newest first")
	assert.Equal(t, "pay_rzp_2", paid.Payments[0].RazorpayPaymentID)
	assert.Equal(t, "order_rzp_2", paid.Payments[0].RazorpayOrderID)
	assert.Equal(t, "rzp_ref_2", paid.Payments[0].Reference)
	assert.Contains(t, paid.Payments[0].PaymentDate, "03 Sep 2026")
	require.NotNil(t, paid.LatestPayment)
	assert.Equal(t, "pay-2", paid.LatestPayment.ID)

	// Overpayment is clamped to a zero balance (legacy math).
	overpaidAmount := 6000.0
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-overpaid", invoiceNumber: "INV-2026-003",
		total: 5900, paid: 6000, paidAmount: &overpaidAmount, paymentStatus: "paid", status: "paid", createdAt: "2026-09-04 09:00:00",
	})
	overpaid, err := reads.LoadPublicPay(ctx, "inv-overpaid")
	require.NoError(t, err)
	assert.Equal(t, 0.0, overpaid.Invoice.OutstandingBalance, "negative balances clamp to zero")
	assert.True(t, overpaid.IsPaid)

	// Partial payment: the portal still bills the remainder.
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-partial", invoiceNumber: "INV-2026-004",
		total: 11800, paid: 5000, paymentStatus: "partially_paid", status: "issued", createdAt: "2026-09-05 09:00:00",
	})
	partial, err := reads.LoadPublicPay(ctx, "inv-partial")
	require.NoError(t, err)
	assert.Equal(t, 6800.0, partial.Invoice.OutstandingBalance)
	assert.False(t, partial.IsPaid)

	// Legacy quirk preserved: an uppercase PAID payment_status marks the link
	// paid even when the stored balance is still outstanding.
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-upper", invoiceNumber: "INV-2026-005",
		total: 11800, paid: 0, paymentStatus: "PAID", status: "issued", createdAt: "2026-09-06 09:00:00",
	})
	upper, err := reads.LoadPublicPay(ctx, "inv-upper")
	require.NoError(t, err)
	assert.True(t, upper.IsPaid)

	// There is no token expiry / invoice-status gate in this legacy public
	// surface: a cancelled invoice still resolves on its link. Pinned so a
	// future change is deliberate.
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-cancelled", invoiceNumber: "INV-2026-006",
		total: 11800, paid: 0, paymentStatus: "pending", status: "cancelled", createdAt: "2026-09-07 09:00:00",
	})
	cancelled, err := reads.LoadPublicPay(ctx, "inv-cancelled")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", cancelled.Invoice.Status)
	assert.False(t, cancelled.IsPaid)
}

func TestPublicPaymentQueries_TenantDerivationAndInvoiceIsolation(t *testing.T) {
	db := newPublicPaymentTestDB(t)
	ctx := context.Background()
	seedPublicPayCompanySettings(t, db)
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "1", tenantName: "Avandab Logistics Network",
		invoiceID: "inv-t1", invoiceNumber: "INV-T1",
		total: 11800, paid: 0, paymentStatus: "pending", status: "issued", createdAt: "2026-09-02 09:00:00",
	})
	seedPublicPayInvoice(t, db, publicPayInvoiceFixture{
		tenantID: "2", tenantName: "Second Tenant Carriers",
		invoiceID: "inv-t2", invoiceNumber: "INV-T2",
		total: 5900, paid: 5900, paymentStatus: "paid", status: "paid", createdAt: "2026-09-03 09:00:00",
	})
	// Each invoice gets its own line item and payment row.
	mustExecPublicPay(t, db, `INSERT INTO invoice_line_items (invoice_id, description, hsn_sac_code, quantity, unit_price, taxable_value, total, created_at)
		VALUES ('inv-t1', 'Tenant one freight', '996511', 1, 11800, 11800, 11800, '2026-09-02 01:00:00')`)
	mustExecPublicPay(t, db, `INSERT INTO invoice_line_items (invoice_id, description, hsn_sac_code, quantity, unit_price, taxable_value, total, created_at)
		VALUES ('inv-t2', 'Tenant two freight', '996519', 1, 5900, 5900, 5900, '2026-09-03 01:00:00')`)
	mustExecPublicPay(t, db, `INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, created_at)
		VALUES ('pay-t2', 'inv-t2', '2026-09-03 10:00:00', 5900, 'razorpay', 'rzp_t2', '2026-09-03 10:00:00')`)

	reads := NewPublicPaymentQueries(db)
	data, err := reads.LoadPublicPay(ctx, "inv-t2")
	require.NoError(t, err)
	// Branding follows the invoice tenant, not the handler acting tenant.
	assert.Equal(t, "Second Tenant Carriers", data.Company.Name)
	assert.Equal(t, "27AAACA1234A1Z5", data.Company.GSTIN, "other branding fields still come from the legacy global settings")
	assert.Empty(t, data.Invoice.TenantID)
	require.Len(t, data.LineItems, 1)
	assert.Equal(t, "Tenant two freight", data.LineItems[0].Description)
	require.Len(t, data.Payments, 1)
	assert.Equal(t, "pay-t2", data.Payments[0].ID)
	assert.True(t, data.IsPaid)

	// The tenant's own name wins over company_settings, and the other
	// invoice's rows never leak into this page.
	other, err := reads.LoadPublicPay(ctx, "inv-t1")
	require.NoError(t, err)
	assert.Equal(t, "Avandab Logistics Network", other.Company.Name)
	require.Len(t, other.LineItems, 1)
	assert.Equal(t, "Tenant one freight", other.LineItems[0].Description)
	assert.Empty(t, other.Payments)
	assert.Equal(t, 11800.0, other.Invoice.OutstandingBalance)
}
