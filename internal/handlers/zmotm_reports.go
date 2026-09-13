package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	db "transport-app/db/generated/sqlite"
	appdb "transport-app/internal/database"
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

type VehicleMasterDTO struct {
	VehicleNo     string   `json:"vehicle_no"`
	Type          string   `json:"type"`
	Category      string   `json:"category"`
	Manufacturer  string   `json:"manufacturer"`
	Model         string   `json:"model"`
	PurchaseDate  string   `json:"purchase_date"`
	PurchaseValue *float64 `json:"purchase_value"`
	Currency      string   `json:"currency"`
	FuelType      string   `json:"fuel_type"`
	FleetNumber   string   `json:"fleet_number"`
	ChassisNo     string   `json:"chassis_no"`
	EngineSNo     string   `json:"engine_sno"`
}

func (h *ReportHandlers) loadVehicleMasterRows(r *http.Request, maxRows int, offset int) ([]VehicleMasterDTO, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	pp := parsePaginationParams(r)

	if h.DB != nil {
		q := db.New(h.DB)
		rows, err := q.SearchVehicles(r.Context(), db.SearchVehiclesParams{
			TenantID:      tenantID,
			Search:        pp.Query,
			StatusAll:     "",
			Status:        pp.Status,
			FleetClassAll: "",
			FleetClass:    "",
			OwnershipAll:  "",
			Ownership:     "",
			Limit:         int64(maxRows),
			Offset:        int64(offset),
		})
		if err != nil {
			return nil, 0, err
		}

		total, err := q.CountVehicles(r.Context(), db.CountVehiclesParams{
			TenantID:      tenantID,
			Search:        pp.Query,
			StatusAll:     "",
			Status:        pp.Status,
			FleetClassAll: "",
			FleetClass:    "",
			OwnershipAll:  "",
			Ownership:     "",
		})
		if err != nil {
			return nil, 0, err
		}

		dtos := make([]VehicleMasterDTO, 0, len(rows))
		for _, v := range rows {
			ownership := v.Ownership
			if ownership == "" {
				ownership = "O"
			}
			fleetClass := v.FleetClass
			if fleetClass == "" {
				fleetClass = "CV"
			}
			curr := v.AcquisitionCurrency
			if curr == "" {
				curr = "INR"
			}
			purchDate := ""
			if v.AcquisitionDate.Valid {
				purchDate = v.AcquisitionDate.Time.Format("2006-01-02")
			}
			var purchVal *float64
			if v.AcquisitionValue.Valid {
				val := v.AcquisitionValue.Float64
				purchVal = &val
			}

			dtos = append(dtos, VehicleMasterDTO{
				VehicleNo:     v.RegistrationNumber,
				Type:          ownership,
				Category:      fleetClass,
				Manufacturer:  v.Manufacturer.String,
				Model:         v.Model.String,
				PurchaseDate:  purchDate,
				PurchaseValue: purchVal,
				Currency:      curr,
				FuelType:      v.FuelType,
				FleetNumber:   v.FleetNumber.String,
				ChassisNo:     v.ChassisNo.String,
				EngineSNo:     v.EngineNumber.String,
			})
		}
		return dtos, total, nil
	}

	if h.Services != nil && h.Services.Vehicles != nil {
		vehicles, total, err := h.Services.Vehicles.ListVehicles(r.Context(), pp.Query, pp.Status, maxRows, offset)
		if err != nil {
			return nil, 0, err
		}
		dtos := make([]VehicleMasterDTO, 0, len(vehicles))
		for _, v := range vehicles {
			dtos = append(dtos, VehicleMasterDTO{
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
		return dtos, total, nil
	}

	return []VehicleMasterDTO{}, 0, nil
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

type GateRegisterDTO struct {
	TripNo       string   `json:"trip_no"`
	VehicleNo    string   `json:"vehicle_no"`
	Driver       string   `json:"driver"`
	Route        string   `json:"route"`
	GateFacility string   `json:"gate_facility"`
	GateOutTime  string   `json:"gate_out_time"`
	GateInTime   string   `json:"gate_in_time"`
	StartReading *float64 `json:"start_reading"`
	CloseReading *float64 `json:"close_reading"`
	DistanceKM   float64  `json:"distance_km"`
	Status       string   `json:"status"`
}

func (h *ReportHandlers) loadGateRegisterRows(r *http.Request, maxRows int, offset int) ([]GateRegisterDTO, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	pp := parsePaginationParams(r)

	countSQL := `
SELECT COUNT(*)
FROM trips t
WHERE t.tenant_id = ?
  AND (? = '' OR t.trip_number LIKE ?)`

	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	qPattern := "%" + pp.Query + "%"
	var total int64
	if err := h.DB.QueryRowContext(r.Context(), reboundCount, tenantID, pp.Query, qPattern).Scan(&total); err != nil {
		return nil, 0, err
	}

	querySQL := `
SELECT t.trip_number, t.status,
       t.start_odometer, t.close_odometer,
       COALESCE(t.gate_facility_id, ''),
       t.started_at, t.completed_at, t.departure_time, t.arrival_time,
       COALESCE(v.registration_number, ''),
       COALESCE(d.first_name, '') || ' ' || COALESCE(d.last_name, ''),
       COALESCE(r.source, '') || ' -> ' || COALESCE(r.destination, ''),
       COALESCE(r.distance, 0.0)
FROM trips t
LEFT JOIN vehicles v ON t.vehicle_id = v.id
LEFT JOIN drivers d ON t.driver_id = d.id
LEFT JOIN routes r ON t.route_id = r.id
WHERE t.tenant_id = ?
  AND (? = '' OR t.trip_number LIKE ?)
ORDER BY t.departure_time DESC
LIMIT ? OFFSET ?`

	rebound, err := appdb.Rebind(querySQL)
	if err != nil {
		return nil, 0, err
	}
	rows, err := h.DB.QueryContext(r.Context(), rebound, tenantID, pp.Query, qPattern, maxRows, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var dtos []GateRegisterDTO
	for rows.Next() {
		var tripNo, status, gateFacility, vehNo, driver, routeStr string
		var startOdo, closeOdo sql.NullFloat64
		var startedAt, completedAt, depTime, arrTime sql.NullTime
		var routeDist float64

		if err := rows.Scan(
			&tripNo, &status, &startOdo, &closeOdo, &gateFacility,
			&startedAt, &completedAt, &depTime, &arrTime,
			&vehNo, &driver, &routeStr, &routeDist,
		); err != nil {
			return nil, 0, err
		}

		gateOut := ""
		if startedAt.Valid {
			gateOut = startedAt.Time.Format("2006-01-02 15:04:05")
		} else if depTime.Valid {
			gateOut = depTime.Time.Format("2006-01-02 15:04:05")
		}

		gateIn := ""
		if completedAt.Valid {
			gateIn = completedAt.Time.Format("2006-01-02 15:04:05")
		} else if arrTime.Valid {
			gateIn = arrTime.Time.Format("2006-01-02 15:04:05")
		}

		var startVal, closeVal *float64
		var dist float64
		if startOdo.Valid {
			v := startOdo.Float64
			startVal = &v
		}
		if closeOdo.Valid {
			v := closeOdo.Float64
			closeVal = &v
		}

		if startVal != nil && closeVal != nil && *closeVal >= *startVal && *startVal > 0 {
			dist = *closeVal - *startVal
		} else if status == "completed" && routeDist > 0 {
			dist = routeDist
		}

		dtos = append(dtos, GateRegisterDTO{
			TripNo:       tripNo,
			VehicleNo:    vehNo,
			Driver:       driver,
			Route:        routeStr,
			GateFacility: gateFacility,
			GateOutTime:  gateOut,
			GateInTime:   gateIn,
			StartReading: startVal,
			CloseReading: closeVal,
			DistanceKM:   dist,
			Status:       status,
		})
	}

	return dtos, total, nil
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

type FuelKMPLDTO struct {
	VehicleNo    string   `json:"vehicle_no"`
	Date         string   `json:"date"`
	IssueNo      string   `json:"issue_no"`
	FuelStation  string   `json:"fuel_station"`
	Driver       string   `json:"driver"`
	TripNo       string   `json:"trip_no"`
	LitresIssued float64  `json:"litres_issued"`
	Rate         *float64 `json:"rate"`
	TotalCost    *float64 `json:"total_cost"`
	Odometer     *float64 `json:"odometer"`
	PrevOdometer *float64 `json:"prev_odometer"`
	DistanceKM   *float64 `json:"distance_km"`
	KMPL         *float64 `json:"kmpl"`
}

func (h *ReportHandlers) loadFuelKMPLRows(r *http.Request, maxRows int, offset int) ([]FuelKMPLDTO, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	countSQL := `SELECT COUNT(*) FROM fuel_issues WHERE tenant_id = ?`
	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := h.DB.QueryRowContext(r.Context(), reboundCount, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	querySQL := `
SELECT f.id, COALESCE(f.issue_number, ''),
       f.litres_issued, f.vehicle_odometer, f.rate_per_litre, f.total_cost,
       f.issued_at, f.vehicle_id,
       COALESCE(v.registration_number, ''),
       COALESCE(vs.registration_number, vs.vehicle_number, ''),
       COALESCE(d.first_name, '') || ' ' || COALESCE(d.last_name, ''),
       COALESCE(t.trip_number, '')
FROM fuel_issues f
LEFT JOIN vehicles v ON f.vehicle_id = v.id
LEFT JOIN vehicles vs ON f.fuel_station_id = vs.id
LEFT JOIN drivers d ON f.driver_id = d.id
LEFT JOIN trips t ON f.trip_id = t.id
WHERE f.tenant_id = ?
ORDER BY v.registration_number, f.issued_at ASC
LIMIT ? OFFSET ?`

	rebound, err := appdb.Rebind(querySQL)
	if err != nil {
		return nil, 0, err
	}
	rows, err := h.DB.QueryContext(r.Context(), rebound, tenantID, maxRows, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var dtos []FuelKMPLDTO

	type fillRow struct {
		id, issueNo, vehNo, station, driver, tripNo string
		vehicleID                                   string
		litres                                      float64
		odo, rate, cost                             sql.NullFloat64
		issuedAt                                    time.Time
	}
	var fills []fillRow
	pageMin := time.Time{}

	for rows.Next() {
		var id, issueNo, vehNo, station, driver, tripNo string
		var vehicleID string
		var litres float64
		var odo, rate, cost sql.NullFloat64
		var issuedAt time.Time

		if err := rows.Scan(
			&id, &issueNo, &litres, &odo, &rate, &cost, &issuedAt, &vehicleID,
			&vehNo, &station, &driver, &tripNo,
		); err != nil {
			return nil, 0, err
		}
		fills = append(fills, fillRow{id, issueNo, vehNo, station, driver, tripNo, vehicleID, litres, odo, rate, cost, issuedAt})
		if pageMin.IsZero() || issuedAt.Before(pageMin) {
			pageMin = issuedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// B13 pagination-carryover fix: page 1 starts each vehicle with a blank
	// previous odometer (correct — first fill ever has no KMPL), but page 2+
	// must seed from the last fill BEFORE the page, else every page's first
	// row per vehicle wrongly shows blank KMPL. Latest pre-page odometer per
	// vehicle wins (max odo breaks exact-timestamp ties).
	prevOdoMap := make(map[string]float64)
	if offset > 0 && !pageMin.IsZero() {
		vidToNo := make(map[string]string, len(fills))
		var vids []string
		for _, f := range fills {
			if _, ok := vidToNo[f.vehicleID]; !ok {
				vidToNo[f.vehicleID] = f.vehNo
				vids = append(vids, f.vehicleID)
			}
		}
		placeholders := make([]string, len(vids))
		seedArgs := make([]any, 0, len(vids)+2)
		seedArgs = append(seedArgs, tenantID, pageMin.Format("2006-01-02 15:04:05"))
		for i, vid := range vids {
			placeholders[i] = "?"
			seedArgs = append(seedArgs, vid)
		}
		seedQ := `
SELECT vehicle_id, vehicle_odometer, issued_at FROM fuel_issues
WHERE tenant_id = ? AND vehicle_odometer IS NOT NULL AND vehicle_odometer > 0 AND issued_at < ?
AND vehicle_id IN (` + strings.Join(placeholders, ",") + `)`
		reboundSeed, err := appdb.Rebind(seedQ)
		if err != nil {
			return nil, 0, err
		}
		seedRows, err := h.DB.QueryContext(r.Context(), reboundSeed, seedArgs...)
		if err != nil {
			return nil, 0, err
		}
		defer func() { _ = seedRows.Close() }()
		bestTS := make(map[string]time.Time)
		for seedRows.Next() {
			var vid string
			var odo float64
			var ts time.Time
			if err := seedRows.Scan(&vid, &odo, &ts); err != nil {
				return nil, 0, err
			}
			vehNo, onPage := vidToNo[vid]
			if !onPage {
				continue
			}
			if b, ok := bestTS[vehNo]; !ok || ts.After(b) || (ts.Equal(b) && odo > prevOdoMap[vehNo]) {
				bestTS[vehNo] = ts
				prevOdoMap[vehNo] = odo
			}
		}
		if err := seedRows.Err(); err != nil {
			return nil, 0, err
		}
	}

	for _, f := range fills {
		odo, rate, cost := f.odo, f.rate, f.cost
		vehNo, litres, issuedAt := f.vehNo, f.litres, f.issuedAt
		issueNo, station, driver, tripNo := f.issueNo, f.station, f.driver, f.tripNo

		var odoVal, rateVal, costVal *float64
		if odo.Valid {
			v := odo.Float64
			odoVal = &v
		}
		if rate.Valid {
			v := rate.Float64
			rateVal = &v
		}
		if cost.Valid {
			v := cost.Float64
			costVal = &v
		}

		var prevVal, distVal, kmplVal *float64
		if odoVal != nil && *odoVal > 0 {
			if prev, exists := prevOdoMap[vehNo]; exists && *odoVal > prev {
				p := prev
				prevVal = &p
				d := *odoVal - prev
				distVal = &d
				if litres > 0 {
					k := d / litres
					kmplVal = &k
				}
			}
			prevOdoMap[vehNo] = *odoVal
		}

		dtos = append(dtos, FuelKMPLDTO{
			VehicleNo:    vehNo,
			Date:         issuedAt.Format("2006-01-02 15:04"),
			IssueNo:      issueNo,
			FuelStation:  station,
			Driver:       driver,
			TripNo:       tripNo,
			LitresIssued: litres,
			Rate:         rateVal,
			TotalCost:    costVal,
			Odometer:     odoVal,
			PrevOdometer: prevVal,
			DistanceKM:   distVal,
			KMPL:         kmplVal,
		})
	}

	return dtos, total, nil
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

type BreakdownDTO struct {
	NotificationID string `json:"notification_id"`
	VehicleNo      string `json:"vehicle_no"`
	Type           string `json:"type"`
	Severity       string `json:"severity"`
	Status         string `json:"status"`
	ReportedAt     string `json:"reported_at"`
	ResolvedAt     string `json:"resolved_at"`
	Description    string `json:"description"`
	ResolutionNote string `json:"resolution_note"`
}

func (h *ReportHandlers) loadBreakdownRows(r *http.Request, maxRows int, offset int) ([]BreakdownDTO, int64, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	// Query ops_alerts (breakdowns)
	queryAlertsSQL := `
SELECT a.id, 'breakdown_alert' AS alert_type, a.severity, a.status, a.title,
       COALESCE(a.description, ''), COALESCE(a.resolution_note, ''),
       a.created_at, a.resolved_at,
       COALESCE(v.registration_number, vt.registration_number, a.entity_id, '')
FROM ops_alerts a
LEFT JOIN vehicles v ON a.entity_type = 'vehicle' AND a.entity_id = v.id
LEFT JOIN trips t ON a.entity_type = 'trip' AND a.entity_id = t.id
LEFT JOIN vehicles vt ON t.vehicle_id = vt.id
WHERE a.tenant_id = ? AND a.alert_type = 'vehicle_breakdown'
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?`

	reboundAlerts, err := appdb.Rebind(queryAlertsSQL)
	if err != nil {
		return nil, 0, err
	}
	rows, err := h.DB.QueryContext(r.Context(), reboundAlerts, tenantID, maxRows, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var dtos []BreakdownDTO
	for rows.Next() {
		var id, notifType, severity, status, title, desc, note, vehNo string
		var createdAt time.Time
		var resolvedAt sql.NullTime

		if err := rows.Scan(&id, &notifType, &severity, &status, &title, &desc, &note, &createdAt, &resolvedAt, &vehNo); err != nil {
			return nil, 0, err
		}

		resStr := ""
		if resolvedAt.Valid {
			resStr = resolvedAt.Time.Format("2006-01-02 15:04")
		}

		fullDesc := title
		if desc != "" {
			fullDesc += ": " + desc
		}

		dtos = append(dtos, BreakdownDTO{
			NotificationID: id,
			VehicleNo:      vehNo,
			Type:           "Breakdown Alert",
			Severity:       severity,
			Status:         status,
			ReportedAt:     createdAt.Format("2006-01-02 15:04"),
			ResolvedAt:     resStr,
			Description:    fullDesc,
			ResolutionNote: note,
		})
	}

	// Query open work orders (job cards) as outstanding notifications
	queryWOSQL := `
SELECT w.id, 'job_card' AS alert_type, 'maintenance' AS severity, w.status, w.title,
       COALESCE(w.description, ''), COALESCE(w.vendor, w.assignee, ''),
       w.created_at, w.closed_at,
       COALESCE(v.registration_number, w.vehicle_id, '')
FROM work_orders w
LEFT JOIN vehicles v ON w.vehicle_id = v.id
WHERE w.tenant_id = ?
ORDER BY w.created_at DESC
LIMIT ? OFFSET ?`

	reboundWO, err := appdb.Rebind(queryWOSQL)
	if err == nil {
		woRows, qErr := h.DB.QueryContext(r.Context(), reboundWO, tenantID, maxRows, offset)
		if qErr == nil {
			defer func() { _ = woRows.Close() }()
			for woRows.Next() {
				var id, notifType, severity, status, title, desc, note, vehNo string
				var createdAt time.Time
				var closedAt sql.NullTime
				if err := woRows.Scan(&id, &notifType, &severity, &status, &title, &desc, &note, &createdAt, &closedAt, &vehNo); err == nil {
					resStr := ""
					if closedAt.Valid {
						resStr = closedAt.Time.Format("2006-01-02 15:04")
					}
					dtos = append(dtos, BreakdownDTO{
						NotificationID: id,
						VehicleNo:      vehNo,
						Type:           "Job Card",
						Severity:       severity,
						Status:         status,
						ReportedAt:     createdAt.Format("2006-01-02 15:04"),
						ResolvedAt:     resStr,
						Description:    title + ": " + desc,
						ResolutionNote: note,
					})
				}
			}
		}
	}

	return dtos, int64(len(dtos)), nil
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

type KMPLSummaryDTO struct {
	VehicleNo    string   `json:"vehicle_no"`
	Month        string   `json:"month"`
	Fills        int      `json:"fills"`
	Litres       float64  `json:"litres"`
	DistanceKM   *float64 `json:"distance_km"`
	KMPL         *float64 `json:"kmpl"`
	StandardKmpl *float64 `json:"standard_kmpl"`
	VariancePct  *float64 `json:"variance_pct"`
	Flag         string   `json:"flag"`
}

func parseMonthParam(s string) (start time.Time, month string, err error) {
	if s == "" {
		now := time.Now().UTC()
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.Format("2006-01"), nil
	}
	m, perr := time.Parse("2006-01", s)
	if perr != nil {
		return time.Time{}, "", perr
	}
	start = time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, s, nil
}

func (h *ReportHandlers) loadKMPLSummaryRows(r *http.Request) ([]KMPLSummaryDTO, int64, string, error) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	monthStart, month, err := parseMonthParam(r.URL.Query().Get("month"))
	if err != nil {
		return nil, 0, "", fmt.Errorf("invalid month %q: want YYYY-MM", r.URL.Query().Get("month"))
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	startStr := monthStart.Format("2006-01-02 15:04:05")
	endStr := monthEnd.Format("2006-01-02 15:04:05")
	vehicleFilter := r.URL.Query().Get("vehicle_id")

	fillQ := `
SELECT f.vehicle_id, COALESCE(v.registration_number, ''),
       f.litres_issued, f.vehicle_odometer, f.issued_at, v.standard_kmpl
FROM fuel_issues f
LEFT JOIN vehicles v ON f.vehicle_id = v.id
WHERE f.tenant_id = ? AND f.issued_at >= ? AND f.issued_at < ?`
	fillArgs := []any{tenantID, startStr, endStr}
	if vehicleFilter != "" {
		fillQ += ` AND f.vehicle_id = ?`
		fillArgs = append(fillArgs, vehicleFilter)
	}
	fillQ += ` ORDER BY COALESCE(v.registration_number, ''), f.issued_at ASC`
	reboundFill, err := appdb.Rebind(fillQ)
	if err != nil {
		return nil, 0, "", err
	}
	rows, err := h.DB.QueryContext(r.Context(), reboundFill, fillArgs...)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = rows.Close() }()

	type fill struct {
		vid, vehNo string
		litres     float64
		odo        sql.NullFloat64
		at         time.Time
		norm       sql.NullFloat64
	}
	byVehicle := make(map[string][]fill)
	order := []string{}
	for rows.Next() {
		var fl fill
		if err := rows.Scan(&fl.vid, &fl.vehNo, &fl.litres, &fl.odo, &fl.at, &fl.norm); err != nil {
			return nil, 0, "", err
		}
		if _, ok := byVehicle[fl.vid]; !ok {
			order = append(order, fl.vid)
		}
		byVehicle[fl.vid] = append(byVehicle[fl.vid], fl)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", err
	}

	// Opening fill per vehicle: latest pre-period fill with a usable odometer.
	// Its fuel belongs to prior consumption, so its litres are excluded while
	// its odometer opens the distance run. Without one, the first in-period
	// fill opens the run instead.
	openQ := `
SELECT vehicle_id, vehicle_odometer, issued_at FROM fuel_issues
WHERE tenant_id = ? AND vehicle_odometer IS NOT NULL AND vehicle_odometer > 0 AND issued_at < ?`
	openArgs := []any{tenantID, startStr}
	if vehicleFilter != "" {
		openQ += ` AND vehicle_id = ?`
		openArgs = append(openArgs, vehicleFilter)
	}
	reboundOpen, err := appdb.Rebind(openQ)
	if err != nil {
		return nil, 0, "", err
	}
	openRows, err := h.DB.QueryContext(r.Context(), reboundOpen, openArgs...)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = openRows.Close() }()
	openOdo := make(map[string]float64)
	openTS := make(map[string]time.Time)
	for openRows.Next() {
		var vid string
		var odo float64
		var ts time.Time
		if err := openRows.Scan(&vid, &odo, &ts); err != nil {
			return nil, 0, "", err
		}
		if b, ok := openTS[vid]; !ok || ts.After(b) || (ts.Equal(b) && odo > openOdo[vid]) {
			openTS[vid] = ts
			openOdo[vid] = odo
		}
	}
	if err := openRows.Err(); err != nil {
		return nil, 0, "", err
	}

	var dtos []KMPLSummaryDTO
	for _, vid := range order {
		fills := byVehicle[vid]
		d := KMPLSummaryDTO{
			VehicleNo: fills[0].vehNo,
			Month:     month,
			Fills:     len(fills),
		}
		if fills[0].norm.Valid && fills[0].norm.Float64 > 0 {
			n := fills[0].norm.Float64
			d.StandardKmpl = &n
		}
		opening, openIdx := 0.0, -1
		if o, ok := openOdo[vid]; ok {
			opening = o
		} else {
			// No pre-period fill: first in-period fill with an odometer opens.
			for i, fl := range fills {
				if fl.odo.Valid && fl.odo.Float64 > 0 {
					opening, openIdx = fl.odo.Float64, i
					break
				}
			}
		}
		fuel := 0.0
		lastOdo := 0.0
		for i, fl := range fills {
			if i == openIdx {
				continue // opening fill's own litres belong to prior consumption
			}
			if fl.litres > 0 {
				fuel += fl.litres
			}
			if fl.odo.Valid && fl.odo.Float64 > 0 {
				lastOdo = fl.odo.Float64
			}
		}
		if dist := lastOdo - opening; opening > 0 && dist > 0 {
			d.DistanceKM = &dist
			if fuel > 0 {
				k := dist / fuel
				d.KMPL = &k
				if d.StandardKmpl != nil && *d.StandardKmpl > 0 {
					v := (k - *d.StandardKmpl) / *d.StandardKmpl * 100
					d.VariancePct = &v
					if k < *d.StandardKmpl {
						d.Flag = "BELOW_NORM"
					}
				}
			}
		}
		// Litres reported = fuel consumed in-period (opening fill excluded).
		d.Litres = fuel
		dtos = append(dtos, d)
	}
	return dtos, int64(len(dtos)), month, nil
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
	page := []KMPLSummaryDTO{}
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
