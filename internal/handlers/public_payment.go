package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	paymentapp "transport-app/internal/payment/application"
	"transport-app/internal/shared"
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
}

// PublicPay renders the customer-facing invoice payment page (GET /pay/{invoiceId}).
func (h *PaymentHandlers) PublicPay(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "invoiceId")
	if invoiceID == "" {
		invoiceID = chi.URLParam(r, "id")
	}
	if invoiceID == "" {
		http.Error(w, "Invoice ID required", http.StatusBadRequest)
		return
	}

	data, err := h.loadPublicPayData(r.Context(), invoiceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Invoice not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to load invoice payment data", "invoice_id", invoiceID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("paid") == "true" {
		data.Success = true
	}

	if wantsJSON(r) || r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
		return
	}

	h.renderPublicPaymentTemplate(w, "invoice_pay.html", data)
}

// PublicRazorpayOrder creates a server-side Razorpay order for the invoice balance (POST /pay/{invoiceId}/razorpay/order).
func (h *PaymentHandlers) PublicRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	h.init()
	if h.orderUC == nil {
		http.Error(w, `{"error":"razorpay not configured"}`, http.StatusServiceUnavailable)
		return
	}

	invoiceID := chi.URLParam(r, "invoiceId")
	if invoiceID == "" {
		invoiceID = chi.URLParam(r, "id")
	}
	if invoiceID == "" {
		var req struct {
			InvoiceID string `json:"invoice_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		invoiceID = req.InvoiceID
	}
	if invoiceID == "" {
		http.Error(w, `{"error":"invoice_id is required"}`, http.StatusBadRequest)
		return
	}

	// Resolve canonical invoice ID and tenant ID
	var canonicalID, tenantID string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, tenant_id FROM invoices WHERE id = ? OR invoice_number = ? LIMIT 1
	`, invoiceID, invoiceID).Scan(&canonicalID, &tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"invoice not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to lookup invoice"}`, http.StatusInternalServerError)
		return
	}

	ctx := shared.ContextWithTenantID(r.Context(), shared.TenantID(tenantID))
	res, err := h.orderUC.Execute(ctx, paymentapp.CreateRazorpayOrderCommand{
		TenantID:  shared.TenantID(tenantID),
		InvoiceID: canonicalID,
	})
	if err != nil {
		switch {
		case errors.Is(err, paymentapp.ErrRazorpayNotConfigured):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "razorpay not configured"})
		case errors.Is(err, paymentapp.ErrInvoiceNotFound):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invoice not found"})
		case errors.Is(err, paymentapp.ErrInvoiceAlreadySettled):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invoice has no outstanding balance"})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create razorpay order", "details": err.Error()})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

// PublicRazorpayVerify validates the checkout signature and records payment in ledger (POST /pay/{invoiceId}/razorpay/verify).
func (h *PaymentHandlers) PublicRazorpayVerify(w http.ResponseWriter, r *http.Request) {
	h.init()
	if h.verifyUC == nil {
		http.Error(w, `{"error":"razorpay not configured"}`, http.StatusServiceUnavailable)
		return
	}

	invoiceID := chi.URLParam(r, "invoiceId")
	if invoiceID == "" {
		invoiceID = chi.URLParam(r, "id")
	}

	var req struct {
		InvoiceID         string `json:"invoice_id"`
		OrderID           string `json:"order_id"`
		PaymentID         string `json:"payment_id"`
		Signature         string `json:"signature"`
		RazorpayOrderID   string `json:"razorpay_order_id"`
		RazorpayPaymentID string `json:"razorpay_payment_id"`
		RazorpaySignature string `json:"razorpay_signature"`
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &req)
	}

	if req.OrderID == "" {
		req.OrderID = req.RazorpayOrderID
	}
	if req.PaymentID == "" {
		req.PaymentID = req.RazorpayPaymentID
	}
	if req.Signature == "" {
		req.Signature = req.RazorpaySignature
	}

	// Fallback to form values if JSON was empty
	if req.OrderID == "" {
		_ = r.ParseForm()
		if req.InvoiceID == "" {
			req.InvoiceID = r.FormValue("invoice_id")
		}
		req.OrderID = r.FormValue("razorpay_order_id")
		if req.OrderID == "" {
			req.OrderID = r.FormValue("order_id")
		}
		req.PaymentID = r.FormValue("razorpay_payment_id")
		if req.PaymentID == "" {
			req.PaymentID = r.FormValue("payment_id")
		}
		req.Signature = r.FormValue("razorpay_signature")
		if req.Signature == "" {
			req.Signature = r.FormValue("signature")
		}
	}

	if req.InvoiceID == "" {
		req.InvoiceID = invoiceID
	}

	if req.InvoiceID == "" || req.OrderID == "" || req.PaymentID == "" || req.Signature == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "invalid_request",
			"message": "invoice_id, razorpay_order_id, razorpay_payment_id, and razorpay_signature are required",
		})
		return
	}

	// Resolve canonical invoice ID and tenant ID
	var canonicalID, tenantID string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, tenant_id FROM invoices WHERE id = ? OR invoice_number = ? LIMIT 1
	`, req.InvoiceID, req.InvoiceID).Scan(&canonicalID, &tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invoice not found"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
		return
	}

	ctx := shared.ContextWithTenantID(r.Context(), shared.TenantID(tenantID))
	paymentID, err := h.verifyUC.Execute(ctx, paymentapp.VerifyRazorpayPaymentCommand{
		TenantID:  shared.TenantID(tenantID),
		InvoiceID: canonicalID,
		OrderID:   req.OrderID,
		PaymentID: req.PaymentID,
		Signature: req.Signature,
	})
	if err != nil {
		switch {
		case errors.Is(err, paymentapp.ErrRazorpayNotConfigured):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "razorpay not configured"})
		case errors.Is(err, paymentapp.ErrRazorpayInvalidSignature):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_signature", "message": "invalid razorpay signature"})
		case errors.Is(err, paymentapp.ErrInvoiceNotFound):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invoice not found"})
		case errors.Is(err, paymentapp.ErrInvoiceAlreadySettled):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invoice has no outstanding balance"})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to verify razorpay payment", "details": err.Error()})
		}
		return
	}

	if wantsJSON(r) || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "success",
			"payment_id": string(paymentID),
			"invoice_id": canonicalID,
		})
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/pay/%s?paid=true", canonicalID), http.StatusSeeOther)
}

func (h *PaymentHandlers) loadPublicPayData(ctx context.Context, invoiceID string) (*PublicPayData, error) {
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

	err := h.DB.QueryRowContext(ctx, `
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
		WHERE i.id = ? OR i.invoice_number = ?
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
		CustomerPhone:      custPhone.String,
		CustomerEmail:      custEmail.String,
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
		TenantID:           tenantID,
	}

	// Load Line Items
	lineItems := make([]PublicInvoiceLineItemView, 0)
	rows, err := h.DB.QueryContext(ctx, `
		SELECT description, COALESCE(hsn_sac_code, ''), quantity, unit_price,
		       taxable_value, cgst_rate, cgst_amount, sgst_rate, sgst_amount,
		       igst_rate, igst_amount, total
		FROM invoice_line_items
		WHERE invoice_id = ?
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
	if err := h.DB.QueryRowContext(ctx, `
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
		if err := h.DB.QueryRowContext(ctx, `SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&tName); err == nil && tName.Valid && tName.String != "" {
			company.Name = tName.String
		}
	}

	// Load Payment History
	payments := make([]PublicPaymentRecordView, 0)
	var latestPayment *PublicPaymentRecordView

	payRows, err := h.DB.QueryContext(ctx, `
		SELECT id, payment_date, amount, method, COALESCE(reference, ''),
		       COALESCE(razorpay_payment_id, ''), COALESCE(razorpay_order_id, '')
		FROM payments
		WHERE invoice_id = ?
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

	keyID := ""
	if h.Config != nil {
		keyID = h.Config.RazorpayKeyID
	}

	return &PublicPayData{
		Title:         fmt.Sprintf("Pay Invoice %s · %s", invoiceNumber, company.Name),
		Invoice:       invView,
		LineItems:     lineItems,
		Company:       company,
		Payments:      payments,
		LatestPayment: latestPayment,
		RazorpayKeyID: keyID,
		IsPaid:        isPaid,
	}, nil
}

func (h *PaymentHandlers) renderPublicPaymentTemplate(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if h.App == nil || h.App.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	tmpl := h.App.Templates.Lookup(name)
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}
