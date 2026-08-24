package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
	"transport-app/internal/auth"
	"transport-app/internal/httpx"
	"transport-app/internal/shared"
)

// AlertInboxHandlers serves the ranked alert inbox API (Spec 22 §2.1).
// Tenant always comes from the request context (RequireAPIAuth), never
// from the request body.
type AlertInboxHandlers struct {
	repo repository.AlertRepository
}

func NewAlertInboxHandlers(repo repository.AlertRepository) *AlertInboxHandlers {
	return &AlertInboxHandlers{repo: repo}
}

// inboxItem is the wire shape of one inbox row (Spec 22 §2.1 example).
type inboxItem struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Severity     string   `json:"severity"`
	SeverityRank int      `json:"severity_rank"`
	Title        string   `json:"title"`
	VehicleID    string   `json:"vehicle_id,omitempty"`
	MoneyAtRisk  float64  `json:"money_at_risk"`
	CreatedAt    string   `json:"created_at"`
	AckStatus    string   `json:"ack_status"`
	SnoozedUntil string   `json:"snoozed_until,omitempty"`
	Actions      []string `json:"actions"`
}

func (h *AlertInboxHandlers) toItem(a domain.Alert) inboxItem {
	item := inboxItem{
		ID:           a.ID,
		Type:         a.AlertType,
		Severity:     a.Severity,
		SeverityRank: a.SeverityRank,
		Title:        a.Title,
		MoneyAtRisk:  a.MoneyAtRisk,
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
		AckStatus:    a.AckStatus,
	}
	if a.EntityType != nil && *a.EntityType == "vehicle" && a.EntityID != nil {
		item.VehicleID = *a.EntityID
	}
	if a.SnoozedUntil != nil {
		item.SnoozedUntil = a.SnoozedUntil.UTC().Format(time.RFC3339)
	}
	item.Actions = []string{"snooze"}
	if item.VehicleID != "" {
		item.Actions = append([]string{"locate", "call_driver"}, "snooze")
	}
	return item
}

// contextUserID extracts the acting user from RequireAPIAuth's context.
func contextUserID(r *http.Request) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil {
		return sess.UserID
	}
	return ""
}

// List handles GET /api/alerts/inbox?status=open|snoozed|acked|all&limit=50.
func (h *AlertInboxHandlers) List(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	alerts, err := h.repo.ListInbox(r.Context(), string(tenantID), status, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	items := make([]inboxItem, 0, len(alerts))
	for _, a := range alerts {
		items = append(items, h.toItem(a))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"alerts": items})
}

// Ack handles POST /api/alerts/{id}/ack. Second-admin acks are no-ops
// that still return ok (Spec 22 edge case 10).
func (h *AlertInboxHandlers) Ack(w http.ResponseWriter, r *http.Request) {
	userID := contextUserID(r)
	ok, err := h.repo.InboxAck(r.Context(), chi.URLParam(r, "id"), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "applied": ok})
}

type snoozeBody struct {
	Minutes int `json:"minutes"`
}

// Snooze handles POST /api/alerts/{id}/snooze {"minutes":120}.
func (h *AlertInboxHandlers) Snooze(w http.ResponseWriter, r *http.Request) {
	var body snoozeBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Minutes <= 0 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "minutes must be a positive integer"})
		return
	}
	until := time.Now().UTC().Add(time.Duration(body.Minutes) * time.Minute)
	ok, err := h.repo.InboxSnooze(r.Context(), chi.URLParam(r, "id"), contextUserID(r), until)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"applied":       ok,
		"snoozed_until": until.Format(time.RFC3339),
	})
}

type snoozeAllBody struct {
	IDs     []string `json:"ids"`
	Minutes int      `json:"minutes"`
}

// SnoozeAll handles POST /api/alerts/snooze-all {"ids":[...],"minutes":120}.
func (h *AlertInboxHandlers) SnoozeAll(w http.ResponseWriter, r *http.Request) {
	var body snoozeAllBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Minutes <= 0 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "minutes must be a positive integer and ids required"})
		return
	}
	if len(body.IDs) == 0 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "ids must not be empty"})
		return
	}
	until := time.Now().UTC().Add(time.Duration(body.Minutes) * time.Minute)
	count, err := h.repo.InboxSnoozeAll(r.Context(), body.IDs, contextUserID(r), until)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "count": count})
}
