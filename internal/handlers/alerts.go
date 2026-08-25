package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
	"transport-app/internal/middleware"
)

// AlertHandlers manages alert viewing, acknowledgment, and resolution.
type AlertHandlers struct {
	App  *App
	repo repository.AlertRepository
}

// NewAlertHandlers creates a new AlertHandlers instance.
func NewAlertHandlers(app *App, repo repository.AlertRepository) *AlertHandlers {
	return &AlertHandlers{
		App:  app,
		repo: repo,
	}
}

// Routes mounts alert routes.
func (h *AlertHandlers) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/unread", h.UnreadBadgeFragment)
	// Ack/resolve mutate shared alert state — require update in addition
	// to the read gate wrapping this mount.
	r.With(middleware.ResourcePermission(h.App.AuthSrv, "alerts", "update")).Post("/{id}/ack", h.Ack)
	r.With(middleware.ResourcePermission(h.App.AuthSrv, "alerts", "update")).Post("/{id}/resolve", h.Resolve)
	r.With(middleware.ResourcePermission(h.App.AuthSrv, "alerts", "update")).Post("/mark-all-read", h.MarkAllRead)
}

// List renders the operational alerts management page.
func (h *AlertHandlers) List(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	if statusParam == "" {
		statusParam = domain.StatusOpen
	}
	// "all" is a display-level chip value; the repository treats "" as no filter.
	filterStatus := statusParam
	if filterStatus == "all" {
		filterStatus = ""
	}

	alertsList, err := h.repo.ListAlerts(r.Context(), filterStatus, 100, 0)
	user, _ := h.App.getUserFromContext(r)
	if err != nil {
		h.App.renderError(w, http.StatusInternalServerError, "Failed to load alerts", err.Error(), user)
		return
	}

	h.App.renderPage(w, r, "alerts_list.html", PageData{
		Title: "Operational Alerts",
		User:  user,
		Extra: map[string]interface{}{
			"Alerts":        alertsList,
			"CurrentStatus": statusParam,
		},
	})
}

// Ack acknowledges an open alert.
func (h *AlertHandlers) Ack(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "id")
	user, _ := h.App.getUserFromContext(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	if err := h.repo.Ack(r.Context(), alertID, userID); err != nil {
		h.App.renderError(w, http.StatusInternalServerError, "Failed to acknowledge alert", err.Error(), user)
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fmt.Sprintf(`<span id="alert-status-%s" class="px-2 py-1 text-xs font-semibold rounded-full bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300">acknowledged</span>`, alertID)))
		return
	}

	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

// Resolve marks an alert as resolved.
func (h *AlertHandlers) Resolve(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "id")
	user, _ := h.App.getUserFromContext(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	if err := h.repo.Resolve(r.Context(), alertID, userID); err != nil {
		h.App.renderError(w, http.StatusInternalServerError, "Failed to resolve alert", err.Error(), user)
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fmt.Sprintf(`<span id="alert-status-%s" class="px-2 py-1 text-xs font-semibold rounded-full bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300">resolved</span>`, alertID)))
		return
	}

	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

// MarkAllRead marks all open alerts as read for the user.
func (h *AlertHandlers) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	user, _ := h.App.getUserFromContext(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	_ = h.repo.MarkAllRead(r.Context(), userID)
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}

// UnreadBadgeFragment returns the HTML fragment for the notification badge.
func (h *AlertHandlers) UnreadBadgeFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	user, _ := h.App.getUserFromContext(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	unreadCount, _ := h.repo.UnreadCount(r.Context(), userID)
	if unreadCount > 99 {
		unreadCount = 99
	}

	var buf strings.Builder
	if unreadCount > 0 {
		buf.WriteString(fmt.Sprintf(`<span id="notif-badge" class="absolute -top-1 -right-1 inline-flex items-center justify-center px-1.5 py-0.5 text-xs font-bold leading-none text-white transform bg-red-600 rounded-full">%d</span>`, unreadCount))
	} else {
		buf.WriteString(`<span id="notif-badge" class="hidden"></span>`)
	}

	_, _ = w.Write([]byte(buf.String()))
}

// Ensure html/template import used
var _ = template.HTML("")
