package agent

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/agent/rl"
	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// ApprovalHandler exposes the admin approval queue over HTTP.
type ApprovalHandler struct {
	approval *ApprovalService
}

func NewApprovalHandler(approval *ApprovalService) *ApprovalHandler {
	return &ApprovalHandler{approval: approval}
}

// RegisterRoutes mounts queue endpoints (admin-only).
func (h *ApprovalHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/agent/actions", h.list)
	r.Post("/api/agent/actions/{id}/approve", h.approve)
	r.Post("/api/agent/actions/{id}/reject", h.reject)
}

// requireAdmin rejects any authenticated user who is not a tenant admin.
// The approval gate executes mutating tools; only global and tenant admins
// may decide. org_admin is the role self-serve tenants receive, so excluding
// it leaves their submitted mutations impossible to approve.
func requireAdmin(w http.ResponseWriter, r *http.Request) *auth.SessionData {
	session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return nil
	}
	if session.Role != string(domain.RoleAdmin) && session.Role != string(domain.RoleOrgAdmin) {
		writeErr(w, http.StatusForbidden, "admin role required")
		return nil
	}
	return session
}

func (h *ApprovalHandler) list(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	status := r.URL.Query().Get("status")
	actions, err := h.approval.rl.ListActions(rl.ActionStatus(status), 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list actions")
		return
	}
	// Approval actions are multi-tenant: never show another tenant's queue.
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	scoped := make([]rl.Action, 0, len(actions))
	for _, action := range actions {
		if action.TenantID == tenantID {
			scoped = append(scoped, action)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"actions": scoped})
}

func (h *ApprovalHandler) approve(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	userID, name := identityFrom(r)
	action, err := h.approval.Approve(r.Context(), chi.URLParam(r, "id"), userID, name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"action": action})
}

func (h *ApprovalHandler) reject(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Reason == "" {
		writeErr(w, http.StatusBadRequest, "reason is required")
		return
	}
	_, name := identityFrom(r)
	action, err := h.approval.Reject(r.Context(), chi.URLParam(r, "id"), name, req.Reason)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"action": action})
}

// identityFrom extracts the acting user from the auth context.
func identityFrom(r *http.Request) (string, string) {
	session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil {
		return "", "Unknown"
	}
	name := session.Name
	if name == "" {
		name = "API user " + session.UserID
	}
	return session.UserID, name
}
