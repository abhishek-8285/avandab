package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/pdf"
	"transport-app/internal/repository"
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
