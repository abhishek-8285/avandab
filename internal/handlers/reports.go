package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/pdf"
	"transport-app/internal/reportbuilder"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// ReportHandlers handles report-related requests.
type ReportHandlers struct {
	*App
}

func (h *ReportHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/", h.Index)

	// HTML views
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/revenue", h.RevenueReport)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/trips", h.TripReport)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/drivers", h.DriverReport)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/vehicles", h.VehicleReport)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/customers", h.CustomerReport)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/pending-payments", h.PendingPaymentsReport)

	// CSV export endpoints
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/revenue.csv", h.ExportRevenueCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/trips.csv", h.ExportTripsCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/drivers.csv", h.ExportDriversCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/vehicles.csv", h.ExportVehiclesCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/vehicle-master.csv", h.ExportVehiclesCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/gate-register.csv", h.ExportGateRegisterCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/fuel-kmpl.csv", h.ExportFuelKMPLCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/kmpl-summary.csv", h.ExportKMPLSummaryCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/breakdown.csv", h.ExportBreakdownCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/customers.csv", h.ExportCustomersCSV)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/pending-payments.csv", h.ExportPendingPaymentsCSV)

	// PDF export endpoints (bounded only)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/revenue.pdf", h.ExportRevenuePDF)
	r.With(middleware.ResourcePermission(h.AuthSrv, "reports", "read")).Get("/pending-payments.pdf", h.ExportPendingPaymentsPDF)
}

func (h *ReportHandlers) Index(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "reports_index.html", PageData{
		Title: "Reports",
		User:  session,
	})
}

func (h *ReportHandlers) RevenueReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	monthlyRev, _ := h.Services.Payments.GetMonthlyRevenue(r.Context())
	totalRev, _ := h.Services.Payments.GetTotalRevenue(r.Context())

	h.renderPage(w, r, "report_revenue.html", PageData{
		Title: "Revenue Report",
		User:  session,
		Extra: map[string]interface{}{
			"MonthlyRevenue": monthlyRev,
			"TotalRevenue":   totalRev,
			"QueryString":    r.URL.RawQuery,
			"ShowPDFExport":  true,
		},
	})
}

func (h *ReportHandlers) TripReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	trips, total, err := h.Services.Trips.ListTrips(r.Context(), pp.Query, pp.Status, pp.Limit, pp.Offset)
	if err != nil {
		trips = []repository.TripWithJoins{}
		total = 0
	}

	today := time.Now().Format("2006-01-02")
	rawCounts, _ := h.Services.Dashboard.GetTodayTripsSummary(r.Context(), today)
	statusCounts := make(map[string]int64)
	for k, v := range rawCounts {
		statusCounts[string(k)] = v
	}

	pd := newPaginationData(pp, total, "/reports/trips")

	h.renderPage(w, r, "report_trips.html", PageData{
		Title: "Trip Performance Report",
		User:  session,
		Extra: map[string]interface{}{
			"Trips":         trips,
			"TotalTrips":    total,
			"StatusCounts":  statusCounts,
			"Pagination":    pd,
			"Query":         pp.Query,
			"StatusFilter":  pp.Status,
			"QueryString":   r.URL.RawQuery,
			"ShowPDFExport": false,
		},
	})
}

func (h *ReportHandlers) DriverReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	drivers, total, err := h.Services.Drivers.ListDrivers(r.Context(), pp.Query, pp.Status, pp.Limit, pp.Offset)
	if err != nil {
		drivers = []domain.Driver{}
		total = 0
	}

	availableCount, _ := h.Services.Dashboard.GetAvailableDriversForDashboard(r.Context())

	pagination := newPaginationData(pp, total, "/reports/drivers")

	h.renderPage(w, r, "report_drivers.html", PageData{
		Title: "Driver Roster Report",
		User:  session,
		Extra: map[string]interface{}{
			"Drivers":          drivers,
			"TotalDrivers":     total,
			"AvailableDrivers": availableCount,
			"Pagination":       pagination,
			"Query":            pp.Query,
			"StatusFilter":     pp.Status,
			"QueryString":      r.URL.RawQuery,
			"ShowPDFExport":    false,
		},
	})
}

func (h *ReportHandlers) VehicleReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	vehicles, total, err := h.Services.Vehicles.ListVehicles(r.Context(), pp.Query, pp.Status, pp.Limit, pp.Offset)
	if err != nil {
		vehicles = []domain.Vehicle{}
		total = 0
	}

	availableCount, _ := h.Services.Dashboard.GetAvailableVehiclesForDashboard(r.Context())

	pagination := newPaginationData(pp, total, "/reports/vehicles")

	h.renderPage(w, r, "report_vehicles.html", PageData{
		Title: "Vehicle Fleet Report",
		User:  session,
		Extra: map[string]interface{}{
			"Vehicles":          vehicles,
			"TotalVehicles":     total,
			"AvailableVehicles": availableCount,
			"Pagination":        pagination,
			"Query":             pp.Query,
			"StatusFilter":      pp.Status,
			"QueryString":       r.URL.RawQuery,
			"ShowPDFExport":     false,
		},
	})
}

func (h *ReportHandlers) CustomerReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	customers, total, err := h.Services.Customers.ListCustomers(r.Context(), pp.Query, pp.Limit, pp.Offset)
	if err != nil {
		customers = []domain.Customer{}
		total = 0
	}
	pagination := newPaginationData(pp, total, "/reports/customers")

	h.renderPage(w, r, "report_customers.html", PageData{
		Title: "Customer Directory Report",
		User:  session,
		Extra: map[string]interface{}{
			"Customers":      customers,
			"TotalCustomers": total,
			"Pagination":     pagination,
			"Query":          pp.Query,
			"QueryString":    r.URL.RawQuery,
			"ShowPDFExport":  false,
		},
	})
}

func (h *ReportHandlers) PendingPaymentsReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	pending, err := h.Services.Invoices.GetPendingInvoices(r.Context())
	if err != nil {
		pending = []repository.InvoiceWithJoins{}
	}

	var totalOutstanding float64
	for _, inv := range pending {
		totalOutstanding += inv.Total
	}

	h.renderPage(w, r, "report_pending_payments.html", PageData{
		Title: "Outstanding Payments Audit Report",
		User:  session,
		Extra: map[string]interface{}{
			"Invoices":         pending,
			"TotalInvoices":    len(pending),
			"TotalOutstanding": totalOutstanding,
			"QueryString":      r.URL.RawQuery,
			"ShowPDFExport":    true,
		},
	})
}

// ── Export Handlers ──────────────────────────────────────────────────────────

func (h *ReportHandlers) ExportRevenueCSV(w http.ResponseWriter, r *http.Request) {
	monthlyRev, _ := h.Services.Payments.GetMonthlyRevenue(r.Context())
	totalRev, _ := h.Services.Payments.GetTotalRevenue(r.Context())

	header := []string{"Month", "Revenue (INR)"}
	var rows [][]string
	for _, m := range monthlyRev {
		rows = append(rows, []string{m.Month, fmt.Sprintf("%.2f", m.Total)})
	}
	rows = append(rows, []string{"Total Cumulative Revenue", fmt.Sprintf("%.2f", totalRev)})

	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	writeCSV(w, "revenue_report.csv", header, rows, maxRows, "")
}

func (h *ReportHandlers) ExportTripsCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	trips, total, err := h.Services.Trips.ListTrips(r.Context(), pp.Query, pp.Status, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load trips report", http.StatusInternalServerError)
		return
	}

	header := []string{"TripNumber", "Driver", "Vehicle", "Route", "DepartureTime", "Status"}
	var rows [][]string
	for _, t := range trips {
		driverName := ""
		if t.DriverFirstName != nil {
			driverName = *t.DriverFirstName
			if t.DriverLastName != nil {
				driverName += " " + *t.DriverLastName
			}
		}
		vehicleReg := ""
		if t.VehicleRegistration != nil {
			vehicleReg = *t.VehicleRegistration
		}
		routeStr := fmt.Sprintf("%s -> %s", t.RouteSource, t.RouteDestination)
		depTime := t.DepartureTime.Format("2006-01-02 15:04:05")

		rows = append(rows, []string{
			t.TripNumber,
			driverName,
			vehicleReg,
			routeStr,
			depTime,
			string(t.Status),
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(trips)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(trips)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "trips_report.csv", header, rows, maxRows, nextURL)
}

func (h *ReportHandlers) ExportDriversCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	drivers, total, err := h.Services.Drivers.ListDrivers(r.Context(), pp.Query, pp.Status, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load drivers report", http.StatusInternalServerError)
		return
	}

	header := []string{"DriverID", "Name", "Phone", "LicenseNumber", "ExperienceYears", "Status"}
	var rows [][]string
	for _, d := range drivers {
		rows = append(rows, []string{
			d.DriverID,
			fmt.Sprintf("%s %s", d.FirstName, d.LastName),
			d.Phone,
			d.LicenseNumber,
			fmt.Sprintf("%d", d.ExperienceYears),
			string(d.Status),
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(drivers)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(drivers)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "drivers_report.csv", header, rows, maxRows, nextURL)
}

func (h *ReportHandlers) ExportCustomersCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	customers, total, err := h.Services.Customers.ListCustomers(r.Context(), pp.Query, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load customers report", http.StatusInternalServerError)
		return
	}

	header := []string{"Name", "Company", "Email", "Phone", "GST"}
	var rows [][]string
	for _, c := range customers {
		company := ""
		if c.Company != nil {
			company = *c.Company
		}
		email := ""
		if c.Email != nil {
			email = *c.Email
		}
		gst := ""
		if c.GST != nil {
			gst = *c.GST
		}
		rows = append(rows, []string{
			c.Name,
			company,
			email,
			c.Phone,
			gst,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(customers)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(customers)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "customers_report.csv", header, rows, maxRows, nextURL)
}

func (h *ReportHandlers) ExportPendingPaymentsCSV(w http.ResponseWriter, r *http.Request) {
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	pending, err := h.Services.Invoices.GetPendingInvoices(r.Context())
	if err != nil {
		http.Error(w, "Failed to load pending payments report", http.StatusInternalServerError)
		return
	}

	header := []string{"InvoiceNumber", "CustomerName", "Subtotal", "Tax", "Total", "PaymentStatus"}
	var rows [][]string
	for _, inv := range pending {
		rows = append(rows, []string{
			inv.InvoiceNumber,
			inv.CustomerName,
			fmt.Sprintf("%.2f", inv.Subtotal),
			fmt.Sprintf("%.2f", inv.Tax),
			fmt.Sprintf("%.2f", inv.Total),
			string(inv.PaymentStatus),
		})
	}

	writeCSV(w, "pending_payments_report.csv", header, rows, maxRows, "")
}

func (h *ReportHandlers) ExportRevenuePDF(w http.ResponseWriter, r *http.Request) {
	monthlyRev, _ := h.Services.Payments.GetMonthlyRevenue(r.Context())
	totalRev, _ := h.Services.Payments.GetTotalRevenue(r.Context())

	header := []string{"Month", "Revenue (INR)"}
	var rows [][]string
	for _, m := range monthlyRev {
		rows = append(rows, []string{m.Month, fmt.Sprintf("%.2f", m.Total)})
	}
	rows = append(rows, []string{"Total Cumulative Revenue", fmt.Sprintf("%.2f", totalRev)})

	companyName := "Avandab Transport Logistics"
	pdfBytes, err := pdf.GenerateReportPDF("Revenue Summary Report", companyName, header, rows)
	if err != nil {
		http.Error(w, "Failed to generate PDF: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="revenue_report.pdf"`)
	_, _ = w.Write(pdfBytes)
}

func (h *ReportHandlers) ExportPendingPaymentsPDF(w http.ResponseWriter, r *http.Request) {
	pending, err := h.Services.Invoices.GetPendingInvoices(r.Context())
	if err != nil {
		http.Error(w, "Failed to load pending payments", http.StatusInternalServerError)
		return
	}

	header := []string{"Invoice #", "Customer", "Subtotal (INR)", "Tax (INR)", "Total (INR)", "Status"}
	var rows [][]string
	for _, inv := range pending {
		rows = append(rows, []string{
			inv.InvoiceNumber,
			inv.CustomerName,
			fmt.Sprintf("%.2f", inv.Subtotal),
			fmt.Sprintf("%.2f", inv.Tax),
			fmt.Sprintf("%.2f", inv.Total),
			string(inv.PaymentStatus),
		})
	}

	companyName := "Avandab Transport Logistics"
	pdfBytes, err := pdf.GenerateReportPDF("Outstanding Invoices & Pending Payments", companyName, header, rows)
	if err != nil {
		http.Error(w, "Failed to generate PDF: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="pending_payments_report.pdf"`)
	_, _ = w.Write(pdfBytes)
}

// ---------------------------------------------------------------------------
// Merged from report_builder.go
// ---------------------------------------------------------------------------

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
		if res != nil {
			// Report generated successfully, but delivery transport is unconfigured or failed.
			writeReportBuilderJSON(w, http.StatusOK, res)
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
