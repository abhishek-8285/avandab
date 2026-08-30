package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"transport-app/internal/customer/domain"
)

type SQLCustomerRepository struct {
	db *sql.DB
}

func NewSQLCustomerRepository(db *sql.DB) *SQLCustomerRepository {
	return &SQLCustomerRepository{db: db}
}

func (r *SQLCustomerRepository) SaveQuote(ctx context.Context, tenantID string, q *domain.Quote) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO customer_quotes (
			id, tenant_id, customer_id, origin, destination, cargo_type, vehicle_type,
			weight_kg, distance_km, base_rate, per_km_rate, estimated_toll, subtotal,
			gst_rate, gst_amount, discount_amount, total_price, status, expires_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, tenantID, q.CustomerID, q.Origin, q.Destination, q.CargoType, q.VehicleType,
		q.WeightKg, q.DistanceKm, q.BaseRate, q.PerKmRate, q.EstimatedToll, q.Subtotal,
		q.GSTRate, q.GSTAmount, q.DiscountAmount, q.TotalPrice, q.Status, q.ExpiresAt, q.CreatedAt)
	return err
}

func (r *SQLCustomerRepository) GetQuote(ctx context.Context, tenantID, quoteID string) (*domain.Quote, error) {
	var q domain.Quote
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, customer_id, origin, destination, cargo_type, vehicle_type,
		       weight_kg, distance_km, base_rate, per_km_rate, estimated_toll, subtotal,
		       gst_rate, gst_amount, discount_amount, total_price, status, expires_at, created_at
		FROM customer_quotes
		WHERE tenant_id = ? AND id = ?`,
		tenantID, quoteID).Scan(
		&q.ID, &q.TenantID, &q.CustomerID, &q.Origin, &q.Destination, &q.CargoType, &q.VehicleType,
		&q.WeightKg, &q.DistanceKm, &q.BaseRate, &q.PerKmRate, &q.EstimatedToll, &q.Subtotal,
		&q.GSTRate, &q.GSTAmount, &q.DiscountAmount, &q.TotalPrice, &q.Status, &q.ExpiresAt, &q.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("quote not found")
		}
		return nil, err
	}
	return &q, nil
}

func (r *SQLCustomerRepository) MarkQuoteConverted(ctx context.Context, tenantID, quoteID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE customer_quotes
		SET status = 'converted'
		WHERE tenant_id = ? AND id = ?`,
		tenantID, quoteID)
	return err
}

func (r *SQLCustomerRepository) GetBookingByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (string, error) {
	var bookingID string
	err := r.db.QueryRowContext(ctx, `
		SELECT booking_id FROM customer_booking_details
		WHERE tenant_id = ? AND idempotency_key = ?`,
		tenantID, idempotencyKey).Scan(&bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return bookingID, nil
}

func (r *SQLCustomerRepository) CreateBookingWithDetails(ctx context.Context, tenantID string, b map[string]interface{}, d map[string]interface{}) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now()
	// 1. Insert core booking
	_, err = tx.ExecContext(ctx, `
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, cargo_weight, price, notes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b["id"], tenantID, b["booking_number"], b["customer_id"], b["pickup_date"], b["route_id"],
		b["vehicle_type"], b["passengers"], b["cargo_weight"], b["price"], b["notes"], b["status"], now, now)
	if err != nil {
		return fmt.Errorf("failed inserting booking: %w", err)
	}

	// 2. Insert customer details
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_booking_details (
			booking_id, tenant_id, quote_id, idempotency_key, pickup_address, pickup_lat, pickup_lng,
			pickup_contact_name, pickup_contact_phone, delivery_address, delivery_lat, delivery_lng,
			delivery_contact_name, delivery_contact_phone, scheduled_at, cargo_description, special_instructions,
			payment_status, payment_method, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d["booking_id"], tenantID, d["quote_id"], d["idempotency_key"], d["pickup_address"], d["pickup_lat"], d["pickup_lng"],
		d["pickup_contact_name"], d["pickup_contact_phone"], d["delivery_address"], d["delivery_lat"], d["delivery_lng"],
		d["delivery_contact_name"], d["delivery_contact_phone"], d["scheduled_at"], d["cargo_description"], d["special_instructions"],
		d["payment_status"], d["payment_method"], now, now)
	if err != nil {
		return fmt.Errorf("failed inserting customer details: %w", err)
	}

	return tx.Commit()
}

func (r *SQLCustomerRepository) CancelCustomerBooking(ctx context.Context, tenantID, customerID, bookingID, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM bookings
		WHERE tenant_id = ? AND id = ? AND customer_id = ?`,
		tenantID, bookingID, customerID).Scan(&currentStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("booking not found or unauthorized")
		}
		return err
	}

	if currentStatus == "completed" {
		return errors.New("cannot cancel a completed booking")
	}
	if currentStatus == "in_progress" || currentStatus == "in_transit" {
		return errors.New("cannot cancel a booking with active in-progress trip")
	}
	if currentStatus == "cancelled" {
		return nil // idempotent cancel
	}

	// Check if there is an active/completed trip for this booking
	var tripStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status FROM trips
		WHERE tenant_id = ? AND booking_id = ?
		ORDER BY created_at DESC LIMIT 1`,
		tenantID, bookingID).Scan(&tripStatus)
	if err == nil {
		if tripStatus == "completed" || tripStatus == "delivered" {
			return errors.New("cannot cancel a completed booking")
		}
		if tripStatus != "assigned" && tripStatus != "cancelled" && tripStatus != "draft" {
			return errors.New("cannot cancel a booking with active in-progress trip")
		}
	}

	now := time.Now()
	_, err = tx.ExecContext(ctx, `UPDATE bookings SET status = 'cancelled', updated_at = ? WHERE tenant_id = ? AND id = ?`, now, tenantID, bookingID)
	if err != nil {
		return err
	}

	_, _ = tx.ExecContext(ctx, `
		UPDATE customer_booking_details
		SET cancellation_reason = ?, cancelled_by = ?, cancelled_at = ?, updated_at = ?
		WHERE tenant_id = ? AND booking_id = ?`,
		reason, customerID, now, now, tenantID, bookingID)

	// Cancel any active dispatch offers for this booking
	_, _ = tx.ExecContext(ctx, `
		UPDATE dispatch_offers
		SET status = 'cancelled', responded_at = ?
		WHERE tenant_id = ? AND booking_id = ? AND status = 'offered'`,
		now, tenantID, bookingID)

	return tx.Commit()
}

func (r *SQLCustomerRepository) GetCustomerTrackingProjection(ctx context.Context, tenantID, customerID, bookingID string) (*domain.CustomerBookingTrackingProjection, error) {
	var p domain.CustomerBookingTrackingProjection
	var quoteID, idempKey, pContactName, pContactPhone, dContactName, dContactPhone, cargoDesc, specialInst, payStatus, payMethod sql.NullString
	var pLat, pLng, dLat, dLng sql.NullFloat64
	var scheduledAt, createdAt, updatedAt sql.NullTime
	var rawPrice float64

	err := r.db.QueryRowContext(ctx, `
		SELECT b.id, b.booking_number, b.status, b.price, b.created_at, b.updated_at,
		       d.quote_id, d.idempotency_key, d.pickup_address, d.pickup_lat, d.pickup_lng,
		       d.pickup_contact_name, d.pickup_contact_phone, d.delivery_address, d.delivery_lat, d.delivery_lng,
		       d.delivery_contact_name, d.delivery_contact_phone, d.scheduled_at, d.cargo_description,
		       d.special_instructions, d.payment_status, d.payment_method
		FROM bookings b
		LEFT JOIN customer_booking_details d ON b.id = d.booking_id AND b.tenant_id = d.tenant_id
		WHERE b.tenant_id = ? AND b.id = ? AND b.customer_id = ?`,
		tenantID, bookingID, customerID).Scan(
		&p.BookingID, &p.BookingNumber, &p.Status, &rawPrice, &createdAt, &updatedAt,
		&quoteID, &idempKey, &p.Pickup.Address, &pLat, &pLng,
		&pContactName, &pContactPhone, &p.Delivery.Address, &dLat, &dLng,
		&dContactName, &dContactPhone, &scheduledAt, &cargoDesc,
		&specialInst, &payStatus, &payMethod)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("booking not found or unauthorized")
		}
		return nil, err
	}

	if pLat.Valid {
		p.Pickup.Latitude = &pLat.Float64
	}
	if pLng.Valid {
		p.Pickup.Longitude = &pLng.Float64
	}
	p.Pickup.ContactName = pContactName.String
	p.Pickup.ContactPhone = pContactPhone.String

	if dLat.Valid {
		p.Delivery.Latitude = &dLat.Float64
	}
	if dLng.Valid {
		p.Delivery.Longitude = &dLng.Float64
	}
	p.Delivery.ContactName = dContactName.String
	p.Delivery.ContactPhone = dContactPhone.String

	p.Payment = domain.PaymentSummary{
		Status:        payStatus.String,
		TotalPrice:    rawPrice,
		Subtotal:      rawPrice / 1.05,
		TaxAmount:     rawPrice - (rawPrice / 1.05),
		PaymentMethod: payMethod.String,
	}
	if p.Payment.Status == "" {
		p.Payment.Status = "pending"
	}

	if createdAt.Valid {
		p.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		p.UpdatedAt = updatedAt.Time
	}

	// 2. Check for assigned Trip, Driver, and Vehicle
	var tripID, driverID, vehicleID, tripStatus string
	err = r.db.QueryRowContext(ctx, `
		SELECT id, driver_id, vehicle_id, status
		FROM trips
		WHERE tenant_id = ? AND booking_id = ?
		ORDER BY created_at DESC LIMIT 1`,
		tenantID, bookingID).Scan(&tripID, &driverID, &vehicleID, &tripStatus)
	if err == nil {
		if p.Status != "completed" && p.Status != "cancelled" {
			p.Status = tripStatus
		}

		// Vehicle details
		var v domain.VehicleView
		var vModel, vPlate, vType sql.NullString
		_ = r.db.QueryRowContext(ctx, `
			SELECT id, plate_number, model, type
			FROM vehicles WHERE tenant_id = ? AND id = ?`,
			tenantID, vehicleID).Scan(&v.VehicleID, &vPlate, &vModel, &vType)
		v.PlateNumber = vPlate.String
		v.Model = vModel.String
		v.VehicleType = vType.String
		p.Vehicle = &v

		// Driver masked details
		var d domain.DriverMaskedView
		var dFirst, dPhone sql.NullString
		var dScore sql.NullFloat64
		_ = r.db.QueryRowContext(ctx, `
			SELECT id, first_name, phone, score
			FROM drivers WHERE tenant_id = ? AND id = ?`,
			tenantID, driverID).Scan(&d.DriverID, &dFirst, &dPhone, &dScore)
		d.FirstName = dFirst.String
		d.PhoneMasked = domain.MaskPhoneNumber(dPhone.String)
		if dScore.Valid {
			d.Score = &dScore.Float64
		}
		p.Driver = &d

		// Live GPS Tracking point from driver_vehicle_latest_positions
		var pos domain.LiveTrackingPoint
		var speed, heading sql.NullFloat64
		var posTime time.Time
		err = r.db.QueryRowContext(ctx, `
			SELECT latitude, longitude, speed_kmph, heading, recorded_at
			FROM driver_vehicle_latest_positions
			WHERE tenant_id = ? AND vehicle_id = ?`,
			tenantID, vehicleID).Scan(&pos.Latitude, &pos.Longitude, &speed, &heading, &posTime)
		if err == nil {
			if speed.Valid {
				pos.SpeedKmph = &speed.Float64
			}
			if heading.Valid {
				pos.Heading = &heading.Float64
			}
			pos.UpdatedAt = posTime
			p.Tracking = &pos
		}
	}

	// 3. Load Documents (LR, E-Way Bill, POD)
	docRows, err := r.db.QueryContext(ctx, `
		SELECT id, document_type, verification_status, file_path, created_at
		FROM driver_documents
		WHERE tenant_id = ? AND entity_type = 'trip' AND entity_id = ?`,
		tenantID, tripID)
	if err == nil {
		defer func() { _ = docRows.Close() }()
		for docRows.Next() {
			var doc domain.DocumentSummary
			var path sql.NullString
			if scanErr := docRows.Scan(&doc.DocumentID, &doc.DocumentType, &doc.Status, &path, &doc.CreatedAt); scanErr == nil {
				doc.URL = path.String
				p.Documents = append(p.Documents, doc)
			}
		}
	}
	if p.Documents == nil {
		p.Documents = []domain.DocumentSummary{}
	}

	return &p, nil
}

func (r *SQLCustomerRepository) ListCustomerBookings(ctx context.Context, tenantID, customerID string, limit, offset int) ([]domain.CustomerBookingTrackingProjection, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM bookings
		WHERE tenant_id = ? AND customer_id = ?
		ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		tenantID, customerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	var results []domain.CustomerBookingTrackingProjection
	for _, id := range ids {
		p, err := r.GetCustomerTrackingProjection(ctx, tenantID, customerID, id)
		if err == nil && p != nil {
			results = append(results, *p)
		}
	}
	return results, nil
}
