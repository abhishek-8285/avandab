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

	"github.com/go-chi/chi/v5"

	paymentapp "transport-app/internal/payment/application"
	"transport-app/internal/shared"
)

// Public payment page view types are owned by the payment application layer
// (handlers-end-state ADR, Phase 2). The aliases keep the template and the
// characterization-test references unchanged.
type (
	PublicInvoiceLineItemView = paymentapp.PublicInvoiceLineItemView
	PublicPaymentRecordView   = paymentapp.PublicPaymentRecordView
	PublicCompanyView         = paymentapp.PublicCompanyView
	PublicInvoiceView         = paymentapp.PublicInvoiceView
	PublicPayData             = paymentapp.PublicPayData
)

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

	data.Lang = langOf(r)
	h.renderPublicPaymentTemplate(w, r, "invoice_pay.html", data)
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

	canonicalID, tenantID, err := h.publicPayQueries().ResolvePublicInvoice(r.Context(), invoiceID)
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

	canonicalID, tenantID, err := h.publicPayQueries().ResolvePublicInvoice(r.Context(), req.InvoiceID)
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
	data, err := h.publicPayQueries().LoadPublicPay(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if h.Config != nil {
		data.RazorpayKeyID = h.Config.RazorpayKeyID
	}
	return data, nil
}

// publicPayQueries wires the public payment read model lazily: handlers built
// outside App construction (characterization tests) call loadPublicPayData
// directly, so the nil guard lives here as well as in init.
func (h *PaymentHandlers) publicPayQueries() *paymentapp.PublicPaymentQueries {
	if h.publicPay == nil {
		h.publicPay = paymentapp.NewPublicPaymentQueries(h.DB)
	}
	return h.publicPay
}

func (h *PaymentHandlers) renderPublicPaymentTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if h.App == nil || h.App.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	tmpl := h.App.templatesFor(r).Lookup(name)
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}
