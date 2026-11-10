package application

// Public invoice payment portal read model, extracted from
// internal/handlers/public_payment.go (handlers-end-state ADR, Phase 2). The
// /pay/{invoiceId} portal is login-free and rate-limited by the server: the
// invoice id or invoice_number carried by the link is the capability, so the
// legacy queries stay intentionally free of tenant filters. The tenant is
// derived from the invoice row for branding only. The handler keeps HTTP,
// rate limiting, JSON/HTML rendering and the Razorpay checkout use-cases.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"transport-app/internal/privacy"
)

// PublicInvoiceLineItemView is a line item displayed on the public payment page.
type PublicInvoiceLineItemView struct {
	Description  string  `json:"description"`
	HSNSAC       string  `json:"hsn_sac_code"`
	Quantity     float64 `json:"quantity"`
	UnitPrice    float64 `json:"unit_price"`
	TaxableValue float64 `json:"taxable_value"`
	CGSTRate     float64 `json:"cgst_rate"`
	CGSTAmount   float64 `json:"cgst_amount"`
	SGSTRate     float64 `json:"sgst_rate"`
	SGSTAmount   float64 `json:"sgst_amount"`
	IGSTRate     float64 `json:"igst_rate"`
	IGSTAmount   float64 `json:"igst_amount"`
	Total        float64 `json:"total"`
}

// PublicPaymentRecordView displays a historical/settled payment against the invoice.
type PublicPaymentRecordView struct {
	ID                string  `json:"id"`
	PaymentDate       string  `json:"payment_date"`
	Amount            float64 `json:"amount"`
	Method            string  `json:"method"`
	Reference         string  `json:"reference"`
	RazorpayPaymentID string  `json:"razorpay_payment_id"`
	RazorpayOrderID   string  `json:"razorpay_order_id"`
}

// PublicCompanyView displays the billing carrier / platform company details.
type PublicCompanyView struct {
	Name      string `json:"name"`
	GSTIN     string `json:"gstin"`
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	LogoURL   string `json:"logo_url"`
	StateCode string `json:"state_code"`
}

// PublicInvoiceView holds all metadata for the public invoice payment portal.
type PublicInvoiceView struct {
	ID                 string  `json:"id"`
	InvoiceNumber      string  `json:"invoice_number"`
	BookingID          string  `json:"booking_id"`
	BookingNumber      string  `json:"booking_number"`
	TripID             string  `json:"trip_id"`
	TripNumber         string  `json:"trip_number"`
	Origin             string  `json:"origin"`
	Destination        string  `json:"destination"`
	CustomerID         string  `json:"customer_id"`
	CustomerName       string  `json:"customer_name"`
	CustomerCompany    string  `json:"customer_company"`
	CustomerGSTIN      string  `json:"customer_gstin"`
	CustomerAddress    string  `json:"customer_address"`
	CustomerPhone      string  `json:"customer_phone"`
	CustomerEmail      string  `json:"customer_email"`
	Subtotal           float64 `json:"subtotal"`
	Tax                float64 `json:"tax"`
	CGST               float64 `json:"cgst"`
	SGST               float64 `json:"sgst"`
	IGST               float64 `json:"igst"`
	Discount           float64 `json:"discount"`
	Total              float64 `json:"total"`
	PaidAmount         float64 `json:"paid_amount"`
	OutstandingBalance float64 `json:"outstanding_balance"`
	PaymentStatus      string  `json:"payment_status"`
	Status             string  `json:"status"`
	CreatedAt          string  `json:"created_at"`
	DueDate            string  `json:"due_date"`
	IRN                string  `json:"irn"`
	SignedQR           string  `json:"signed_qr"`
	TenantID           string  `json:"tenant_id"`
}

// PublicPayData is passed to the invoice_pay.html template.
type PublicPayData struct {
	Title         string                      `json:"title"`
	Invoice       PublicInvoiceView           `json:"invoice"`
	LineItems     []PublicInvoiceLineItemView `json:"line_items"`
	Company       PublicCompanyView           `json:"company"`
	Payments      []PublicPaymentRecordView   `json:"payments"`
	LatestPayment *PublicPaymentRecordView    `json:"latest_payment,omitempty"`
	RazorpayKeyID string                      `json:"razorpay_key_id"`
	IsPaid        bool                        `json:"is_paid"`
	Success       bool                        `json:"success"`
	ErrorMessage  string                      `json:"error_message,omitempty"`
	Lang          string                      `json:"-"`
}

// PublicPaymentQueries is the read model behind the login-free payment
// portal. It is stateless and cheap to construct per request.
type PublicPaymentQueries struct {
	db *sql.DB
}

// NewPublicPaymentQueries wires the read model against the database.
func NewPublicPaymentQueries(db *sql.DB) *PublicPaymentQueries {
	return &PublicPaymentQueries{db: db}
}

// ResolvePublicInvoice resolves a public payment reference (invoice id or
// invoice_number) to the canonical invoice id and its tenant id. The
// Razorpay order/verify use-cases need the tenant context, so the handler
// resolves it from the same login-free link. Not tenant-scoped by design:
// the link is the capability. sql.ErrNoRows surfaces for unknown references
// (the handler maps it to 404).
func (q *PublicPaymentQueries) ResolvePublicInvoice(ctx context.Context, invoiceRef string) (canonicalID, tenantID string, err error) {
	if q.db == nil {
		return "", "", sql.ErrNoRows
	}
	err = q.db.QueryRowContext(ctx, `
		SELECT id, tenant_id FROM invoices WHERE id = $1 OR invoice_number = $2 LIMIT 1
	`, invoiceRef, invoiceRef).Scan(&canonicalID, &tenantID)
	if err != nil {
		return "", "", err
	}
	return canonicalID, tenantID, nil
}

// LoadPublicPay loads everything the public payment page renders: invoice +
// customer + booking/route/trip labels, line items (with the legacy fallback
// row), company branding (company_settings then tenant name), payment history
// and the paid/outstanding state. Every legacy error/best-effort behavior is
// preserved: the header lookup error is returned (sql.ErrNoRows = not
// found), while the line-item, company and payment-history queries stay
// best-effort. Customer phone/email are masked here.
func (q *PublicPaymentQueries) LoadPublicPay(ctx context.Context, invoiceID string) (*PublicPayData, error) {
	if q.db == nil {
		return nil, sql.ErrNoRows
	}
	var (
		id, invoiceNumber, bookingID, customerID, tripIDNull string
		subtotal, tax, discount, total, paidAmount           float64
		paymentStatus, status, tenantID                      string
		dueDateNull                                          sql.NullTime
		createdAt                                            time.Time
		cgst, sgst, igst                                     float64
		irnNull, qrNull                                      sql.NullString
		custName, custComp, custPhone, custEmail             sql.NullString
		custGst, custAddr, custBillAddr                      sql.NullString
		bookingNumber, tripNumber, routeSrc, routeDst        sql.NullString
	)

	err := q.db.QueryRowContext(ctx, `
		SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, COALESCE(i.trip_id, ''),
		       i.subtotal, i.tax, i.discount, i.total, i.paid_amount, i.payment_status,
		       i.status, i.due_date, i.created_at, i.tenant_id,
		       COALESCE(i.cgst, 0), COALESCE(i.sgst, 0), COALESCE(i.igst, 0),
		       i.irn, i.signed_qr,
		       c.name, c.company, c.phone, c.email, c.gst, c.address, c.billing_address,
		       b.booking_number, t.trip_number, r.source, r.destination
		FROM invoices i
		LEFT JOIN customers c ON c.id = i.customer_id
		LEFT JOIN bookings b ON b.id = i.booking_id
		LEFT JOIN routes r ON r.id = b.route_id
		LEFT JOIN trips t ON t.id = i.trip_id
		WHERE i.id = $1 OR i.invoice_number = $2
		LIMIT 1
	`, invoiceID, invoiceID).Scan(
		&id, &invoiceNumber, &bookingID, &customerID, &tripIDNull,
		&subtotal, &tax, &discount, &total, &paidAmount, &paymentStatus,
		&status, &dueDateNull, &createdAt, &tenantID,
		&cgst, &sgst, &igst,
		&irnNull, &qrNull,
		&custName, &custComp, &custPhone, &custEmail, &custGst, &custAddr, &custBillAddr,
		&bookingNumber, &tripNumber, &routeSrc, &routeDst,
	)
	if err != nil {
		return nil, err
	}

	// Calculate outstanding balance
	outstanding := total - paidAmount
	if outstanding < 0 {
		outstanding = 0
	}

	isPaid := outstanding <= 0.001 || strings.EqualFold(paymentStatus, "paid")

	dueDateStr := ""
	if dueDateNull.Valid {
		dueDateStr = dueDateNull.Time.Format("02 Jan 2006")
	}

	custAddressFinal := custBillAddr.String
	if custAddressFinal == "" {
		custAddressFinal = custAddr.String
	}

	invView := PublicInvoiceView{
		ID:                 id,
		InvoiceNumber:      invoiceNumber,
		BookingID:          bookingID,
		BookingNumber:      bookingNumber.String,
		TripID:             tripIDNull,
		TripNumber:         tripNumber.String,
		Origin:             routeSrc.String,
		Destination:        routeDst.String,
		CustomerID:         customerID,
		CustomerName:       custName.String,
		CustomerCompany:    custComp.String,
		CustomerGSTIN:      custGst.String,
		CustomerAddress:    custAddressFinal,
		CustomerPhone:      privacy.MaskPhone(custPhone.String),
		CustomerEmail:      privacy.MaskEmail(custEmail.String),
		Subtotal:           subtotal,
		Tax:                tax,
		CGST:               cgst,
		SGST:               sgst,
		IGST:               igst,
		Discount:           discount,
		Total:              total,
		PaidAmount:         paidAmount,
		OutstandingBalance: outstanding,
		PaymentStatus:      paymentStatus,
		Status:             status,
		CreatedAt:          createdAt.Format("02 Jan 2006"),
		DueDate:            dueDateStr,
		IRN:                irnNull.String,
		SignedQR:           qrNull.String,
		// Security hygiene: the internal tenant ID is an operational identifier,
		// not something a customer payment page needs. Internal IDs (customer_id,
		// booking_id, trip_id) stay because the payment flow references them,
		// but the tenant registry key never leaves the perimeter.
		TenantID: "",
	}

	// Load Line Items
	lineItems := make([]PublicInvoiceLineItemView, 0)
	rows, err := q.db.QueryContext(ctx, `
		SELECT description, COALESCE(hsn_sac_code, ''), quantity, unit_price,
		       taxable_value, cgst_rate, cgst_amount, sgst_rate, sgst_amount,
		       igst_rate, igst_amount, total
		FROM invoice_line_items
		WHERE invoice_id = $1
		ORDER BY created_at ASC
	`, id)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var li PublicInvoiceLineItemView
			if err := rows.Scan(
				&li.Description, &li.HSNSAC, &li.Quantity, &li.UnitPrice,
				&li.TaxableValue, &li.CGSTRate, &li.CGSTAmount,
				&li.SGSTRate, &li.SGSTAmount, &li.IGSTRate, &li.IGSTAmount,
				&li.Total,
			); err == nil {
				lineItems = append(lineItems, li)
			}
		}
	}

	// Fallback single line item if invoice_line_items is empty
	if len(lineItems) == 0 {
		lineItems = append(lineItems, PublicInvoiceLineItemView{
			Description:  "Freight & Transportation Services",
			HSNSAC:       "996511",
			Quantity:     1,
			UnitPrice:    subtotal,
			TaxableValue: subtotal,
			CGSTAmount:   cgst,
			SGSTAmount:   sgst,
			IGSTAmount:   igst,
			Total:        total,
		})
	}

	// Load Company Settings
	company := PublicCompanyView{
		Name:      "Avandab Logistics Network",
		GSTIN:     "27AAACA1234A1Z5",
		Address:   "101 Freight Terminal Hub, Mumbai, Maharashtra",
		StateCode: "27",
	}

	var cName, cGst, cAddr, cPhone, cEmail, cLogo, cState sql.NullString
	if err := q.db.QueryRowContext(ctx, `
		SELECT company_name, gst_number, address, phone, email, logo_path, state_code
		FROM company_settings WHERE id = 1 LIMIT 1
	`).Scan(&cName, &cGst, &cAddr, &cPhone, &cEmail, &cLogo, &cState); err == nil {
		if cName.Valid && cName.String != "" {
			company.Name = cName.String
		}
		if cGst.Valid && cGst.String != "" {
			company.GSTIN = cGst.String
		}
		if cAddr.Valid && cAddr.String != "" {
			company.Address = cAddr.String
		}
		if cPhone.Valid && cPhone.String != "" {
			company.Phone = cPhone.String
		}
		if cEmail.Valid && cEmail.String != "" {
			company.Email = cEmail.String
		}
		if cLogo.Valid && cLogo.String != "" {
			company.LogoURL = cLogo.String
		}
		if cState.Valid && cState.String != "" {
			company.StateCode = cState.String
		}
	}

	if tenantID != "" {
		var tName sql.NullString
		if err := q.db.QueryRowContext(ctx, `SELECT name FROM tenants WHERE id = $1`, tenantID).Scan(&tName); err == nil && tName.Valid && tName.String != "" {
			company.Name = tName.String
		}
	}

	// Load Payment History
	payments := make([]PublicPaymentRecordView, 0)
	var latestPayment *PublicPaymentRecordView

	payRows, err := q.db.QueryContext(ctx, `
		SELECT id, payment_date, amount, method, COALESCE(reference, ''),
		       COALESCE(razorpay_payment_id, ''), COALESCE(razorpay_order_id, '')
		FROM payments
		WHERE invoice_id = $1
		ORDER BY payment_date DESC, created_at DESC
	`, id)
	if err == nil {
		defer func() { _ = payRows.Close() }()
		for payRows.Next() {
			var p PublicPaymentRecordView
			var pDate time.Time
			if err := payRows.Scan(&p.ID, &pDate, &p.Amount, &p.Method, &p.Reference, &p.RazorpayPaymentID, &p.RazorpayOrderID); err == nil {
				p.PaymentDate = pDate.Format("02 Jan 2006, 15:04 MST")
				payments = append(payments, p)
			}
		}
	}

	if len(payments) > 0 {
		latestPayment = &payments[0]
	}

	return &PublicPayData{
		Title:         fmt.Sprintf("Pay Invoice %s · %s", invoiceNumber, company.Name),
		Invoice:       invView,
		LineItems:     lineItems,
		Company:       company,
		Payments:      payments,
		LatestPayment: latestPayment,
		IsPaid:        isPaid,
	}, nil
}
