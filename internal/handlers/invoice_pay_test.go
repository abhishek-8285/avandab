package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

	"transport-app/internal/config"
	invoiceagg "transport-app/internal/invoice/domain/aggregate"
	invoicerepo "transport-app/internal/invoice/infrastructure/persistence/sql"
	paymentapp "transport-app/internal/payment/application"
	"transport-app/internal/payment/razorpay"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

type fakePaymentOrderAPI struct {
	lastData map[string]interface{}
}

func (f *fakePaymentOrderAPI) Create(data map[string]interface{}, _ map[string]string) (map[string]interface{}, error) {
	f.lastData = data
	return map[string]interface{}{
		"id":     "order_test_999",
		"status": "created",
		"amount": data["amount"],
	}, nil
}

func computeHMACSignature(orderID, paymentID, secret string) string {
	data := orderID + "|" + paymentID
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func setupPublicPayTest(t *testing.T) (*sql.DB, *App, *PaymentHandlers, string, string) {
	t.Helper()
	db := handlerTestDB(t)
	authSvc := &mockAuthSvc{}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	const testKeyID = "rzp_test_key_123"
	const testKeySecret = "rzp_test_secret_abc456"

	cfg := &config.Config{
		RazorpayKeyID:     testKeyID,
		RazorpayKeySecret: testKeySecret,
	}

	app := &App{
		DB:        db,
		Templates: tmpl,
		Config:    cfg,
		AuthSrv:   authSvc,
	}

	sqlUoW := uow.NewSQLUnitOfWork(db)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()

	recordUC := paymentapp.NewRecordPaymentUseCase(sqlUoW, idGen, realClock)
	listUC := paymentapp.NewListPaymentsUseCase(sqlUoW)
	getUC := paymentapp.NewGetPaymentUseCase(sqlUoW)

	client := razorpay.NewRazorpayClient(testKeyID, testKeySecret)
	fakeAPI := &fakePaymentOrderAPI{}
	mockCreator := &mockOrderCreator{api: fakeAPI}
	orderUC := paymentapp.NewCreateRazorpayOrderUseCase(sqlUoW, mockCreator, testKeyID)
	verifyUC := paymentapp.NewVerifyRazorpayPaymentUseCase(sqlUoW, recordUC, client, testKeySecret, realClock)

	h := &PaymentHandlers{
		App:      app,
		recordUC: recordUC,
		listUC:   listUC,
		getUC:    getUC,
		orderUC:  orderUC,
		verifyUC: verifyUC,
	}
	app.Payments = h

	// Seed tenant, company_settings, customer, route, booking, trip, invoices
	_, _ = db.Exec(`INSERT OR REPLACE INTO tenants (id, name, slug) VALUES ('1', 'Avandab Logistics Network', 'avandab')`)
	_, _ = db.Exec(`INSERT INTO company_settings (id, company_name, gst_number, address, phone, email, currency, timezone, state_code)
		VALUES (1, 'Avandab Logistics Network', '27AAACA1234A1Z5', '101 Freight Terminal, Mumbai', '+91 9876543210', 'billing@avandab.com', 'INR', 'Asia/Kolkata', '27')
		ON CONFLICT(id) DO UPDATE SET company_name = 'Avandab Logistics Network', gst_number = '27AAACA1234A1Z5'`)

	_, err = db.Exec(`INSERT INTO customers (id, name, company, phone, email, gst, address, billing_address, tenant_id)
		VALUES ('cust_pay_1', 'Rajesh Logistics', 'Rajesh Enterprises Ltd', '9820011223', 'rajesh@enterprises.com', '27AABCR1234M1Z2', 'Plot 45, MIDC, Navi Mumbai', 'Plot 45, MIDC, Navi Mumbai', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r_pay_1', 'Mumbai JNPT', 'Pune Bhosari', 160.0, 4.0, 10000.0, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id)
		VALUES ('bk_pay_1', 'BK-2026-001', 'cust_pay_1', '2026-09-02 09:00:00', 'r_pay_1', 'trailer', 10000.0, 'confirmed', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id)
		VALUES ('trip_pay_1', 'TRP-2026-001', 'bk_pay_1', 'r_pay_1', '2026-09-02 09:00:00', 'delivered', '1')`)
	require.NoError(t, err)

	unpaidInvID := "inv_unpaid_101"
	paidInvID := "inv_paid_202"

	// Create unpaid invoice (10000 subtotal + 1800 tax = 11800 total)
	tripIDVal := "trip_pay_1"
	invRepo := invoicerepo.NewInvoiceRepository(db)
	unpaidAgg := invoiceagg.NewInvoiceAggregate(
		invoiceagg.InvoiceID(unpaidInvID),
		shared.TenantID("1"),
		"INV-2026-001",
		"bk_pay_1",
		"cust_pay_1",
		&tripIDVal,
		10000.0,
		1800.0,
		0.0,
		11800.0,
		invoiceagg.PaymentStatusPending,
		time.Now(),
	)
	sacCode := "996511"
	unpaidAgg.AddLineItem(invoiceagg.LineItem{
		ID:           "li_1",
		TenantID:     shared.TenantID("1"),
		InvoiceID:    invoiceagg.InvoiceID(unpaidInvID),
		LineType:     invoiceagg.LineTypeFreight,
		HSNSACCode:   &sacCode,
		Description:  "Primary Freight Transportation JNPT to Bhosari",
		Quantity:     1,
		UnitPrice:    10000.0,
		Amount:       10000.0,
		TaxableValue: 10000.0,
		CgstRate:     9.0,
		CgstAmount:   900.0,
		SgstRate:     9.0,
		SgstAmount:   900.0,
		Total:        11800.0,
	})
	require.NoError(t, invRepo.Save(context.Background(), unpaidAgg))

	// Create fully paid invoice
	paidAgg := invoiceagg.NewInvoiceAggregate(
		invoiceagg.InvoiceID(paidInvID),
		shared.TenantID("1"),
		"INV-2026-002",
		"bk_pay_1",
		"cust_pay_1",
		&tripIDVal,
		5000.0,
		900.0,
		0.0,
		5900.0,
		invoiceagg.PaymentStatusPaid,
		time.Now(),
	)
	paidAgg.Cgst = 450.0
	paidAgg.Sgst = 450.0
	_ = paidAgg.ApplyPayment(5900.0, time.Now())
	require.NoError(t, invRepo.Save(context.Background(), paidAgg))

	// Insert payment record for paid invoice
	_, err = db.Exec(`INSERT INTO payments (id, tenant_id, invoice_id, payment_date, amount, method, reference, razorpay_payment_id, razorpay_order_id)
		VALUES ('pay_settled_1', '1', ?, '2026-09-02 10:00:00', 5900.0, 'razorpay', 'pay_rzp_existing_001', 'pay_rzp_existing_001', 'order_rzp_existing_001')`, paidInvID)
	require.NoError(t, err)

	return db, app, h, unpaidInvID, paidInvID
}

type mockOrderCreator struct {
	api *fakePaymentOrderAPI
}

func (m *mockOrderCreator) CreateOrder(invoiceID string, amountINR float64, currency string) (*razorpay.Order, error) {
	amountPaise := int64(amountINR * 100)
	if currency == "" {
		currency = "INR"
	}
	_, _ = m.api.Create(map[string]interface{}{
		"amount":   amountPaise,
		"currency": currency,
		"receipt":  "rcpt_" + invoiceID,
		"notes": map[string]interface{}{
			"invoice_id": invoiceID,
		},
	}, nil)

	return &razorpay.Order{
		ID:       "order_test_999",
		Amount:   amountPaise,
		Currency: currency,
		Receipt:  "rcpt_" + invoiceID,
		Status:   "created",
	}, nil
}

func TestPublicPay_UnpaidInvoice_HTMLAndJSON(t *testing.T) {
	_, _, h, unpaidInvID, _ := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Get("/pay/{invoiceId}", h.PublicPay)

	// 1. HTML Rendering Test
	reqHTML := httptest.NewRequest("GET", "/pay/"+unpaidInvID, nil)
	wHTML := httptest.NewRecorder()
	r.ServeHTTP(wHTML, reqHTML)

	assert.Equal(t, http.StatusOK, wHTML.Code)
	assert.Contains(t, wHTML.Header().Get("Content-Type"), "text/html")
	htmlBody := wHTML.Body.String()
	assert.Contains(t, htmlBody, "INV-2026-001", "must contain invoice number")
	assert.Contains(t, htmlBody, "Avandab Logistics Network", "must contain company name")
	assert.Contains(t, htmlBody, "27AAACA1234A1Z5", "must contain company GSTIN")
	assert.Contains(t, htmlBody, "Rajesh Logistics", "must contain customer name")
	assert.Contains(t, htmlBody, "Primary Freight Transportation", "must contain line item description")
	assert.Contains(t, htmlBody, "11800.00", "must display outstanding balance")
	assert.Contains(t, htmlBody, "checkout.razorpay.com", "must include Razorpay checkout script")
	assert.Contains(t, htmlBody, "PAYMENT DUE", "must show pending status badge")

	// 2. JSON Format Test
	reqJSON := httptest.NewRequest("GET", "/pay/"+unpaidInvID+"?format=json", nil)
	wJSON := httptest.NewRecorder()
	r.ServeHTTP(wJSON, reqJSON)

	assert.Equal(t, http.StatusOK, wJSON.Code)
	var payData PublicPayData
	err := json.Unmarshal(wJSON.Body.Bytes(), &payData)
	require.NoError(t, err)
	assert.Equal(t, unpaidInvID, payData.Invoice.ID)
	assert.Equal(t, "INV-2026-001", payData.Invoice.InvoiceNumber)
	assert.Equal(t, 11800.0, payData.Invoice.OutstandingBalance)
	assert.False(t, payData.IsPaid)
	assert.Equal(t, "rzp_test_key_123", payData.RazorpayKeyID)
	assert.Len(t, payData.LineItems, 1)
	assert.Equal(t, "996511", payData.LineItems[0].HSNSAC)
}

func TestPublicPay_PaidInvoice_Receipt(t *testing.T) {
	_, _, h, _, paidInvID := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Get("/pay/{invoiceId}", h.PublicPay)

	// HTML Receipt Test
	reqHTML := httptest.NewRequest("GET", "/pay/"+paidInvID, nil)
	wHTML := httptest.NewRecorder()
	r.ServeHTTP(wHTML, reqHTML)

	assert.Equal(t, http.StatusOK, wHTML.Code)
	htmlBody := wHTML.Body.String()
	assert.Contains(t, htmlBody, "INV-2026-002")
	assert.Contains(t, htmlBody, "PAID IN FULL")
	assert.Contains(t, htmlBody, "pay_rzp_existing_001", "must display settled payment transaction ID")
	assert.Contains(t, htmlBody, "Invoice Settled &amp; Paid in Full")

	// JSON Receipt Test
	reqJSON := httptest.NewRequest("GET", "/pay/"+paidInvID+"?format=json", nil)
	wJSON := httptest.NewRecorder()
	r.ServeHTTP(wJSON, reqJSON)

	assert.Equal(t, http.StatusOK, wJSON.Code)
	var payData PublicPayData
	err := json.Unmarshal(wJSON.Body.Bytes(), &payData)
	require.NoError(t, err)
	assert.True(t, payData.IsPaid)
	assert.Equal(t, 0.0, payData.Invoice.OutstandingBalance)
	assert.NotEmpty(t, payData.Payments)
	assert.Equal(t, "pay_rzp_existing_001", payData.Payments[0].RazorpayPaymentID)
}

func TestPublicPay_NotFound(t *testing.T) {
	_, _, h, _, _ := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Get("/pay/{invoiceId}", h.PublicPay)

	req := httptest.NewRequest("GET", "/pay/inv_non_existent_999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPublicRazorpayOrder_ServerSideGeneration(t *testing.T) {
	_, _, h, unpaidInvID, paidInvID := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Post("/pay/{invoiceId}/razorpay/order", h.PublicRazorpayOrder)

	// 1. Unpaid Invoice Order Generation
	reqBody := fmt.Sprintf(`{"invoice_id":"%s"}`, unpaidInvID)
	req := httptest.NewRequest("POST", "/pay/"+unpaidInvID+"/razorpay/order", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var orderRes map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &orderRes)
	require.NoError(t, err)
	assert.Equal(t, "order_test_999", orderRes["order_id"])
	assert.Equal(t, "rzp_test_key_123", orderRes["razorpay_key_id"])
	assert.Equal(t, float64(1180000), orderRes["amount_paise"], "11800 INR balance must equal 1180000 paise")
	assert.Equal(t, "INR", orderRes["currency"])

	// 2. Paid Invoice Order Rejection (400 Bad Request)
	reqPaid := httptest.NewRequest("POST", "/pay/"+paidInvID+"/razorpay/order", strings.NewReader(`{}`))
	reqPaid.Header.Set("Content-Type", "application/json")
	wPaid := httptest.NewRecorder()
	r.ServeHTTP(wPaid, reqPaid)

	assert.Equal(t, http.StatusBadRequest, wPaid.Code)
	assert.Contains(t, wPaid.Body.String(), "invoice has no outstanding balance")

	// 3. Not Found Invoice
	req404 := httptest.NewRequest("POST", "/pay/inv_ghost/razorpay/order", strings.NewReader(`{}`))
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, req404)
	assert.Equal(t, http.StatusNotFound, w404.Code)
}

func TestPublicRazorpayVerify_ValidSignature_InstantLedgerSettlement(t *testing.T) {
	db, _, h, unpaidInvID, _ := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Post("/pay/{invoiceId}/razorpay/verify", h.PublicRazorpayVerify)

	orderID := "order_test_999"
	paymentID := "pay_test_live_888"
	const secret = "rzp_test_secret_abc456"
	validSignature := computeHMACSignature(orderID, paymentID, secret)

	verifyPayload := map[string]string{
		"invoice_id":          unpaidInvID,
		"razorpay_order_id":   orderID,
		"razorpay_payment_id": paymentID,
		"razorpay_signature":  validSignature,
	}
	bodyBytes, _ := json.Marshal(verifyPayload)

	req := httptest.NewRequest("POST", "/pay/"+unpaidInvID+"/razorpay/verify", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var verifyRes map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &verifyRes)
	require.NoError(t, err)
	assert.Equal(t, "success", verifyRes["status"])
	assert.NotEmpty(t, verifyRes["payment_id"])

	// Verify Payment Record in Database
	var payMethod, rzpPayID, rzpOrderID string
	var payAmount float64
	err = db.QueryRow(`
		SELECT method, amount, razorpay_payment_id, razorpay_order_id
		FROM payments
		WHERE invoice_id = ? AND razorpay_payment_id = ?
	`, unpaidInvID, paymentID).Scan(&payMethod, &payAmount, &rzpPayID, &rzpOrderID)
	require.NoError(t, err)
	assert.Equal(t, "razorpay", payMethod)
	assert.Equal(t, 11800.0, payAmount)
	assert.Equal(t, paymentID, rzpPayID)
	assert.Equal(t, orderID, rzpOrderID)

	// Verify Invoice Settled in Ledger
	var invPaidAmount float64
	var invPayStatus, invStatus string
	err = db.QueryRow(`
		SELECT paid_amount, payment_status, status
		FROM invoices
		WHERE id = ?
	`, unpaidInvID).Scan(&invPaidAmount, &invPayStatus, &invStatus)
	require.NoError(t, err)
	assert.Equal(t, 11800.0, invPaidAmount)
	assert.Equal(t, "paid", invPayStatus)
	assert.Equal(t, "paid", invStatus)

	// Test Idempotent Duplicate Verification
	reqDup := httptest.NewRequest("POST", "/pay/"+unpaidInvID+"/razorpay/verify", bytes.NewReader(bodyBytes))
	reqDup.Header.Set("Content-Type", "application/json")
	wDup := httptest.NewRecorder()
	r.ServeHTTP(wDup, reqDup)

	assert.Equal(t, http.StatusOK, wDup.Code)
}

func TestPublicRazorpayVerify_FakeSignature_Rejected(t *testing.T) {
	db, _, h, unpaidInvID, _ := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Post("/pay/{invoiceId}/razorpay/verify", h.PublicRazorpayVerify)

	orderID := "order_test_999"
	paymentID := "pay_test_fake_000"
	fakeSignature := "deadbeefbadf00d1234567890abcdef1234567890abcdef1234567890abcdef1"

	verifyPayload := map[string]string{
		"invoice_id":          unpaidInvID,
		"razorpay_order_id":   orderID,
		"razorpay_payment_id": paymentID,
		"razorpay_signature":  fakeSignature,
	}
	bodyBytes, _ := json.Marshal(verifyPayload)

	req := httptest.NewRequest("POST", "/pay/"+unpaidInvID+"/razorpay/verify", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_signature")

	// Ensure database was NOT modified
	var payCount int
	err := db.QueryRow(`SELECT count(*) FROM payments WHERE razorpay_payment_id = ?`, paymentID).Scan(&payCount)
	require.NoError(t, err)
	assert.Equal(t, 0, payCount, "no payment row must be created for fake signature")
}

func TestPublicRazorpayVerify_MissingFields(t *testing.T) {
	_, _, h, unpaidInvID, _ := setupPublicPayTest(t)

	r := chi.NewRouter()
	r.Post("/pay/{invoiceId}/razorpay/verify", h.PublicRazorpayVerify)

	// Missing signature
	verifyPayload := map[string]string{
		"invoice_id":          unpaidInvID,
		"razorpay_order_id":   "order_123",
		"razorpay_payment_id": "pay_123",
	}
	bodyBytes, _ := json.Marshal(verifyPayload)

	req := httptest.NewRequest("POST", "/pay/"+unpaidInvID+"/razorpay/verify", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "required")
}
