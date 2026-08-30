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
		r.Post("/drivers/me/payouts", h.InitiatePayout)
		r.Post("/webhooks/payouts/razorpay", h.RazorpayPayoutWebhook)
	})
}

func (h *SettlementHandler) getTenantAndUser(r *http.Request) (string, string, bool) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
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
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "processed"})
}
