package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	ctapp "transport-app/internal/controltower/application"
	"transport-app/internal/controltower/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// ControlTowerHandlers serves web cockpit UI for the Dispatcher Control Tower.
type ControlTowerHandlers struct {
	*App
	service *ctapp.Service
}

// NewControlTowerHandlers constructs a new ControlTowerHandlers.
func NewControlTowerHandlers(app *App, service *ctapp.Service) *ControlTowerHandlers {
	return &ControlTowerHandlers{
		App:     app,
		service: service,
	}
}

// Routes registers web UI routes for the control tower under /control-tower.
func (h *ControlTowerHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "trips", "read")).Get("/", h.WebControlTowerPage)
}

// WebControlTowerPage renders the operations cockpit / control tower dashboard.
func (h *ControlTowerHandlers) WebControlTowerPage(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	user, _ := h.getUserFromContext(r)
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	var trips []domain.ControlTowerTrip
	if h.service != nil {
		res, err := h.service.GetTrips(r.Context(), tenantID, statusFilter)
		if err != nil {
			h.renderError(w, http.StatusInternalServerError, "Failed to load control tower data", err.Error(), user)
			return
		}
		trips = res
	}

	var activeCount, delayedCount, sosCount, staleCount int
	for _, t := range trips {
		st := strings.ToUpper(t.Status)
		if st == "STARTED" || st == "IN_TRANSIT" || st == "ASSIGNED" || st == "SCHEDULED" {
			activeCount++
		}
		if t.Safety.HasActiveSOS {
			sosCount++
		}
		if t.SyncState.IsStale {
			staleCount++
		}
		if t.Safety.IsDeviated || t.Safety.ActiveAlertsCount > 0 {
			delayedCount++
		}
	}

	data := PageData{
		Title: "Control Tower",
		User:  user,
		Extra: map[string]interface{}{
			"Trips":        trips,
			"ActiveCount":  activeCount,
			"DelayedCount": delayedCount,
			"SOSCount":     sosCount,
			"StaleCount":   staleCount,
			"StatusFilter": statusFilter,
		},
	}
	h.renderPage(w, r, "control_tower.html", data)
}
