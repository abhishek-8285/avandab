package handlers

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/agent"
	"transport-app/internal/domain"
	"transport-app/internal/middleware"
)

// AgentAdminHandlers renders the AI agent approval queue and learning stats.
type AgentAdminHandlers struct {
	*App
	Approval *agent.ApprovalService
}

// NewAgentAdminHandlers wires the approval queue page.
func NewAgentAdminHandlers(app *App, approval *agent.ApprovalService) *AgentAdminHandlers {
	return &AgentAdminHandlers{App: app, Approval: approval}
}

func (h *AgentAdminHandlers) Routes(r chi.Router) {
	r.With(middleware.RoleRequired(domain.DefaultRoleID(domain.RoleAdmin))).Get("/", h.Queue)
	r.With(middleware.RoleRequired(domain.DefaultRoleID(domain.RoleAdmin))).Post("/{id}/approve", h.Approve)
	r.With(middleware.RoleRequired(domain.DefaultRoleID(domain.RoleAdmin))).Post("/{id}/reject", h.Reject)
}

func (h *AgentAdminHandlers) Queue(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.Role != "admin" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	pending, err := h.Approval.ListPending()
	if err != nil {
		pending = nil
	}

	h.renderPage(w, r, "agent_actions.html", PageData{
		Title: "Agent Approval Queue",
		User:  session,
		Extra: map[string]any{"Actions": pending},
	})
}

func (h *AgentAdminHandlers) Approve(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if _, err := h.Approval.Approve(r.Context(), chi.URLParam(r, "id"), session.UserID, session.Name); err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<div class="px-4 py-2 text-xs font-semibold text-status-alert bg-status-alert/10 rounded">` + template.HTMLEscapeString(err.Error()) + `</div>`))
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isDatastarRequest(r) {
		w.Header().Set("HX-Trigger", `{"showToast": {"tone":"success","msg":"Action approved"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="px-4 py-3 text-xs font-semibold text-status-success bg-status-success/10 rounded border border-status-success/20">✓ Approved — pending list will refresh</div>`))
		return
	}
	http.Redirect(w, r, "/agent-actions", http.StatusSeeOther)
}

func (h *AgentAdminHandlers) Reject(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	reason := r.FormValue("reason")
	if reason == "" {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<div class="px-4 py-2 text-xs font-semibold text-status-alert bg-status-alert/10 rounded">reason is required</div>`))
			return
		}
		http.Error(w, "reason is required", http.StatusBadRequest)
		return
	}
	if _, err := h.Approval.Reject(r.Context(), chi.URLParam(r, "id"), session.Name, reason); err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<div class="px-4 py-2 text-xs font-semibold text-status-alert bg-status-alert/10 rounded">` + template.HTMLEscapeString(err.Error()) + `</div>`))
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isDatastarRequest(r) {
		w.Header().Set("HX-Trigger", `{"showToast": {"tone":"success","msg":"Action rejected"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="px-4 py-3 text-xs font-semibold text-status-success bg-status-success/10 rounded border border-status-success/20">Rejected</div>`))
		return
	}
	http.Redirect(w, r, "/agent-actions", http.StatusSeeOther)
}
