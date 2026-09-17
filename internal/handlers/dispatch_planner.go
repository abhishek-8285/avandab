package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	dispatchapp "transport-app/internal/dispatch/application"
	dispatchdomain "transport-app/internal/dispatch/domain"
	"transport-app/internal/httpx"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// DispatchHandlers — multi-route dispatch planner board (Spec:
// docs/design/dispatcher-route-planner/03-cto-architecture.md §4 API surface).
type DispatchHandlers struct {
	*App
	Planner *dispatchapp.PlannerService
	Tuner   *dispatchapp.TunerService
}

func (h *DispatchHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "read")).
		Get("/", h.ListRuns)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "create")).
		Get("/new", h.NewRun)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "create")).
		Post("/runs", h.CreateRun)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "read")).
		Get("/runs/{runID}", h.ViewRun)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "update")).
		Post("/runs/{runID}/plan", h.PlanRun)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "update")).
		Post("/runs/{runID}/tune", h.TuneRun)
	r.With(middleware.ResourcePermission(h.AuthSrv, "dispatch", "update")).
		Patch("/runs/{runID}", h.TuneRun)
}

// rootMsg returns the outermost message of err's chain for client-facing
// JSON: domain errors (sentinel at chain head) read cleanly, while wrapped
// driver/database noise is replaced with a fixed string.
func rootMsg(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 {
		msg = msg[:i]
	}
	if errors.Is(err, dispatchdomain.ErrBookingUnknown) ||
		errors.Is(err, dispatchdomain.ErrInvalidStopType) ||
		errors.Is(err, dispatchdomain.ErrNoStops) ||
		errors.Is(err, dispatchdomain.ErrNoVehicles) ||
		errors.Is(err, dispatchdomain.ErrInvalidOp) ||
		errors.Is(err, dispatchdomain.ErrInvalidSeq) {
		return msg
	}
	return "request could not be processed"
}

// ListRuns — GET /dispatch and GET /api/v1/dispatch/runs.
func (h *DispatchHandlers) ListRuns(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	runs, err := h.Planner.ListRuns(r.Context(), tenantID, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
		return
	}
	if isDatastarRequest(r) || strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "dispatch_runs.html", PageData{
		Title: "Dispatch Planner",
		User:  session,
		Extra: map[string]interface{}{"Runs": runs},
	})
}

// NewRun renders the create-run form.
func (h *DispatchHandlers) NewRun(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "dispatch_run_new.html", PageData{
		Title: "New Planner Run",
		User:  session,
	})
}

// CreateRun — POST /dispatch/runs (form or JSON).
func (h *DispatchHandlers) CreateRun(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	user, _ := h.getUserFromContext(r)
	actorID := ""
	if user != nil {
		actorID = user.UserID
	}

	cmd := dispatchapp.CreateRunCommand{TenantID: tenantID, ActorID: actorID}
	if strings.HasPrefix(r.URL.Path, "/api/") || r.Header.Get("Content-Type") == "application/json" {
		var req struct {
			Source string                         `json:"source"`
			Stops  []dispatchapp.PlannerStopInput `json:"stops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		cmd.Source, cmd.Stops = req.Source, req.Stops
	} else {
		cmd.Source = r.PostFormValue("source")
		var stops []dispatchapp.PlannerStopInput
		raw := r.PostFormValue("stops")
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &stops); err != nil {
				httpx.Error(w, r, apperr.New(apperr.CodeValidation).WithCause(err))
				return
			}
		}
		var bookings []string
		rawB := r.PostFormValue("booking_ids")
		for _, b := range strings.FieldsFunc(rawB, func(c rune) bool { return c == ',' || c == '\n' || c == ' ' }) {
			if b = strings.TrimSpace(b); b != "" {
				bookings = append(bookings, b)
			}
		}
		for _, b := range bookings {
			stops = append(stops, dispatchapp.PlannerStopInput{BookingID: b})
		}
		cmd.Stops = stops
	}

	runID, err := h.Planner.CreateRun(r.Context(), cmd)
	if err != nil {
		// Only the domain-validation message goes to the client; the wrap
		// chain (SQL detail) stays in the server log.
		slog.Warn("dispatch create run rejected", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": rootMsg(err)})
		return
	}
	writeAuditLog(r, h.DB, "dispatch.run_create", "planner_runs", runID, map[string]any{
		"source": cmd.Source, "stops": len(cmd.Stops),
	})
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusCreated, map[string]string{"run_id": runID})
		return
	}
	http.Redirect(w, r, "/dispatch/runs/"+runID, http.StatusSeeOther)
}

// ViewRun — GET /dispatch/runs/{runID}.
func (h *DispatchHandlers) ViewRun(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	runID := chi.URLParam(r, "runID")
	detail, err := h.Planner.GetRun(r.Context(), tenantID, runID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusOK, detail)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "dispatch_run_detail.html", PageData{
		Title: "Planner Run " + runID,
		User:  session,
		Extra: map[string]interface{}{"Run": detail},
	})
}

// TuneRun — POST /dispatch/runs/{runID}/tune (web form) and
// PATCH /api/v1/dispatch/runs/{runID} (API). Body/form carries one TuneOp.
func (h *DispatchHandlers) TuneRun(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	runID := chi.URLParam(r, "runID")

	var op dispatchapp.TuneOp
	if r.Method == http.MethodPatch || strings.HasPrefix(r.URL.Path, "/api/") {
		if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
	} else {
		op = dispatchapp.TuneOp{
			Op:        r.PostFormValue("op"),
			StopID:    r.PostFormValue("stop_id"),
			RouteID:   r.PostFormValue("route_id"),
			ToRouteID: r.PostFormValue("to_route_id"),
		}
		op.Seq, _ = strconv.Atoi(r.PostFormValue("seq"))
		op.SplitPart, _ = strconv.Atoi(r.PostFormValue("split_part"))
	}

	kpi, err := h.Tuner.Apply(r.Context(), tenantID, runID, op)
	if err != nil {
		slog.Warn("dispatch tune rejected", "run_id", runID, "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": rootMsg(err)})
		return
	}
	writeAuditLog(r, h.DB, "dispatch.run_tune", "planner_runs", runID, map[string]any{
		"op": op.Op, "stop_id": op.StopID, "route_id": op.RouteID,
	})
	if r.Method == http.MethodPatch || strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID, "kpi": kpi})
		return
	}
	target := safeRedirect(r, "/dispatch/runs/"+runID, "/dispatch")
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}

// PlanRun — POST /dispatch/runs/{runID}/plan (solve + persist).
func (h *DispatchHandlers) PlanRun(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	runID := chi.URLParam(r, "runID")
	kpi, err := h.Planner.Plan(r.Context(), dispatchapp.PlanCommand{TenantID: tenantID, RunID: runID})
	if err != nil {
		slog.Warn("dispatch plan rejected", "run_id", runID, "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": rootMsg(err)})
		return
	}
	writeAuditLog(r, h.DB, "dispatch.run_plan", "planner_runs", runID, map[string]any{
		"total_km": kpi.TotalKM, "unassigned": kpi.UnassignedCount,
	})
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID, "kpi": kpi})
		return
	}
	target := safeRedirect(r, "/dispatch/runs/"+runID, "/dispatch")
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}
