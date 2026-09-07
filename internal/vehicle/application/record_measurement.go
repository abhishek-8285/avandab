package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	sqlrepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// RecordMeasurementCommand records one SOP IK11 measuring document against a
// point. DifferenceReading and TotalCounterReading are computed here so the
// counter story stays consistent: difference = counter − last counter
// (first document for a point: difference = counter itself, per TMS_SOP p.6),
// total = counter.
type RecordMeasurementCommand struct {
	TenantID   shared.TenantID
	PointID    string
	Counter    float64
	MeasuredAt time.Time
	ReadBy     string
	Remarks    string
	RecordedBy string
}

// RecordMeasurementUseCase persists measuring documents with the SOP
// monotonic-counter guard: a counter that only moves forward must not move
// backwards (unless the point allows it).
type RecordMeasurementUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

func NewRecordMeasurementUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *RecordMeasurementUseCase {
	return &RecordMeasurementUseCase{uow: uow, idGen: idGen, clock: clock}
}

func (uc *RecordMeasurementUseCase) Execute(ctx context.Context, cmd RecordMeasurementCommand) (domain.Measurement, error) {
	if cmd.PointID == "" {
		return domain.Measurement{}, errors.New("measuring point id is required")
	}
	if cmd.Counter < 0 {
		return domain.Measurement{}, errors.New("counter reading must not be negative")
	}

	var out domain.Measurement
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(sqlrepo.MeasurementRepo)
		if !ok {
			return errors.New("vehicle repository does not support measurements")
		}

		last, err := repo.LastMeasurement(txCtx, cmd.TenantID, cmd.PointID)
		if err != nil && !errors.Is(err, sqlrepo.ErrNoMeasurement) {
			return err
		}
		lastCounter := 0.0
		if err == nil {
			lastCounter = last.CounterReading
		}
		if cmd.Counter < lastCounter {
			return errors.New("counter reading moved backwards (monotonic counter)")
		}

		measuredAt := cmd.MeasuredAt
		if measuredAt.IsZero() {
			measuredAt = uc.clock.Now()
		}

		out, err = repo.RecordMeasurement(txCtx, cmd.TenantID, domain.Measurement{
			ID:                  uc.idGen.GenerateUUID(),
			PointID:             cmd.PointID,
			CounterReading:      cmd.Counter,
			DifferenceReading:   cmd.Counter - lastCounter,
			TotalCounterReading: cmd.Counter,
			MeasuredAt:          measuredAt,
			ReadBy:              cmd.ReadBy,
			Remarks:             cmd.Remarks,
			RecordedBy:          cmd.RecordedBy,
		})
		return err
	})
	if err != nil {
		return domain.Measurement{}, err
	}
	return out, nil
}
