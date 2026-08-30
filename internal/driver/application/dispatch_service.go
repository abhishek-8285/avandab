package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/driver/domain"
)

type DispatchOfferDTO struct {
	ID          string    `json:"id"`
	BookingID   string    `json:"booking_id"`
	DriverID    string    `json:"driver_id"`
	VehicleID   string    `json:"vehicle_id"`
	Status      string    `json:"status"`
	OfferedAt   time.Time `json:"offered_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Origin      string    `json:"origin,omitempty"`
	Destination string    `json:"destination,omitempty"`
	CargoType   string    `json:"cargo_type,omitempty"`
	Payout      float64   `json:"payout,omitempty"`
}

type DriverCommandRequest struct {
	CommandID string                 `json:"command_id"`
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
}

type DriverCommandResponse struct {
	Success   bool                   `json:"success"`
	Status    string                 `json:"status"`
	Message   string                 `json:"message"`
	TripID    string                 `json:"trip_id,omitempty"`
	TripState string                 `json:"trip_state,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

func (s *DriverAppService) CreateDispatchOffer(ctx context.Context, tenantID, bookingID, driverID, vehicleID string, ttlMinutes int) (*DispatchOfferDTO, error) {
	if ttlMinutes == 0 {
		ttlMinutes = 15
	}
	offerID := uuid.NewString()
	now := time.Now()
	expiresAt := now.Add(time.Duration(ttlMinutes) * time.Minute)

	ex := s.exec(ctx)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO dispatch_offers (id, tenant_id, booking_id, driver_id, vehicle_id, status, offered_at, expires_at)
		VALUES (?, ?, ?, ?, ?, 'offered', ?, ?)`,
		offerID, tenantID, bookingID, driverID, vehicleID, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed creating dispatch offer: %w", err)
	}

	return &DispatchOfferDTO{
		ID:        offerID,
		BookingID: bookingID,
		DriverID:  driverID,
		VehicleID: vehicleID,
		Status:    "offered",
		OfferedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *DriverAppService) GetPendingOffersForDriver(ctx context.Context, tenantID, driverID string) ([]DispatchOfferDTO, error) {
	ex := s.exec(ctx)
	rows, err := ex.QueryContext(ctx, `
		SELECT id, booking_id, driver_id, vehicle_id, status, offered_at, expires_at
		FROM dispatch_offers
		WHERE tenant_id = ? AND driver_id = ? AND status = 'offered' AND expires_at > ?
		ORDER BY offered_at DESC`,
		tenantID, driverID, time.Now())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var offers []DispatchOfferDTO
	for rows.Next() {
		var o DispatchOfferDTO
		if err := rows.Scan(&o.ID, &o.BookingID, &o.DriverID, &o.VehicleID, &o.Status, &o.OfferedAt, &o.ExpiresAt); err != nil {
			return nil, err
		}
		offers = append(offers, o)
	}
	return offers, nil
}

func (s *DriverAppService) ProcessDriverCommand(ctx context.Context, tenantID, driverID string, req DriverCommandRequest) (DriverCommandResponse, error) {
	if req.CommandID == "" {
		req.CommandID = "cmd_" + uuid.NewString()
	}

	// 1. Idempotency Check: if command already processed, return stored result
	ex := s.exec(ctx)
	var existingStatus, existingPayload string
	err := ex.QueryRowContext(ctx, `
		SELECT status, response_payload FROM driver_commands
		WHERE tenant_id = ? AND command_id = ?`,
		tenantID, req.CommandID).Scan(&existingStatus, &existingPayload)
	if err == nil {
		var resp DriverCommandResponse
		if jsonErr := json.Unmarshal([]byte(existingPayload), &resp); jsonErr == nil {
			return resp, nil
		}
	}

	// 2. Execute Command
	var resp DriverCommandResponse
	var execErr error

	switch req.Type {
	case "ACCEPT_OFFER":
		resp, execErr = s.executeAcceptOffer(ctx, tenantID, driverID, req.Payload)
	case "REJECT_OFFER":
		resp, execErr = s.executeRejectOffer(ctx, tenantID, driverID, req.Payload)
	case "START_TRIP", "EN_ROUTE_PICKUP", "ARRIVE_PICKUP", "ARRIVED_PICKUP", "START_LOADING", "COMPLETE_LOADING", "LOADED", "START_DELIVERY", "IN_TRANSIT", "ARRIVE_DELIVERY", "ARRIVED_DELIVERY", "START_UNLOADING", "COMPLETE_TRIP", "CANCEL_TRIP", "INTERRUPT_TRIP", "TRIP_INTERRUPTED":
		resp, execErr = s.executeTripTransition(ctx, tenantID, driverID, req.Type, req.Payload)
	default:
		execErr = fmt.Errorf("unsupported command type: %s", req.Type)
	}

	if execErr != nil {
		return DriverCommandResponse{
			Success: false,
			Status:  "REJECTED",
			Message: execErr.Error(),
		}, execErr
	}

	// 3. Record command execution for idempotency
	payloadBytes, _ := json.Marshal(resp)
	_, _ = ex.ExecContext(ctx, `
		INSERT INTO driver_commands (command_id, tenant_id, driver_id, command_type, status, response_payload)
		VALUES (?, ?, ?, ?, 'processed', ?)`,
		req.CommandID, tenantID, driverID, req.Type, string(payloadBytes))

	return resp, nil
}

func (s *DriverAppService) executeAcceptOffer(ctx context.Context, tenantID, driverID string, payload map[string]interface{}) (DriverCommandResponse, error) {
	offerID, _ := payload["offer_id"].(string)
	if offerID == "" {
		return DriverCommandResponse{}, errors.New("offer_id is required")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return DriverCommandResponse{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var bookingID, vehicleID, status string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT booking_id, vehicle_id, status, expires_at
		FROM dispatch_offers
		WHERE tenant_id = ? AND id = ? AND driver_id = ?`,
		tenantID, offerID, driverID).Scan(&bookingID, &vehicleID, &status, &expiresAt)
	if err != nil {
		return DriverCommandResponse{}, fmt.Errorf("offer not found: %w", err)
	}

	if status != "offered" {
		return DriverCommandResponse{}, fmt.Errorf("offer is already %s", status)
	}
	if time.Now().After(expiresAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE dispatch_offers SET status = 'expired' WHERE id = ?`, offerID)
		return DriverCommandResponse{}, errors.New("offer has expired")
	}

	// Concurrency guard: Ensure no other driver accepted this booking
	var acceptedCount int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dispatch_offers
		WHERE tenant_id = ? AND booking_id = ? AND status = 'accepted'`,
		tenantID, bookingID).Scan(&acceptedCount)
	if err != nil {
		return DriverCommandResponse{}, err
	}
	if acceptedCount > 0 {
		_, _ = tx.ExecContext(ctx, `UPDATE dispatch_offers SET status = 'cancelled' WHERE id = ?`, offerID)
		return DriverCommandResponse{}, errors.New("offer already taken by another driver")
	}

	// 1. Mark this offer accepted
	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		UPDATE dispatch_offers
		SET status = 'accepted', responded_at = ?
		WHERE id = ?`, now, offerID)
	if err != nil {
		return DriverCommandResponse{}, err
	}

	// 2. Cancel competing offers for this booking
	_, _ = tx.ExecContext(ctx, `
		UPDATE dispatch_offers
		SET status = 'cancelled', responded_at = ?
		WHERE tenant_id = ? AND booking_id = ? AND id != ? AND status = 'offered'`,
		now, tenantID, bookingID, offerID)

	// 3. Create / assign Trip
	tripID := "trip_" + uuid.NewString()
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO trips (id, tenant_id, booking_id, driver_id, vehicle_id, status, started_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'assigned', ?, ?, ?)`,
		tripID, tenantID, bookingID, driverID, vehicleID, now, now, now)

	if commitErr := tx.Commit(); commitErr != nil {
		return DriverCommandResponse{}, commitErr
	}

	return DriverCommandResponse{
		Success:   true,
		Status:    "ACCEPTED",
		Message:   "Offer accepted and trip assigned",
		TripID:    tripID,
		TripState: "ACCEPTED",
	}, nil
}

func (s *DriverAppService) executeRejectOffer(ctx context.Context, tenantID, driverID string, payload map[string]interface{}) (DriverCommandResponse, error) {
	offerID, _ := payload["offer_id"].(string)
	if offerID == "" {
		return DriverCommandResponse{}, errors.New("offer_id is required")
	}

	ex := s.exec(ctx)
	res, err := ex.ExecContext(ctx, `
		UPDATE dispatch_offers
		SET status = 'rejected', responded_at = ?
		WHERE tenant_id = ? AND id = ? AND driver_id = ? AND status = 'offered'`,
		time.Now(), tenantID, offerID, driverID)
	if err != nil {
		return DriverCommandResponse{}, err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return DriverCommandResponse{}, errors.New("offer not found or already closed")
	}

	return DriverCommandResponse{
		Success: true,
		Status:  "REJECTED",
		Message: "Offer rejected successfully",
	}, nil
}

func (s *DriverAppService) executeTripTransition(ctx context.Context, tenantID, driverID, cmdType string, payload map[string]interface{}) (DriverCommandResponse, error) {
	tripID, _ := payload["trip_id"].(string)
	if tripID == "" {
		return DriverCommandResponse{}, errors.New("trip_id is required")
	}

	ex := s.exec(ctx)
	var currentStatus string
	err := ex.QueryRowContext(ctx, `
		SELECT status FROM trips
		WHERE tenant_id = ? AND id = ? AND driver_id = ?`,
		tenantID, tripID, driverID).Scan(&currentStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DriverCommandResponse{}, errors.New("trip not found or not assigned to driver")
		}
		return DriverCommandResponse{}, err
	}

	var nextStatus string
	switch cmdType {
	case "START_TRIP", "EN_ROUTE_PICKUP":
		if currentStatus != "assigned" && currentStatus != "accepted" && currentStatus != "draft" {
			return DriverCommandResponse{}, fmt.Errorf("cannot start trip from status %s", currentStatus)
		}
		nextStatus = "en_route_pickup"
	case "ARRIVE_PICKUP", "ARRIVED_PICKUP":
		nextStatus = "arrived_pickup"
	case "START_LOADING":
		nextStatus = "loading"
	case "COMPLETE_LOADING", "LOADED":
		nextStatus = "loaded"
	case "START_DELIVERY", "IN_TRANSIT":
		nextStatus = "in_transit"
	case "ARRIVE_DELIVERY", "ARRIVED_DELIVERY":
		nextStatus = "arrived_delivery"
	case "START_UNLOADING":
		nextStatus = "unloading"
	case "COMPLETE_TRIP":
		// Document verification check: if pod_required is set, verify POD is present
		podRequired, _ := payload["pod_required"].(bool)
		podDocID, _ := payload["pod_document_id"].(string)
		if podRequired && podDocID == "" {
			var count int
			_ = ex.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM driver_documents
				WHERE tenant_id = ? AND entity_type = 'trip' AND entity_id = ? AND document_type = 'POD'`,
				tenantID, tripID).Scan(&count)
			if count == 0 {
				return DriverCommandResponse{}, errors.New("proof of delivery (POD) document is required to complete trip")
			}
		}
		nextStatus = "completed"
	case "CANCEL_TRIP":
		nextStatus = "cancelled"
	case "INTERRUPT_TRIP", "TRIP_INTERRUPTED":
		nextStatus = "trip_interrupted"
	default:
		return DriverCommandResponse{}, fmt.Errorf("unsupported trip command: %s", cmdType)
	}

	now := time.Now()
	_, err = ex.ExecContext(ctx, `
		UPDATE trips
		SET status = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		nextStatus, now, tenantID, tripID)
	if err != nil {
		return DriverCommandResponse{}, fmt.Errorf("failed updating trip state: %w", err)
	}

	// Record audit event
	_ = s.repo.RecordAuditEvent(ctx, tenantID, domain.AuditEventRecord{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		ActorUserID: &driverID,
		EntityType:  "trip",
		EntityID:    tripID,
		Action:      cmdType,
		OldState:    &currentStatus,
		NewState:    &nextStatus,
		CreatedAt:   now,
	})

	return DriverCommandResponse{
		Success:   true,
		Status:    "PROCESSED",
		Message:   fmt.Sprintf("Trip transitioned to %s", nextStatus),
		TripID:    tripID,
		TripState: nextStatus,
	}, nil
}
