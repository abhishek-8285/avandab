package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/httpx"
	privacyapp "transport-app/internal/privacy/application"
)

// ── Per-tenant operational purge (GDPR/offboarding) ─────────────────────
// Destructive: deletes every operational row of one tenant in FK-safe order
// inside a single transaction. The purge scope, protected-table guard,
// bootstrap-tenant guard, preview counts, dry-run/execute split and
// transaction boundary live in internal/privacy/application
// (handlers-end-state ADR Phase 2); this file keeps the platform-only route
// gate (tenants:manage in Routes), typed-confirm validation, HTTP
// status/message mapping and the audit write.

// PurgePreview returns dry-run counts for the confirm screen (admin only via route gate).
func (h *TenantsHandlers) PurgePreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "missing tenant id"})
		return
	}
	counts, err := privacyapp.NewTenantPurgeService(h.DB).Preview(r.Context(), id)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "tenant_id": id, "would_delete": counts})
}

// Purge executes the operational purge after typed confirmation.
func (h *TenantsHandlers) Purge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "invalid form"})
		return
	}
	if id == "" || r.PostFormValue("confirm") != id {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "type the tenant id to confirm purge"})
		return
	}
	deleted, err := privacyapp.NewTenantPurgeService(h.DB).Execute(r.Context(), id)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": err.Error()})
		return
	}
	h.auditTenant(r, "tenant.purge", id, map[string]string{"tables": fmt.Sprintf("%d", len(deleted))})
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "tenant_id": id, "deleted": deleted})
}
