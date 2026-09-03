package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"transport-app/internal/operations/notifications"
)

// EmailPoolHandler exposes dynamic provider switching + quota dashboards.
type EmailPoolHandler struct {
	App  *App
	Pool *notifications.EmailPool
}

func NewEmailPoolHandler(app *App, pool *notifications.EmailPool) *EmailPoolHandler {
	return &EmailPoolHandler{App: app, Pool: pool}
}

func (h *EmailPoolHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/admin/email/providers", h.ListProviders)
	r.Get("/api/admin/email/usage", h.GetUsage)
	r.Get("/api/admin/email/providers/{name}", h.GetProvider)
	r.Put("/api/admin/email/providers/{name}", h.UpdateProvider)
	r.Post("/api/admin/email/providers/{name}/switch-primary", h.SwitchPrimary)
	r.Post("/api/admin/email/providers/{name}/reset-usage", h.ResetUsage)
}

// ListProviders returns all providers with specs.
func (h *EmailPoolHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	specs := h.Pool.ListProviders()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"providers": specs})
}

// GetProvider returns single provider spec.
func (h *EmailPoolHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	for _, s := range h.Pool.ListProviders() {
		if s.Name == name {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s)
			return
		}
	}
	http.Error(w, `{"error":"provider not found"}`, http.StatusNotFound)
}

// GetUsage returns quota snapshots — daily/monthly used, remaining, exhausted.
func (h *EmailPoolHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	usage := h.Pool.GetUsage()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"strategy":  h.Pool.Strategy(),
		"providers": usage,
	})
}

type updateProviderRequest struct {
	Enabled  *bool `json:"enabled"`
	Priority *int  `json:"priority"`
}

// UpdateProvider dynamically toggles enabled or reprioritizes a provider.
func (h *EmailPoolHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	var req updateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Enabled != nil {
		if err := h.Pool.SetProviderEnabled(name, *req.Enabled); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}
	}
	if req.Priority != nil {
		if err := h.Pool.SetProviderPriority(name, *req.Priority); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "updated", "provider": name})
}

func (h *EmailPoolHandler) SwitchPrimary(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.Pool.SetPrimary(name); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "switched", "primary": name})
}

func (h *EmailPoolHandler) ResetUsage(w http.ResponseWriter, r *http.Request) {
	if h.Pool == nil {
		http.Error(w, `{"error":"email pool not configured"}`, http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.Pool.ResetUsage(name); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "reset", "provider": name})
}
