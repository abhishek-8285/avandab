package handlers

import (
	"net/http"
	"time"

	"transport-app/internal/alerts/repository"
	"transport-app/internal/apperr"
	"transport-app/internal/auth"
	"transport-app/internal/httpx"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// ConsoleHandlers serves the owner Command Center (Spec 22 §4.1).
// Step 1 shipped the ranked alert inbox; Step 2 adds the money strip
// (Spec 22 §2.2). Fleet strip, live map and context panel arrive in Step 3.
type ConsoleHandlers struct {
	app  *App
	repo repository.AlertRepository
	pnl  *service.PNLService
}

func NewConsoleHandlers(app *App, repo repository.AlertRepository, pnl *service.PNLService) *ConsoleHandlers {
	return &ConsoleHandlers{app: app, repo: repo, pnl: pnl}
}

// Page handles GET /console.
func (h *ConsoleHandlers) Page(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	alerts, err := h.repo.ListInbox(r.Context(), tenantID, "open", 50)
	if err != nil {
		h.app.renderError(w, http.StatusInternalServerError, "Console unavailable", "Could not load the alert inbox.", nil)
		return
	}
	user, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	inbox := make([]map[string]any, 0, len(alerts))
	for _, a := range alerts {
		item := map[string]any{
			"ID":           a.ID,
			"Title":        a.Title,
			"Severity":     a.Severity,
			"SeverityRank": a.SeverityRank,
			"MoneyAtRisk":  a.MoneyAtRisk,
			"CreatedAt":    a.CreatedAt.Format("02 Jan 15:04"),
			"AckStatus":    a.AckStatus,
			"Occurrences":  a.Occurrences,
		}
		if a.EntityType != nil && *a.EntityType == "vehicle" && a.EntityID != nil {
			item["VehicleID"] = *a.EntityID
		}
		inbox = append(inbox, item)
	}

	// Money strip is best-effort on the page render: without PNL service or
	// on error the partial renders placeholders instead of failing the page.
	var strip *service.MoneyStrip
	if h.pnl != nil {
		strip, _ = h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	}

	h.app.renderPage(w, r, "console.html", PageData{
		Title: "Command Center",
		User:  user,
		Extra: map[string]interface{}{
			"InboxAlerts": inbox,
			"MoneyStrip":  strip,
		},
	})
}

// MoneyStrip handles GET /api/dashboard/money-strip (Spec 22 §2.2).
func (h *ConsoleHandlers) MoneyStrip(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if h.pnl == nil || h.repo == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	strip, err := h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	open, critical, err := h.repo.InboxCounts(r.Context(), tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"date":        strip.Date,
		"revenue":     strip.Revenue,
		"spent":       strip.Spent,
		"receivables": strip.Receivables,
		"open_alerts": open,
		"critical":    critical,
	})
}
