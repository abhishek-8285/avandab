package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/route/optimizer"
	"transport-app/internal/shared"
)

// RouteHandlers handles route management.
type RouteHandlers struct {
	*App
}

func (h *RouteHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "delete")).Post("/{id}/delete", h.Delete)
	// Spec 18 Wave A — route optimization
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "read")).Get("/optimize", h.OptimizePage)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "create")).Post("/optimize/jobs", h.Optimize)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "read")).Get("/optimize/jobs", h.OptimizeJobs)
	r.With(middleware.ResourcePermission(h.AuthSrv, "routes", "read")).Get("/optimize/jobs/{jobID}", h.OptimizeJobStatus)
}

// OptimizePage renders the route optimization UI (Spec 18).
func (h *RouteHandlers) OptimizePage(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "route_optimize.html", PageData{
		Title: "Route Optimization",
		User:  session,
		Extra: map[string]interface{}{
			"RoutingProvider": h.Config.Routing.Provider,
		},
	})
}

// OptimizeRequest is the API payload for POST /api/v1/routes/optimize and POST /routes/optimize/jobs
type OptimizeRequest struct {
	Shipments   []optimizer.Shipment  `json:"shipments"`
	Vehicles    []optimizer.Vehicle   `json:"vehicles"`
	Constraints optimizer.Constraints `json:"constraints,omitempty"`
	Provider    string                `json:"provider,omitempty"` // override ROUTING_PROVIDER
}

// Optimize handles POST /routes/optimize/jobs (web) and POST /api/v1/routes/optimize (API).
// It creates a pending job, solves synchronously (mock/OSRM fallback is fast), and returns 202 with job_id.
func (h *RouteHandlers) Optimize(w http.ResponseWriter, r *http.Request) {
	var req OptimizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	in := optimizer.OptimizationInput{
		Shipments:   req.Shipments,
		Vehicles:    req.Vehicles,
		Constraints: req.Constraints,
	}
	if err := in.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(in.Shipments) > 50 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many shipments: max 50"})
		return
	}

	tenantID := string(shared.MustTenantID(r.Context()))
	user, _ := h.getUserFromContext(r)
	createdBy := ""
	if user != nil {
		createdBy = user.UserID
	}

	jobID := uuid.NewString()
	inputJSON, _ := json.Marshal(in)
	prov := req.Provider
	if prov == "" {
		prov = h.Config.Routing.Provider
	}
	if prov == "" {
		prov = "mock"
	}

	// Insert pending job
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO route_optimization_jobs (id, tenant_id, input_json, status, provider, created_by) VALUES ($1, $2, $3, 'pending', $4, $5)`,
		jobID, tenantID, string(inputJSON), prov, createdBy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create job"})
		return
	}

	// Solve synchronously — providers are fast (mock <10ms, OSRM <2s with fallback). Mark processing first.
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE route_optimization_jobs SET status='processing' WHERE id=$1`, jobID)

	opt := optimizer.Get(prov)
	// If OSRM self-host URL configured, prefer it
	if prov == "osrm-selfhost" && h.Config.Routing.OSRMURL != "" {
		opt = &optimizer.OSRMClient{BaseURL: h.Config.Routing.OSRMURL}
	}
	result, solveErr := opt.Solve(r.Context(), in)
	if solveErr != nil {
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE route_optimization_jobs SET status='failed', error_message=$1, completed_at=CURRENT_TIMESTAMP WHERE id=$2`,
			solveErr.Error(), jobID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": solveErr.Error(), "job_id": jobID})
		return
	}
	resJSON, _ := json.Marshal(result)
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE route_optimization_jobs SET status='completed', result_json=$1, completed_at=CURRENT_TIMESTAMP WHERE id=$2`,
		string(resJSON), jobID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":   jobID,
		"status":   "completed",
		"provider": result.ProviderName,
		"result":   result,
	})
}

// OptimizeJobs lists recent jobs for current tenant (web fragment + JSON).
func (h *RouteHandlers) OptimizeJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.MustTenantID(r.Context()))
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, status, provider, created_at, completed_at FROM route_optimization_jobs WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 20`, tenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()
	type jobRow struct {
		ID          string  `json:"id"`
		Status      string  `json:"status"`
		Provider    string  `json:"provider"`
		CreatedAt   string  `json:"created_at"`
		CompletedAt *string `json:"completed_at,omitempty"`
	}
	var jobs []jobRow
	for rows.Next() {
		var j jobRow
		var ca, comp sql.NullString
		_ = rows.Scan(&j.ID, &j.Status, &j.Provider, &ca, &comp)
		j.CreatedAt = ca.String
		if comp.Valid {
			j.CompletedAt = &comp.String
		}
		jobs = append(jobs, j)
	}
	if isDatastarRequest(r) {
		h.renderFragment(w, "route_optimize_jobs.html", map[string]interface{}{"Jobs": jobs})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"jobs": jobs})
}

// OptimizeJobStatus returns a single job (polling).
func (h *RouteHandlers) OptimizeJobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	tenantID := string(shared.MustTenantID(r.Context()))
	var id, status, provider, inputJSON, resultJSON sql.NullString
	var errMsg sql.NullString
	var createdAt, completedAt sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, status, provider, input_json, result_json, error_message, created_at, completed_at FROM route_optimization_jobs WHERE id=$1 AND tenant_id=$2`, jobID, tenantID).Scan(&id, &status, &provider, &inputJSON, &resultJSON, &errMsg, &createdAt, &completedAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	resp := map[string]interface{}{
		"id":         id.String,
		"status":     status.String,
		"provider":   provider.String,
		"created_at": createdAt.String,
	}
	if resultJSON.Valid && resultJSON.String != "" {
		var res optimizer.OptimizationOutput
		_ = json.Unmarshal([]byte(resultJSON.String), &res)
		resp["result"] = res
	}
	if errMsg.Valid {
		resp["error"] = errMsg.String
	}
	if completedAt.Valid {
		resp["completed_at"] = completedAt.String
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *RouteHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	list, total, err := h.Services.Routes.ListRoutes(r.Context(), pp.Query, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to list routes", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, total, "/routes")

	if isDatastarRequest(r) {
		h.renderFragment(w, "route_list_table.html", map[string]interface{}{
			"Routes":     list,
			"Pagination": pd,
			"Query":      pp.Query,
		})
		return
	}

	h.renderPage(w, r, "route_list.html", PageData{
		Title: "Routes",
		User:  session,
		Extra: map[string]interface{}{"Routes": list, "Pagination": pd, "Query": pp.Query},
	})
}

func (h *RouteHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "route_edit.html", PageData{Title: "New Route", User: session})
}

func (h *RouteHandlers) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req := h.parseRouteForm(r)

	_, err := h.Services.Routes.CreateRouteFull(r.Context(), req)
	if err != nil {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "route_edit.html", PageData{Title: "New Route", User: session, FlashError: err.Error()})
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Location", "/routes")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/routes", http.StatusSeeOther)
}

func (h *RouteHandlers) View(w http.ResponseWriter, r *http.Request) {
	id := domain.RouteID(chi.URLParam(r, "id"))
	route, err := h.Services.Routes.GetRoute(r.Context(), id)
	if err != nil {
		http.Error(w, "Route not found", http.StatusNotFound)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "route_view.html", PageData{Title: "View Route", User: session, Extra: map[string]interface{}{"Route": route}})
}

func (h *RouteHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	id := domain.RouteID(chi.URLParam(r, "id"))
	route, err := h.Services.Routes.GetRoute(r.Context(), id)
	if err != nil {
		http.Error(w, "Route not found", http.StatusNotFound)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "route_edit.html", PageData{Title: "Edit Route", User: session, Extra: map[string]interface{}{"Route": route}})
}

func (h *RouteHandlers) Update(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := domain.RouteID(chi.URLParam(r, "id"))
	req := h.parseRouteForm(r)

	_, err := h.Services.Routes.UpdateRouteFull(r.Context(), id, domain.UpdateRouteRequest{
		Source:              req.Source,
		Destination:         req.Destination,
		Distance:            req.Distance,
		EstimatedHours:      req.EstimatedHours,
		StandardFare:        req.StandardFare,
		ReverseDistance:     req.ReverseDistance,
		ReverseStandardFare: req.ReverseStandardFare,
		Direction:           req.Direction,
		IsActive:            true,
		Remarks:             req.Remarks,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/routes/"+id.String(), http.StatusSeeOther)
}

func (h *RouteHandlers) parseRouteForm(r *http.Request) domain.CreateRouteRequest {
	distance, _ := strconv.ParseFloat(r.PostFormValue("distance"), 64)
	estHours, _ := strconv.ParseFloat(r.PostFormValue("estimated_hours"), 64)
	fare, _ := strconv.ParseFloat(r.PostFormValue("standard_fare"), 64)

	var revDist, revFare *float64
	if v := r.PostFormValue("reverse_distance"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		revDist = &f
	}
	if v := r.PostFormValue("reverse_standard_fare"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		revFare = &f
	}

	direction := r.PostFormValue("direction")
	if direction == "" {
		direction = "oneway"
	}

	return domain.CreateRouteRequest{
		Source:              r.PostFormValue("source"),
		Destination:         r.PostFormValue("destination"),
		Distance:            distance,
		EstimatedHours:      estHours,
		StandardFare:        fare,
		ReverseDistance:     revDist,
		ReverseStandardFare: revFare,
		Direction:           direction,
		Remarks:             r.PostFormValue("remarks"),
	}
}

func (h *RouteHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.RouteID(chi.URLParam(r, "id"))
	if err := h.Services.Routes.DeleteRoute(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete route", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/routes", http.StatusSeeOther)
}
