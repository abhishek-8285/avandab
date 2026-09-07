package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// ExperimentHandlers exposes the A/B experiment API (Spec 16 §5) plus the
// feature flag evaluation endpoint other services query.
type ExperimentHandlers struct {
	App     *App
	Service *service.ExperimentsService
	authSrv auth.AuthorizationService
}

// NewExperimentHandlers constructs an ExperimentHandlers instance.
func NewExperimentHandlers(app *App, svc *service.ExperimentsService, authSrv auth.AuthorizationService) *ExperimentHandlers {
	return &ExperimentHandlers{App: app, Service: svc, authSrv: authSrv}
}

// RegisterRoutes mounts the experiment endpoints under /api/v1/experiments.
func (h *ExperimentHandlers) RegisterRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Get("/api/v1/experiments", h.List)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Get("/api/v1/experiments/{id}", h.Get)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments", h.Create)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments/{id}/start", h.Start)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments/{id}/pause", h.Pause)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments/{id}/resume", h.Resume)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments/{id}/complete", h.Complete)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "write")).
		Post("/api/v1/experiments/{id}/archive", h.Archive)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Get("/api/v1/experiments/evaluate", h.Evaluate)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Get("/api/v1/experiments/{id}/assignments", h.ListAssignments)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Post("/api/v1/experiments/{id}/metrics", h.RecordMetric)
	r.With(middleware.RequirePermission(h.authSrv, "experiments", "read")).
		Get("/api/v1/experiments/{id}/results", h.Results)
}

// tenantID resolves the acting org. Fail closed: experiment routes sit behind
// auth middleware which always sets tenant (panics surface as 500 via Recoverer).
func (h *ExperimentHandlers) tenantID(r *http.Request) string {
	return string(shared.MustTenantID(r.Context()))
}

// Create handles POST /api/v1/experiments
func (h *ExperimentHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		VariantA     string  `json:"variant_a"`
		VariantB     string  `json:"variant_b"`
		TrafficSplit float64 `json:"traffic_split"`
		StartDate    *string `json:"start_date"`
		EndDate      *string `json:"end_date"`
		MetricName   string  `json:"metric_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}

	var startDate, endDate *time.Time
	if req.StartDate != nil && *req.StartDate != "" {
		t, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			http.Error(w, `{"error":"invalid start_date, expected YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
		startDate = &t
	}
	if req.EndDate != nil && *req.EndDate != "" {
		t, err := time.Parse("2006-01-02", *req.EndDate)
		if err != nil {
			http.Error(w, `{"error":"invalid end_date, expected YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
		endDate = &t
	}

	userID := "system"
	if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
		userID = s.UserID
	}

	id, err := h.Service.CreateExperiment(r.Context(), service.Experiment{
		TenantID:     h.tenantID(r),
		Name:         req.Name,
		Description:  req.Description,
		VariantA:     req.VariantA,
		VariantB:     req.VariantB,
		TrafficSplit: req.TrafficSplit,
		StartDate:    startDate,
		EndDate:      endDate,
		MetricName:   req.MetricName,
		CreatedBy:    userID,
	})
	if err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, "'")+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"experiment_id": id, "status": service.ExperimentStatusDraft})
}

// Get handles GET /api/v1/experiments/{id}
func (h *ExperimentHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"experiment id required"}`, http.StatusBadRequest)
		return
	}
	exp, err := h.Service.GetExperiment(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"experiment not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(exp)
}

// List handles GET /api/v1/experiments?status=running
func (h *ExperimentHandlers) List(w http.ResponseWriter, r *http.Request) {
	exps, err := h.Service.ListExperiments(r.Context(), h.tenantID(r), r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, `{"error":"failed to list experiments"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"experiments": exps, "total": len(exps)})
}

// Start handles POST /api/v1/experiments/{id}/start
func (h *ExperimentHandlers) Start(w http.ResponseWriter, r *http.Request) {
	h.lifecycleTransition(w, r, h.Service.StartExperiment)
}

// Pause handles POST /api/v1/experiments/{id}/pause
func (h *ExperimentHandlers) Pause(w http.ResponseWriter, r *http.Request) {
	h.lifecycleTransition(w, r, h.Service.PauseExperiment)
}

// Resume handles POST /api/v1/experiments/{id}/resume
func (h *ExperimentHandlers) Resume(w http.ResponseWriter, r *http.Request) {
	h.lifecycleTransition(w, r, h.Service.ResumeExperiment)
}

// Complete handles POST /api/v1/experiments/{id}/complete
func (h *ExperimentHandlers) Complete(w http.ResponseWriter, r *http.Request) {
	h.lifecycleTransition(w, r, h.Service.CompleteExperiment)
}

// Archive handles POST /api/v1/experiments/{id}/archive
func (h *ExperimentHandlers) Archive(w http.ResponseWriter, r *http.Request) {
	h.lifecycleTransition(w, r, h.Service.ArchiveExperiment)
}

func (h *ExperimentHandlers) lifecycleTransition(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id string) error) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"experiment id required"}`, http.StatusBadRequest)
		return
	}
	if err := fn(r.Context(), id); err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, "'")+`"}`, http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"experiment_id": id, "ok": true})
}

// Evaluate handles GET /api/v1/experiments/evaluate?name=X&subject_type=user&subject_id=Y
func (h *ExperimentHandlers) Evaluate(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("name")
	subjectType := q.Get("subject_type")
	subjectID := q.Get("subject_id")
	if name == "" || subjectType == "" || subjectID == "" {
		http.Error(w, `{"error":"name, subject_type and subject_id are required"}`, http.StatusBadRequest)
		return
	}
	if !isValidSubjectType(subjectType) {
		http.Error(w, `{"error":"invalid subject_type"}`, http.StatusBadRequest)
		return
	}
	variant := h.Service.EvaluateFeatureFlag(r.Context(), h.tenantID(r), name, subjectType, subjectID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"experiment":   name,
		"subject_type": subjectType,
		"subject_id":   subjectID,
		"variant":      variant,
		"in_treatment": variant == service.VariantB,
	})
}

func isValidSubjectType(t string) bool {
	switch t {
	case service.SubjectTypeUser, service.SubjectTypeDriver, service.SubjectTypeVehicle, service.SubjectTypeCustomer:
		return true
	}
	return false
}

// ListAssignments handles GET /api/v1/experiments/{id}/assignments
func (h *ExperimentHandlers) ListAssignments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"experiment id required"}`, http.StatusBadRequest)
		return
	}
	assignments, err := h.Service.ListAssignments(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"failed to list assignments"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"assignments": assignments, "total": len(assignments)})
}

// RecordMetric handles POST /api/v1/experiments/{id}/metrics
func (h *ExperimentHandlers) RecordMetric(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"experiment id required"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		SubjectType string  `json:"subject_type"`
		SubjectID   string  `json:"subject_id"`
		MetricValue float64 `json:"metric_value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if !isValidSubjectType(req.SubjectType) || req.SubjectID == "" {
		http.Error(w, `{"error":"subject_type and subject_id are required"}`, http.StatusBadRequest)
		return
	}
	if err := h.Service.RecordMetric(r.Context(), h.tenantID(r), id, req.SubjectType, req.SubjectID, req.MetricValue); err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, "'")+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"experiment_id": id, "ok": true})
}

// Results handles GET /api/v1/experiments/{id}/results
func (h *ExperimentHandlers) Results(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"experiment id required"}`, http.StatusBadRequest)
		return
	}
	results, err := h.Service.GetExperimentResults(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error":"failed to compute results"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}
