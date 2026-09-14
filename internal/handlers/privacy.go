package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// PrivacyHandlers serves the DPDP breach-notice ledger (00154). All routes
// mount behind RequirePermission(privacy:manage) — org admins and platform
// admins only. Reads are tenant-scoped fail-closed in the service layer.
type PrivacyHandlers struct {
	*App
}

func privacyTenant(r *http.Request) string {
	return string(shared.TenantIDFromContext(r.Context()))
}

func breachJSON(b *service.BreachIncident) map[string]interface{} {
	out := map[string]interface{}{
		"id":             b.ID,
		"title":          b.Title,
		"description":    b.Description,
		"nature":         b.Nature,
		"extent":         b.Extent,
		"affected_count": b.AffectedCount,
		"status":         b.Status,
		"detected_at":    b.DetectedAt.UTC().Format(time.RFC3339),
		"detail_due_at":  b.DetectedAt.UTC().Add(service.BreachDetailWindow).Format(time.RFC3339),
		"detail_report":  b.DetailReport,
	}
	if b.BoardNotifiedAt.Valid {
		out["board_notified_at"] = b.BoardNotifiedAt.Time.UTC().Format(time.RFC3339)
	}
	if b.PrincipalsNotifiedAt.Valid {
		out["principals_notified_at"] = b.PrincipalsNotifiedAt.Time.UTC().Format(time.RFC3339)
	}
	if b.DetailedAt.Valid {
		out["detailed_at"] = b.DetailedAt.Time.UTC().Format(time.RFC3339)
	}
	return out
}

// ListBreachesAPI lists the tenant's incidents (?status=, ?overdue=1 for the
// 72h escalation watchlist).
func (h *PrivacyHandlers) ListBreachesAPI(w http.ResponseWriter, r *http.Request) {
	tenantID := privacyTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var incidents []service.BreachIncident
	var err error
	if r.URL.Query().Get("overdue") == "1" {
		incidents, err = h.Services.Privacy.ListOverdueBreaches(r.Context(), tenantID)
	} else {
		incidents, err = h.Services.Privacy.ListBreaches(r.Context(), tenantID, r.URL.Query().Get("status"))
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not list incidents")
		return
	}
	out := make([]map[string]interface{}, 0, len(incidents))
	for i := range incidents {
		out = append(out, breachJSON(&incidents[i]))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"incidents": out})
}

// ReportBreachAPI files a new incident (status open, detected now).
func (h *PrivacyHandlers) ReportBreachAPI(w http.ResponseWriter, r *http.Request) {
	tenantID := privacyTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Title         string `json:"title"`
		Description   string `json:"description"`
		Nature        string `json:"nature"`
		Extent        string `json:"extent"`
		AffectedCount int64  `json:"affected_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}
	id, err := h.Services.Privacy.ReportBreach(r.Context(), tenantID, req.Title, req.Description, req.Nature, req.Extent, req.AffectedCount)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not file incident")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})
}

func (h *PrivacyHandlers) breachByID(w http.ResponseWriter, r *http.Request) (*service.BreachIncident, bool) {
	tenantID := privacyTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	b, err := h.Services.Privacy.GetBreach(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "incident not found")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "could not read incident")
		}
		return nil, false
	}
	return b, true
}

// GetBreachAPI reads one incident inside the caller's tenant.
func (h *PrivacyHandlers) GetBreachAPI(w http.ResponseWriter, r *http.Request) {
	b, ok := h.breachByID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(breachJSON(b))
}

// NotifyBreachAPI stamps board/principals notification ({"board":true}).
func (h *PrivacyHandlers) NotifyBreachAPI(w http.ResponseWriter, r *http.Request) {
	b, ok := h.breachByID(w, r)
	if !ok {
		return
	}
	var req struct {
		Board      bool `json:"board"`
		Principals bool `json:"principals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (!req.Board && !req.Principals) {
		writeJSONError(w, http.StatusBadRequest, "set board and/or principals")
		return
	}
	tenantID := privacyTenant(r)
	if err := h.Services.Privacy.MarkNotified(r.Context(), tenantID, b.ID, req.Board, req.Principals); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not record notification")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// DetailBreachAPI files the detailed report (due ≤72h after detection).
func (h *PrivacyHandlers) DetailBreachAPI(w http.ResponseWriter, r *http.Request) {
	b, ok := h.breachByID(w, r)
	if !ok {
		return
	}
	var req struct {
		Report string `json:"report"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Report == "" {
		writeJSONError(w, http.StatusBadRequest, "report is required")
		return
	}
	tenantID := privacyTenant(r)
	if err := h.Services.Privacy.FileDetail(r.Context(), tenantID, b.ID, req.Report); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not file detail")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// CloseBreachAPI closes an incident (ledger stays append-only: no reopen).
func (h *PrivacyHandlers) CloseBreachAPI(w http.ResponseWriter, r *http.Request) {
	b, ok := h.breachByID(w, r)
	if !ok {
		return
	}
	tenantID := privacyTenant(r)
	if err := h.Services.Privacy.CloseBreach(r.Context(), tenantID, b.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not close incident")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
