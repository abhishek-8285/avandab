// Package fleetreports owns the SAP ZMOTM_MR read models (vehicle master and
// gate register). HTTP handlers parse requests and render CSV/JSON; the queries
// live here so internal/handlers/ stays an HTTP + composition adapter layer
// (docs/tech-specs/handlers-end-state-adr.md, Phase 1).
package fleetreports

import (
	"context"
	"database/sql"

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
