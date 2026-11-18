package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/payment/application"
	"transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
)

// APIPaymentHandler handles REST endpoints for the payment vertical slice.
type APIPaymentHandler struct {
	recordUC        *application.RecordPaymentUseCase
	getUC           *application.GetPaymentUseCase
	listUC          *application.ListPaymentsUseCase
	reverseUC       *application.ReversePaymentUseCase
	listByInvoiceUC *application.ListPaymentsByInvoiceUseCase
	webhookUC       *application.RazorpayWebhookUseCase
	orderUC         *application.CreateRazorpayOrderUseCase
	verifyUC        *application.VerifyRazorpayPaymentUseCase
	authSrv         auth.AuthorizationService
}

// NewAPIPaymentHandler constructs an APIPaymentHandler.
func NewAPIPaymentHandler(
	recordUC *application.RecordPaymentUseCase,
	getUC *application.GetPaymentUseCase,
	listUC *application.ListPaymentsUseCase,
	reverseUC *application.ReversePaymentUseCase,
	listByInvoiceUC *application.ListPaymentsByInvoiceUseCase,
	webhookUC *application.RazorpayWebhookUseCase,
	orderUC *application.CreateRazorpayOrderUseCase,
	verifyUC *application.VerifyRazorpayPaymentUseCase,
	authSrv auth.AuthorizationService,
) *APIPaymentHandler {
	h := &APIPaymentHandler{
		recordUC:        recordUC,
		getUC:           getUC,
		listUC:          listUC,
		reverseUC:       reverseUC,
		listByInvoiceUC: listByInvoiceUC,
		webhookUC:       webhookUC,
		orderUC:         orderUC,
		verifyUC:        verifyUC,
		authSrv:         authSrv,
	}
	if h.webhookUC != nil {
		h.webhookUC.SetReversePaymentUseCase(reverseUC)
	}
	return h
}

// Register mounts all payment routes.
func (h *APIPaymentHandler) Register(r chi.Router) {
	r.Route("/api/v1/payments", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.authSrv, "payments", "create")).Post("/", h.Record)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "create")).Post("/razorpay-order", h.RazorpayOrder)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "create")).Post("/razorpay/verify", h.RazorpayVerify)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "read")).Get("/", h.List)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "read")).Get("/{id}", h.Get)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "read")).Get("/by-invoice/{invoiceID}", h.ListByInvoice)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "delete")).Post("/{id}/reverse", h.Reverse)
		r.With(middleware.RequirePermission(h.authSrv, "payments", "read")).Get("/razorpay-webhook/status", h.RazorpayWebhookStatus)
	})
}

// RazorpayWebhook handles Razorpay payment webhooks. Intentionally mounted
// outside the authenticated API group by cmd/server/main.go.
func (h *APIPaymentHandler) RazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookUC == nil {
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var ev application.RazorpayWebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Event id comes from the signed body, not the strippable header.
	id, err := h.webhookUC.ExecuteEvent(r.Context(), body, r.Header.Get("X-Razorpay-Signature"), application.EventIDFromWebhook(ev), ev)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrWebhookNotConfigured):
			http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		case errors.Is(err, application.ErrWebhookInvalidSignature):
			http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		default:
			http.Error(w, "failed to process webhook", http.StatusBadRequest)
		}
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"received": true, "payment_id": string(id)})
}

// RazorpayOrder creates a server-side Razorpay order for the invoice balance.
// The amount is always derived server-side — the client can never set it
// (Spec 11 §5.1).
func (h *APIPaymentHandler) RazorpayOrder(w http.ResponseWriter, r *http.Request) {
	if h.orderUC == nil {
		http.Error(w, "razorpay not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == "" {
		http.Error(w, "invoice_id is required", http.StatusBadRequest)
		return
	}

	res, err := h.orderUC.Execute(r.Context(), application.CreateRazorpayOrderCommand{
		TenantID:  shared.TenantIDFromContext(r.Context()),
		InvoiceID: req.InvoiceID,
	})
	if err != nil {
		switch {
		case errors.Is(err, application.ErrRazorpayNotConfigured):
			http.Error(w, "razorpay not configured", http.StatusServiceUnavailable)
		case errors.Is(err, application.ErrInvoiceNotFound):
			http.Error(w, "invoice not found", http.StatusNotFound)
		case errors.Is(err, application.ErrInvoiceAlreadySettled):
			http.Error(w, "invoice has no outstanding balance", http.StatusPaymentRequired)
		default:
			http.Error(w, "failed to create razorpay order", http.StatusInternalServerError)
		}
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

// RazorpayVerify validates the checkout signature and records the payment
// against the invoice balance. All four fields are required.
func (h *APIPaymentHandler) RazorpayVerify(w http.ResponseWriter, r *http.Request) {
	if h.verifyUC == nil {
		http.Error(w, "razorpay not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		InvoiceID string `json:"invoice_id"`
		OrderID   string `json:"order_id"`
		PaymentID string `json:"payment_id"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.InvoiceID == "" || req.OrderID == "" || req.PaymentID == "" || req.Signature == "" {
		http.Error(w, "invoice_id, order_id, payment_id and signature are required", http.StatusBadRequest)
		return
	}

	id, err := h.verifyUC.Execute(r.Context(), application.VerifyRazorpayPaymentCommand{
		TenantID:  shared.TenantIDFromContext(r.Context()),
		InvoiceID: req.InvoiceID,
		OrderID:   req.OrderID,
		PaymentID: req.PaymentID,
		Signature: req.Signature,
	})
	if err != nil {
		switch {
		case errors.Is(err, application.ErrRazorpayNotConfigured):
			http.Error(w, "razorpay not configured", http.StatusServiceUnavailable)
		case errors.Is(err, application.ErrRazorpayInvalidSignature):
			http.Error(w, "invalid_signature", http.StatusUnauthorized)
		case errors.Is(err, application.ErrInvoiceNotFound):
			http.Error(w, "invoice not found", http.StatusNotFound)
		case errors.Is(err, application.ErrInvoiceAlreadySettled):
			http.Error(w, "invoice has no outstanding balance", http.StatusPaymentRequired)
		default:
			http.Error(w, "failed to verify razorpay payment", http.StatusBadRequest)
		}
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id), "method": "razorpay"})
}

func (h *APIPaymentHandler) Record(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InvoiceID   string  `json:"invoice_id"`
		PaymentDate string  `json:"payment_date"`
		Amount      float64 `json:"amount"`
		Method      string  `json:"method"`
		Reference   *string `json:"reference"`
		Remarks     *string `json:"remarks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch aggregate.PaymentMethod(req.Method) {
	case aggregate.PaymentMethodCash, aggregate.PaymentMethodUPI, aggregate.PaymentMethodBankTransfer, aggregate.PaymentMethodCheque, aggregate.PaymentMethodRazorpay:
	default:
		http.Error(w, "invalid payment method", http.StatusBadRequest)
		return
	}

	payDate, err := time.Parse(time.RFC3339, req.PaymentDate)
	if err != nil {
		http.Error(w, "payment_date must be RFC3339", http.StatusBadRequest)
		return
	}

	id, err := h.recordUC.Execute(r.Context(), application.RecordPaymentCommand{
		TenantID:    shared.TenantIDFromContext(r.Context()),
		InvoiceID:   req.InvoiceID,
		PaymentDate: payDate,
		Amount:      req.Amount,
		Method:      aggregate.PaymentMethod(req.Method),
		Reference:   req.Reference,
		Remarks:     req.Remarks,
	})
	if err != nil {
		if errors.Is(err, application.ErrInvoiceNotFound) {
			http.Error(w, "Invoice not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to record payment", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id)})
}

func (h *APIPaymentHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}

	res, err := h.listUC.Execute(r.Context(), application.ListPaymentsQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     page,
		Limit:    limit,
		Method:   r.URL.Query().Get("method"),
	})
	if err != nil {
		http.Error(w, "Failed to list payments", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"payments": res.Payments, "total": res.Total})
}

func (h *APIPaymentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := h.getUC.Execute(r.Context(), application.GetPaymentQuery{
		ID:       aggregate.PaymentID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *APIPaymentHandler) ListByInvoice(w http.ResponseWriter, r *http.Request) {
	invoiceID := chi.URLParam(r, "invoiceID")
	payments, err := h.listByInvoiceUC.Execute(r.Context(), application.ListPaymentsByInvoiceQuery{
		TenantID:  shared.TenantIDFromContext(r.Context()),
		InvoiceID: invoiceID,
	})
	if err != nil {
		http.Error(w, "Failed to list payments for invoice", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"payments": payments})
}

func (h *APIPaymentHandler) Reverse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OriginalPaymentID string `json:"original_payment_id"`
		Reason            string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := h.reverseUC.Execute(r.Context(), application.ReversePaymentCommand{
		TenantID:      shared.TenantIDFromContext(r.Context()),
		OriginalPayID: aggregate.PaymentID(req.OriginalPaymentID),
		Reason:        req.Reason,
	})
	if err != nil {
		if errors.Is(err, application.ErrPaymentNotFound) || errors.Is(err, application.ErrInvoiceNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id)})
}

// RazorpayWebhookStatus returns the last webhook received timestamp and counts by event type.
func (h *APIPaymentHandler) RazorpayWebhookStatus(w http.ResponseWriter, r *http.Request) {
	if h.webhookUC == nil {
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	status := h.webhookUC.Status()
	_ = json.NewEncoder(w).Encode(status)
}
