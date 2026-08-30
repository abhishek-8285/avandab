package application

import (
	"context"
	"errors"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

// SubmitStopPODCommand contains parameters to record a stop POD.
type SubmitStopPODCommand struct {
	TripID       aggregate.TripID
	StopID       string
	TenantID     shared.TenantID
	PODURL       string
	SignatureURL string
	Notes        string
	OTP          string
}

// SubmitStopPODUseCase orchestrates submitting proof of delivery and verifying OTP for a stop.
type SubmitStopPODUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

// NewSubmitStopPODUseCase creates a new SubmitStopPODUseCase.
func NewSubmitStopPODUseCase(uow ports.UnitOfWork, clock ports.Clock) *SubmitStopPODUseCase {
	return &SubmitStopPODUseCase{uow: uow, clock: clock}
}

// Execute verifies OTP (if required) and submits the stop POD.
func (uc *SubmitStopPODUseCase) Execute(ctx context.Context, cmd SubmitStopPODCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}

		now := uc.clock.Now()
		if cmd.OTP != "" {
			if err := t.VerifyStopOTP(cmd.StopID, cmd.OTP, now); err != nil {
				return err
			}
		}

		if err := t.SubmitStopPOD(cmd.StopID, cmd.PODURL, cmd.SignatureURL, cmd.Notes, now); err != nil {
			return err
		}

		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionUpdate, string(t.ID), nil, nil)
		return nil
	})
}
