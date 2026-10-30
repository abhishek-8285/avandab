package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/reportbuilder"
	"transport-app/internal/shared"
)

type ReportBuilderHandlers struct {
	*App
}

func (h *ReportBuilderHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/report-builder", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/datasets", h.APIGetDatasets)

		r.Route("/reports", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/", h.APIListReports)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Post("/", h.APICreateReport)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/{id}", h.APIGetReport)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Put("/{id}", h.APIUpdateReport)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Delete("/{id}", h.APIDeleteReport)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Post("/{id}/run", h.APIRunReport)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Post("/run", h.APIRunAdHoc)

		r.Route("/schedules", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/", h.APIListSchedules)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Post("/", h.APICreateSchedule)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/{id}", h.APIGetSchedule)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Put("/{id}", h.APIUpdateSchedule)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Delete("/{id}", h.APIDeleteSchedule)
			r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Post("/{id}/trigger", h.APITriggerSchedule)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/executions", h.APIListExecutions)
	})
}

func writeReportBuilderJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeReportBuilderError(w http.ResponseWriter, code int, msg string) {
	writeReportBuilderJSON(w, code, map[string]string{"error": msg})
}

func (h *ReportBuilderHandlers) service() *reportbuilder.Service {
	return reportbuilder.NewService(reportbuilder.NewRepository(h.DB))
}

func getReportUser(r *http.Request, defaultVal string) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil && sess.UserID != "" {
		return sess.UserID
	}
	return defaultVal
}

// Dataset Metadata

func (h *ReportBuilderHandlers) APIGetDatasets(w http.ResponseWriter, r *http.Request) {
	svc := h.service()
	catalog := svc.GetCatalog()
	writeReportBuilderJSON(w, http.StatusOK, catalog)
}

// Custom Reports Handlers

func (h *ReportBuilderHandlers) APIListReports(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	svc := h.service()

	var filter reportbuilder.ListReportsFilter
	q := r.URL.Query()

	if ds := q.Get("dataset"); ds != "" {
		datasetType := reportbuilder.DatasetType(strings.ToUpper(ds))
		filter.Dataset = &datasetType
	}
	if isSys := q.Get("is_system"); isSys != "" {
		val := isSys == "true" || isSys == "1"
		filter.IsSystem = &val
	}
	if search := q.Get("search"); search != "" {
		filter.Search = search
	}
	if limit := q.Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}

	reports, total, err := svc.ListReports(r.Context(), tenantID, filter)
	if err != nil {
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to query reports: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, map[string]interface{}{
		"reports": reports,
		"total":   total,
	})
}

func (h *ReportBuilderHandlers) APICreateReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	userID := getReportUser(r, "system")

	var in reportbuilder.CreateReportInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeReportBuilderError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	rep, err := svc.CreateReport(r.Context(), tenantID, userID, in)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrInvalidInput) || errors.Is(err, reportbuilder.ErrInvalidDataset) ||
			errors.Is(err, reportbuilder.ErrInvalidColumn) || errors.Is(err, reportbuilder.ErrInvalidAggregation) ||
			errors.Is(err, reportbuilder.ErrInvalidFilter) {
			writeReportBuilderError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to create report: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusCreated, rep)
}

func (h *ReportBuilderHandlers) APIGetReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	svc := h.service()

	rep, err := svc.GetReport(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "report not found")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to fetch report: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, rep)
}

func (h *ReportBuilderHandlers) APIUpdateReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in reportbuilder.UpdateReportInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeReportBuilderError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	rep, err := svc.UpdateReport(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "report not found")
			return
		}
		if errors.Is(err, reportbuilder.ErrSystemReportImmutable) {
			writeReportBuilderError(w, http.StatusForbidden, "built-in system reports cannot be modified")
			return
		}
		if errors.Is(err, reportbuilder.ErrInvalidInput) || errors.Is(err, reportbuilder.ErrInvalidColumn) ||
			errors.Is(err, reportbuilder.ErrInvalidAggregation) {
			writeReportBuilderError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to update report: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, rep)
}

func (h *ReportBuilderHandlers) APIDeleteReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	if err := svc.DeleteReport(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "report not found")
			return
		}
		if errors.Is(err, reportbuilder.ErrSystemReportImmutable) {
			writeReportBuilderError(w, http.StatusForbidden, "built-in system reports cannot be deleted")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to delete report: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Execution Handlers

func (h *ReportBuilderHandlers) APIRunReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in reportbuilder.RunReportInput
	// Decode JSON body if provided; ignore EOF if body is empty
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	// Check query params fallback
	if format := r.URL.Query().Get("format"); format != "" {
		in.Format = format
	} else if strings.Contains(r.Header.Get("Accept"), "text/csv") {
		in.Format = "CSV"
	}

	if limit := r.URL.Query().Get("limit"); limit != "" && in.Limit == 0 {
		if l, err := strconv.Atoi(limit); err == nil {
			in.Limit = l
		}
	}
	if offset := r.URL.Query().Get("offset"); offset != "" && in.Offset == 0 {
		if o, err := strconv.Atoi(offset); err == nil {
			in.Offset = o
		}
	}

	svc := h.service()
	res, err := svc.RunReport(r.Context(), tenantID, id, in, "MANUAL")
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "report not found")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to execute report: "+err.Error())
		return
	}

	if strings.ToUpper(res.Format) == "CSV" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"report-%s.csv\"", id))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.CSVData)
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, res)
}

type AdHocRunRequest struct {
	Report reportbuilder.CreateReportInput `json:"report"`
	Run    reportbuilder.RunReportInput    `json:"run"`
}

func (h *ReportBuilderHandlers) APIRunAdHoc(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var req AdHocRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeReportBuilderError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Fallback to name if missing in ad-hoc
	if req.Report.Name == "" {
		req.Report.Name = "Ad-hoc Report Preview"
	}

	svc := h.service()
	res, err := svc.RunAdHoc(r.Context(), tenantID, req.Report, req.Run)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrInvalidInput) || errors.Is(err, reportbuilder.ErrInvalidDataset) ||
			errors.Is(err, reportbuilder.ErrInvalidColumn) || errors.Is(err, reportbuilder.ErrInvalidAggregation) {
			writeReportBuilderError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to run ad-hoc report: "+err.Error())
		return
	}

	if strings.ToUpper(res.Format) == "CSV" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\"adhoc-report.csv\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.CSVData)
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, res)
}

// Schedules Handlers

func (h *ReportBuilderHandlers) APIListSchedules(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	svc := h.service()

	var filter reportbuilder.ListSchedulesFilter
	q := r.URL.Query()

	if repID := q.Get("report_id"); repID != "" {
		filter.ReportID = &repID
	}
	if active := q.Get("is_active"); active != "" {
		val := active == "true" || active == "1"
		filter.IsActive = &val
	}
	if limit := q.Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}

	schedules, total, err := svc.ListSchedules(r.Context(), tenantID, filter)
	if err != nil {
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to query schedules: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, map[string]interface{}{
		"schedules": schedules,
		"total":     total,
	})
}

func (h *ReportBuilderHandlers) APICreateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	userID := getReportUser(r, "system")

	var in reportbuilder.CreateScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeReportBuilderError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	sched, err := svc.CreateSchedule(r.Context(), tenantID, userID, in)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrInvalidInput) || errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to create schedule: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusCreated, sched)
}

func (h *ReportBuilderHandlers) APIGetSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	svc := h.service()

	sched, err := svc.GetSchedule(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "schedule not found")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to fetch schedule: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, sched)
}

func (h *ReportBuilderHandlers) APIUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in reportbuilder.UpdateScheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeReportBuilderError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	sched, err := svc.UpdateSchedule(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "schedule not found")
			return
		}
		if errors.Is(err, reportbuilder.ErrInvalidInput) {
			writeReportBuilderError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to update schedule: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, sched)
}

func (h *ReportBuilderHandlers) APIDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	if err := svc.DeleteSchedule(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "schedule not found")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to delete schedule: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *ReportBuilderHandlers) APITriggerSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	res, err := svc.TriggerSchedule(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, reportbuilder.ErrNotFound) {
			writeReportBuilderError(w, http.StatusNotFound, "schedule not found")
			return
		}
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to trigger schedule: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, res)
}

// Execution History Handlers

func (h *ReportBuilderHandlers) APIListExecutions(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	svc := h.service()

	var filter reportbuilder.ListExecutionsFilter
	q := r.URL.Query()

	if repID := q.Get("report_id"); repID != "" {
		filter.ReportID = &repID
	}
	if status := q.Get("status"); status != "" {
		filter.Status = &status
	}
	if limit := q.Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}

	execs, total, err := svc.ListExecutions(r.Context(), tenantID, filter)
	if err != nil {
		writeReportBuilderError(w, http.StatusInternalServerError, "failed to query executions: "+err.Error())
		return
	}

	writeReportBuilderJSON(w, http.StatusOK, map[string]interface{}{
		"executions": execs,
		"total":      total,
	})
}
