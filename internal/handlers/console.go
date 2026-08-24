package handlers

import (
	"net/http"

	"transport-app/internal/alerts/repository"
	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// ConsoleHandlers serves the owner Command Center (Spec 22 §4.1).
// Step 1 ships a placeholder page: ranked alert inbox only. Money strip,
// fleet strip, live map and context panel arrive in Steps 2-3.
type ConsoleHandlers struct {
	app  *App
	repo repository.AlertRepository
}

func NewConsoleHandlers(app *App, repo repository.AlertRepository) *ConsoleHandlers {
	return &ConsoleHandlers{app: app, repo: repo}
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

	h.app.renderPage(w, r, "console.html", PageData{
		Title: "Command Center",
		User:  user,
		Extra: map[string]interface{}{
			"InboxAlerts": inbox,
		},
	})
}
