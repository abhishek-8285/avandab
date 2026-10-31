package coldchain

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Repository interface {
	UpsertProfile(ctx context.Context, profile *ReeferProfile) error
	GetProfile(ctx context.Context, tenantID, vehicleID string) (*ReeferProfile, error)
	UpsertTelemetry(ctx context.Context, telem *ReeferTelemetry) error
	GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*ReeferTelemetry, error)
	ListReeferVehicles(ctx context.Context, tenantID string) ([]ColdChainVehicleStatus, error)
	GetActiveExcursion(ctx context.Context, tenantID, vehicleID string, zoneNumber int) (*TemperatureExcursion, error)
	CreateExcursion(ctx context.Context, exc *TemperatureExcursion) error
	UpdateExcursion(ctx context.Context, exc *TemperatureExcursion) error
	ListExcursions(ctx context.Context, tenantID, vehicleID, status string, limit, offset int) ([]TemperatureExcursion, int, error)
	GetExcursion(ctx context.Context, tenantID, excursionID string) (*TemperatureExcursion, error)
	CreateTripSummary(ctx context.Context, summary *ReeferTripSummary) error
	GetTripSummary(ctx context.Context, tenantID, tripID string) (*ReeferTripSummary, error)
	ListTripSummaries(ctx context.Context, tenantID string, limit, offset int) ([]ReeferTripSummary, int, error)
	GetFleetMetrics(ctx context.Context, tenantID string) (*ColdChainFleetMetrics, error)
}

type SQLRepository struct {
	db *sql.DB
}

func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) UpsertProfile(ctx context.Context, p *ReeferProfile) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	query := `INSERT INTO vehicle_reefer_profiles (
		vehicle_id, tenant_id, reefer_make_model, num_zones,
		zone1_type, zone1_target_temp_c, zone1_min_temp_c, zone1_max_temp_c,
		zone2_type, zone2_target_temp_c, zone2_min_temp_c, zone2_max_temp_c,
		humidity_monitoring, target_humidity_rh, defrost_mode,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	ON CONFLICT (vehicle_id) DO UPDATE SET
		reefer_make_model = EXCLUDED.reefer_make_model,
		num_zones = EXCLUDED.num_zones,
		zone1_type = EXCLUDED.zone1_type,
		zone1_target_temp_c = EXCLUDED.zone1_target_temp_c,
		zone1_min_temp_c = EXCLUDED.zone1_min_temp_c,
		zone1_max_temp_c = EXCLUDED.zone1_max_temp_c,
		zone2_type = EXCLUDED.zone2_type,
		zone2_target_temp_c = EXCLUDED.zone2_target_temp_c,
		zone2_min_temp_c = EXCLUDED.zone2_min_temp_c,
		zone2_max_temp_c = EXCLUDED.zone2_max_temp_c,
		humidity_monitoring = EXCLUDED.humidity_monitoring,
		target_humidity_rh = EXCLUDED.target_humidity_rh,
		defrost_mode = EXCLUDED.defrost_mode,
		updated_at = EXCLUDED.updated_at`

	humidityInt := 0
	if p.HumidityMonitoring {
		humidityInt = 1
	}

	_, err := r.db.ExecContext(ctx, query,
		p.VehicleID, p.TenantID, p.ReeferMakeModel, p.NumZones,
		p.Zone1Type, p.Zone1TargetTempC, p.Zone1MinTempC, p.Zone1MaxTempC,
		p.Zone2Type, p.Zone2TargetTempC, p.Zone2MinTempC, p.Zone2MaxTempC,
		humidityInt, p.TargetHumidityRH, p.DefrostMode,
		p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetProfile(ctx context.Context, tenantID, vehicleID string) (*ReeferProfile, error) {
	query := `SELECT
		vehicle_id, tenant_id, reefer_make_model, num_zones,
		zone1_type, zone1_target_temp_c, zone1_min_temp_c, zone1_max_temp_c,
		zone2_type, zone2_target_temp_c, zone2_min_temp_c, zone2_max_temp_c,
		humidity_monitoring, target_humidity_rh, defrost_mode,
		created_at, updated_at
	FROM vehicle_reefer_profiles
	WHERE tenant_id = $1 AND vehicle_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var p ReeferProfile
	var humidityInt int
	err := row.Scan(
		&p.VehicleID, &p.TenantID, &p.ReeferMakeModel, &p.NumZones,
		&p.Zone1Type, &p.Zone1TargetTempC, &p.Zone1MinTempC, &p.Zone1MaxTempC,
		&p.Zone2Type, &p.Zone2TargetTempC, &p.Zone2MinTempC, &p.Zone2MaxTempC,
		&humidityInt, &p.TargetHumidityRH, &p.DefrostMode,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.HumidityMonitoring = humidityInt == 1
	return &p, nil
}

func (r *SQLRepository) UpsertTelemetry(ctx context.Context, t *ReeferTelemetry) error {
	if t.LastSyncAt.IsZero() {
		t.LastSyncAt = time.Now().UTC()
	}

	query := `INSERT INTO vehicle_reefer_telemetry_latest (
		vehicle_id, tenant_id, reefer_power_status, reefer_mode,
		zone1_temp_c, zone1_setpoint_c, zone2_temp_c, zone2_setpoint_c,
		humidity_rh, door_status, door_open_duration_sec,
		reefer_fuel_level_pct, reefer_battery_v, has_active_excursion,
		last_sync_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	ON CONFLICT (vehicle_id) DO UPDATE SET
		reefer_power_status = EXCLUDED.reefer_power_status,
		reefer_mode = EXCLUDED.reefer_mode,
		zone1_temp_c = EXCLUDED.zone1_temp_c,
		zone1_setpoint_c = EXCLUDED.zone1_setpoint_c,
		zone2_temp_c = EXCLUDED.zone2_temp_c,
		zone2_setpoint_c = EXCLUDED.zone2_setpoint_c,
		humidity_rh = EXCLUDED.humidity_rh,
		door_status = EXCLUDED.door_status,
		door_open_duration_sec = EXCLUDED.door_open_duration_sec,
		reefer_fuel_level_pct = EXCLUDED.reefer_fuel_level_pct,
		reefer_battery_v = EXCLUDED.reefer_battery_v,
		has_active_excursion = EXCLUDED.has_active_excursion,
		last_sync_at = EXCLUDED.last_sync_at`

	excursionInt := 0
	if t.HasActiveExcursion {
		excursionInt = 1
	}

	_, err := r.db.ExecContext(ctx, query,
		t.VehicleID, t.TenantID, t.ReeferPowerStatus, t.ReeferMode,
		t.Zone1TempC, t.Zone1SetpointC, t.Zone2TempC, t.Zone2SetpointC,
		t.HumidityRH, t.DoorStatus, t.DoorOpenDurationSec,
		t.ReeferFuelLevelPct, t.ReeferBatteryV, excursionInt,
		t.LastSyncAt,
	)
	return err
}

func (r *SQLRepository) GetLatestTelemetry(ctx context.Context, tenantID, vehicleID string) (*ReeferTelemetry, error) {
	query := `SELECT
		vehicle_id, tenant_id, reefer_power_status, reefer_mode,
		zone1_temp_c, zone1_setpoint_c, zone2_temp_c, zone2_setpoint_c,
		humidity_rh, door_status, door_open_duration_sec,
		reefer_fuel_level_pct, reefer_battery_v, has_active_excursion,
		last_sync_at
	FROM vehicle_reefer_telemetry_latest
	WHERE tenant_id = $1 AND vehicle_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var t ReeferTelemetry
	var excursionInt int
	err := row.Scan(
		&t.VehicleID, &t.TenantID, &t.ReeferPowerStatus, &t.ReeferMode,
		&t.Zone1TempC, &t.Zone1SetpointC, &t.Zone2TempC, &t.Zone2SetpointC,
		&t.HumidityRH, &t.DoorStatus, &t.DoorOpenDurationSec,
		&t.ReeferFuelLevelPct, &t.ReeferBatteryV, &excursionInt,
		&t.LastSyncAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.HasActiveExcursion = excursionInt == 1
	return &t, nil
}

func (r *SQLRepository) ListReeferVehicles(ctx context.Context, tenantID string) ([]ColdChainVehicleStatus, error) {
	query := `SELECT
		v.id, v.registration_number, v.vehicle_number, v.vehicle_type, v.status,
		p.reefer_make_model, p.num_zones, p.zone1_type, p.zone1_target_temp_c, p.zone1_min_temp_c, p.zone1_max_temp_c,
		p.zone2_type, p.zone2_target_temp_c, p.zone2_min_temp_c, p.zone2_max_temp_c,
		p.humidity_monitoring, p.target_humidity_rh, p.defrost_mode, p.created_at, p.updated_at,
		t.reefer_power_status, t.reefer_mode, t.zone1_temp_c, t.zone1_setpoint_c, t.zone2_temp_c, t.zone2_setpoint_c,
		t.humidity_rh, t.door_status, t.door_open_duration_sec, t.reefer_fuel_level_pct, t.reefer_battery_v, t.has_active_excursion, t.last_sync_at,
		(SELECT COUNT(*) FROM reefer_temperature_excursions e WHERE e.vehicle_id = v.id AND e.tenant_id = v.tenant_id AND e.status IN ('OPEN', 'ACKNOWLEDGED')) as open_excursions
	FROM vehicles v
	INNER JOIN vehicle_reefer_profiles p ON v.id = p.vehicle_id AND v.tenant_id = p.tenant_id
	LEFT JOIN vehicle_reefer_telemetry_latest t ON v.id = t.vehicle_id AND v.tenant_id = t.tenant_id
	WHERE v.tenant_id = $1
	ORDER BY v.vehicle_number ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []ColdChainVehicleStatus
	for rows.Next() {
		var (
			v ColdChainVehicleStatus
			// Profile
			pMake, pZ1Type, pDefrost  *string
			pNumZ                     *int
			pZ1Target, pZ1Min, pZ1Max *float64
			pZ2Type                   *string
			pZ2Target, pZ2Min, pZ2Max *float64
			pHumidityMon              *int
			pTargetHumidity           *float64
			pCreated, pUpdated        *time.Time
			// Telemetry
			tPower, tMode   *string
			tZ1Temp, tZ1Set *float64
			tZ2Temp, tZ2Set *float64
			tHumidity       *float64
			tDoor           *string
			tDoorDuration   *int
			tFuel, tBatt    *float64
			tExcursion      *int
			tLastSync       *time.Time
			openExc         int
		)

		err := rows.Scan(
			&v.VehicleID, &v.RegistrationNumber, &v.VehicleNumber, &v.VehicleType, &v.Status,
			&pMake, &pNumZ, &pZ1Type, &pZ1Target, &pZ1Min, &pZ1Max,
			&pZ2Type, &pZ2Target, &pZ2Min, &pZ2Max,
			&pHumidityMon, &pTargetHumidity, &pDefrost, &pCreated, &pUpdated,
			&tPower, &tMode, &tZ1Temp, &tZ1Set, &tZ2Temp, &tZ2Set,
			&tHumidity, &tDoor, &tDoorDuration, &tFuel, &tBatt, &tExcursion, &tLastSync,
			&openExc,
		)
		if err != nil {
			return nil, err
		}

		if pMake != nil {
			humMon := false
			if pHumidityMon != nil && *pHumidityMon == 1 {
				humMon = true
			}
			v.Profile = &ReeferProfile{
				VehicleID:          v.VehicleID,
				TenantID:           tenantID,
				ReeferMakeModel:    *pMake,
				NumZones:           *pNumZ,
				Zone1Type:          *pZ1Type,
				Zone1TargetTempC:   *pZ1Target,
				Zone1MinTempC:      *pZ1Min,
				Zone1MaxTempC:      *pZ1Max,
				Zone2Type:          pZ2Type,
				Zone2TargetTempC:   pZ2Target,
				Zone2MinTempC:      pZ2Min,
				Zone2MaxTempC:      pZ2Max,
				HumidityMonitoring: humMon,
				TargetHumidityRH:   pTargetHumidity,
				DefrostMode:        *pDefrost,
				CreatedAt:          *pCreated,
				UpdatedAt:          *pUpdated,
			}
		}

		if tPower != nil && tZ1Temp != nil {
			hasExc := false
			if tExcursion != nil && *tExcursion == 1 {
				hasExc = true
			}
			v.Telemetry = &ReeferTelemetry{
				VehicleID:           v.VehicleID,
				TenantID:            tenantID,
				ReeferPowerStatus:   *tPower,
				ReeferMode:          *tMode,
				Zone1TempC:          *tZ1Temp,
				Zone1SetpointC:      *tZ1Set,
				Zone2TempC:          tZ2Temp,
				Zone2SetpointC:      tZ2Set,
				HumidityRH:          tHumidity,
				DoorStatus:          *tDoor,
				DoorOpenDurationSec: *tDoorDuration,
				ReeferFuelLevelPct:  *tFuel,
				ReeferBatteryV:      *tBatt,
				HasActiveExcursion:  hasExc,
				LastSyncAt:          *tLastSync,
			}
			if *tDoor == DoorOpen {
				v.IsDoorOpen = true
			}
		}

		v.ActiveExcursionsCount = openExc
		v.IsInExcursion = openExc > 0 || (v.Telemetry != nil && v.Telemetry.HasActiveExcursion)

		results = append(results, v)
	}

	return results, rows.Err()
}

func (r *SQLRepository) GetActiveExcursion(ctx context.Context, tenantID, vehicleID string, zoneNumber int) (*TemperatureExcursion, error) {
	query := `SELECT
		id, tenant_id, vehicle_id, trip_id, zone_number, excursion_type,
		severity, start_time, end_time, duration_seconds, peak_temp_c,
		threshold_temp_c, setpoint_c, status, resolution_note, resolved_by,
		created_at, updated_at
	FROM reefer_temperature_excursions
	WHERE tenant_id = $1 AND vehicle_id = $2 AND zone_number = $3 AND status IN ('OPEN', 'ACKNOWLEDGED')
	ORDER BY start_time DESC LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID, zoneNumber)
	var e TemperatureExcursion
	err := row.Scan(
		&e.ID, &e.TenantID, &e.VehicleID, &e.TripID, &e.ZoneNumber, &e.ExcursionType,
		&e.Severity, &e.StartTime, &e.EndTime, &e.DurationSeconds, &e.PeakTempC,
		&e.ThresholdTempC, &e.SetpointC, &e.Status, &e.ResolutionNote, &e.ResolvedBy,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *SQLRepository) CreateExcursion(ctx context.Context, e *TemperatureExcursion) error {
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now

	query := `INSERT INTO reefer_temperature_excursions (
		id, tenant_id, vehicle_id, trip_id, zone_number, excursion_type,
		severity, start_time, end_time, duration_seconds, peak_temp_c,
		threshold_temp_c, setpoint_c, status, resolution_note, resolved_by,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`

	_, err := r.db.ExecContext(ctx, query,
		e.ID, e.TenantID, e.VehicleID, e.TripID, e.ZoneNumber, e.ExcursionType,
		e.Severity, e.StartTime, e.EndTime, e.DurationSeconds, e.PeakTempC,
		e.ThresholdTempC, e.SetpointC, e.Status, e.ResolutionNote, e.ResolvedBy,
		e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) UpdateExcursion(ctx context.Context, e *TemperatureExcursion) error {
	e.UpdatedAt = time.Now().UTC()

	query := `UPDATE reefer_temperature_excursions SET
		end_time = $1,
		duration_seconds = $2,
		peak_temp_c = $3,
		status = $4,
		resolution_note = $5,
		resolved_by = $6,
		updated_at = $7
	WHERE tenant_id = $8 AND id = $9`

	_, err := r.db.ExecContext(ctx, query,
		e.EndTime, e.DurationSeconds, e.PeakTempC, e.Status,
		e.ResolutionNote, e.ResolvedBy, e.UpdatedAt,
		e.TenantID, e.ID,
	)
	return err
}

func (r *SQLRepository) ListExcursions(ctx context.Context, tenantID, vehicleID, status string, limit, offset int) ([]TemperatureExcursion, int, error) {
	if limit <= 0 {
		limit = 20
	}

	whereClause := "WHERE e.tenant_id = $1"
	args := []interface{}{tenantID}

	if vehicleID != "" {
		args = append(args, vehicleID)
		whereClause += " AND e.vehicle_id = $" + string(rune('0'+len(args)))
	}
	if status != "" {
		args = append(args, status)
		whereClause += " AND e.status = $" + string(rune('0'+len(args)))
	}

	countQuery := `SELECT COUNT(*) FROM reefer_temperature_excursions e ` + whereClause
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT
		e.id, e.tenant_id, e.vehicle_id, e.trip_id, e.zone_number, e.excursion_type,
		e.severity, e.start_time, e.end_time, e.duration_seconds, e.peak_temp_c,
		e.threshold_temp_c, e.setpoint_c, e.status, e.resolution_note, e.resolved_by,
		e.created_at, e.updated_at, v.vehicle_number
	FROM reefer_temperature_excursions e
	LEFT JOIN vehicles v ON e.vehicle_id = v.id AND e.tenant_id = v.tenant_id
	` + whereClause + `
	ORDER BY e.start_time DESC
	LIMIT $` + string(rune('0'+len(args)+1)) + ` OFFSET $` + string(rune('0'+len(args)+2))

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var excursions []TemperatureExcursion
	for rows.Next() {
		var e TemperatureExcursion
		var vNum *string
		err := rows.Scan(
			&e.ID, &e.TenantID, &e.VehicleID, &e.TripID, &e.ZoneNumber, &e.ExcursionType,
			&e.Severity, &e.StartTime, &e.EndTime, &e.DurationSeconds, &e.PeakTempC,
			&e.ThresholdTempC, &e.SetpointC, &e.Status, &e.ResolutionNote, &e.ResolvedBy,
			&e.CreatedAt, &e.UpdatedAt, &vNum,
		)
		if err != nil {
			return nil, 0, err
		}
		e.VehicleNumber = vNum
		excursions = append(excursions, e)
	}

	return excursions, total, rows.Err()
}

func (r *SQLRepository) GetExcursion(ctx context.Context, tenantID, excursionID string) (*TemperatureExcursion, error) {
	query := `SELECT
		e.id, e.tenant_id, e.vehicle_id, e.trip_id, e.zone_number, e.excursion_type,
		e.severity, e.start_time, e.end_time, e.duration_seconds, e.peak_temp_c,
		e.threshold_temp_c, e.setpoint_c, e.status, e.resolution_note, e.resolved_by,
		e.created_at, e.updated_at, v.vehicle_number
	FROM reefer_temperature_excursions e
	LEFT JOIN vehicles v ON e.vehicle_id = v.id AND e.tenant_id = v.tenant_id
	WHERE e.tenant_id = $1 AND e.id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, excursionID)
	var e TemperatureExcursion
	var vNum *string
	err := row.Scan(
		&e.ID, &e.TenantID, &e.VehicleID, &e.TripID, &e.ZoneNumber, &e.ExcursionType,
		&e.Severity, &e.StartTime, &e.EndTime, &e.DurationSeconds, &e.PeakTempC,
		&e.ThresholdTempC, &e.SetpointC, &e.Status, &e.ResolutionNote, &e.ResolvedBy,
		&e.CreatedAt, &e.UpdatedAt, &vNum,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.VehicleNumber = vNum
	return &e, nil
}

func (r *SQLRepository) CreateTripSummary(ctx context.Context, s *ReeferTripSummary) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}

	query := `INSERT INTO reefer_trip_summaries (
		id, tenant_id, trip_id, vehicle_id, start_time, end_time,
		zone1_min_temp_c, zone1_max_temp_c, zone1_mean_kinetic_temp_c,
		total_excursion_duration_sec, door_open_count, compliance_status,
		certified_at, certified_by, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.TenantID, s.TripID, s.VehicleID, s.StartTime, s.EndTime,
		s.Zone1MinTempC, s.Zone1MaxTempC, s.Zone1MeanKineticTempC,
		s.TotalExcursionDurationSec, s.DoorOpenCount, s.ComplianceStatus,
		s.CertifiedAt, s.CertifiedBy, s.CreatedAt,
	)
	return err
}

func (r *SQLRepository) GetTripSummary(ctx context.Context, tenantID, tripID string) (*ReeferTripSummary, error) {
	query := `SELECT
		id, tenant_id, trip_id, vehicle_id, start_time, end_time,
		zone1_min_temp_c, zone1_max_temp_c, zone1_mean_kinetic_temp_c,
		total_excursion_duration_sec, door_open_count, compliance_status,
		certified_at, certified_by, created_at
	FROM reefer_trip_summaries
	WHERE tenant_id = $1 AND trip_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, tripID)
	var s ReeferTripSummary
	err := row.Scan(
		&s.ID, &s.TenantID, &s.TripID, &s.VehicleID, &s.StartTime, &s.EndTime,
		&s.Zone1MinTempC, &s.Zone1MaxTempC, &s.Zone1MeanKineticTempC,
		&s.TotalExcursionDurationSec, &s.DoorOpenCount, &s.ComplianceStatus,
		&s.CertifiedAt, &s.CertifiedBy, &s.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SQLRepository) ListTripSummaries(ctx context.Context, tenantID string, limit, offset int) ([]ReeferTripSummary, int, error) {
	if limit <= 0 {
		limit = 20
	}

	countQuery := `SELECT COUNT(*) FROM reefer_trip_summaries WHERE tenant_id = $1`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT
		id, tenant_id, trip_id, vehicle_id, start_time, end_time,
		zone1_min_temp_c, zone1_max_temp_c, zone1_mean_kinetic_temp_c,
		total_excursion_duration_sec, door_open_count, compliance_status,
		certified_at, certified_by, created_at
	FROM reefer_trip_summaries
	WHERE tenant_id = $1
	ORDER BY start_time DESC
	LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var summaries []ReeferTripSummary
	for rows.Next() {
		var s ReeferTripSummary
		err := rows.Scan(
			&s.ID, &s.TenantID, &s.TripID, &s.VehicleID, &s.StartTime, &s.EndTime,
			&s.Zone1MinTempC, &s.Zone1MaxTempC, &s.Zone1MeanKineticTempC,
			&s.TotalExcursionDurationSec, &s.DoorOpenCount, &s.ComplianceStatus,
			&s.CertifiedAt, &s.CertifiedBy, &s.CreatedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		summaries = append(summaries, s)
	}

	return summaries, total, rows.Err()
}

func (r *SQLRepository) GetFleetMetrics(ctx context.Context, tenantID string) (*ColdChainFleetMetrics, error) {
	metrics := &ColdChainFleetMetrics{}

	query := `SELECT
		COUNT(DISTINCT p.vehicle_id),
		COALESCE(SUM(CASE WHEN t.reefer_mode = 'COOLING' AND t.reefer_power_status = 'ON' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN t.has_active_excursion = TRUE THEN 1 ELSE 0 END), 0),
		(SELECT COUNT(*) FROM reefer_temperature_excursions e WHERE e.tenant_id = $1 AND e.status IN ('OPEN', 'ACKNOWLEDGED')),
		COALESCE(SUM(CASE WHEN t.door_status = 'OPEN' THEN 1 ELSE 0 END), 0),
		COALESCE(AVG(t.zone1_temp_c), 0.0)
	FROM vehicle_reefer_profiles p
	LEFT JOIN vehicle_reefer_telemetry_latest t ON p.vehicle_id = t.vehicle_id AND p.tenant_id = t.tenant_id
	WHERE p.tenant_id = $1`

	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&metrics.TotalReefers,
		&metrics.ActiveCoolingCount,
		&metrics.InExcursionCount,
		&metrics.OpenExcursionsCount,
		&metrics.DoorOpenAlerts,
		&metrics.AvgZone1TempC,
	)
	if err != nil {
		return nil, err
	}

	// Compliance rate from trip summaries
	queryCompliance := `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN compliance_status = 'COMPLIANT' THEN 1 ELSE 0 END), 0)
	FROM reefer_trip_summaries
	WHERE tenant_id = $1`

	var totalTrips, compliantTrips int
	err = r.db.QueryRowContext(ctx, queryCompliance, tenantID).Scan(&totalTrips, &compliantTrips)
	if err == nil && totalTrips > 0 {
		metrics.FleetComplianceRatePct = float64(compliantTrips) / float64(totalTrips) * 100.0
	} else {
		metrics.FleetComplianceRatePct = 100.0 // Default 100% when no trips closed
	}

	return metrics, nil
}
