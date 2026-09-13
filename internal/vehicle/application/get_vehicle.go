package application

import (
	"context"
	"errors"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/vehicle/domain"
	"transport-app/internal/vehicle/domain/aggregate"
)

type VehicleResponseDTO struct {
	ID                 string                   `json:"id"`
	RegistrationNumber string                   `json:"registration_number"`
	VehicleNumber      string                   `json:"vehicle_number"`
	VehicleType        string                   `json:"vehicle_type"`
	Capacity           int64                    `json:"capacity"`
	FuelType           string                   `json:"fuel_type"`
	InsuranceExpiry    time.Time                `json:"insurance_expiry"`
	FitnessExpiry      time.Time                `json:"fitness_expiry"`
	PermitExpiry       time.Time                `json:"permit_expiry"`
	Status             string                   `json:"status"`
	CurrentMileage     *float64                 `json:"current_mileage"`
	StandardKmpl       *float64                 `json:"standard_kmpl"`
	Blocked            bool                     `json:"blocked"`
	BlockedReason      string                   `json:"blocked_reason"`
	RCExpiry           *time.Time               `json:"rc_expiry"`
	PUCExpiry          *time.Time               `json:"puc_expiry"`
	Odometer           float64                  `json:"odometer"`
	Profile            aggregate.VehicleProfile `json:"profile"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

type GetVehicleQuery struct {
	ID       aggregate.VehicleID
	TenantID shared.TenantID
}

type GetVehicleUseCase struct {
	uow ports.UnitOfWork
}

func NewGetVehicleUseCase(uow ports.UnitOfWork) *GetVehicleUseCase {
	return &GetVehicleUseCase{uow: uow}
}

func (uc *GetVehicleUseCase) Execute(ctx context.Context, q GetVehicleQuery) (VehicleResponseDTO, error) {
	var dto VehicleResponseDTO
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Vehicles().(domain.VehicleRepository)
		if !ok {
			return errors.New("failed to retrieve vehicle repository")
		}

		v, err := repo.GetReadModel(txCtx, q.ID, q.TenantID)
		if err != nil {
			return err
		}

		dto = VehicleResponseDTO{
			ID:                 v.ID,
			RegistrationNumber: v.RegistrationNumber,
			VehicleNumber:      v.VehicleNumber,
			VehicleType:        v.VehicleType,
			Capacity:           v.Capacity,
			FuelType:           v.FuelType,
			InsuranceExpiry:    v.InsuranceExpiry,
			FitnessExpiry:      v.FitnessExpiry,
			PermitExpiry:       v.PermitExpiry,
			Status:             v.Status,
			CurrentMileage:     v.CurrentMileage,
			StandardKmpl:       v.StandardKmpl,
			Blocked:            v.Blocked,
			BlockedReason:      v.BlockedReason,
			RCExpiry:           v.RCExpiry,
			PUCExpiry:          v.PUCExpiry,
			Odometer:           v.Odometer,
			Profile:            v.Profile,
			CreatedAt:          v.CreatedAt,
			UpdatedAt:          v.UpdatedAt,
		}
		return nil
	})
	return dto, err
}
