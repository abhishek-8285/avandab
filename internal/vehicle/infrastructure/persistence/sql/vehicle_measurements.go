package sql

import (
	"context"
	"database/sql"
	"errors"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/domain"
)

// Measurement point / document persistence (IK01/IK11 parity, spec §4.2).
// Implemented on vehicleRepository and asserted optionally (same pattern as
// dateRangeVehicleRepo) so existing implementations/mocks keep compiling.

// MeasurementRepo is implemented by repositories that persist SOP measuring
// points and measuring documents.
type MeasurementRepo interface {
	CreateMeasuringPoint(ctx context.Context, tenantID shared.TenantID, p domain.MeasuringPoint) (domain.MeasuringPoint, error)
	ListMeasuringPoints(ctx context.Context, tenantID shared.TenantID, vehicleID string) ([]domain.MeasuringPoint, error)
	RecordMeasurement(ctx context.Context, tenantID shared.TenantID, m domain.Measurement) (domain.Measurement, error)
	ListMeasurements(ctx context.Context, tenantID shared.TenantID, pointID string, limit int, offset int) ([]domain.Measurement, error)
	LastMeasurement(ctx context.Context, tenantID shared.TenantID, pointID string) (domain.Measurement, error)
}

// ErrNoMeasurement is returned by LastMeasurement when the point has no
// documents yet (first IK11 for a point: difference = counter itself).
var ErrNoMeasurement = errors.New("no measurements recorded for point")

func (r *vehicleRepository) CreateMeasuringPoint(ctx context.Context, tenantID shared.TenantID, p domain.MeasuringPoint) (domain.MeasuringPoint, error) {
	// NOTE: hand-written SQL (not sqlc) — sqlc v1.31.1 drops the trailing
	// bytes of these INSERT statements during codegen. SELECTs generate
	// fine; see the daterange.go precedent for raw SQL in this package.
	_, err := execTx(ctx, r.dbConn, `
INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, category, kind,
    meas_position, unit, decimal_places, annual_estimate, count_backwards, is_counter, description)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, string(tenantID), p.VehicleID, p.Category, p.Kind, p.MeasPosition,
		p.Unit, p.DecimalPlaces,
		sql.NullFloat64{Float64: p.AnnualEstimate, Valid: p.AnnualEstimate != 0},
		boolToInt(p.CountBackwards), boolToInt(p.IsCounter), p.Description,
	)
	if err != nil {
		return domain.MeasuringPoint{}, err
	}
	row, err := r.Q(ctx).GetMeasuringPointByID(ctx, db.GetMeasuringPointByIDParams{
		ID:       p.ID,
		TenantID: string(tenantID),
	})
	if err != nil {
		return domain.MeasuringPoint{}, err
	}
	return domain.MeasuringPoint{
		ID: row.ID, VehicleID: row.VehicleID, Category: row.Category, Kind: row.Kind,
		MeasPosition: row.MeasPosition, Unit: row.Unit, DecimalPlaces: row.DecimalPlaces,
		AnnualEstimate: row.AnnualEstimate.Float64, CountBackwards: row.CountBackwards != 0,
		IsCounter: row.IsCounter != 0, Description: row.Description, CreatedAt: row.CreatedAt,
	}, nil
}

func (r *vehicleRepository) ListMeasuringPoints(ctx context.Context, tenantID shared.TenantID, vehicleID string) ([]domain.MeasuringPoint, error) {
	rows, err := r.Q(ctx).ListMeasuringPointsByVehicle(ctx, db.ListMeasuringPointsByVehicleParams{
		VehicleID: vehicleID,
		TenantID:  string(tenantID),
	})
	if err != nil {
		return nil, err
	}
	points := make([]domain.MeasuringPoint, len(rows))
	for i, row := range rows {
		points[i] = domain.MeasuringPoint{
			ID: row.ID, VehicleID: row.VehicleID, Category: row.Category, Kind: row.Kind,
			MeasPosition: row.MeasPosition, Unit: row.Unit, DecimalPlaces: row.DecimalPlaces,
			AnnualEstimate: row.AnnualEstimate.Float64, CountBackwards: row.CountBackwards != 0,
			IsCounter: row.IsCounter != 0, Description: row.Description, CreatedAt: row.CreatedAt,
		}
	}
	return points, nil
}

func (r *vehicleRepository) RecordMeasurement(ctx context.Context, tenantID shared.TenantID, m domain.Measurement) (domain.Measurement, error) {
	var measuredAt sql.NullTime
	if !m.MeasuredAt.IsZero() {
		measuredAt = sql.NullTime{Time: m.MeasuredAt, Valid: true}
	}
	// NOTE: hand-written SQL (not sqlc) — see CreateMeasuringPoint above.
	_, err := execTx(ctx, r.dbConn, `
INSERT INTO vehicle_measurements (id, tenant_id, point_id, doc_number, counter_reading,
    difference_reading, total_counter_reading, measured_at, read_by, remarks, recorded_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, string(tenantID), m.PointID,
		sql.NullString{String: m.DocNumber, Valid: m.DocNumber != ""},
		m.CounterReading,
		sql.NullFloat64{Float64: m.DifferenceReading, Valid: true},
		sql.NullFloat64{Float64: m.TotalCounterReading, Valid: true},
		measuredAt,
		sql.NullString{String: m.ReadBy, Valid: m.ReadBy != ""},
		sql.NullString{String: m.Remarks, Valid: m.Remarks != ""},
		sql.NullString{String: m.RecordedBy, Valid: m.RecordedBy != ""},
	)
	if err != nil {
		return domain.Measurement{}, err
	}
	// NOTE: no RETURNING (sqlc v1.31.1 truncates the final RETURNING
	// identifier); re-select the written row.
	got, err := r.Q(ctx).GetMeasurementByID(ctx, db.GetMeasurementByIDParams{
		ID:       m.ID,
		TenantID: string(tenantID),
	})
	if err != nil {
		return domain.Measurement{}, err
	}
	return domain.Measurement{
		ID: got.ID, PointID: got.PointID, DocNumber: got.DocNumber.String,
		CounterReading: got.CounterReading, DifferenceReading: got.DifferenceReading.Float64,
		TotalCounterReading: got.TotalCounterReading.Float64,
		MeasuredAt:          got.MeasuredAt.Time,
		ReadBy:              got.ReadBy.String, Remarks: got.Remarks.String,
		RecordedBy: got.RecordedBy.String, RecordedAt: got.RecordedAt,
	}, nil
}

func (r *vehicleRepository) ListMeasurements(ctx context.Context, tenantID shared.TenantID, pointID string, limit int, offset int) ([]domain.Measurement, error) {
	rows, err := r.Q(ctx).ListMeasurementsByPoint(ctx, db.ListMeasurementsByPointParams{
		PointID:  pointID,
		TenantID: string(tenantID),
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Measurement, len(rows))
	for i, row := range rows {
		out[i] = domain.Measurement{
			ID: row.ID, PointID: row.PointID, DocNumber: row.DocNumber.String,
			CounterReading: row.CounterReading, DifferenceReading: row.DifferenceReading.Float64,
			TotalCounterReading: row.TotalCounterReading.Float64,
			MeasuredAt:          row.MeasuredAt.Time,
			ReadBy:              row.ReadBy.String, Remarks: row.Remarks.String,
			RecordedBy: row.RecordedBy.String, RecordedAt: row.RecordedAt,
		}
	}
	return out, nil
}

func (r *vehicleRepository) LastMeasurement(ctx context.Context, tenantID shared.TenantID, pointID string) (domain.Measurement, error) {
	row, err := r.Q(ctx).GetLastMeasurementByPoint(ctx, db.GetLastMeasurementByPointParams{
		PointID:  pointID,
		TenantID: string(tenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Measurement{}, ErrNoMeasurement
		}
		return domain.Measurement{}, err
	}
	return domain.Measurement{
		ID: row.ID, PointID: row.PointID, DocNumber: row.DocNumber.String,
		CounterReading: row.CounterReading, DifferenceReading: row.DifferenceReading.Float64,
		TotalCounterReading: row.TotalCounterReading.Float64,
		MeasuredAt:          row.MeasuredAt.Time,
		ReadBy:              row.ReadBy.String, Remarks: row.Remarks.String,
		RecordedBy: row.RecordedBy.String, RecordedAt: row.RecordedAt,
	}, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// execTx runs a write on the UoW transaction when one is in context,
// otherwise on the pool. Writes must never bypass an open tx: on
// shared-cache SQLite (tests) a pool write beside an open write-tx
// deadlocks, and in prod it would break atomicity.
func execTx(ctx context.Context, dbConn *sql.DB, query string, args ...any) (sql.Result, error) {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	return dbConn.ExecContext(ctx, query, args...)
}
