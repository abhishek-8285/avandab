package comm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// EventSubscriber listens to domain events across the system and queues
// appropriate transactional emails and WhatsApp dispatches into comm_outbox.
type EventSubscriber struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewEventSubscriber creates a new EventSubscriber instance.
func NewEventSubscriber(db *sql.DB, logger *slog.Logger) *EventSubscriber {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventSubscriber{
		db:     db,
		logger: logger,
	}
}

// SubscribeEvents registers all transactional communication event listeners on the event bus.
func (s *EventSubscriber) SubscribeEvents(bus events.EventBus) {
	if bus == nil {
		return
	}

	// 1. Invoice Events (Email)
	invoiceEvents := []string{
		"invoice.issued",
		"invoice.created",
		"invoice.generated",
		events.InvoiceGenerated,
		"InvoiceGeneratedEvent",
		"InvoiceIssuedEvent",
		"InvoiceCreatedEvent",
	}
	for _, et := range invoiceEvents {
		bus.Subscribe(et, s.HandleInvoiceEvent)
	}

	// 2. POD / Trip Delivery Events (Email & WhatsApp)
	podEvents := []string{
		"pod.submitted",
		"epod.submitted",
		"epod.verified",
		"trip.completed",
		events.TripCompleted,
		"TripCompletedEvent",
		"trip.delivered",
		"TripDeliveredEvent",
	}
	for _, et := range podEvents {
		bus.Subscribe(et, s.HandlePODEvent)
	}

	// 3. Auth & Registration Events (Email)
	authEvents := []string{
		"auth.password_reset",
		"user.registered",
		"auth.registered",
		"user.created",
	}
	for _, et := range authEvents {
		bus.Subscribe(et, s.HandleAuthEvent)
	}

	// 4. Trip Dispatch Events (WhatsApp to Driver)
	dispatchEvents := []string{
		"trip.assigned",
		"trip.created",
		"trip.scheduled",
		events.TripAssigned,
		events.TripCreated,
		events.TripScheduled,
		"TripAssignedEvent",
		"TripCreatedEvent",
		"TripScheduledEvent",
	}
	for _, et := range dispatchEvents {
		bus.Subscribe(et, s.HandleTripDispatchEvent)
	}

	// 5. Tracking Events (WhatsApp to Shipper/Customer)
	trackingEvents := []string{
		"booking.confirmed",
		"trip.started",
		"trip.in_transit",
		events.BookingConfirmed,
		events.TripStarted,
		events.TripInTransit,
		"BookingConfirmedEvent",
		"TripStartedEvent",
		"TripInTransitEvent",
	}
	for _, et := range trackingEvents {
		bus.Subscribe(et, s.HandleTrackingEvent)
	}
}

// HandleInvoiceEvent processes invoice events and enqueues billing emails into comm_outbox.
func (s *EventSubscriber) HandleInvoiceEvent(ctx context.Context, e events.Event) error {
	pMap := toMap(e.Payload)

	invoiceID := getString(pMap, "invoice_id", "id", "ID")
	invoiceNumber := getString(pMap, "invoice_number", "InvoiceNumber", "number")
	tenantID := getString(pMap, "tenant_id", "TenantID")
	customerEmail := getString(pMap, "customer_email", "email", "recipient", "Recipient", "CustomerEmail")
	dueDate := getString(pMap, "due_date", "DueDate")
	total := getFloat(pMap, "total", "Total", "amount", "Amount")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}

	// If details are missing, query from DB
	if s.db != nil && (customerEmail == "" || invoiceNumber == "" || total == 0) && invoiceID != "" {
		var dbTenant, dbNum, dbEmail, dbDueDate sql.NullString
		var dbTotal sql.NullFloat64
		err := s.db.QueryRowContext(ctx, `
			SELECT i.tenant_id, i.invoice_number, i.total, c.email, i.due_date
			FROM invoices i
			LEFT JOIN customers c ON i.customer_id = c.id
			WHERE i.id = ?`, invoiceID).Scan(&dbTenant, &dbNum, &dbTotal, &dbEmail, &dbDueDate)
		if err == nil {
			if tenantID == "" && dbTenant.Valid {
				tenantID = dbTenant.String
			}
			if invoiceNumber == "" && dbNum.Valid {
				invoiceNumber = dbNum.String
			}
			if total == 0 && dbTotal.Valid {
				total = dbTotal.Float64
			}
			if customerEmail == "" && dbEmail.Valid {
				customerEmail = dbEmail.String
			}
			if dueDate == "" && dbDueDate.Valid {
				dueDate = dbDueDate.String
			}
		}
	}

	if dueDate == "" {
		dueDate = time.Now().AddDate(0, 0, 15).Format("2006-01-02")
	}
	if invoiceNumber == "" {
		invoiceNumber = invoiceID
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	if customerEmail == "" {
		s.logger.Warn("comm event subscriber: invoice email skipped (no recipient found)",
			"invoice_id", invoiceID, "event_type", e.Type)
		return nil
	}

	_, err := EnqueueInvoiceEmail(ctx, s.db, tenantID, customerEmail, invoiceNumber, total, dueDate, nil)
	if err != nil {
		s.logger.Error("comm event subscriber: failed to enqueue invoice email",
			"invoice_id", invoiceID, "recipient", customerEmail, "error", err)
		return err
	}

	s.logger.Info("comm event subscriber: queued invoice email",
		"invoice_id", invoiceID, "invoice_number", invoiceNumber, "recipient", customerEmail)
	return nil
}

// HandlePODEvent processes e-POD & trip delivery events and enqueues delivery receipts (email & WhatsApp).
func (s *EventSubscriber) HandlePODEvent(ctx context.Context, e events.Event) error {
	pMap := toMap(e.Payload)

	tripID := getString(pMap, "trip_id", "id", "ID", "TripID")
	tripNumber := getString(pMap, "trip_number", "TripNumber", "number")
	tenantID := getString(pMap, "tenant_id", "TenantID")
	customerEmail := getString(pMap, "customer_email", "email", "recipient", "Recipient", "CustomerEmail")
	customerPhone := getString(pMap, "customer_phone", "phone", "recipient_phone", "CustomerPhone", "consignee_phone")
	podURL := getString(pMap, "pod_url", "PODURL", "url", "pod_document_url")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}

	// If details are missing, query from DB
	if s.db != nil && (customerEmail == "" || customerPhone == "" || tripNumber == "") && tripID != "" {
		var dbTenant, dbNum, dbEmail, dbPhone, dbPOD sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT t.tenant_id, t.trip_number, c.email, c.phone, t.pod_url
			FROM trips t
			LEFT JOIN bookings b ON t.booking_id = b.id
			LEFT JOIN customers c ON b.customer_id = c.id
			WHERE t.id = ?`, tripID).Scan(&dbTenant, &dbNum, &dbEmail, &dbPhone, &dbPOD)
		if err == nil {
			if tenantID == "" && dbTenant.Valid {
				tenantID = dbTenant.String
			}
			if tripNumber == "" && dbNum.Valid {
				tripNumber = dbNum.String
			}
			if customerEmail == "" && dbEmail.Valid {
				customerEmail = dbEmail.String
			}
			if customerPhone == "" && dbPhone.Valid {
				customerPhone = dbPhone.String
			}
			if podURL == "" && dbPOD.Valid {
				podURL = dbPOD.String
			}
		}
	}

	if tripNumber == "" {
		tripNumber = tripID
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	// 1. Enqueue Email receipt if email is present
	if customerEmail != "" {
		if _, err := EnqueuePODEmail(ctx, s.db, tenantID, customerEmail, tripNumber, podURL, nil); err != nil {
			s.logger.Error("comm event subscriber: failed to enqueue POD receipt email",
				"trip_id", tripID, "recipient", customerEmail, "error", err)
		} else {
			s.logger.Info("comm event subscriber: queued POD receipt email",
				"trip_id", tripID, "trip_number", tripNumber, "recipient", customerEmail)
		}
	}

	// 2. Enqueue WhatsApp delivery completion message if phone is present
	if customerPhone != "" {
		if _, err := EnqueuePODWhatsApp(ctx, s.db, tenantID, customerPhone, tripNumber, podURL); err != nil {
			s.logger.Error("comm event subscriber: failed to enqueue POD receipt WhatsApp",
				"trip_id", tripID, "recipient", customerPhone, "error", err)
		} else {
			s.logger.Info("comm event subscriber: queued POD receipt WhatsApp",
				"trip_id", tripID, "trip_number", tripNumber, "recipient", customerPhone)
		}
	}

	if customerEmail == "" && customerPhone == "" {
		s.logger.Warn("comm event subscriber: POD receipt skipped (no recipient email or phone found)",
			"trip_id", tripID, "event_type", e.Type)
	}

	return nil
}

// HandleTripDispatchEvent processes trip dispatch events and enqueues WhatsApp messages to the driver.
func (s *EventSubscriber) HandleTripDispatchEvent(ctx context.Context, e events.Event) error {
	pMap := toMap(e.Payload)

	tripID := getString(pMap, "trip_id", "id", "ID", "TripID")
	driverID := getString(pMap, "driver_id", "DriverID")
	driverPhone := getString(pMap, "driver_phone", "phone", "recipient", "Recipient", "Phone")
	origin := getString(pMap, "origin", "source", "Source", "Origin")
	destination := getString(pMap, "destination", "Destination")
	vehicleID := getString(pMap, "vehicle_id", "VehicleID", "vehicle_number", "VehicleNumber")
	tenantID := getString(pMap, "tenant_id", "TenantID")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}

	// Query DB for missing driver phone, origin, destination, vehicleID
	if s.db != nil && tripID != "" && (driverPhone == "" || origin == "" || destination == "" || vehicleID == "") {
		var dbTenant, dbDriverPhone, dbOrigin, dbDest, dbVehID, dbDriverID sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT t.tenant_id, d.phone, r.source, r.destination,
			       COALESCE(NULLIF(v.registration_number, ''), NULLIF(v.vehicle_number, ''), t.vehicle_id, ''),
			       t.driver_id
			FROM trips t
			LEFT JOIN drivers d ON (t.driver_id = d.id OR t.driver_id = d.driver_id)
			LEFT JOIN routes r ON t.route_id = r.id
			LEFT JOIN vehicles v ON t.vehicle_id = v.id
			WHERE t.id = ?`, tripID).Scan(&dbTenant, &dbDriverPhone, &dbOrigin, &dbDest, &dbVehID, &dbDriverID)
		if err == nil {
			if tenantID == "" && dbTenant.Valid {
				tenantID = dbTenant.String
			}
			if driverPhone == "" && dbDriverPhone.Valid {
				driverPhone = dbDriverPhone.String
			}
			if origin == "" && dbOrigin.Valid {
				origin = dbOrigin.String
			}
			if destination == "" && dbDest.Valid {
				destination = dbDest.String
			}
			if vehicleID == "" && dbVehID.Valid {
				vehicleID = dbVehID.String
			}
			if driverID == "" && dbDriverID.Valid {
				driverID = dbDriverID.String
			}
		}
	}

	// If driverPhone still empty and driverID present, query drivers table directly
	if s.db != nil && driverPhone == "" && driverID != "" {
		var dbPhone sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT phone FROM drivers WHERE id = ? OR driver_id = ?`, driverID, driverID).Scan(&dbPhone)
		if err == nil && dbPhone.Valid {
			driverPhone = dbPhone.String
		}
	}

	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	if driverPhone == "" {
		s.logger.Warn("comm event subscriber: trip dispatch WhatsApp skipped (no driver phone found)",
			"trip_id", tripID, "driver_id", driverID, "event_type", e.Type)
		return nil
	}

	if vehicleID == "" {
		vehicleID = "unassigned"
	}

	_, err := EnqueueTripDispatchWhatsApp(ctx, s.db, tenantID, driverPhone, origin, destination, vehicleID)
	if err != nil {
		s.logger.Error("comm event subscriber: failed to enqueue trip dispatch WhatsApp",
			"trip_id", tripID, "driver_phone", driverPhone, "error", err)
		return err
	}

	s.logger.Info("comm event subscriber: queued trip dispatch WhatsApp",
		"trip_id", tripID, "driver_phone", driverPhone, "origin", origin, "destination", destination, "vehicle_id", vehicleID)
	return nil
}

// HandleTrackingEvent processes booking confirmation and trip started events and enqueues WhatsApp tracking links to customers.
func (s *EventSubscriber) HandleTrackingEvent(ctx context.Context, e events.Event) error {
	pMap := toMap(e.Payload)

	bookingID := getString(pMap, "booking_id", "BookingID")
	bookingNumber := getString(pMap, "booking_number", "BookingNumber", "number")
	tripID := getString(pMap, "trip_id", "TripID", "id", "ID")
	tripNumber := getString(pMap, "trip_number", "TripNumber")
	customerPhone := getString(pMap, "customer_phone", "phone", "recipient", "Recipient", "CustomerPhone")
	origin := getString(pMap, "origin", "source", "Source", "Origin")
	destination := getString(pMap, "destination", "Destination")
	vehicleID := getString(pMap, "vehicle_id", "VehicleID", "vehicle_number", "VehicleNumber")
	tenantID := getString(pMap, "tenant_id", "TenantID")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}

	isBooking := strings.Contains(strings.ToLower(e.Type), "booking") || (bookingID != "" && tripID == "")

	if isBooking {
		if s.db != nil && bookingID != "" && (customerPhone == "" || bookingNumber == "" || origin == "" || destination == "") {
			var dbTenant, dbBookingNum, dbPhone, dbOrigin, dbDest sql.NullString
			err := s.db.QueryRowContext(ctx, `
				SELECT b.tenant_id, b.booking_number, c.phone, r.source, r.destination
				FROM bookings b
				LEFT JOIN customers c ON b.customer_id = c.id
				LEFT JOIN routes r ON b.route_id = r.id
				WHERE b.id = ?`, bookingID).Scan(&dbTenant, &dbBookingNum, &dbPhone, &dbOrigin, &dbDest)
			if err == nil {
				if tenantID == "" && dbTenant.Valid {
					tenantID = dbTenant.String
				}
				if bookingNumber == "" && dbBookingNum.Valid {
					bookingNumber = dbBookingNum.String
				}
				if customerPhone == "" && dbPhone.Valid {
					customerPhone = dbPhone.String
				}
				if origin == "" && dbOrigin.Valid {
					origin = dbOrigin.String
				}
				if destination == "" && dbDest.Valid {
					destination = dbDest.String
				}
			}
		}

		if bookingNumber == "" {
			bookingNumber = bookingID
		}
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}

		if customerPhone == "" {
			s.logger.Warn("comm event subscriber: booking tracking WhatsApp skipped (no customer phone found)",
				"booking_id", bookingID, "event_type", e.Type)
			return nil
		}

		trackingURL := fmt.Sprintf("https://avandab.com/tracking#b=%s", bookingNumber)
		if vehicleID != "" {
			trackingURL = fmt.Sprintf("https://avandab.com/tracking#v=%s", vehicleID)
		}

		_, err := EnqueueBookingTrackingWhatsApp(ctx, s.db, tenantID, customerPhone, bookingNumber, origin, destination, trackingURL)
		if err != nil {
			s.logger.Error("comm event subscriber: failed to enqueue booking tracking WhatsApp",
				"booking_id", bookingID, "recipient", customerPhone, "error", err)
			return err
		}

		s.logger.Info("comm event subscriber: queued booking tracking WhatsApp",
			"booking_id", bookingID, "booking_number", bookingNumber, "recipient", customerPhone)
		return nil
	}

	// Trip tracking event (trip.started, trip.in_transit, etc.)
	if s.db != nil && tripID != "" && (customerPhone == "" || tripNumber == "" || origin == "" || destination == "" || vehicleID == "") {
		var dbTenant, dbTripNum, dbPhone, dbOrigin, dbDest, dbVehID sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT t.tenant_id, t.trip_number, c.phone, r.source, r.destination,
			       COALESCE(NULLIF(v.registration_number, ''), NULLIF(v.vehicle_number, ''), t.vehicle_id, '')
			FROM trips t
			LEFT JOIN bookings b ON t.booking_id = b.id
			LEFT JOIN customers c ON b.customer_id = c.id
			LEFT JOIN routes r ON t.route_id = r.id
			LEFT JOIN vehicles v ON t.vehicle_id = v.id
			WHERE t.id = ?`, tripID).Scan(&dbTenant, &dbTripNum, &dbPhone, &dbOrigin, &dbDest, &dbVehID)
		if err == nil {
			if tenantID == "" && dbTenant.Valid {
				tenantID = dbTenant.String
			}
			if tripNumber == "" && dbTripNum.Valid {
				tripNumber = dbTripNum.String
			}
			if customerPhone == "" && dbPhone.Valid {
				customerPhone = dbPhone.String
			}
			if origin == "" && dbOrigin.Valid {
				origin = dbOrigin.String
			}
			if destination == "" && dbDest.Valid {
				destination = dbDest.String
			}
			if vehicleID == "" && dbVehID.Valid {
				vehicleID = dbVehID.String
			}
		}
	}

	if tripNumber == "" {
		tripNumber = tripID
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	if customerPhone == "" {
		s.logger.Warn("comm event subscriber: trip tracking WhatsApp skipped (no customer phone found)",
			"trip_id", tripID, "event_type", e.Type)
		return nil
	}

	_, err := EnqueueTripTrackingWhatsApp(ctx, s.db, tenantID, customerPhone, tripNumber, origin, destination, vehicleID)
	if err != nil {
		s.logger.Error("comm event subscriber: failed to enqueue trip tracking WhatsApp",
			"trip_id", tripID, "recipient", customerPhone, "error", err)
		return err
	}

	s.logger.Info("comm event subscriber: queued trip tracking WhatsApp",
		"trip_id", tripID, "trip_number", tripNumber, "recipient", customerPhone)
	return nil
}

// HandleAuthEvent processes password reset and user registration events.
func (s *EventSubscriber) HandleAuthEvent(ctx context.Context, e events.Event) error {
	pMap := toMap(e.Payload)

	email := getString(pMap, "email", "recipient", "Recipient", "to")
	tenantID := getString(pMap, "tenant_id", "TenantID")
	userName := getString(pMap, "name", "user_name", "UserName", "full_name")
	resetLink := getString(pMap, "reset_link", "reset_url", "link", "token")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	if email == "" {
		s.logger.Warn("comm event subscriber: auth email skipped (no email provided)", "event_type", e.Type)
		return nil
	}

	isReset := strings.Contains(strings.ToLower(e.Type), "password") || resetLink != ""
	if isReset {
		subject := "Reset Your Password - Avandab Fleet"
		textBody := fmt.Sprintf("A password reset was requested for your account.\n\nReset your password using the link below:\n%s\n\nIf you did not request this, ignore this email.", resetLink)
		htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #0f172a; color: #f8fafc; padding: 24px; margin: 0;">
  <div style="max-width: 600px; margin: 0 auto; background-color: #1e293b; border-radius: 12px; border: 1px solid #334155; overflow: hidden; padding: 32px;">
    <div style="border-bottom: 1px solid #334155; padding-bottom: 16px; margin-bottom: 24px;">
      <h1 style="color: #38bdf8; font-size: 24px; margin: 0;">Avandab Fleet Security</h1>
    </div>
    <div style="background-color: #0f172a; border-radius: 8px; padding: 20px; margin-bottom: 24px;">
      <h2 style="color: #f8fafc; font-size: 18px; margin: 0 0 12px 0;">Password Reset Request</h2>
      <p style="color: #cbd5e1; font-size: 14px; margin: 0 0 20px 0;">Click the button below to choose a new secure password for your account.</p>
      <a href="%s" style="display: inline-block; background-color: #0284c7; color: #ffffff; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600; font-size: 14px;">Reset Password</a>
    </div>
    <p style="color: #64748b; font-size: 12px;">If you did not request a password reset, please ignore this email.</p>
  </div>
</body>
</html>`, resetLink)

		_, err := EnqueueRichEmail(ctx, s.db, tenantID, email, "password_reset", EmailPayload{
			Subject:  subject,
			Body:     textBody,
			HTMLBody: htmlBody,
		})
		if err != nil {
			s.logger.Error("comm event subscriber: failed to enqueue password reset email", "email", email, "error", err)
			return err
		}
		s.logger.Info("comm event subscriber: queued password reset email", "email", email)
		return nil
	}

	// Welcome / User registration
	if userName == "" {
		userName = "Valued User"
	}
	subject := "Welcome to Avandab Fleet Management"
	textBody := fmt.Sprintf("Hello %s,\n\nWelcome to Avandab Fleet. Your account has been registered successfully.\n\nLog in at any time to manage fleet operations, bookings, and tracking.", userName)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #0f172a; color: #f8fafc; padding: 24px; margin: 0;">
  <div style="max-width: 600px; margin: 0 auto; background-color: #1e293b; border-radius: 12px; border: 1px solid #334155; overflow: hidden; padding: 32px;">
    <div style="border-bottom: 1px solid #334155; padding-bottom: 16px; margin-bottom: 24px;">
      <h1 style="color: #38bdf8; font-size: 24px; margin: 0;">Avandab Fleet</h1>
      <p style="color: #94a3b8; font-size: 14px; margin: 4px 0 0 0;">Next-Gen Fleet &amp; Logistics Platform</p>
    </div>
    <div style="background-color: #0f172a; border-radius: 8px; padding: 20px; margin-bottom: 24px;">
      <h2 style="color: #f8fafc; font-size: 18px; margin: 0 0 12px 0;">Welcome, %s!</h2>
      <p style="color: #cbd5e1; font-size: 14px; line-height: 1.5; margin: 0;">Your account has been activated. You can now access real-time dispatch, automated e-Way bills, live GPS tracking, and instant invoicing.</p>
    </div>
    <div style="margin-top: 32px; border-top: 1px solid #334155; padding-top: 16px; font-size: 12px; color: #64748b;">
      Avandab Fleet Logistics • Automated Transactional Communication
    </div>
  </div>
</body>
</html>`, userName)

	_, err := EnqueueRichEmail(ctx, s.db, tenantID, email, "welcome", EmailPayload{
		Subject:  subject,
		Body:     textBody,
		HTMLBody: htmlBody,
	})
	if err != nil {
		s.logger.Error("comm event subscriber: failed to enqueue welcome email", "email", email, "error", err)
		return err
	}
	s.logger.Info("comm event subscriber: queued welcome email", "email", email)
	return nil
}

// toMap converts arbitrary struct / map payload into map[string]interface{}.
func toMap(v any) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok && val != nil {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
			if s := fmt.Sprint(val); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func getFloat(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		if val, ok := m[k]; ok && val != nil {
			switch v := val.(type) {
			case float64:
				return v
			case float32:
				return float64(v)
			case int:
				return float64(v)
			case int64:
				return float64(v)
			}
		}
	}
	return 0
}
