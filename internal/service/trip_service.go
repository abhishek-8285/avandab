package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"transport-app/internal/domain"
	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/events"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// TripService handles trip management with business rule enforcement.
type TripService struct {
	baseService
	compliance *ComplianceService
}

// CreateTripRequest contains fields needed to create a trip.
type CreateTripRequest struct {
	BookingID     *domain.BookingID
	RouteID       domain.RouteID
	DriverID      *domain.DriverID
	VehicleID     *domain.VehicleID
	DepartureTime string
	ArrivalTime   string
	Remarks       string
}

// CreateTrip creates a new trip in draft status.
func (s *TripService) CreateTrip(ctx context.Context, req CreateTripRequest) (domain.Trip, error) {
	if req.RouteID == "" {
		return domain.Trip{}, fmt.Errorf("route is required")
	}

	// Validate route exists
	if _, err := s.store.GetRouteByID(ctx, req.RouteID); err != nil {
		return domain.Trip{}, domain.ErrRouteNotFound
	}

	// Validate booking exists if provided
	if req.BookingID != nil {
		if _, err := s.store.GetBookingByID(ctx, *req.BookingID); err != nil {
			return domain.Trip{}, domain.ErrBookingNotFound
		}
	}

	// Validate driver exists if provided
	if req.DriverID != nil {
		if _, err := s.store.GetDriverByID(ctx, *req.DriverID); err != nil {
			return domain.Trip{}, domain.ErrDriverNotFound
		}
	}

	// Validate vehicle exists if provided
	if req.VehicleID != nil {
		if _, err := s.store.GetVehicleByID(ctx, *req.VehicleID); err != nil {
			return domain.Trip{}, domain.ErrVehicleNotFound
		}
	}

	depTime, err := parseDateTime(req.DepartureTime)
	if err != nil {
		return domain.Trip{}, fmt.Errorf("invalid departure time")
	}

	var arrTime *time.Time
	if req.ArrivalTime != "" {
		t, err := parseDateTime(req.ArrivalTime)
		if err != nil {
			return domain.Trip{}, fmt.Errorf("invalid arrival time")
		}
		arrTime = &t
	}

	trip := domain.Trip{
		ID:            domain.TripID(generateID()),
		TripNumber:    s.generateTripNumber(ctx),
		BookingID:     req.BookingID,
		DriverID:      req.DriverID,
		VehicleID:     req.VehicleID,
		RouteID:       req.RouteID,
		DepartureTime: depTime,
		ArrivalTime:   arrTime,
		Status:        domain.TripDraft,
		Remarks:       strPtr(req.Remarks),
	}

	created, err := s.store.CreateTrip(ctx, trip)
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip created", "trip_id", created.ID, "trip_number", created.TripNumber)

	s.logAudit(ctx, nil, "create", "trips", string(created.ID), nil, nil)
	s.events.Publish(ctx, events.Event{
		Type: events.TripCreated,
		Payload: tripevents.TripCreatedEvent{
			TripID:        created.ID,
			TenantID:      shared.TenantIDFromContext(ctx),
			TripNumber:    created.TripNumber,
			RouteID:       created.RouteID,
			DriverID:      created.DriverID,
			VehicleID:     created.VehicleID,
			DepartureTime: created.DepartureTime,
			OccurredAt:    time.Now(),
		},
	})

	return created, nil
}

// GetTrip retrieves a trip by ID.
func (s *TripService) GetTrip(ctx context.Context, id domain.TripID) (repository.TripWithJoins, error) {
	return s.store.GetTripByID(ctx, id)
}

// GetTripByNumber retrieves a trip by its number.
func (s *TripService) GetTripByNumber(ctx context.Context, number string) (repository.TripWithJoins, error) {
	return s.store.GetTripByNumber(ctx, number)
}

// ListTrips retrieves trips with search and pagination.
func (s *TripService) ListTrips(ctx context.Context, query, status string, limit, offset int) ([]repository.TripWithJoins, int64, error) {
	trips, err := s.store.SearchTrips(ctx, query, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountTrips(ctx, query, status)
	if err != nil {
		return nil, 0, err
	}
	return trips, total, nil
}

// ScheduleTrip schedules a trip (changes status from draft to scheduled).
func (s *TripService) ScheduleTrip(ctx context.Context, id domain.TripID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if err := trip.CanSchedule(); err != nil {
		return domain.Trip{}, err
	}

	status := domain.TripScheduled
	if trip.DriverID != nil && trip.VehicleID != nil {
		status = domain.TripAssigned
	}

	updated, err := s.store.UpdateTripStatus(ctx, id, status)
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip scheduled", "trip_id", id, "status", status)
	s.logAudit(ctx, nil, "schedule", "trips", string(id), nil, nil)
	return updated, nil
}

// AssignDriver assigns a driver to a trip with conflict checking.
func (s *TripService) AssignDriver(ctx context.Context, tripID domain.TripID, driverID domain.DriverID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, tripID)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	// Cannot modify cancelled or completed trips
	if trip.Status == domain.TripCancelled {
		return domain.Trip{}, domain.ErrCancelledTripImmutable
	}
	if trip.Status == domain.TripCompleted {
		return domain.Trip{}, domain.ErrCompletedTripImmutable
	}

	// Validate driver exists and is available
	driver, err := s.store.GetDriverByID(ctx, driverID)
	if err != nil {
		return domain.Trip{}, domain.ErrDriverNotFound
	}

	// Enforce 5-doc dispatch compliance gate (Spec 05 §5)
	if s.compliance != nil {
		if _, err := s.compliance.CheckDispatchCompliance(ctx, string(driverID), ""); err != nil {
			return domain.Trip{}, err
		}
	} else {
		if err := driver.CanAcceptTrip(); err != nil {
			return domain.Trip{}, err
		}
	}

	// Check for scheduling conflicts
	conflicts, err := s.store.CheckDriverConflict(ctx, driverID, &tripID)
	if err != nil {
		return domain.Trip{}, err
	}
	if len(conflicts) > 0 {
		return domain.Trip{}, domain.ErrDriverOnTrip
	}

	// If trip already has a vehicle, check if it is blocked for maintenance
	if trip.VehicleID != nil && *trip.VehicleID != "" {
		if blocked, reason, err := s.store.IsMaintenanceBlocked(ctx, string(*trip.VehicleID)); err == nil && blocked {
			return domain.Trip{}, fmt.Errorf("%w: %s", domain.ErrVehicleMaintenanceBlocked, reason)
		}
	}

	var updated domain.Trip
	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		u, err := s.store.AssignDriver(ctx, tripID, driverID)
		if err != nil {
			return err
		}
		updated = u

		// Update trip to assigned status if not already
		if updated.Status == domain.TripScheduled {
			updated, err = s.store.UpdateTripStatus(ctx, tripID, domain.TripAssigned)
			if err != nil {
				return err
			}
		}

		// Update driver status to on_trip
		driver.Status = domain.DriverOnTrip
		if _, err := s.store.UpdateDriver(ctx, driver); err != nil {
			return err
		}

		s.logAudit(ctx, nil, "assign_driver", "trips", string(tripID), nil, nil)
		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("driver assigned to trip", "driver_id", driverID, "trip_id", tripID)
	// Issue the delivery OTP at dispatch (best-effort — regenerable from the
	// trip view).
	if _, otpErr := s.EnsurePODOTP(ctx, string(tripID)); otpErr != nil {
		s.log.Warn("pod otp issue failed at dispatch", "trip_id", string(tripID), "error", otpErr)
	}
	if s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type: "TripAssignedEvent",
			Payload: map[string]interface{}{
				"trip_id":     string(tripID),
				"tenant_id":   string(shared.TenantIDFromContext(ctx)),
				"driver_id":   string(driverID),
				"occurred_at": time.Now().UTC(),
			},
		})
	}
	return updated, nil
}

// AssignVehicle assigns a vehicle to a trip with conflict checking.
func (s *TripService) AssignVehicle(ctx context.Context, tripID domain.TripID, vehicleID domain.VehicleID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, tripID)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if trip.Status == domain.TripCancelled {
		return domain.Trip{}, domain.ErrCancelledTripImmutable
	}
	if trip.Status == domain.TripCompleted {
		return domain.Trip{}, domain.ErrCompletedTripImmutable
	}

	// Validate vehicle exists and is available
	vehicle, err := s.store.GetVehicleByID(ctx, vehicleID)
	if err != nil {
		return domain.Trip{}, domain.ErrVehicleNotFound
	}

	// Enforce 5-doc dispatch compliance gate (Spec 05 §5)
	if s.compliance != nil {
		if _, err := s.compliance.CheckDispatchCompliance(ctx, "", string(vehicleID)); err != nil {
			return domain.Trip{}, err
		}
	} else {
		if err := vehicle.CanAssign(); err != nil {
			return domain.Trip{}, err
		}
	}

	// Check for scheduling conflicts
	conflicts, err := s.store.CheckVehicleConflict(ctx, vehicleID, &tripID)
	if err != nil {
		return domain.Trip{}, err
	}
	if len(conflicts) > 0 {
		return domain.Trip{}, domain.ErrVehicleAssigned
	}

	// Check maintenance block (Spec 04 §6, §12)
	if blocked, reason, err := s.store.IsMaintenanceBlocked(ctx, string(vehicleID)); err == nil && blocked {
		return domain.Trip{}, fmt.Errorf("%w: %s", domain.ErrVehicleMaintenanceBlocked, reason)
	}

	var updated domain.Trip
	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		u, err := s.store.AssignVehicle(ctx, tripID, vehicleID)
		if err != nil {
			return err
		}
		updated = u

		// Update trip to assigned status if not already
		if updated.Status == domain.TripScheduled {
			updated, err = s.store.UpdateTripStatus(ctx, tripID, domain.TripAssigned)
			if err != nil {
				return err
			}
		}

		// Update vehicle status to running
		vehicle.Status = domain.VehicleRunning
		if _, err := s.store.UpdateVehicle(ctx, vehicle); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("vehicle assigned to trip", "vehicle_id", vehicleID, "trip_id", tripID)
	if s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type: "TripAssignedEvent",
			Payload: map[string]interface{}{
				"trip_id":     string(tripID),
				"tenant_id":   string(shared.TenantIDFromContext(ctx)),
				"vehicle_id":  string(vehicleID),
				"occurred_at": time.Now().UTC(),
			},
		})
	}
	return updated, nil
}

// StartTrip starts a trip (validate status is assigned).
func (s *TripService) StartTrip(ctx context.Context, id domain.TripID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if err := trip.CanStart(); err != nil {
		return domain.Trip{}, err
	}

	// Re-validate full compliance gate before allowing trip to start (Spec 05 §5)
	if s.compliance != nil {
		dID := ""
		if trip.DriverID != nil {
			dID = string(*trip.DriverID)
		}
		vID := ""
		if trip.VehicleID != nil {
			vID = string(*trip.VehicleID)
		}
		if _, err := s.compliance.CheckDispatchCompliance(ctx, dID, vID); err != nil {
			return domain.Trip{}, err
		}
	}

	var updated domain.Trip
	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		u, err := s.store.UpdateTripStatus(ctx, id, domain.TripStarted)
		if err != nil {
			return err
		}
		updated = u
		s.logAudit(ctx, nil, "start_trip", "trips", string(id), nil, nil)
		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip started", "trip_id", id)
	s.events.Publish(ctx, events.Event{
		Type: events.TripStarted,
		Payload: tripevents.TripStartedEvent{
			TripID:     id,
			TenantID:   shared.TenantIDFromContext(ctx),
			StartedAt:  time.Now(),
			OccurredAt: time.Now(),
		},
	})
	return updated, nil
}

// CompleteTrip completes a trip and returns domain trip.
func (s *TripService) CompleteTrip(ctx context.Context, id domain.TripID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if err := trip.CanComplete(); err != nil {
		return domain.Trip{}, err
	}

	var completed domain.Trip
	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		u, err := s.store.UpdateTripStatus(ctx, id, domain.TripCompleted)
		if err != nil {
			return err
		}
		completed = u

		// Release driver and vehicle
		if trip.DriverID != nil {
			driver, err := s.store.GetDriverByID(ctx, *trip.DriverID)
			if err == nil {
				driver.Status = domain.DriverAvailable
				_, _ = s.store.UpdateDriver(ctx, driver)
			}
		}
		if trip.VehicleID != nil {
			vehicle, err := s.store.GetVehicleByID(ctx, *trip.VehicleID)
			if err == nil {
				vehicle.Status = domain.VehicleAvailable
				_, _ = s.store.UpdateVehicle(ctx, vehicle)
			}
		}

		s.logAudit(ctx, nil, "complete_trip", "trips", string(id), nil, nil)
		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip completed", "trip_id", id)
	s.events.Publish(ctx, events.Event{
		Type: events.TripCompleted,
		Payload: tripevents.TripCompletedEvent{
			TripID:      id,
			TenantID:    shared.TenantIDFromContext(ctx),
			CompletedAt: time.Now(),
			OccurredAt:  time.Now(),
		},
	})
	return completed, nil
}

// DeliverTripWithPOD captures e-POD, sets status to Delivered, auto-triggers GST Invoice + Driver Settlement.
// Rule 2: Trip Status Automation.
func (s *TripService) DeliverTripWithPOD(ctx context.Context, id domain.TripID, podURL string) (domain.Trip, error) {
	if podURL == "" {
		return domain.Trip{}, fmt.Errorf("e-POD URL is required to mark trip as delivered")
	}

	var delivered domain.Trip
	err := s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		trip, err := s.store.GetTripByID(ctx, id)
		if err != nil {
			return domain.ErrTripNotFound
		}

		if err := trip.CanDeliver(); err != nil {
			return err
		}

		u, err := s.store.UpdateTripStatus(ctx, id, domain.TripDelivered)
		if err != nil {
			return err
		}
		delivered = u
		delivered.PODURL = &podURL
		podInfo := fmt.Sprintf("pod_url=%s", podURL)
		s.logAudit(ctx, nil, "deliver_epod", "trips", string(id), nil, &podInfo)
		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip delivered with e-POD", "trip_id", id, "pod_url", podURL)
	s.events.Publish(ctx, events.Event{
		Type: events.TripDelivered,
		Payload: map[string]interface{}{
			"trip_id":      id,
			"tenant_id":    string(shared.TenantIDFromContext(ctx)),
			"booking_id":   delivered.BookingID,
			"driver_id":    delivered.DriverID,
			"pod_url":      podURL,
			"delivered_at": time.Now(),
			"occurred_at":  time.Now(),
		},
	})
	return delivered, nil
}

// DeliverWithPODRequest holds the full e-POD submission payload from the driver mobile app.
type DeliverWithPODRequest struct {
	PODPhotoURL    string
	SignatureURL   string
	ConsigneeName  string
	ConsigneePhone string
	Notes          string
	OTPCode        string // consignee-read-back code; verified server-side
	OTPVerified    bool   // set by the server only — client values are ignored
}

// ErrPODOTPRequired is returned when a trip has an active delivery OTP but
// the submission does not carry a matching code.
var ErrPODOTPRequired = errors.New("delivery OTP required: get the code from the consignee (see trip view) and submit it with the delivery")

// DeliverWithPOD marks a trip as delivered using e-POD metadata and returns the trip number.
// This is the mobile driver entry-point; photo/signature URLs are pre-uploaded by the handler.
// OTPVerified is never trusted from a client flag: it is recorded only when the trip record
// already confirms a consignee-accepted POD on the server side.
func (s *TripService) DeliverWithPOD(ctx context.Context, tripIDStr string, req DeliverWithPODRequest) (string, error) {
	id := domain.TripID(tripIDStr)
	podURL := req.PODPhotoURL
	if podURL == "" {
		podURL = req.SignatureURL
	}

	if _, err := s.store.GetTripByID(ctx, id); err != nil {
		return "", domain.ErrTripNotFound
	}
	req.OTPVerified = false

	// OTP gate: when the trip carries an active code, the delivery must
	// repeat it back. Legacy trips without a code deliver unverified.
	if err := s.verifyPODOTP(ctx, tripIDStr, req.OTPCode, &req.OTPVerified); err != nil {
		return "", err
	}

	delivered, err := s.DeliverTripWithPOD(ctx, id, podURL)
	if err != nil {
		return "", err
	}

	meta := fmt.Sprintf("consignee=%s phone=%s otp=%v sig=%s notes=%s",
		req.ConsigneeName, req.ConsigneePhone, req.OTPVerified, req.SignatureURL, req.Notes)
	s.logAudit(ctx, nil, "deliver_pod_meta", "trips", tripIDStr, nil, &meta)
	s.log.Info("e-POD delivered", "trip_id", tripIDStr, "consignee", req.ConsigneeName)

	return delivered.TripNumber, nil
}

// podOTPTTL bounds how long a delivery code stays valid.
const podOTPTTL = 48 * time.Hour

func (s *TripService) tripDB() *sql.DB {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return nil
	}
	return getter.DB()
}

// EnsurePODOTP returns the trip's active delivery code, generating (and
// persisting) a fresh one when absent or expired. The operator sees this in
// the trip view and relays it to the consignee until SMS is configured.
func (s *TripService) EnsurePODOTP(ctx context.Context, tripID string) (string, error) {
	db := s.tripDB()
	if db == nil {
		return "", nil
	}
	var otp, expires string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(pod_otp,''), COALESCE(pod_otp_expires_at,'') FROM trips WHERE id = $1`, tripID).
		Scan(&otp, &expires)
	if err != nil {
		return "", err
	}
	if otp != "" && expires != "" {
		if exp, perr := time.Parse(time.RFC3339, expires); perr == nil && time.Now().Before(exp) {
			return otp, nil
		}
	}

	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	expiresAt := time.Now().Add(podOTPTTL).UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`UPDATE trips SET pod_otp = $1, pod_otp_expires_at = $2 WHERE id = $3`, code, expiresAt, tripID); err != nil {
		return "", err
	}
	s.logAudit(ctx, nil, "pod_otp_issued", "trips", tripID, nil, nil)
	return code, nil
}

// verifyPODOTP enforces the read-back when the trip has an active code and
// records pod_otp_verified on success.
func (s *TripService) verifyPODOTP(ctx context.Context, tripID, code string, verified *bool) error {
	db := s.tripDB()
	if db == nil {
		return nil
	}
	var otp, expires string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(pod_otp,''), COALESCE(pod_otp_expires_at,'') FROM trips WHERE id = $1`, tripID).
		Scan(&otp, &expires)
	if err != nil || otp == "" {
		return nil // legacy trip, no gate
	}
	if exp, perr := time.Parse(time.RFC3339, expires); perr != nil && !time.Now().Before(exp) {
		return nil // expired → gate no longer enforceable by us; deliver unverified
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(otp)) != 1 {
		return ErrPODOTPRequired
	}
	*verified = true
	if _, err := db.ExecContext(ctx,
		`UPDATE trips SET pod_otp_verified = 1, pod_otp = '' WHERE id = $1`, tripID); err != nil {
		return err
	}
	return nil
}

// CancelTrip cancels a trip (cannot cancel completed trips).
func (s *TripService) CancelTrip(ctx context.Context, id domain.TripID) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if err := trip.CanCancel(); err != nil {
		return domain.Trip{}, err
	}

	var updated domain.Trip
	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		u, err := s.store.UpdateTripStatus(ctx, id, domain.TripCancelled)
		if err != nil {
			return err
		}
		updated = u

		// Release driver and vehicle if assigned
		if trip.DriverID != nil {
			driver, err := s.store.GetDriverByID(ctx, *trip.DriverID)
			if err == nil && driver.Status == domain.DriverOnTrip {
				driver.Status = domain.DriverAvailable
				_, _ = s.store.UpdateDriver(ctx, driver)
			}
		}
		if trip.VehicleID != nil {
			vehicle, err := s.store.GetVehicleByID(ctx, *trip.VehicleID)
			if err == nil && vehicle.Status == domain.VehicleRunning {
				vehicle.Status = domain.VehicleAvailable
				_, _ = s.store.UpdateVehicle(ctx, vehicle)
			}
		}

		s.logAudit(ctx, nil, "cancel_trip", "trips", string(id), nil, nil)
		return nil
	})
	if err != nil {
		return domain.Trip{}, err
	}

	s.log.Info("trip cancelled", "trip_id", id)
	return updated, nil
}

// DeleteTrip deletes a trip (draft only).
func (s *TripService) DeleteTrip(ctx context.Context, id domain.TripID) error {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.ErrTripNotFound
	}

	if trip.Status != domain.TripDraft {
		return fmt.Errorf("only draft trips can be deleted")
	}

	return s.store.DeleteTrip(ctx, id)
}

// GetTripByBooking returns the trip associated with a booking.
func (s *TripService) GetTripByBooking(ctx context.Context, bookingID domain.BookingID) (*domain.Trip, error) {
	twb, err := s.store.GetTripByBookingID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	return &twb.Trip, nil
}

// UpdateTrip updates an existing trip's details.
func (s *TripService) UpdateTrip(ctx context.Context, id domain.TripID, req CreateTripRequest) (domain.Trip, error) {
	trip, err := s.store.GetTripByID(ctx, id)
	if err != nil {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if trip.Status == domain.TripCancelled {
		return domain.Trip{}, domain.ErrCancelledTripImmutable
	}
	if trip.Status == domain.TripCompleted {
		return domain.Trip{}, domain.ErrCompletedTripImmutable
	}

	if req.RouteID != "" {
		if _, err := s.store.GetRouteByID(ctx, req.RouteID); err != nil {
			return domain.Trip{}, domain.ErrRouteNotFound
		}
		trip.RouteID = req.RouteID
	}

	if req.DriverID != nil {
		if _, err := s.store.GetDriverByID(ctx, *req.DriverID); err != nil {
			return domain.Trip{}, domain.ErrDriverNotFound
		}
		trip.DriverID = req.DriverID
	}

	if req.VehicleID != nil {
		if _, err := s.store.GetVehicleByID(ctx, *req.VehicleID); err != nil {
			return domain.Trip{}, domain.ErrVehicleNotFound
		}
		trip.VehicleID = req.VehicleID
	}

	if req.DepartureTime != "" {
		depTime, err := parseDateTime(req.DepartureTime)
		if err != nil {
			return domain.Trip{}, fmt.Errorf("invalid departure time")
		}
		trip.DepartureTime = depTime
	}

	if req.ArrivalTime != "" {
		arrTime, err := parseDateTime(req.ArrivalTime)
		if err != nil {
			return domain.Trip{}, fmt.Errorf("invalid arrival time")
		}
		trip.ArrivalTime = &arrTime
	}

	if req.Remarks != "" {
		trip.Remarks = &req.Remarks
	}

	return s.store.UpdateTrip(ctx, trip.Trip)
}
