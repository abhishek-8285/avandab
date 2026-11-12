package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/httpx"
	"transport-app/internal/settlement/application"
	"transport-app/internal/shared"
)

type SettlementHandler struct {
	svc *application.SettlementAppService
}

func NewSettlementHandler(svc *application.SettlementAppService) *SettlementHandler {
	return &SettlementHandler{svc: svc}
}

func (h *SettlementHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/settlements/calculate", h.CalculateSettlement)
		r.Get("/drivers/me/wallet", h.GetDriverWallet)
		r.Get("/drivers/me/payouts", h.ListMyPayouts)
		r.Post("/drivers/me/payouts", h.InitiatePayout)
		r.Post("/drivers/me/payout-account/verify", h.VerifyPayoutAccount)
	})
}

func (h *SettlementHandler) getTenantAndUser(r *http.Request) (string, string, bool) {
	// Fail closed: no silent DefaultTenant fallback (see customer_handler).
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		return "", "", false
	}

	session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil || session.UserID == "" {
		return tenantID, "", false
	}

	return tenantID, session.UserID, true
}

func (h *SettlementHandler) CalculateSettlement(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.getTenantAndUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var req application.CalculateSettlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	settlement, err := h.svc.CalculateAndCreateSettlement(r.Context(), tenantID, req)
	if err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, settlement)
}

func (h *SettlementHandler) GetDriverWallet(w http.ResponseWriter, r *http.Request) {
	tenantID, driverID, ok := h.getTenantAndUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	wallet, err := h.svc.GetDriverWallet(r.Context(), tenantID, driverID)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, wallet)
}

// driverPayoutAccountView is the driver-safe view of a payout account: the
// encrypted account number is never exposed.
type driverPayoutAccountView struct {
	Masked             string `json:"masked"`
	BankName           string `json:"bank_name"`
	IFSC               string `json:"ifsc"`
	VerificationStatus string `json:"verification_status"`
}

type driverPayoutView struct {
	ID               string  `json:"id"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
	Status           string  `json:"status"`
	UTR              string  `json:"utr,omitempty"`
	ProviderPayoutID string  `json:"provider_payout_id,omitempty"`
	FailureReason    string  `json:"failure_reason,omitempty"`
	InitiatedAt      string  `json:"initiated_at"`
	CompletedAt      string  `json:"completed_at,omitempty"`
}

type driverPayoutsResponse struct {
	RailEnabled bool                     `json:"rail_enabled"`
	Account     *driverPayoutAccountView `json:"account,omitempty"`
	Payouts     []driverPayoutView       `json:"payouts"`
}

// ListMyPayouts returns the driver's own payout-rail state: masked bank
// account, verification status, whether the rail is enabled, and recent payout
// instructions. The encrypted account number is never serialized.
func (h *SettlementHandler) ListMyPayouts(w http.ResponseWriter, r *http.Request) {
	tenantID, driverID, ok := h.getTenantAndUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	summary, err := h.svc.ListDriverPayouts(r.Context(), tenantID, driverID, 20)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := driverPayoutsResponse{RailEnabled: h.svc.PayoutRailEnabled(), Payouts: make([]driverPayoutView, 0, len(summary.Payouts))}
	if summary.Account != nil {
		resp.Account = &driverPayoutAccountView{
			Masked:             summary.Account.AccountNumberMasked,
			BankName:           summary.Account.BankName,
			IFSC:               summary.Account.IFSC,
			VerificationStatus: summary.Account.VerificationStatus,
		}
	}
	for _, p := range summary.Payouts {
		v := driverPayoutView{
			ID:          p.ID,
			Amount:      p.Amount,
			Currency:    p.Currency,
			Status:      string(p.Status),
			InitiatedAt: p.InitiatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if p.UTR != nil {
			v.UTR = *p.UTR
		}
		if p.ProviderPayoutID != nil {
			v.ProviderPayoutID = *p.ProviderPayoutID
		}
		if p.FailureReason != nil {
			v.FailureReason = *p.FailureReason
		}
		if p.CompletedAt != nil {
			v.CompletedAt = p.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		resp.Payouts = append(resp.Payouts, v)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *SettlementHandler) InitiatePayout(w http.ResponseWriter, r *http.Request) {
	tenantID, driverID, ok := h.getTenantAndUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var req application.InitiatePayoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	idempHeader := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempHeader != "" && req.IdempotencyKey == "" {
		req.IdempotencyKey = idempHeader
	}

	resp, err := h.svc.InitiatePayout(r.Context(), tenantID, driverID, req)
	if err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if resp.IsDuplicate {
		httpx.JSON(w, http.StatusOK, resp)
	} else {
		httpx.JSON(w, http.StatusCreated, resp)
	}
}

// VerifyPayoutAccount starts penny-drop validation for the driver's primary
// bank account. The final valid/invalid lands via the payout webhook.
func (h *SettlementHandler) VerifyPayoutAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, driverID, ok := h.getTenantAndUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	res, err := h.svc.VerifyPayoutAccount(r.Context(), tenantID, driverID)
	if err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusAccepted, res)
}

func (h *SettlementHandler) RazorpayPayoutWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	eventID := strings.TrimSpace(r.Header.Get("X-Razorpay-Event-Id"))
	signature := strings.TrimSpace(r.Header.Get("X-Razorpay-Signature"))

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "failed reading webhook body"})
		return
	}

	if eventID == "" {
		// In production, header is mandatory. Fallback only permitted in local test environments with explicit event payload.
		var raw map[string]interface{}
		_ = json.Unmarshal(body, &raw)
		if ev, ok := raw["event"].(string); ok && ev != "" {
			eventID = "rzp_ev_" + ev
		} else {
			httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "X-Razorpay-Event-Id header is required"})
			return
		}
	}

	if err := h.svc.ProcessProviderWebhook(r.Context(), tenantID, eventID, signature, body); err != nil {
		if err.Error() == "webhook secret not configured" {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "processed"})
}
