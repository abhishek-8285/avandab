package application

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	bookingDomain "transport-app/internal/booking/domain"
	bookingAggregate "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

type CreateTripCommand struct {
	TenantID       shared.TenantID
	BookingID      *string
	RouteID        string
	DepartureTime  time.Time
	Remarks        string
	IdempotencyKey string
	Stops          []aggregate.TripStop
}

type CreateTripUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock
}

func NewCreateTripUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *CreateTripUseCase {
	return &CreateTripUseCase{uow: uow, idGen: idGen, clock: clock}
}

func (uc *CreateTripUseCase) Execute(ctx context.Context, cmd CreateTripCommand) (aggregate.TripID, error) {
	if cmd.RouteID == "" {
		return "", errors.New("route ID is required")
	}
	if cmd.DepartureTime.IsZero() {
		return "", errors.New("departure time is required")
	}

	tripID := aggregate.TripID(uc.idGen.GenerateUUID())
	tripNumber := uc.idGen.GenerateDisplayID("TR")

	trip := aggregate.NewTripAggregate(
		tripID,
		cmd.TenantID,
		tripNumber,
		cmd.BookingID,
		cmd.RouteID,
		cmd.DepartureTime,
		cmd.Remarks,
		uc.clock.Now(),
	)
	trip.SetIdempotencyKey(cmd.IdempotencyKey)

	for _, s := range cmd.Stops {
		trip.AddStop(s)
	}

	var resultID aggregate.TripID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		// Idempotent replay: same key returns the original trip, no new row.
		if cmd.IdempotencyKey != "" {
			if existing, err := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); err == nil && existing != nil {
				resultID = existing.ID
				return nil
			}
		}

		var bookingCustID string
		var consigneeName, consigneePhone, consigneeEmail string

		// If a booking is linked, it must exist in the same tenant, be in a
		// trippable state, and not already have an active trip. Prevents
		// orphan trips and duplicate trips per booking.
		if cmd.BookingID != nil && *cmd.BookingID != "" {
			bookingRepo, ok := txCtx.Repositories().Bookings().(bookingDomain.BookingRepository)
			if !ok {
				return errors.New("failed to retrieve booking repository")
			}
			booking, err := bookingRepo.Find(txCtx, bookingAggregate.BookingID(*cmd.BookingID), cmd.TenantID)
			if err != nil {
				return fmt.Errorf("booking %s not found: %w", *cmd.BookingID, err)
			}
			if string(booking.TenantID) != string(cmd.TenantID) {
				return errors.New("booking belongs to a different tenant")
			}
			switch booking.Status {
			case bookingAggregate.BookingPending, bookingAggregate.BookingConfirmed:
				// allowed
			default:
				return fmt.Errorf("booking %s is %s, cannot create trip", *cmd.BookingID, booking.Status)
			}
			if existing, err := repo.FindByBookingID(txCtx, *cmd.BookingID, cmd.TenantID); err == nil && existing != nil {
				switch existing.Status {
				case aggregate.TripCompleted, aggregate.TripCancelled:
					// terminal — a replacement trip is allowed
				default:
					return fmt.Errorf("booking %s already has active trip %s", *cmd.BookingID, existing.TripNumber)
				}
			}
			bookingCustID = booking.CustomerID

			// Look up customer contact details for consignee defaults
			var exec interface {
				QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
				ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
			}
			if tx := repository.TxFromContext(txCtx); tx != nil {
				exec = tx
			} else if dbGetter, ok := txCtx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
				exec = dbGetter.DB()
			}
			if exec != nil && bookingCustID != "" {
				var cName, cPhone, cEmail sql.NullString
				_ = exec.QueryRowContext(txCtx,
					`SELECT name, phone, email FROM customers WHERE id = $1 AND (tenant_id = $2 OR tenant_id = '1')`,
					bookingCustID, string(cmd.TenantID),
				).Scan(&cName, &cPhone, &cEmail)
				if cName.Valid {
					consigneeName = cName.String
				}
				if cPhone.Valid {
					consigneePhone = cPhone.String
				}
				if cEmail.Valid {
					consigneeEmail = cEmail.String
				}
			}
		}

		// Auto-seed default pickup (sequence 1) and drop (sequence 2) stops
		// from the assigned route if no stops were explicitly attached.
		if len(trip.Stops) == 0 {
			source := "Pickup"
			destination := "Drop"
			var estHours float64
			var sLat, sLng, dLat, dLng sql.NullFloat64

			var exec interface {
				ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
				QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
			}
			if tx := repository.TxFromContext(txCtx); tx != nil {
				exec = tx
			} else if dbGetter, ok := txCtx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
				exec = dbGetter.DB()
			}

			if exec != nil {
				var rSrc, rDest string
				var rHours sql.NullFloat64
				err := exec.QueryRowContext(txCtx,
					`SELECT source, destination, estimated_hours FROM routes WHERE id = $1 AND (tenant_id = $2 OR tenant_id = '1')`,
					cmd.RouteID, string(cmd.TenantID),
				).Scan(&rSrc, &rDest, &rHours)
				if err == nil {
					if strings.TrimSpace(rSrc) != "" {
						source = rSrc
					}
					if strings.TrimSpace(rDest) != "" {
						destination = rDest
					}
					if rHours.Valid {
						estHours = rHours.Float64
					}
				}

				// Check if route_locations has geocoded coordinates
				_ = exec.QueryRowContext(txCtx,
					`SELECT source_lat, source_lng, dest_lat, dest_lng FROM route_locations WHERE route_id = $1`,
					cmd.RouteID,
				).Scan(&sLat, &sLng, &dLat, &dLng)
			}

			now := uc.clock.Now()
			pickupStop := aggregate.TripStop{
				ID:              uc.idGen.GenerateUUID(),
				TenantID:        cmd.TenantID,
				TripID:          trip.ID,
				StopSequence:    1,
				StopType:        aggregate.StopTypePickup,
				LocationName:    source,
				Address:         source,
				PlannedArrival:  &cmd.DepartureTime,
				Status:          aggregate.StopStatusPending,
				OTPRequired:     false,
				PODRequired:     false,
				GeofenceRadiusM: 150.0,
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if sLat.Valid && sLng.Valid {
				pickupStop.Latitude = &sLat.Float64
				pickupStop.Longitude = &sLng.Float64
			}

			dropEta := cmd.DepartureTime.Add(time.Duration(estHours * float64(time.Hour)))
			if estHours <= 0 {
				dropEta = cmd.DepartureTime.Add(4 * time.Hour)
			}

			otpN, _ := rand.Int(rand.Reader, big.NewInt(900000))
			otpCode := fmt.Sprintf("%06d", otpN.Int64()+100000)
			otpExpires := dropEta.Add(48 * time.Hour)

			dropStop := aggregate.TripStop{
				ID:              uc.idGen.GenerateUUID(),
				TenantID:        cmd.TenantID,
				TripID:          trip.ID,
				StopSequence:    2,
				StopType:        aggregate.StopTypeDrop,
				LocationName:    destination,
				Address:         destination,
				PlannedArrival:  &dropEta,
				Status:          aggregate.StopStatusPending,
				OTPRequired:     true,
				OTPCode:         otpCode,
				OTPExpiresAt:    &otpExpires,
				PODRequired:     true,
				ConsigneeName:   consigneeName,
				ConsigneePhone:  consigneePhone,
				ConsigneeEmail:  consigneeEmail,
				GeofenceRadiusM: 150.0,
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if dLat.Valid && dLng.Valid {
				dropStop.Latitude = &dLat.Float64
				dropStop.Longitude = &dLng.Float64
			}

			trip.AddStop(pickupStop)
			trip.AddStop(dropStop)
		}

		if err := repo.Save(txCtx, trip); err != nil {
			// Lost-race replay: a concurrent insert with the same key won.
			// Return the winner instead of a duplicate error.
			if cmd.IdempotencyKey != "" && strings.Contains(err.Error(), "UNIQUE constraint failed") {
				if existing, rerr := repo.FindByIdempotencyKey(txCtx, cmd.IdempotencyKey, cmd.TenantID); rerr == nil && existing != nil {
					resultID = existing.ID
					return nil
				}
			}
			return err
		}

		// Also sync default drop OTP/consignee to legacy trips table columns if present
		var exec interface {
			ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
		}
		if tx := repository.TxFromContext(txCtx); tx != nil {
			exec = tx
		} else if dbGetter, ok := txCtx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
			exec = dbGetter.DB()
		}
		if exec != nil {
			for _, st := range trip.Stops {
				if st.StopSequence == 2 && st.OTPCode != "" {
					_, _ = exec.ExecContext(txCtx, `
						UPDATE trips
						SET pod_otp = $1,
						    pod_otp_expires_at = $2,
						    pod_consignee_name = $3,
						    pod_consignee_phone = $4
						WHERE id = $5 AND tenant_id = $6
					`, st.OTPCode, st.OTPExpiresAt, st.ConsigneeName, st.ConsigneePhone, string(trip.ID), string(cmd.TenantID))
					break
				}
			}
		}

		resultID = trip.ID
		logAudit(txCtx, ActionCreate, string(trip.ID), nil, nil)
		return nil
	})

	if err != nil {
		return "", err
	}

	return resultID, nil
}
