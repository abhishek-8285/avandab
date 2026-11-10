// Package fleetreports owns the SAP ZMOTM_MR read models (vehicle master and
// gate register). HTTP handlers parse requests and render CSV/JSON; the queries
// live here so internal/handlers/ stays an HTTP + composition adapter layer
// (docs/tech-specs/handlers-end-state-adr.md, Phase 1).
package fleetreports

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	db "transport-app/db/generated/sqlite"
	appdb "transport-app/internal/database"
)

// Filter is the shared read-model filter for report pages.
type Filter struct {
	Query  string
	Status string
}

// Service runs ZMOTM_MR read queries.
type Service struct {
	db *sql.DB
}

// New builds a read-model service over the app database.
func New(database *sql.DB) *Service {
	return &Service{db: database}
}

// VehicleMasterRow is one row of the vehicle master report (SAP ZMOTM_MR p.18).
type VehicleMasterRow struct {
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

// VehicleMaster returns the vehicle master report page.
func (s *Service) VehicleMaster(ctx context.Context, tenantID string, f Filter, limit, offset int) ([]VehicleMasterRow, int64, error) {
	if s == nil || s.db == nil {
		return []VehicleMasterRow{}, 0, nil
	}
	q := db.New(s.db)
	rows, err := q.SearchVehicles(ctx, db.SearchVehiclesParams{
		TenantID:      tenantID,
		Search:        f.Query,
		StatusAll:     "",
		Status:        f.Status,
		FleetClassAll: "",
		FleetClass:    "",
		OwnershipAll:  "",
		Ownership:     "",
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := q.CountVehicles(ctx, db.CountVehiclesParams{
		TenantID:      tenantID,
		Search:        f.Query,
		StatusAll:     "",
		Status:        f.Status,
		FleetClassAll: "",
		FleetClass:    "",
		OwnershipAll:  "",
		Ownership:     "",
	})
	if err != nil {
		return nil, 0, err
	}

	out := make([]VehicleMasterRow, 0, len(rows))
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
		out = append(out, VehicleMasterRow{
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
	return out, total, nil
}

// GateRegisterRow is one row of the gate register report (SAP ZMOTM_MR p.18).
type GateRegisterRow struct {
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

// GateRegister returns the gate register report page.
func (s *Service) GateRegister(ctx context.Context, tenantID string, f Filter, limit, offset int) ([]GateRegisterRow, int64, error) {
	if s == nil || s.db == nil {
		return []GateRegisterRow{}, 0, nil
	}
	countSQL := `
SELECT COUNT(*)
FROM trips t
WHERE t.tenant_id = ?
  AND (? = '' OR t.trip_number LIKE ?)`
	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	qPattern := "%" + f.Query + "%"
	var total int64
	if err := s.db.QueryRowContext(ctx, reboundCount, tenantID, f.Query, qPattern).Scan(&total); err != nil {
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
	rows, err := s.db.QueryContext(ctx, rebound, tenantID, f.Query, qPattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var out []GateRegisterRow
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
		if startOdo.Valid {
			v := startOdo.Float64
			startVal = &v
		}
		if closeOdo.Valid {
			v := closeOdo.Float64
			closeVal = &v
		}
		dist := 0.0
		if startVal != nil && closeVal != nil && *closeVal >= *startVal && *startVal > 0 {
			dist = *closeVal - *startVal
		} else if status == "completed" && routeDist > 0 {
			dist = routeDist
		}
		out = append(out, GateRegisterRow{
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
	return out, total, rows.Err()
}

// FuelKMPLRow is one row of the fuel & KMPL report (SAP ZMOTM_MR p.19).
type FuelKMPLRow struct {
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

// FuelKMPL returns the fuel & KMPL report page.
func (s *Service) FuelKMPL(ctx context.Context, tenantID string, limit, offset int) ([]FuelKMPLRow, int64, error) {
	if s == nil || s.db == nil {
		return []FuelKMPLRow{}, 0, nil
	}
	countSQL := `SELECT COUNT(*) FROM fuel_issues WHERE tenant_id = ?`
	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, reboundCount, tenantID).Scan(&total); err != nil {
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
	rows, err := s.db.QueryContext(ctx, rebound, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

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

	// B13 pagination-carryover: page 2+ seeds each vehicle's previous odometer
	// from the last fill BEFORE the page so KMPL is not blank on first row.
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
		seedRows, err := s.db.QueryContext(ctx, reboundSeed, seedArgs...)
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

	out := make([]FuelKMPLRow, 0, len(fills))
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
		out = append(out, FuelKMPLRow{
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
	return out, total, nil
}

// BreakdownRow is one row of the breakdown/outstanding-notification report.
type BreakdownRow struct {
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

// Breakdown returns breakdown alerts plus outstanding job cards.
func (s *Service) Breakdown(ctx context.Context, tenantID string, limit, offset int) ([]BreakdownRow, int64, error) {
	if s == nil || s.db == nil {
		return []BreakdownRow{}, 0, nil
	}
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
	rows, err := s.db.QueryContext(ctx, reboundAlerts, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var out []BreakdownRow
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
		out = append(out, BreakdownRow{
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
		woRows, qErr := s.db.QueryContext(ctx, reboundWO, tenantID, limit, offset)
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
					out = append(out, BreakdownRow{
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
	return out, int64(len(out)), nil
}

// KMPLSummaryRow is one vehicle × month KMPL summary row (B13).
type KMPLSummaryRow struct {
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

// parseMonthParam validates ?month=YYYY-MM (defaults to the current UTC month).
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

// KMPLSummary returns per-vehicle tank-to-tank KMPL for one month.
func (s *Service) KMPLSummary(ctx context.Context, tenantID, monthParam, vehicleFilter string) ([]KMPLSummaryRow, int64, string, error) {
	if s == nil || s.db == nil {
		return []KMPLSummaryRow{}, 0, "", nil
	}
	monthStart, month, err := parseMonthParam(monthParam)
	if err != nil {
		return nil, 0, "", fmt.Errorf("invalid month %q: want YYYY-MM", monthParam)
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	startStr := monthStart.Format("2006-01-02 15:04:05")
	endStr := monthEnd.Format("2006-01-02 15:04:05")

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
	rows, err := s.db.QueryContext(ctx, reboundFill, fillArgs...)
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
	var order []string
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
	openRows, err := s.db.QueryContext(ctx, reboundOpen, openArgs...)
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

	var out []KMPLSummaryRow
	for _, vid := range order {
		fills := byVehicle[vid]
		d := KMPLSummaryRow{
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
				continue
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
		d.Litres = fuel
		out = append(out, d)
	}
	return out, int64(len(out)), month, nil
}
