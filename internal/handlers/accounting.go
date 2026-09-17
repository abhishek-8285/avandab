package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/integration/accounting"
	"transport-app/internal/middleware"
)

// AccountingHandlers exposes REST APIs for external accounting sync and reconciliation.
type AccountingHandlers struct {
	*App
	consumer *accounting.Consumer
	authSrv  auth.AuthorizationService
}

// NewAccountingHandlers constructs a new AccountingHandlers instance.
func NewAccountingHandlers(app *App, consumer *accounting.Consumer, authSrv auth.AuthorizationService) *AccountingHandlers {
	return &AccountingHandlers{
		App:      app,
		consumer: consumer,
		authSrv:  authSrv,
	}
}

// Mount registers accounting sync endpoints.
func (h *AccountingHandlers) Mount(r chi.Router) {
	setupRoutes := func(sub chi.Router) {
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "read")).Get("/sync/status", h.GetStatus)
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "sync")).Post("/sync/trigger", h.TriggerSync)
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "sync")).Post("/contacts/sync", h.SyncContacts)
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "read")).Get("/reconcile", h.Reconcile)
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "read")).Get("/settings", h.GetSetting)
		sub.With(middleware.RequirePermission(h.authSrv, "accounting", "sync")).Post("/settings", h.SaveSetting)
	}

	r.Route("/api/accounting", setupRoutes)
	r.Route("/api/v1/accounting", setupRoutes)
}

// GetStatus returns adapter info, pending sync items, and last synced timestamp.
func (h *AccountingHandlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.consumer.GetStatus(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// TriggerSync flushes pending/failed outbox events to the accounting adapter.
func (h *AccountingHandlers) TriggerSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SinceMinutes int `json:"since_minutes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	res, err := h.consumer.TriggerSync(r.Context(), req.SinceMinutes)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// SyncContacts pushes customer and driver contacts to the external accounting system.
func (h *AccountingHandlers) SyncContacts(w http.ResponseWriter, r *http.Request) {
	res, err := h.consumer.SyncContacts(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"synced": res.Synced,
		"failed": res.Failed,
		"errors": res.Errors,
	})
}

// GetSetting returns the caller's tenant accounting choice (00161).
// Tenant-scoped: a tenant only ever sees its own row, never another org's.
func (h *AccountingHandlers) GetSetting(w http.ResponseWriter, r *http.Request) {
	s := accounting.GetSetting(r.Context(), h.DB)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"provider":  s.Provider,
		"endpoint":  s.Endpoint,
		"live_push": s.LivePush,
		"note":      "Live push only for tally. zoho/busy_excel/excel use CSV import.",
	})
}

// SaveSetting stores the caller's tenant accounting choice (00161).
func (h *AccountingHandlers) SaveSetting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	s, err := accounting.SaveSetting(r.Context(), h.DB, req.Provider, req.Endpoint)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "tenant not set") {
			status = http.StatusUnauthorized
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

// Reconcile provides a comparison of local sync logs vs external accounting status.
func (h *AccountingHandlers) Reconcile(w http.ResponseWriter, r *http.Request) {
	res, err := h.consumer.Reconcile(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
