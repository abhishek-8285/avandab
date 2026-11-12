package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	fleetreports "transport-app/internal/fleetreports"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// RegisterAPIRoutes registers the ZMOTM_MR report REST API routes.
func (h *ReportHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/reports", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/vehicle-master", h.APIVehicleMaster)
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/gate-register", h.APIGateRegister)
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/fuel-kmpl", h.APIFuelKMPL)
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/kmpl-summary", h.APIKMPLSummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "reports", "read")).Get("/breakdown", h.APIBreakdown)
	})
}

// ── 1. Vehicle Master Report (SAP ZMOTM_MR p.18 Parity) ──

var vehicleMasterHeaders = []string{
	"Vehicle No", "Type", "Category", "Manufacturer", "Model",
	"Purchase Date", "Purchase Value", "Currency", "Fuel Type",
	"Fleet Number", "Chassis No", "Engine SNo",
}

func (h *ReportHandlers) loadVehicleMasterRows(r *http.Request, maxRows int, offset int) ([]fleetreports.VehicleMasterRow, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	pp := parsePaginationParams(r)

	if h.DB != nil {
		return fleetreports.New(h.DB).VehicleMaster(r.Context(), tenantID, fleetreports.Filter{Query: pp.Query, Status: pp.Status}, maxRows, offset)
	}

	if h.Services != nil && h.Services.Vehicles != nil {
		vehicles, total, err := h.Services.Vehicles.ListVehicles(r.Context(), pp.Query, pp.Status, maxRows, offset)
		if err != nil {
			return nil, 0, err
		}
		out := make([]fleetreports.VehicleMasterRow, 0, len(vehicles))
		for _, v := range vehicles {
			out = append(out, fleetreports.VehicleMasterRow{
				VehicleNo:    v.RegistrationNumber,
				Type:         "O",
				Category:     "CV",
				Manufacturer: "",
				Model:        "",
				PurchaseDate: "",
				Currency:     "INR",
				FuelType:     string(v.FuelType),
				FleetNumber:  v.VehicleNumber,
			})
		}
		return out, total, nil
	}

	return []fleetreports.VehicleMasterRow{}, 0, nil
}

func (h *ReportHandlers) ExportVehiclesCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	dtos, total, err := h.loadVehicleMasterRows(r, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load vehicle master report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([][]string, 0, len(dtos))
	for _, d := range dtos {
		valStr := ""
		if d.PurchaseValue != nil {
			valStr = fmt.Sprintf("%.2f", *d.PurchaseValue)
		}
		rows = append(rows, []string{
			d.VehicleNo,
			d.Type,
			d.Category,
			d.Manufacturer,
			d.Model,
			d.PurchaseDate,
			valStr,
			d.Currency,
			d.FuelType,
			d.FleetNumber,
			d.ChassisNo,
			d.EngineSNo,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(dtos)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(dtos)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "vehicles_report.csv", vehicleMasterHeaders, rows, maxRows, nextURL)
}

func (h *ReportHandlers) APIVehicleMaster(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	dtos, total, err := h.loadVehicleMasterRows(r, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"vehicles": dtos,
		"total":    total,
		"limit":    pp.Limit,
		"offset":   pp.Offset,
	})
}

// ── 2. Gate Register Report (SAP ZMOTM_MR p.18 Parity) ──

var gateRegisterHeaders = []string{
	"Trip No", "Vehicle No", "Driver", "Route", "Gate Facility",
	"Gate Out Time", "Gate In Time", "Start Reading", "Close Reading",
	"Distance KM", "Status",
}

func (h *ReportHandlers) loadGateRegisterRows(r *http.Request, maxRows int, offset int) ([]fleetreports.GateRegisterRow, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	pp := parsePaginationParams(r)
	return fleetreports.New(h.DB).GateRegister(r.Context(), tenantID, fleetreports.Filter{Query: pp.Query, Status: pp.Status}, maxRows, offset)
}

func (h *ReportHandlers) ExportGateRegisterCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	dtos, total, err := h.loadGateRegisterRows(r, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load gate register: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([][]string, 0, len(dtos))
	for _, d := range dtos {
		startStr := ""
		if d.StartReading != nil {
			startStr = fmt.Sprintf("%.1f", *d.StartReading)
		}
		closeStr := ""
		if d.CloseReading != nil {
			closeStr = fmt.Sprintf("%.1f", *d.CloseReading)
		}
		distStr := ""
		if d.DistanceKM > 0 {
			distStr = fmt.Sprintf("%.1f", d.DistanceKM)
		}

		rows = append(rows, []string{
			d.TripNo,
			d.VehicleNo,
			d.Driver,
			d.Route,
			d.GateFacility,
			d.GateOutTime,
			d.GateInTime,
			startStr,
			closeStr,
			distStr,
			d.Status,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(dtos)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(dtos)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "gate_register.csv", gateRegisterHeaders, rows, maxRows, nextURL)
}

func (h *ReportHandlers) APIGateRegister(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	dtos, total, err := h.loadGateRegisterRows(r, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"records": dtos,
		"total":   total,
		"limit":   pp.Limit,
		"offset":  pp.Offset,
	})
}

// ── 3. Fuel & KMPL Report (SAP ZMOTM_MR p.19 Parity) ──

var fuelKMPLHeaders = []string{
	"Vehicle No", "Date", "Issue No", "Fuel Station", "Driver", "Trip No",
	"Litres Issued", "Rate", "Total Cost", "Odometer", "Previous Odometer",
	"Distance KM", "KMPL",
}

func (h *ReportHandlers) loadFuelKMPLRows(r *http.Request, maxRows int, offset int) ([]fleetreports.FuelKMPLRow, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	return fleetreports.New(h.DB).FuelKMPL(r.Context(), tenantID, maxRows, offset)
}

func (h *ReportHandlers) ExportFuelKMPLCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	dtos, total, err := h.loadFuelKMPLRows(r, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load fuel & KMPL report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([][]string, 0, len(dtos))
	for _, d := range dtos {
		rateStr := ""
		if d.Rate != nil {
			rateStr = fmt.Sprintf("%.2f", *d.Rate)
		}
		costStr := ""
		if d.TotalCost != nil {
			costStr = fmt.Sprintf("%.2f", *d.TotalCost)
		}
		odoStr := ""
		if d.Odometer != nil {
			odoStr = fmt.Sprintf("%.1f", *d.Odometer)
		}
		prevStr := ""
		if d.PrevOdometer != nil {
			prevStr = fmt.Sprintf("%.1f", *d.PrevOdometer)
		}
		distStr := ""
		if d.DistanceKM != nil {
			distStr = fmt.Sprintf("%.1f", *d.DistanceKM)
		}
		kmplStr := ""
		if d.KMPL != nil {
			kmplStr = fmt.Sprintf("%.2f", *d.KMPL)
		}

		rows = append(rows, []string{
			d.VehicleNo,
			d.Date,
			d.IssueNo,
			d.FuelStation,
			d.Driver,
			d.TripNo,
			fmt.Sprintf("%.2f", d.LitresIssued),
			rateStr,
			costStr,
			odoStr,
			prevStr,
			distStr,
			kmplStr,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(dtos)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(dtos)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "fuel_kmpl_report.csv", fuelKMPLHeaders, rows, maxRows, nextURL)
}

func (h *ReportHandlers) APIFuelKMPL(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	dtos, total, err := h.loadFuelKMPLRows(r, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"records": dtos,
		"total":   total,
		"limit":   pp.Limit,
		"offset":  pp.Offset,
	})
}

// ── 4. Breakdown & Outstanding Notifications Report (SAP ZMOTM_MR p.20 Parity) ──

var breakdownHeaders = []string{
	"Notification ID", "Vehicle No", "Type", "Severity", "Status",
	"Reported At", "Resolved At", "Description", "Resolution Note",
}

func (h *ReportHandlers) loadBreakdownRows(r *http.Request, maxRows int, offset int) ([]fleetreports.BreakdownRow, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	return fleetreports.New(h.DB).Breakdown(r.Context(), tenantID, maxRows, offset)
}

func (h *ReportHandlers) ExportBreakdownCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	dtos, total, err := h.loadBreakdownRows(r, maxRows, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to load breakdown report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([][]string, 0, len(dtos))
	for _, d := range dtos {
		rows = append(rows, []string{
			d.NotificationID,
			d.VehicleNo,
			d.Type,
			d.Severity,
			d.Status,
			d.ReportedAt,
			d.ResolvedAt,
			d.Description,
			d.ResolutionNote,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(dtos)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(dtos)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "breakdown_report.csv", breakdownHeaders, rows, maxRows, nextURL)
}

func (h *ReportHandlers) APIBreakdown(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	dtos, total, err := h.loadBreakdownRows(r, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"records": dtos,
		"total":   total,
		"limit":   pp.Limit,
		"offset":  pp.Offset,
	})
}

// ── 5. KMPL Summary Report (per vehicle × month, B13) ──
// Tank-to-tank method: distance = last odometer − opening odometer, fuel =
// litres filled after the opening fill. The opening fill is the latest fill
// before the period (its fuel belongs to prior consumption); without one,
// the first in-period fill opens the run and its litres are excluded.

var kmplSummaryHeaders = []string{
	"Vehicle No", "Month", "Fills", "Litres", "Distance KM", "KMPL",
	"Standard KMPL", "Variance %", "Flag",
}

func (h *ReportHandlers) loadKMPLSummaryRows(r *http.Request) ([]fleetreports.KMPLSummaryRow, int64, string, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleFilter := r.URL.Query().Get("vehicle_id")
	return fleetreports.New(h.DB).KMPLSummary(r.Context(), tenantID, r.URL.Query().Get("month"), vehicleFilter)
}

func (h *ReportHandlers) APIKMPLSummary(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	dtos, total, month, err := h.loadKMPLSummaryRows(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	end := pp.Offset + pp.Limit
	if end > len(dtos) {
		end = len(dtos)
	}
	page := []fleetreports.KMPLSummaryRow{}
	if pp.Offset < len(dtos) {
		page = dtos[pp.Offset:end]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"records": page,
		"total":   total,
		"month":   month,
		"limit":   pp.Limit,
		"offset":  pp.Offset,
	})
}

func (h *ReportHandlers) ExportKMPLSummaryCSV(w http.ResponseWriter, r *http.Request) {
	pp := parsePaginationParams(r)
	maxRows := h.Config.ExportMaxRows
	if maxRows <= 0 {
		maxRows = 50000
	}

	dtos, total, month, err := h.loadKMPLSummaryRows(r)
	if err != nil {
		http.Error(w, "Failed to load KMPL summary report: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(dtos) > maxRows {
		dtos = dtos[:maxRows]
	}

	rows := make([][]string, 0, len(dtos))
	for _, d := range dtos {
		distStr := ""
		if d.DistanceKM != nil {
			distStr = fmt.Sprintf("%.1f", *d.DistanceKM)
		}
		kmplStr := ""
		if d.KMPL != nil {
			kmplStr = fmt.Sprintf("%.2f", *d.KMPL)
		}
		normStr := ""
		if d.StandardKmpl != nil {
			normStr = fmt.Sprintf("%.2f", *d.StandardKmpl)
		}
		varStr := ""
		if d.VariancePct != nil {
			varStr = fmt.Sprintf("%+.1f", *d.VariancePct)
		}
		rows = append(rows, []string{
			d.VehicleNo,
			d.Month,
			fmt.Sprintf("%d", d.Fills),
			fmt.Sprintf("%.2f", d.Litres),
			distStr,
			kmplStr,
			normStr,
			varStr,
			d.Flag,
		})
	}

	nextURL := ""
	if total > int64(pp.Offset+len(dtos)) {
		q := r.URL.Query()
		q.Set("offset", fmt.Sprintf("%d", pp.Offset+len(dtos)))
		nextURL = fmt.Sprintf("%s?%s", r.URL.Path, q.Encode())
	}

	writeCSV(w, "kmpl_summary_"+month+".csv", kmplSummaryHeaders, rows, maxRows, nextURL)
}
