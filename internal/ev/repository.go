package ev

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Repository interface {
	UpsertProfile(ctx context.Context, profile *EVProfile) error
	GetProfile(ctx context.Context, tenantID, vehicleID string) (*EVProfile, error)
	UpsertTelemetry(ctx context.Context, telem *EVTelemetry) error
	GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*EVTelemetry, error)
	ListEVVehicles(ctx context.Context, tenantID string) ([]EVVehicleStatus, error)
	GetActiveChargingSession(ctx context.Context, tenantID, vehicleID string) (*ChargingSession, error)
	CreateChargingSession(ctx context.Context, sess *ChargingSession) error
	UpdateChargingSession(ctx context.Context, sess *ChargingSession) error
	ListChargingSessions(ctx context.Context, tenantID, vehicleID string, limit, offset int) ([]ChargingSession, int, error)
	RecordEnergySummary(ctx context.Context, summary *EVEnergySummary) error
	GetFleetMetrics(ctx context.Context, tenantID string) (*EVFleetMetrics, error)
	ListRecentEnergySummaries(ctx context.Context, tenantID string, limit int) ([]EVEnergySummary, error)
}

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) UpsertProfile(ctx context.Context, p *EVProfile) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	query := `INSERT INTO vehicle_ev_profiles (
		vehicle_id, tenant_id, battery_capacity_kwh, max_range_km,
		charging_standard, min_safe_soc, nominal_efficiency_wh_km, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (vehicle_id) DO UPDATE SET
		battery_capacity_kwh = EXCLUDED.battery_capacity_kwh,
		max_range_km = EXCLUDED.max_range_km,
		charging_standard = EXCLUDED.charging_standard,
		min_safe_soc = EXCLUDED.min_safe_soc,
		nominal_efficiency_wh_km = EXCLUDED.nominal_efficiency_wh_km,
		updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, query,
		p.VehicleID, p.TenantID, p.BatteryCapacityKWh, p.MaxRangeKM,
		p.ChargingStandard, p.MinSafeSoC, p.NominalEfficiencyWhKM, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetProfile(ctx context.Context, tenantID, vehicleID string) (*EVProfile, error) {
	query := `SELECT vehicle_id, tenant_id, battery_capacity_kwh, max_range_km,
		charging_standard, min_safe_soc, nominal_efficiency_wh_km, created_at, updated_at
	FROM vehicle_ev_profiles
	WHERE tenant_id = $1 AND vehicle_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var p EVProfile
	err := row.Scan(
		&p.VehicleID, &p.TenantID, &p.BatteryCapacityKWh, &p.MaxRangeKM,
		&p.ChargingStandard, &p.MinSafeSoC, &p.NominalEfficiencyWhKM, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *SQLRepository) UpsertTelemetry(ctx context.Context, t *EVTelemetry) error {
	if t.LastSyncAt.IsZero() {
		t.LastSyncAt = time.Now().UTC()
	}

	query := `INSERT INTO vehicle_ev_telemetry_latest (
		vehicle_id, tenant_id, soc_percent, soh_percent, battery_temp_c,
		remaining_range_km, charging_status, charging_power_kw,
		current_draw_a, voltage_v, last_sync_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (vehicle_id) DO UPDATE SET
		soc_percent = EXCLUDED.soc_percent,
		soh_percent = EXCLUDED.soh_percent,
		battery_temp_c = EXCLUDED.battery_temp_c,
		remaining_range_km = EXCLUDED.remaining_range_km,
		charging_status = EXCLUDED.charging_status,
		charging_power_kw = EXCLUDED.charging_power_kw,
		current_draw_a = EXCLUDED.current_draw_a,
		voltage_v = EXCLUDED.voltage_v,
		last_sync_at = EXCLUDED.last_sync_at`

	_, err := r.db.ExecContext(ctx, query,
		t.VehicleID, t.TenantID, t.SoCPercent, t.SoHPercent, t.BatteryTempC,
		t.RemainingRangeKM, t.ChargingStatus, t.ChargingPowerKW,
		t.CurrentDrawA, t.VoltageV, t.LastSyncAt,
	)
	return err
}

func (r *SQLRepository) GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*EVTelemetry, error) {
	query := `SELECT vehicle_id, tenant_id, soc_percent, soh_percent, battery_temp_c,
		remaining_range_km, charging_status, charging_power_kw,
		current_draw_a, voltage_v, last_sync_at
	FROM vehicle_ev_telemetry_latest
	WHERE tenant_id = $1 AND vehicle_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var t EVTelemetry
	err := row.Scan(
		&t.VehicleID, &t.TenantID, &t.SoCPercent, &t.SoHPercent, &t.BatteryTempC,
		&t.RemainingRangeKM, &t.ChargingStatus, &t.ChargingPowerKW,
		&t.CurrentDrawA, &t.VoltageV, &t.LastSyncAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *SQLRepository) ListEVVehicles(ctx context.Context, tenantID string) ([]EVVehicleStatus, error) {
	query := `SELECT
		v.id, v.registration_number, v.vehicle_number, v.vehicle_type, v.fuel_type, v.status,
		p.battery_capacity_kwh, p.max_range_km, p.charging_standard, p.min_safe_soc, p.nominal_efficiency_wh_km, p.created_at, p.updated_at,
		t.soc_percent, t.soh_percent, t.battery_temp_c, t.remaining_range_km, t.charging_status, t.charging_power_kw, t.current_draw_a, t.voltage_v, t.last_sync_at
	FROM vehicles v
	LEFT JOIN vehicle_ev_profiles p ON v.id = p.vehicle_id AND v.tenant_id = p.tenant_id
	LEFT JOIN vehicle_ev_telemetry_latest t ON v.id = t.vehicle_id AND v.tenant_id = t.tenant_id
	WHERE v.tenant_id = $1 AND (p.vehicle_id IS NOT NULL OR v.fuel_type = 'electric')
	ORDER BY v.vehicle_number ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []EVVehicleStatus
	for rows.Next() {
		var (
			v EVVehicleStatus
			// Profile nullable fields
			pCap, pRange, pSafeSoC, pEff *float64
			pStd                         *string
			pCreated, pUpdated           *time.Time
			// Telemetry nullable fields
			tSoC, tSoH, tTemp, tRange *float64
			tStatus                   *string
			tPower, tCurrent, tVolt   *float64
			tLastSync                 *time.Time
		)

		err := rows.Scan(
			&v.VehicleID, &v.RegistrationNumber, &v.VehicleNumber, &v.VehicleType, &v.FuelType, &v.Status,
			&pCap, &pRange, &pStd, &pSafeSoC, &pEff, &pCreated, &pUpdated,
			&tSoC, &tSoH, &tTemp, &tRange, &tStatus, &tPower, &tCurrent, &tVolt, &tLastSync,
		)
		if err != nil {
			return nil, err
		}

		if pCap != nil && pRange != nil && pStd != nil {
			v.Profile = &EVProfile{
				VehicleID:             v.VehicleID,
				TenantID:              tenantID,
				BatteryCapacityKWh:    *pCap,
				MaxRangeKM:            *pRange,
				ChargingStandard:      *pStd,
				MinSafeSoC:            *pSafeSoC,
				NominalEfficiencyWhKM: *pEff,
				CreatedAt:             *pCreated,
				UpdatedAt:             *pUpdated,
			}
		}

		if tSoC != nil && tStatus != nil {
			v.Telemetry = &EVTelemetry{
				VehicleID:        v.VehicleID,
				TenantID:         tenantID,
				SoCPercent:       *tSoC,
				SoHPercent:       *tSoH,
				BatteryTempC:     tTemp,
				RemainingRangeKM: *tRange,
				ChargingStatus:   *tStatus,
				ChargingPowerKW:  *tPower,
				CurrentDrawA:     *tCurrent,
				VoltageV:         *tVolt,
				LastSyncAt:       *tLastSync,
			}

			minSafe := 15.0
			if v.Profile != nil {
				minSafe = v.Profile.MinSafeSoC
			}
			if *tSoC < minSafe {
				v.IsLowBattery = true
			}
		}

		results = append(results, v)
	}

	return results, rows.Err()
}

func (r *SQLRepository) GetActiveChargingSession(ctx context.Context, tenantID, vehicleID string) (*ChargingSession, error) {
	query := `SELECT id, tenant_id, vehicle_id, facility_id, start_time, end_time,
		start_soc, end_soc, energy_added_kwh, peak_power_kw, charger_type,
		estimated_cost, status, created_at, updated_at
	FROM ev_charging_sessions
	WHERE tenant_id = $1 AND vehicle_id = $2 AND status = 'IN_PROGRESS'
	ORDER BY start_time DESC LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var s ChargingSession
	err := row.Scan(
		&s.ID, &s.TenantID, &s.VehicleID, &s.FacilityID, &s.StartTime, &s.EndTime,
		&s.StartSoC, &s.EndSoC, &s.EnergyAddedKWh, &s.PeakPowerKW, &s.ChargerType,
		&s.EstimatedCost, &s.Status, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SQLRepository) CreateChargingSession(ctx context.Context, s *ChargingSession) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now

	query := `INSERT INTO ev_charging_sessions (
		id, tenant_id, vehicle_id, facility_id, start_time, end_time,
		start_soc, end_soc, energy_added_kwh, peak_power_kw, charger_type,
		estimated_cost, status, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.TenantID, s.VehicleID, s.FacilityID, s.StartTime, s.EndTime,
		s.StartSoC, s.EndSoC, s.EnergyAddedKWh, s.PeakPowerKW, s.ChargerType,
		s.EstimatedCost, s.Status, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) UpdateChargingSession(ctx context.Context, s *ChargingSession) error {
	s.UpdatedAt = time.Now().UTC()

	query := `UPDATE ev_charging_sessions SET
		facility_id = $1,
		end_time = $2,
		end_soc = $3,
		energy_added_kwh = $4,
		peak_power_kw = $5,
		charger_type = $6,
		estimated_cost = $7,
		status = $8,
		updated_at = $9
	WHERE tenant_id = $10 AND id = $11`

	_, err := r.db.ExecContext(ctx, query,
		s.FacilityID, s.EndTime, s.EndSoC, s.EnergyAddedKWh, s.PeakPowerKW,
		s.ChargerType, s.EstimatedCost, s.Status, s.UpdatedAt,
		s.TenantID, s.ID,
	)
	return err
}

func (r *SQLRepository) ListChargingSessions(ctx context.Context, tenantID, vehicleID string, limit, offset int) ([]ChargingSession, int, error) {
	if limit <= 0 {
		limit = 20
	}

	whereClause := "WHERE s.tenant_id = $1"
	args := []interface{}{tenantID}

	if vehicleID != "" {
		args = append(args, vehicleID)
		whereClause += " AND s.vehicle_id = $" + string(rune('0'+len(args)))
	}

	countQuery := `SELECT COUNT(*) FROM ev_charging_sessions s ` + whereClause
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT
		s.id, s.tenant_id, s.vehicle_id, s.facility_id, s.start_time, s.end_time,
		s.start_soc, s.end_soc, s.energy_added_kwh, s.peak_power_kw, s.charger_type,
		s.estimated_cost, s.status, s.created_at, s.updated_at,
		f.name, v.vehicle_number
	FROM ev_charging_sessions s
	LEFT JOIN facilities f ON s.facility_id = f.id AND s.tenant_id = f.tenant_id
	LEFT JOIN vehicles v ON s.vehicle_id = v.id AND s.tenant_id = v.tenant_id
	` + whereClause + `
	ORDER BY s.start_time DESC
	LIMIT $` + string(rune('0'+len(args)+1)) + ` OFFSET $` + string(rune('0'+len(args)+2))

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []ChargingSession
	for rows.Next() {
		var s ChargingSession
		var fName, vNum *string
		err := rows.Scan(
			&s.ID, &s.TenantID, &s.VehicleID, &s.FacilityID, &s.StartTime, &s.EndTime,
			&s.StartSoC, &s.EndSoC, &s.EnergyAddedKWh, &s.PeakPowerKW, &s.ChargerType,
			&s.EstimatedCost, &s.Status, &s.CreatedAt, &s.UpdatedAt,
			&fName, &vNum,
		)
		if err != nil {
			return nil, 0, err
		}
		s.FacilityName = fName
		s.VehicleNumber = vNum
		sessions = append(sessions, s)
	}

	return sessions, total, rows.Err()
}

func (r *SQLRepository) RecordEnergySummary(ctx context.Context, s *EVEnergySummary) error {
	if s.RecordedAt.IsZero() {
		s.RecordedAt = time.Now().UTC()
	}

	query := `INSERT INTO ev_energy_summaries (
		id, tenant_id, vehicle_id, trip_id, distance_km,
		energy_consumed_kwh, efficiency_wh_km, co2_avoided_kg, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.TenantID, s.VehicleID, s.TripID, s.DistanceKM,
		s.EnergyConsumedKWh, s.EfficiencyWhKM, s.CO2AvoidedKG, s.RecordedAt,
	)
	return err
}

func (r *SQLRepository) GetFleetMetrics(ctx context.Context, tenantID string) (*EVFleetMetrics, error) {
	metrics := &EVFleetMetrics{}

	// 1. Vehicle counts and telemetry aggregates.
	// FleetAvgSoH reports 0 (= no data) when no real SoH measurement exists —
	// never a fabricated 100% fleet health (P0 "stop fake data").
	queryVehicles := `SELECT
		COUNT(DISTINCT v.id),
		COALESCE(SUM(CASE WHEN t.charging_status IN ('CHARGING', 'FAST_CHARGING') THEN 1 ELSE 0 END), 0),
		COALESCE(AVG(t.soc_percent), 0.0),
		COALESCE(AVG(t.soh_percent), 0.0),
		COALESCE(SUM(CASE WHEN t.soc_percent < COALESCE(p.min_safe_soc, 15.0) THEN 1 ELSE 0 END), 0)
	FROM vehicles v
	LEFT JOIN vehicle_ev_profiles p ON v.id = p.vehicle_id AND v.tenant_id = p.tenant_id
	LEFT JOIN vehicle_ev_telemetry_latest t ON v.id = t.vehicle_id AND v.tenant_id = t.tenant_id
	WHERE v.tenant_id = $1 AND (p.vehicle_id IS NOT NULL OR v.fuel_type = 'electric')`

	err := r.db.QueryRowContext(ctx, queryVehicles, tenantID).Scan(
		&metrics.TotalEVs,
		&metrics.ActiveChargingCount,
		&metrics.FleetAvgSoC,
		&metrics.FleetAvgSoH,
		&metrics.LowBatteryAlerts,
	)
	if err != nil {
		return nil, err
	}

	// 2. Cumulative distance, energy, and carbon offset from summaries
	queryEnergy := `SELECT
		COALESCE(SUM(distance_km), 0.0),
		COALESCE(SUM(energy_consumed_kwh), 0.0),
		COALESCE(SUM(co2_avoided_kg), 0.0),
		COALESCE(AVG(CASE WHEN efficiency_wh_km > 0 THEN efficiency_wh_km ELSE NULL END), 0.0)
	FROM ev_energy_summaries
	WHERE tenant_id = $1`

	err = r.db.QueryRowContext(ctx, queryEnergy, tenantID).Scan(
		&metrics.TotalGreenDistanceKM,
		&metrics.TotalEnergyKWh,
		&metrics.TotalCO2AvoidedKG,
		&metrics.AvgEfficiencyWhKM,
	)
	if err != nil {
		return nil, err
	}

	return metrics, nil
}

func (r *SQLRepository) ListRecentEnergySummaries(ctx context.Context, tenantID string, limit int) ([]EVEnergySummary, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, tenant_id, vehicle_id, trip_id, distance_km,
		energy_consumed_kwh, efficiency_wh_km, co2_avoided_kg, recorded_at
	FROM ev_energy_summaries
	WHERE tenant_id = $1
	ORDER BY recorded_at DESC
	LIMIT $2`

	rows, err := r.db.QueryContext(ctx, query, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []EVEnergySummary
	for rows.Next() {
		var s EVEnergySummary
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.VehicleID, &s.TripID, &s.DistanceKM,
			&s.EnergyConsumedKWh, &s.EfficiencyWhKM, &s.CO2AvoidedKG, &s.RecordedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, s)
	}

	return results, rows.Err()
}
