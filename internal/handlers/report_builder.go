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

// Routes mounts the Custom Report Builder web UI handlers at /report-builder.
func (h *ReportBuilderHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/", h.WebReportBuilderDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Post("/reports", h.WebCreateReport)
	r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Post("/reports/{id}/run", h.WebRunReport)
	r.With(middleware.RequirePermission(h.AuthSrv, "reports", "write")).Post("/schedules", h.WebCreateSchedule)
}

// WebReportBuilderDashboard renders custom report templates, scheduler, and execution history.
func (h *ReportBuilderHandlers) WebReportBuilderDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	svc := h.service()
	catalog := svc.GetCatalog()

	reports, _, _ := svc.ListReports(r.Context(), tenantID, reportbuilder.ListReportsFilter{
		Limit: 50,
	})

	schedules, _, _ := svc.ListSchedules(r.Context(), tenantID, reportbuilder.ListSchedulesFilter{
		Limit: 50,
	})

	executions, _, _ := svc.ListExecutions(r.Context(), tenantID, reportbuilder.ListExecutionsFilter{
		Limit: 25,
	})

	data := PageData{
		Title: "Custom Report Builder",
		User:  user,
		Extra: map[string]interface{}{
			"Catalog":    catalog,
			"Reports":    reports,
			"Schedules":  schedules,
			"Executions": executions,
		},
	}

	h.renderPage(w, r, "report_builder_dashboard.html", data)
}

// WebCreateReport handles creation of a new custom query template.
func (h *ReportBuilderHandlers) WebCreateReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderError(w, http.StatusBadRequest, "Report name is required", "Name cannot be empty", user)
		return
	}

	ds := reportbuilder.DatasetType(strings.ToUpper(strings.TrimSpace(r.FormValue("dataset"))))
	if ds == "" {
		ds = reportbuilder.DatasetTrips
	}

	catalog := reportbuilder.GetDatasetCatalog()
	meta, ok := catalog[ds]
	var cols []string
	if ok {
		for _, c := range meta.Columns {
			cols = append(cols, c.Name)
		}
	} else {
		cols = []string{"id", "created_at"}
	}

	chartType := strings.ToUpper(strings.TrimSpace(r.FormValue("chart_type")))
	if chartType == "" {
		chartType = "TABLE"
	}

	userID := "system"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	in := reportbuilder.CreateReportInput{
		Name:            name,
		Description:     strings.TrimSpace(r.FormValue("description")),
		Dataset:         ds,
		SelectedColumns: cols,
		ChartType:       chartType,
		SortDirection:   "DESC",
	}

	if _, err := h.service().CreateReport(r.Context(), tenantID, userID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create custom report", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/report-builder", http.StatusSeeOther)
}

// WebRunReport executes the report and streams the CSV download directly.
func (h *ReportBuilderHandlers) WebRunReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	format := strings.ToUpper(strings.TrimSpace(r.FormValue("format")))
	if format == "" {
		format = "CSV"
	}

	in := reportbuilder.RunReportInput{
		Format: format,
		Limit:  1000,
	}

	res, err := h.service().RunReport(r.Context(), tenantID, id, in, "MANUAL")
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to execute report", err.Error(), user)
		return
	}

	if strings.ToUpper(res.Format) == "CSV" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"report-%s.csv\"", id))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.CSVData)
		return
	}

	http.Redirect(w, r, "/report-builder", http.StatusSeeOther)
}

// WebCreateSchedule handles automated cron distribution setup.
func (h *ReportBuilderHandlers) WebCreateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	reportID := strings.TrimSpace(r.FormValue("report_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	frequency := strings.ToUpper(strings.TrimSpace(r.FormValue("frequency")))
	if frequency == "" {
		frequency = "DAILY"
	}

	cronExpr := "0 8 * * *"
	switch frequency {
	case "WEEKLY":
		cronExpr = "0 8 * * 1"
	case "MONTHLY":
		cronExpr = "0 8 1 * *"
	}

	recipientStr := strings.TrimSpace(r.FormValue("recipients"))
	var recipients []string
	for _, rec := range strings.Split(recipientStr, ",") {
		rec = strings.TrimSpace(rec)
		if rec != "" {
			recipients = append(recipients, rec)
		}
	}
	if len(recipients) == 0 {
		recipients = []string{"fleetops@flyfleet.io"}
	}

	userID := "system"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	in := reportbuilder.CreateScheduleInput{
		ReportID:       reportID,
		Name:           name,
		Frequency:      frequency,
		CronExpression: cronExpr,
		Format:         "CSV",
		Channel:        "EMAIL",
		Recipients:     recipients,
	}

	if _, err := h.service().CreateSchedule(r.Context(), tenantID, userID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create report schedule", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/report-builder", http.StatusSeeOther)
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
