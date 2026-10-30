package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/freightaudit"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type FreightAuditHandlers struct {
	*App
}

func (h *FreightAuditHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/freight-audit", func(r chi.Router) {
		r.Route("/rates", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/", h.APIListRateCards)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/", h.APICreateRateCard)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/{id}", h.APIGetRateCard)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Put("/{id}", h.APIUpdateRateCard)
		})

		r.Route("/audits", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/", h.APIListAudits)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/", h.APICreateAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/{id}", h.APIGetAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/approve-deduction", h.APIApproveDeduction)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/dispute", h.APIDisputeAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/settle", h.APISettleAudit)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/summary", h.APIGetSummary)
	})
}

func writeFreightAuditJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFreightAuditError(w http.ResponseWriter, code int, msg string) {
	writeFreightAuditJSON(w, code, map[string]string{"error": msg})
}

func (h *FreightAuditHandlers) service() *freightaudit.Service {
	return freightaudit.NewService(freightaudit.NewRepository(h.DB))
}

// Rate Card Handlers

func (h *FreightAuditHandlers) APIListRateCards(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := freightaudit.RateCardFilter{
		RouteID:     r.URL.Query().Get("route_id"),
		VehicleType: r.URL.Query().Get("vehicle_type"),
		ActiveOnly:  r.URL.Query().Get("active_only") == "true",
		Search:      r.URL.Query().Get("search"),
		Limit:       limit,
		Offset:      offset,
	}

	cards, total, err := h.service().ListRateCards(r.Context(), tenantID, filter)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to query rate cards: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, map[string]interface{}{
		"rate_cards": cards,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (h *FreightAuditHandlers) APICreateRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in freightaudit.CreateRateCardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	card, err := h.service().CreateRateCard(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, freightaudit.ErrContractNumberExists) {
			writeFreightAuditError(w, http.StatusConflict, "contract number already exists")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to create rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusCreated, card)
}

func (h *FreightAuditHandlers) APIGetRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	card, err := h.service().GetRateCard(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "rate card not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to fetch rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, card)
}

func (h *FreightAuditHandlers) APIUpdateRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in freightaudit.UpdateRateCardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	card, err := h.service().UpdateRateCard(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "rate card not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to update rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, card)
}

// Audit Handlers

func (h *FreightAuditHandlers) APIListAudits(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := freightaudit.AuditFilter{
		Status:          r.URL.Query().Get("status"),
		TripID:          r.URL.Query().Get("trip_id"),
		InvoiceID:       r.URL.Query().Get("invoice_id"),
		DiscrepancyOnly: r.URL.Query().Get("discrepancy_only") == "true",
		Search:          r.URL.Query().Get("search"),
		Limit:           limit,
		Offset:          offset,
	}

	audits, total, err := h.service().ListAudits(r.Context(), tenantID, filter)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to query freight audits: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, map[string]interface{}{
		"audits": audits,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *FreightAuditHandlers) APICreateAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in freightaudit.CreateAuditInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	audit, err := h.service().CreateAudit(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to create freight audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusCreated, audit)
}

func (h *FreightAuditHandlers) APIGetAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	audit, err := h.service().GetAudit(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to fetch freight audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func getAuditUser(r *http.Request, defaultVal string) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil && sess.UserID != "" {
		return sess.UserID
	}
	return defaultVal
}

func (h *FreightAuditHandlers) APIApproveDeduction(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "dispatcher")

	var in freightaudit.ApproveDeductionInput
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	audit, err := h.service().ApproveDeduction(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidStatus) {
			writeFreightAuditError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to approve deduction: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APIDisputeAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "carrier")

	var in freightaudit.DisputeAuditInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeFreightAuditError(w, http.StatusBadRequest, "dispute reason is required")
		return
	}

	audit, err := h.service().DisputeAudit(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidStatus) {
			writeFreightAuditError(w, http.StatusConflict, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to dispute audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APISettleAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "finance")

	var in freightaudit.SettleAuditInput
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	audit, err := h.service().SettleAudit(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to settle audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APIGetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetSummary(r.Context(), tenantID)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to get freight audit summary: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, summary)
}
