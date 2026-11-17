package handlers

// Public payment read-model wiring, split out of public_payment.go to keep
// that file under its ratchet cap (raw SQL stays in the application layer).

import (
	"context"

	paymentapp "transport-app/internal/payment/application"
)

func (h *PaymentHandlers) loadPublicPayData(ctx context.Context, invoiceID string) (*PublicPayData, error) {
	data, err := h.publicPayQueries().LoadPublicPay(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if h.Config != nil {
		data.RazorpayKeyID = h.Config.RazorpayKeyID
	}
	// H2 branding (company_settings id=1 vs per-tenant profile) is resolved in
	// the read model, where the invoice tenant is already known.
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
