package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	"transport-app/internal/httpx"
	opserrors "transport-app/internal/operations/errors"
	"transport-app/internal/shared"
)

type OpsErrorsHandler struct {
	*App
	Reporter *opserrors.Reporter
}

func NewOpsErrorsHandler(app *App, reporter *opserrors.Reporter) *OpsErrorsHandler {
	return &OpsErrorsHandler{App: app, Reporter: reporter}
}

func (h *OpsErrorsHandler) errorFilter(r *http.Request, tenantID string) opserrors.ErrorFilter {
	q := r.URL.Query()
	f := opserrors.ErrorFilter{
		TenantID:    tenantID,
		Severity:    q.Get("severity"),
		Fingerprint: q.Get("fingerprint"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			f.Limit = n
		}
	}
	if f.Limit == 0 {
		f.Limit = 100
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			f.Offset = n
		}
	}
	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			f.From = t
		} else if d, err := time.Parse("2006-01-02", from); err == nil {
			f.From = d
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			f.To = t
		} else if d, err := time.Parse("2006-01-02", to); err == nil {
			f.To = d.Add(24*time.Hour - time.Second)
		}
	}
	return f
}

func (h *OpsErrorsHandler) APIClientReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var body struct {
		Message     string   `json:"message"`
		Stack       string   `json:"stack"`
		Path        string   `json:"path"`
		UserAgent   string   `json:"user_agent"`
		Viewport    string   `json:"viewport"`
		RequestID   string   `json:"request_id"`
		Breadcrumbs []string `json:"breadcrumbs"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeMalformedJSON).WithCause(err))
		return
	}

	userID := ""
	if s, ok := h.getUserFromContext(r); ok && s != nil {
		userID = s.UserID
	}

	message := strings.TrimSpace(body.Message)
	if message == "" {
		message = "unknown client error"
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	stack := body.Stack
	if len(stack) > 8192 {
		stack = stack[:8192]
	}
	crumbs := strings.Join(body.Breadcrumbs, "\n")
	if len(crumbs) > 4096 {
		crumbs = crumbs[len(crumbs)-4096:]
	}
	path := body.Path
	if len(path) > 512 {
		path = path[:512]
	}

	report, err := h.Reporter.Report(r.Context(), opserrors.ErrorReport{
		RequestID:  body.RequestID,
		UserID:     userID,
		TenantID:   tenantID,
		URL:        path,
		Method:     "CLIENT",
		Message:    message,
		StackTrace: stack,
		Severity:   opserrors.SeverityMedium,
		UserAgent:  body.UserAgent,
		Metadata: map[string]interface{}{
			"viewport":    body.Viewport,
			"breadcrumbs": crumbs,
			"source":      "client",
		},
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "client error report failed to persist",
			slog.String("request_id", body.RequestID),
			slog.Any("error", err),
		)
	}
	httpx.JSON(w, http.StatusOK, map[string]string{
		"status":         "received",
		"error_id":       report.ID,
		"correlation_id": report.RequestID,
	})
}

// tenantID resolves the acting org. Fail closed: ops-error routes sit behind
// auth middleware which always sets tenant (panics surface as 500 via Recoverer).
func (h *OpsErrorsHandler) tenantID(r *http.Request) string {
	return string(shared.MustTenantID(r.Context()))
}

func (h *OpsErrorsHandler) Page(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	tenantID := h.tenantID(r)

	pp := parsePaginationParams(r)
	filter := h.errorFilter(r, tenantID)
	filter.Limit = pp.Limit
	filter.Offset = pp.Offset

	errs, err := h.Reporter.ListErrors(r.Context(), filter)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	total, _ := h.Reporter.CountErrors(r.Context(), filter)
	incidents, err := h.Reporter.ListIncidents(r.Context(), opserrors.IncidentFilter{
		TenantID: tenantID,
		Limit:    100,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	openIncidents := 0
	for _, inc := range incidents {
		if inc.Status == "OPEN" || inc.Status == "ASSIGNED" {
			openIncidents++
		}
	}

	q := r.URL.Query()
	h.renderPage(w, r, "errors.html", PageData{
		Title: "Error Reports",
		User:  session,
		Extra: map[string]interface{}{
			"Errors":        errs,
			"Total":         total,
			"Incidents":     incidents,
			"OpenIncidents": openIncidents,
			"Severity":      q.Get("severity"),
			"Fingerprint":   q.Get("fingerprint"),
			"From":          q.Get("from"),
			"To":            q.Get("to"),
			"Pagination":    newPaginationData(pp, int64(total), "/ops/errors"),
		},
	})
}

// APIGetError returns one deduplicated error group by fingerprint — powers
// the /ops/errors detail drawer (Spec 16 §4).
func (h *OpsErrorsHandler) APIGetError(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	fp := chi.URLParam(r, "fingerprint")

	report, err := h.Reporter.GetError(r.Context(), fp, tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
			return
		}
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, report)
}

func (h *OpsErrorsHandler) APIList(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	errs, err := h.Reporter.ListErrors(r.Context(), h.errorFilter(r, tenantID))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	total, err := h.Reporter.CountErrors(r.Context(), h.errorFilter(r, tenantID))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	httpx.JSON(w, http.StatusOK, map[string]interface{}{
		"errors": errs,
		"total":  total,
		"limit":  q.Get("limit"),
		"offset": q.Get("offset"),
	})
}

func (h *OpsErrorsHandler) APIListIncidents(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	status := r.URL.Query().Get("status")
	incs, err := h.Reporter.ListIncidents(r.Context(), opserrors.IncidentFilter{
		TenantID: tenantID,
		Status:   status,
		Limit:    100,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"incidents": incs})
}

func (h *OpsErrorsHandler) APIResolveIncident(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "incidentID")

	var body struct {
		Status     string `json:"status"`
		AssignedTo string `json:"assigned_to"`
		RootCause  string `json:"root_cause"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeMalformedJSON).WithCause(err))
		return
	}
	switch body.Status {
	case "RESOLVED", "ASSIGNED", "OPEN":
	default:
		httpx.Error(w, r, apperr.New(apperr.CodeValidation).
			WithDetail("status must be one of OPEN, ASSIGNED, RESOLVED"))
		return
	}

	if err := h.Reporter.ResolveIncident(r.Context(), id, tenantID, body.Status, body.AssignedTo, body.RootCause); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
			return
		}
		httpx.Error(w, r, err)
		return
	}

	slog.InfoContext(r.Context(), "incident updated",
		slog.String("incident_id", id),
		slog.String("status", body.Status),
		slog.String("tenant_id", tenantID),
	)
	httpx.JSON(w, http.StatusOK, map[string]string{"status": body.Status})
}
