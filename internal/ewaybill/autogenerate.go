package ewaybill

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// SubscribeTripEvents registers listeners for trip lifecycle events.
func (s *EWayBillService) SubscribeTripEvents(bus events.EventBus) {
	if bus == nil {
		return
	}

	handler := func(ctx context.Context, e events.Event) error {
		tripID := extractTripID(e.Payload)
		if tripID == "" {
			return nil
		}

		tenantID := extractTenantID(e.Payload)
		if tenantID == "" {
			tenantID = string(shared.TenantIDFromContext(ctx))
		}
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}

		// Check company_config for ewaybill_auto_generate per-tenant
		autoGen := s.isAutoGenerateEnabled(ctx, tenantID)
		if !autoGen {
			s.logger.Debug("ewaybill auto-generate skipped (disabled in config)", "trip_id", tripID, "tenant_id", tenantID)
			return nil
		}

		// Check if an E-Way Bill already exists for this trip
		var existingEwb string
		err := s.db.QueryRowContext(ctx, `
			SELECT ewb_number FROM eway_bills
			WHERE trip_id = ? AND status != 'cancelled' LIMIT 1
		`, tripID).Scan(&existingEwb)
		if err == nil && existingEwb != "" {
			s.logger.Debug("ewaybill auto-generate skipped (already exists)", "trip_id", tripID, "ewb_number", existingEwb)
			return nil
		}

		// Load trip goods_value
		var goodsVal float64
		err = s.db.QueryRowContext(ctx, `
			SELECT b.price
			FROM trips t
			JOIN bookings b ON t.booking_id = b.id
			WHERE t.id = ?
		`, tripID).Scan(&goodsVal)
		if err != nil {
			s.logger.Warn("could not resolve goods value for trip auto-generate", "trip_id", tripID, "error", err)
			return nil
		}

		if goodsVal <= s.cfg.MinInvoiceValue {
			s.logger.Debug("ewaybill auto-generate skipped (goods value <= 50k)", "trip_id", tripID, "goods_value", goodsVal)
			return nil
		}

		_, err = s.GeneratePartA(ctx, GeneratePartARequest{
			TripID:     tripID,
			GoodsValue: goodsVal,
			GenMode:    "AUTO",
		})
		if err != nil {
			s.logger.Warn("failed to auto-generate ewaybill", "trip_id", tripID, "error", err)
		}
		return nil
	}

	bus.Subscribe("TripConfirmedEvent", handler)
	bus.Subscribe("trip.confirmed", handler)
	bus.Subscribe("TripAssignedEvent", func(ctx context.Context, e events.Event) error {
		tripID := extractTripID(e.Payload)
		vehID := extractVehicleID(e.Payload)
		if tripID == "" || vehID == "" {
			return nil
		}

		var regNum string
		err := s.db.QueryRowContext(ctx, `SELECT registration_number FROM vehicles WHERE id = ?`, vehID).Scan(&regNum)
		if err != nil || regNum == "" {
			return nil
		}

		var ewbNum string
		err = s.db.QueryRowContext(ctx, `SELECT ewb_number FROM eway_bills WHERE trip_id = ? AND (vehicle_number IS NULL OR vehicle_number = '') AND status != 'cancelled'`, tripID).Scan(&ewbNum)
		if err == nil && ewbNum != "" {
			_, _ = s.AttachPartB(ctx, ewbNum, regNum, "")
		}
		return nil
	})

	deliveryHandler := func(ctx context.Context, e events.Event) error {
		tripID := extractTripID(e.Payload)
		if tripID == "" {
			return nil
		}

		var ewbNum string
		err := s.db.QueryRowContext(ctx, `
			SELECT ewb_number FROM eway_bills
			WHERE trip_id = ? AND status IN ('active', 'part_a')
			LIMIT 1`, tripID).Scan(&ewbNum)
		if err != nil || ewbNum == "" {
			return nil
		}

		// Reconcile delivery: mark E-Way Bill as delivered/completed
		_, err = s.db.ExecContext(ctx, `
			UPDATE eway_bills
			SET status = 'delivered'
			WHERE ewb_number = ? AND status IN ('active', 'part_a')
		`, ewbNum)
		if err == nil {
			eventID := uuid.NewString()
			_, _ = s.db.ExecContext(ctx, `
				INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at)
				VALUES (?, ?, ?, 'DELIVERED', '{"reason":"trip_delivered"}', 'system', datetime('now'))
			`, eventID, ewbNum, tripID)
		}
		return nil
	}

	bus.Subscribe("TripDeliveredEvent", deliveryHandler)
	bus.Subscribe("trip.delivered", deliveryHandler)
	bus.Subscribe("TripCompletedEvent", deliveryHandler)
	bus.Subscribe("trip.completed", deliveryHandler)
	bus.Subscribe("EPODVerifiedEvent", deliveryHandler)
	bus.Subscribe("epod.verified", deliveryHandler)
}

func (s *EWayBillService) isAutoGenerateEnabled(ctx context.Context, tenantID string) bool {
	var val string
	err := s.db.QueryRowContext(ctx, `
		SELECT value FROM company_config
		WHERE tenant_id = ? AND key = 'ewaybill_auto_generate'
		LIMIT 1`, tenantID).Scan(&val)
	if err != nil {
		if err == sql.ErrNoRows {
			return true // default true per spec
		}
		return true
	}
	val = strings.TrimSpace(strings.ToLower(val))
	return val == "true" || val == "1" || val == "yes"
}
