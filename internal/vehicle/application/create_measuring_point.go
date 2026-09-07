package application

import (
	"context"
	"errors"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	sqlrepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// CreateMeasuringPointCommand creates one SOP IK01 measuring point on a
// fleet object (ODO/DISTANCE counter, fuel top-up, or pump reading).
type CreateMeasuringPointCommand struct {
	TenantID       shared.TenantID
	VehicleID      string
	Kind           string
	MeasPosition   string
	Unit           string
	AnnualEstimate float64
	Description    string
}

// defaultPosition maps a point kind to its SAP characteristic (TMS_SOP p.12:
// ODO counter 1337/DISTANCE, fuel-top-up counter 1339/FUELTOPUP).
func defaultPosition(kind string) string {
	switch kind {
	case "FUEL_TOPUP":
		return "FUELTOPUP"
	case "PUMP":
		return "PUMP"
	default:
		return "DISTANCE"
	}
}

type CreateMeasuringPointUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
}

func NewCreateMeasuringPointUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator) *CreateMeasuringPointUseCase {
	return &CreateMeasuringPointUseCase{uow: uow, idGen: idGen}
}

func (uc *CreateMeasuringPointUseCase) Execute(ctx context.Context, cmd CreateMeasuringPointCommand) (domain.MeasuringPoint, error) {
	if cmd.VehicleID == "" {
		return domain.MeasuringPoint{}, errors.New("vehicle id is required")
	}
	switch cmd.Kind {
	case "ODO", "FUEL_TOPUP", "PUMP":
	default:
		return domain.MeasuringPoint{}, errors.New("invalid point kind (want ODO, FUEL_TOPUP or PUMP)")
	}
	unit := cmd.Unit
	if unit == "" {
		unit = "KM"
	}
	switch unit {
	case "KM", "L":
	default:
		return domain.MeasuringPoint{}, errors.New("invalid unit (want KM or L)")
	}
	position := cmd.MeasPosition
	if position == "" {
		position = defaultPosition(cmd.Kind)
	}

	var out domain.MeasuringPoint
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(sqlrepo.MeasurementRepo)
		if !ok {
			return errors.New("vehicle repository does not support measuring points")
		}
		var err error
		out, err = repo.CreateMeasuringPoint(txCtx, cmd.TenantID, domain.MeasuringPoint{
			ID:             uc.idGen.GenerateUUID(),
			VehicleID:      cmd.VehicleID,
			Category:       "M",
			Kind:           cmd.Kind,
			MeasPosition:   position,
			Unit:           unit,
			AnnualEstimate: cmd.AnnualEstimate,
			IsCounter:      true,
			Description:    cmd.Description,
		})
		return err
	})
	if err != nil {
		return domain.MeasuringPoint{}, err
	}
	return out, nil
}
