package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/repository"
)

// AuditLogHandlers handles audit log views.
type AuditLogHandlers struct {
	*App
}

func (h *AuditLogHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "audit_logs", "read")).Get("/", h.List)
	// Mark-all-read is a personal read-state action — any authenticated
	// user can record when they last read their notification feed.
	r.Post("/read-all", h.MarkAllRead)
}

func (h *AuditLogHandlers) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if ok && session != nil {
		uid := domain.UserID(session.UserID)
		_ = h.Services.Audit.LogAction(r.Context(), &uid, "mark_notifications_read", "notifications", string(uid), nil, nil)
	}
	// Set a cookie with the read timestamp so the next page render can
	// count only entries created after this moment as unread.
	http.SetCookie(w, &http.Cookie{
		Name:     "notif_read_at",
		Value:    time.Now().UTC().Format(time.RFC3339),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30, // 30 days
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (h *AuditLogHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	logs, total, err := h.Services.Audit.ListAuditLogsFiltered(r.Context(), pp.Query, pp.DateFrom, pp.DateTo, pp.Limit, pp.Offset)
	if err != nil {
		logs = []repository.AuditLogWithUser{}
		total = 0
	}

	pd := newPaginationData(pp, total, "/audit-logs")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	extra := map[string]interface{}{
		"AuditLogs":    logs,
		"Pagination":   pd,
		"Query":        pp.Query,
		"StatusFilter": pp.Status,
		"DateFrom":     pp.DateFrom,
		"DateTo":       pp.DateTo,
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "audit_logs_list_table.html", extra)
		return
	}

	h.renderPage(w, r, "audit_logs_list.html", PageData{
		Title: "Audit Logs",
		User:  session,
		Extra: extra,
	})
}
