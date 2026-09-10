package fuel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SQLFuelIssueRepository implements FuelIssueRepository over a standard SQL database.
type SQLFuelIssueRepository struct {
	db *sql.DB
}

// NewSQLFuelIssueRepository constructs a new SQLFuelIssueRepository.
func NewSQLFuelIssueRepository(db *sql.DB) *SQLFuelIssueRepository {
	return &SQLFuelIssueRepository{db: db}
}

func (r *SQLFuelIssueRepository) RecordFuelIssue(ctx context.Context, tenantID, actorID string, req RecordFuelIssueRequest) (*FuelIssue, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(req.FuelStationID) == "" {
		return nil, errors.New("fuel_station_id is required")
	}
	if strings.TrimSpace(req.VehicleID) == "" {
		return nil, errors.New("vehicle_id is required")
	}
	if req.OpeningReading < 0 {
		return nil, errors.New("opening_reading must not be negative")
	}
	if req.ClosingReading <= req.OpeningReading {
		return nil, errors.New("closing_reading must be greater than opening_reading")
	}
	litresIssued := req.ClosingReading - req.OpeningReading

	fuelType := strings.ToLower(strings.TrimSpace(req.FuelType))
	if fuelType == "" {
		fuelType = "diesel"
	}

	issuedAt := time.Now().UTC()
	if req.IssuedAt != nil && !req.IssuedAt.IsZero() {
		issuedAt = req.IssuedAt.UTC()
	}

	// 1. Validate Fuel Station
	var stationFleetClass sql.NullString
	var stationValidTo sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT fleet_class, valid_to FROM vehicles
		WHERE id = $1 AND tenant_id = $2`,
		req.FuelStationID, tenantID).Scan(&stationFleetClass, &stationValidTo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("fuel station %s not found in tenant", req.FuelStationID)
		}
		return nil, err
	}

	if stationValidTo.Valid && stationValidTo.Time.Before(issuedAt) {
		return nil, fmt.Errorf("fuel station %s validity expired on %s (TMS_SOP p.9)", req.FuelStationID, stationValidTo.Time.Format("2006-01-02"))
	}

	// 2. Validate Receiving Vehicle
	var vehStatus string
	var vehBlocked bool
	var vehBlockedReason sql.NullString
	var currentOdo sql.NullFloat64
	err = r.db.QueryRowContext(ctx, `
		SELECT status, blocked, blocked_reason, odometer FROM vehicles
		WHERE id = $1 AND tenant_id = $2`,
		req.VehicleID, tenantID).Scan(&vehStatus, &vehBlocked, &vehBlockedReason, &currentOdo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("receiving vehicle %s not found in tenant", req.VehicleID)
		}
		return nil, err
	}

	if vehBlocked || vehStatus == "blocked" {
		reason := "vehicle is compliance blocked"
		if vehBlockedReason.Valid && vehBlockedReason.String != "" {
			reason = vehBlockedReason.String
		}
		return nil, fmt.Errorf("dispatch/fuel issue blocked: %s", reason)
	}

	// 3. Compute cost if rate provided
	var totalCost *float64
	if req.RatePerLitre != nil && *req.RatePerLitre > 0 {
		c := litresIssued * (*req.RatePerLitre)
		totalCost = &c
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 4. Record measurement document on pump measuring point if provided
	if req.PumpPointID != nil && *req.PumpPointID != "" {
		var pumpKind string
		err = tx.QueryRowContext(ctx, `
			SELECT kind FROM vehicle_measuring_points
			WHERE id = $1 AND vehicle_id = $2 AND tenant_id = $3`,
			*req.PumpPointID, req.FuelStationID, tenantID).Scan(&pumpKind)
		if err == nil && pumpKind == "PUMP" {
			measDocID := uuid.NewString()
			docNum := fmt.Sprintf("DOC-%d", time.Now().Unix())
			remarks := fmt.Sprintf("Fuel issued to vehicle %s (%0.2f L)", req.VehicleID, litresIssued)
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO vehicle_measurements (
					id, tenant_id, point_id, doc_number, counter_reading, difference_reading,
					total_counter_reading, measured_at, read_by, remarks, recorded_by
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				measDocID, tenantID, *req.PumpPointID, docNum, req.ClosingReading, litresIssued,
				req.ClosingReading, issuedAt, actorID, remarks, actorID)
		}
	}

	// 5. If receiving vehicle has a FUEL_TOPUP measuring point, record document
	var topupPointID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM vehicle_measuring_points
		WHERE vehicle_id = $1 AND tenant_id = $2 AND kind = 'FUEL_TOPUP'
		LIMIT 1`, req.VehicleID, tenantID).Scan(&topupPointID)
	if err == nil && topupPointID != "" {
		var lastCounter float64
		_ = tx.QueryRowContext(ctx, `
			SELECT total_counter_reading FROM vehicle_measurements
			WHERE point_id = $1 AND tenant_id = $2
			ORDER BY recorded_at DESC LIMIT 1`, topupPointID, tenantID).Scan(&lastCounter)
		newCounter := lastCounter + litresIssued
		topupDocID := uuid.NewString()
		remarks := fmt.Sprintf("Fuel issue from station %s", req.FuelStationID)
		_, _ = tx.ExecContext(ctx, `
			INSERT INTO vehicle_measurements (
				id, tenant_id, point_id, counter_reading, difference_reading,
				total_counter_reading, measured_at, read_by, remarks, recorded_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			topupDocID, tenantID, topupPointID, newCounter, litresIssued,
			newCounter, issuedAt, actorID, remarks, actorID)
	}

	// 6. Update vehicle odometer if provided and higher than existing
	if req.VehicleOdometer != nil && *req.VehicleOdometer > 0 {
		if !currentOdo.Valid || *req.VehicleOdometer > currentOdo.Float64 {
			_, _ = tx.ExecContext(ctx, `
				UPDATE vehicles SET odometer = $1, updated_at = CURRENT_TIMESTAMP
				WHERE id = $2 AND tenant_id = $3`,
				*req.VehicleOdometer, req.VehicleID, tenantID)
		}
	}

	// 7. Insert Fuel Issue record
	issueID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fuel_issues (
			id, tenant_id, issue_number, fuel_station_id, pump_point_id, vehicle_id,
			driver_id, trip_id, fuel_type, opening_reading, closing_reading, litres_issued,
			vehicle_odometer, rate_per_litre, total_cost, remarks, issued_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		issueID, tenantID, req.IssueNumber, req.FuelStationID, req.PumpPointID, req.VehicleID,
		req.DriverID, req.TripID, fuelType, req.OpeningReading, req.ClosingReading, litresIssued,
		req.VehicleOdometer, req.RatePerLitre, totalCost, req.Remarks, issuedAt, actorID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.GetFuelIssue(ctx, tenantID, issueID)
}

func (r *SQLFuelIssueRepository) GetFuelIssue(ctx context.Context, tenantID, id string) (*FuelIssue, error) {
	var fi FuelIssue
	var issueNum, pumpPointID, driverID, tripID sql.NullString
	var vehOdo, rate, totalCost sql.NullFloat64

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, issue_number, fuel_station_id, pump_point_id, vehicle_id,
		       driver_id, trip_id, fuel_type, opening_reading, closing_reading, litres_issued,
		       vehicle_odometer, rate_per_litre, total_cost, remarks, issued_at, created_by,
		       created_at, updated_at
		FROM fuel_issues
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID).Scan(
		&fi.ID, &fi.TenantID, &issueNum, &fi.FuelStationID, &pumpPointID, &fi.VehicleID,
		&driverID, &tripID, &fi.FuelType, &fi.OpeningReading, &fi.ClosingReading, &fi.LitresIssued,
		&vehOdo, &rate, &totalCost, &fi.Remarks, &fi.IssuedAt, &fi.CreatedBy,
		&fi.CreatedAt, &fi.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if issueNum.Valid {
		fi.IssueNumber = &issueNum.String
	}
	if pumpPointID.Valid {
		fi.PumpPointID = &pumpPointID.String
	}
	if driverID.Valid {
		fi.DriverID = &driverID.String
	}
	if tripID.Valid {
		fi.TripID = &tripID.String
	}
	if vehOdo.Valid {
		fi.VehicleOdometer = &vehOdo.Float64
	}
	if rate.Valid {
		fi.RatePerLitre = &rate.Float64
	}
	if totalCost.Valid {
		fi.TotalCost = &totalCost.Float64
	}

	return &fi, nil
}

func (r *SQLFuelIssueRepository) ListFuelIssues(ctx context.Context, tenantID string, filter FuelIssueFilter) ([]FuelIssue, error) {
	if tenantID == "" {
		return []FuelIssue{}, nil
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT id, tenant_id, issue_number, fuel_station_id, pump_point_id, vehicle_id,
		       driver_id, trip_id, fuel_type, opening_reading, closing_reading, litres_issued,
		       vehicle_odometer, rate_per_litre, total_cost, remarks, issued_at, created_by,
		       created_at, updated_at
		FROM fuel_issues
		WHERE tenant_id = $1`
	args := []interface{}{tenantID}

	if filter.VehicleID != "" {
		args = append(args, filter.VehicleID)
		query += fmt.Sprintf(" AND vehicle_id = $%d", len(args))
	}
	if filter.FuelStationID != "" {
		args = append(args, filter.FuelStationID)
		query += fmt.Sprintf(" AND fuel_station_id = $%d", len(args))
	}

	query += fmt.Sprintf(" ORDER BY issued_at DESC LIMIT %d OFFSET %d", limit, filter.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []FuelIssue
	for rows.Next() {
		var fi FuelIssue
		var issueNum, pumpPointID, driverID, tripID sql.NullString
		var vehOdo, rate, totalCost sql.NullFloat64

		if err := rows.Scan(
			&fi.ID, &fi.TenantID, &issueNum, &fi.FuelStationID, &pumpPointID, &fi.VehicleID,
			&driverID, &tripID, &fi.FuelType, &fi.OpeningReading, &fi.ClosingReading, &fi.LitresIssued,
			&vehOdo, &rate, &totalCost, &fi.Remarks, &fi.IssuedAt, &fi.CreatedBy,
			&fi.CreatedAt, &fi.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if issueNum.Valid {
			fi.IssueNumber = &issueNum.String
		}
		if pumpPointID.Valid {
			fi.PumpPointID = &pumpPointID.String
		}
		if driverID.Valid {
			fi.DriverID = &driverID.String
		}
		if tripID.Valid {
			fi.TripID = &tripID.String
		}
		if vehOdo.Valid {
			fi.VehicleOdometer = &vehOdo.Float64
		}
		if rate.Valid {
			fi.RatePerLitre = &rate.Float64
		}
		if totalCost.Valid {
			fi.TotalCost = &totalCost.Float64
		}
		list = append(list, fi)
	}

	if list == nil {
		list = []FuelIssue{}
	}
	return list, rows.Err()
}
