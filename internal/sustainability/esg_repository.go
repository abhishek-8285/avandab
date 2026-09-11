package sustainability

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TripDetails contains trip operational data needed for carbon calculations.
type TripDetails struct {
	TripID             string
	TripNumber         string
	TenantID           string
	VehicleID          string
	VehicleNumber      string
	VehicleType        string
	FuelType           string
	DistanceKM         float64
	PayloadTonnes      float64
	FuelConsumedLitres float64
	CompletedAt        *time.Time
}

// AggregatedPeriodMetrics contains summarized trip metrics for a reporting window.
type AggregatedPeriodMetrics struct {
	TotalTrips      int
	TotalDistanceKM float64
	TotalCargoTKM   float64
	TotalFuelLitres float64
	TotalCO2eKG     float64
	AvgCO2ePerTKM   float64
	EVDistanceKM    float64
	BS6DistanceKM   float64
	BS4DistanceKM   float64
}

// ESGRepository defines storage contracts for carbon metrics and snapshots.
type ESGRepository interface {
	RecordTripESG(ctx context.Context, m TripESGMetrics) (*TripESGMetrics, error)
	GetTripESG(ctx context.Context, tenantID, tripID string) (*TripESGMetrics, error)
	GetTripDetails(ctx context.Context, tenantID, tripID string) (*TripDetails, error)
	ListSnapshots(ctx context.Context, tenantID string) ([]ESGEmissionSnapshot, error)
	GetSnapshot(ctx context.Context, tenantID, periodStart, periodEnd string) (*ESGEmissionSnapshot, error)
	SaveSnapshot(ctx context.Context, s ESGEmissionSnapshot) (*ESGEmissionSnapshot, error)
	AggregatePeriodTrips(ctx context.Context, tenantID, periodStart, periodEnd string) (*AggregatedPeriodMetrics, error)
}

// SQLESGRepository implements ESGRepository against SQLite / PostgreSQL.
type SQLESGRepository struct {
	db *sql.DB
}

// NewSQLESGRepository creates a new SQLESGRepository.
func NewSQLESGRepository(db *sql.DB) *SQLESGRepository {
	return &SQLESGRepository{db: db}
}

// RecordTripESG inserts or updates carbon metrics for a trip.
func (r *SQLESGRepository) RecordTripESG(ctx context.Context, m TripESGMetrics) (*TripESGMetrics, error) {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	m.CreatedAt = now

	query := `
		INSERT INTO trip_esg_metrics (
			id, tenant_id, trip_id, distance_km, payload_tonnes,
			fuel_consumed_litres, co2e_kg, co2e_per_tkm, emission_norm, methodology, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, trip_id) DO UPDATE SET
			distance_km = EXCLUDED.distance_km,
			payload_tonnes = EXCLUDED.payload_tonnes,
			fuel_consumed_litres = EXCLUDED.fuel_consumed_litres,
			co2e_kg = EXCLUDED.co2e_kg,
			co2e_per_tkm = EXCLUDED.co2e_per_tkm,
			emission_norm = EXCLUDED.emission_norm,
			methodology = EXCLUDED.methodology,
			created_at = EXCLUDED.created_at`

	_, err := r.db.ExecContext(ctx, query,
		m.ID, m.TenantID, m.TripID, m.DistanceKM, m.PayloadTonnes,
		m.FuelConsumedLitres, m.CO2eKG, m.CO2ePerTKM, string(m.EmissionNorm), string(m.Methodology), m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record trip esg metrics: %w", err)
	}

	return &m, nil
}

// GetTripESG retrieves recorded ESG metrics for a trip.
func (r *SQLESGRepository) GetTripESG(ctx context.Context, tenantID, tripID string) (*TripESGMetrics, error) {
	var m TripESGMetrics
	var norm, meth string

	query := `
		SELECT id, tenant_id, trip_id, distance_km, payload_tonnes,
		       fuel_consumed_litres, co2e_kg, co2e_per_tkm, emission_norm, methodology, created_at
		FROM trip_esg_metrics
		WHERE tenant_id = $1 AND trip_id = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, tripID).Scan(
		&m.ID, &m.TenantID, &m.TripID, &m.DistanceKM, &m.PayloadTonnes,
		&m.FuelConsumedLitres, &m.CO2eKG, &m.CO2ePerTKM, &norm, &meth, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	m.EmissionNorm = EmissionNorm(norm)
	m.Methodology = EmissionMethodology(meth)
	return &m, nil
}

// GetTripDetails loads operational details for carbon calculation.
func (r *SQLESGRepository) GetTripDetails(ctx context.Context, tenantID, tripID string) (*TripDetails, error) {
	var td TripDetails
	var vehID, vehNum, vehType, fuelType sql.NullString
	var routeDist, fuelLitres, cargoWeight sql.NullFloat64
	var completedAt sql.NullTime

	query := `
		SELECT t.id, t.trip_number, t.tenant_id,
		       t.vehicle_id, v.vehicle_number, v.vehicle_type, v.fuel_type,
		       r.distance, t.fuel_consumed_liters, b.cargo_weight, t.completed_at
		FROM trips t
		LEFT JOIN vehicles v ON t.vehicle_id = v.id AND v.tenant_id = t.tenant_id
		LEFT JOIN routes r ON t.route_id = r.id AND r.tenant_id = t.tenant_id
		LEFT JOIN bookings b ON t.booking_id = b.id AND b.tenant_id = t.tenant_id
		WHERE t.tenant_id = $1 AND t.id = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, tripID).Scan(
		&td.TripID, &td.TripNumber, &td.TenantID,
		&vehID, &vehNum, &vehType, &fuelType,
		&routeDist, &fuelLitres, &cargoWeight, &completedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("trip not found: %w", err)
	}

	if vehID.Valid {
		td.VehicleID = vehID.String
	}
	if vehNum.Valid {
		td.VehicleNumber = vehNum.String
	}
	if vehType.Valid {
		td.VehicleType = vehType.String
	}
	if fuelType.Valid {
		td.FuelType = fuelType.String
	}
	if routeDist.Valid {
		td.DistanceKM = routeDist.Float64
	}
	if fuelLitres.Valid {
		td.FuelConsumedLitres = fuelLitres.Float64
	}
	if cargoWeight.Valid {
		// Cargo weight in bookings can be in MT or kg; standard assumption is kg if > 100
		w := cargoWeight.Float64
		if w > 100 {
			td.PayloadTonnes = w / 1000.0
		} else {
			td.PayloadTonnes = w
		}
	}
	if td.PayloadTonnes <= 0 {
		td.PayloadTonnes = 10.0 // Default commercial truck payload
	}
	if completedAt.Valid {
		td.CompletedAt = &completedAt.Time
	}

	return &td, nil
}

// ListSnapshots returns all emission snapshots for a tenant in descending period order.
func (r *SQLESGRepository) ListSnapshots(ctx context.Context, tenantID string) ([]ESGEmissionSnapshot, error) {
	query := `
		SELECT id, tenant_id, period_start, period_end, total_trips, total_distance_km,
		       total_cargo_tkm, total_fuel_litres, total_co2e_kg, avg_co2e_per_tkm,
		       ev_distance_km, bs6_distance_km, bs4_distance_km, created_by, created_at
		FROM esg_emission_snapshots
		WHERE tenant_id = $1
		ORDER BY period_start DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []ESGEmissionSnapshot
	for rows.Next() {
		var s ESGEmissionSnapshot
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.PeriodStart, &s.PeriodEnd, &s.TotalTrips, &s.TotalDistanceKM,
			&s.TotalCargoTKM, &s.TotalFuelLitres, &s.TotalCO2eKG, &s.AvgCO2ePerTKM,
			&s.EVDistanceKM, &s.BS6DistanceKM, &s.BS4DistanceKM, &s.CreatedBy, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

// GetSnapshot fetches a specific periodic snapshot by period boundaries.
func (r *SQLESGRepository) GetSnapshot(ctx context.Context, tenantID, periodStart, periodEnd string) (*ESGEmissionSnapshot, error) {
	var s ESGEmissionSnapshot
	query := `
		SELECT id, tenant_id, period_start, period_end, total_trips, total_distance_km,
		       total_cargo_tkm, total_fuel_litres, total_co2e_kg, avg_co2e_per_tkm,
		       ev_distance_km, bs6_distance_km, bs4_distance_km, created_by, created_at
		FROM esg_emission_snapshots
		WHERE tenant_id = $1 AND period_start = $2 AND period_end = $3`

	err := r.db.QueryRowContext(ctx, query, tenantID, periodStart, periodEnd).Scan(
		&s.ID, &s.TenantID, &s.PeriodStart, &s.PeriodEnd, &s.TotalTrips, &s.TotalDistanceKM,
		&s.TotalCargoTKM, &s.TotalFuelLitres, &s.TotalCO2eKG, &s.AvgCO2ePerTKM,
		&s.EVDistanceKM, &s.BS6DistanceKM, &s.BS4DistanceKM, &s.CreatedBy, &s.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveSnapshot inserts or supersedes an emission snapshot for a period.
func (r *SQLESGRepository) SaveSnapshot(ctx context.Context, s ESGEmissionSnapshot) (*ESGEmissionSnapshot, error) {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	s.CreatedAt = now

	query := `
		INSERT INTO esg_emission_snapshots (
			id, tenant_id, period_start, period_end, total_trips, total_distance_km,
			total_cargo_tkm, total_fuel_litres, total_co2e_kg, avg_co2e_per_tkm,
			ev_distance_km, bs6_distance_km, bs4_distance_km, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, period_start, period_end) DO UPDATE SET
			total_trips = EXCLUDED.total_trips,
			total_distance_km = EXCLUDED.total_distance_km,
			total_cargo_tkm = EXCLUDED.total_cargo_tkm,
			total_fuel_litres = EXCLUDED.total_fuel_litres,
			total_co2e_kg = EXCLUDED.total_co2e_kg,
			avg_co2e_per_tkm = EXCLUDED.avg_co2e_per_tkm,
			ev_distance_km = EXCLUDED.ev_distance_km,
			bs6_distance_km = EXCLUDED.bs6_distance_km,
			bs4_distance_km = EXCLUDED.bs4_distance_km,
			created_by = EXCLUDED.created_by,
			created_at = EXCLUDED.created_at`

	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.TenantID, s.PeriodStart, s.PeriodEnd, s.TotalTrips, s.TotalDistanceKM,
		s.TotalCargoTKM, s.TotalFuelLitres, s.TotalCO2eKG, s.AvgCO2ePerTKM,
		s.EVDistanceKM, s.BS6DistanceKM, s.BS4DistanceKM, s.CreatedBy, s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to save esg snapshot: %w", err)
	}

	return &s, nil
}

// AggregatePeriodTrips aggregates trip carbon metrics for trips completed within [periodStart, periodEnd].
func (r *SQLESGRepository) AggregatePeriodTrips(ctx context.Context, tenantID, periodStart, periodEnd string) (*AggregatedPeriodMetrics, error) {
	// Query trip_esg_metrics joined with trips or directly
	query := `
		SELECT 
			COUNT(*),
			COALESCE(SUM(m.distance_km), 0),
			COALESCE(SUM(m.distance_km * m.payload_tonnes), 0),
			COALESCE(SUM(m.fuel_consumed_litres), 0),
			COALESCE(SUM(m.co2e_kg), 0),
			COALESCE(SUM(CASE WHEN m.emission_norm = 'EV' THEN m.distance_km ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.emission_norm = 'BS6' THEN m.distance_km ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.emission_norm = 'BS4' THEN m.distance_km ELSE 0 END), 0)
		FROM trip_esg_metrics m
		JOIN trips t ON m.trip_id = t.id AND m.tenant_id = t.tenant_id
		WHERE m.tenant_id = $1
		  AND (
		      (t.completed_at IS NOT NULL AND t.completed_at >= $2 AND t.completed_at <= $3)
		      OR (m.created_at >= $2 AND m.created_at <= $3)
		  )`

	var agg AggregatedPeriodMetrics
	err := r.db.QueryRowContext(ctx, query, tenantID, periodStart+" 00:00:00", periodEnd+" 23:59:59").Scan(
		&agg.TotalTrips,
		&agg.TotalDistanceKM,
		&agg.TotalCargoTKM,
		&agg.TotalFuelLitres,
		&agg.TotalCO2eKG,
		&agg.EVDistanceKM,
		&agg.BS6DistanceKM,
		&agg.BS4DistanceKM,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate period metrics: %w", err)
	}

	if agg.TotalCargoTKM > 0 {
		agg.AvgCO2ePerTKM = agg.TotalCO2eKG / agg.TotalCargoTKM
	} else {
		agg.AvgCO2ePerTKM = 0
	}

	return &agg, nil
}

// DeriveEmissionNorm derives standard emission norm from vehicle fuel and engine metadata.
func DeriveEmissionNorm(fuelType, vehicleType string) EmissionNorm {
	cleanFuel := strings.ToLower(strings.TrimSpace(fuelType))
	switch cleanFuel {
	case "electric", "ev":
		return EmissionNormEV
	case "cng", "gas":
		return EmissionNormCNG
	case "diesel", "petrol":
		return EmissionNormBS6
	default:
		return EmissionNormBS6
	}
}
