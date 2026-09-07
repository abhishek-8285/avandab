package application

import (
	"context"
	"errors"
	"sort"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	sqlrepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// ListVehicleMeasurements returns the newest measuring documents across all
// of a vehicle's points (IK11 journal for KMPL-style follow-ups).
type ListVehicleMeasurements struct {
	uow ports.UnitOfWork
}

func NewListVehicleMeasurements(uow ports.UnitOfWork) *ListVehicleMeasurements {
	return &ListVehicleMeasurements{uow: uow}
}

func (uc *ListVehicleMeasurements) Execute(ctx context.Context, tenantID shared.TenantID, vehicleID string, limit int) ([]domain.Measurement, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out []domain.Measurement
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(sqlrepo.MeasurementRepo)
		if !ok {
			return errors.New("vehicle repository does not support measurements")
		}
		points, err := repo.ListMeasuringPoints(txCtx, tenantID, vehicleID)
		if err != nil {
			return err
		}
		for _, p := range points {
			docs, err := repo.ListMeasurements(txCtx, tenantID, p.ID, limit, 0)
			if err != nil {
				return err
			}
			out = append(out, docs...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.After(out[j].RecordedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
