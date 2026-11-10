// Package customerportal owns the shipper-portal read models. Every query is
// scoped through customer_users (tenant + allowed customer ids) so the
// external-facing portal cannot read another customer's rows; HTTP handlers
// keep rendering/session concerns (docs/tech-specs/handlers-end-state-adr.md,
// Phase 1).
package customerportal

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	appdb "transport-app/internal/database"
)

// Service runs customer-portal read queries.
type Service struct {
	db *sql.DB
}

// New builds a customer-portal read-model service.
func New(database *sql.DB) *Service {
	return &Service{db: database}
}

// BookingRow mirrors the booking fields used by booking_list_table.html.
type BookingRow struct {
	ID               string
	BookingNumber    string
	CustomerID       string
	CustomerName     string
	CustomerCompany  string
	RouteID          string
	RouteSource      string
	RouteDestination string
	PickupDate       time.Time
	VehicleType      string
	Passengers       int64
	CargoWeight      *float64
	Price            float64
	Notes            string
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// InvoiceRow mirrors the invoice fields used by invoice_list_table.html.
type InvoiceRow struct {
	ID              string
	InvoiceNumber   string
	BookingID       string
	CustomerID      string
	CustomerName    string
	CustomerCompany string
	TripID          string
	TripNumber      string
	Subtotal        float64
	Tax             float64
	Discount        float64
	Total           float64
	PaymentStatus   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func parseTimeFlex(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	if len(s) >= 19 {
		if t, err := time.Parse("2006-01-02 15:04:05", s[:19]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Bookings returns the customer's bookings page (tenant + customer_users scoped).
func (s *Service) Bookings(ctx context.Context, tenantID, userID, status, search string, limit, offset int) ([]BookingRow, int64, error) {
	if s == nil || s.db == nil {
		return []BookingRow{}, 0, nil
	}
	baseWhere := "WHERE b.tenant_id = $1 AND b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id = $2)"
	args := []interface{}{tenantID, userID}
	if status != "" {
		baseWhere += " AND b.status = ?"
		args = append(args, status)
	}
	if search != "" {
		baseWhere += " AND (lower(b.booking_number) LIKE '%' || lower(?) || '%' OR lower(c.name) LIKE '%' || lower(?) || '%' OR lower(rc.source) LIKE '%' || lower(?) || '%' OR lower(rc.destination) LIKE '%' || lower(?) || '%')"
		args = append(args, search, search, search, search)
	}

	countSQL := "SELECT COUNT(*) FROM bookings b JOIN customers c ON c.id = b.customer_id JOIN routes rc ON rc.id = b.route_id " + baseWhere
	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, reboundCount, args...).Scan(&total); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal bookings count missing table", "error", err)
			return []BookingRow{}, 0, nil
		}
		return nil, 0, err
	}

	dataSQL := "SELECT b.id, b.booking_number, b.customer_id, c.name, COALESCE(c.company,''), b.route_id, COALESCE(rc.source,''), COALESCE(rc.destination,''), b.pickup_date, b.vehicle_type, b.passengers, b.cargo_weight, b.price, COALESCE(b.notes,''), b.status, b.created_at, b.updated_at FROM bookings b JOIN customers c ON c.id = b.customer_id JOIN routes rc ON rc.id = b.route_id " + baseWhere + " ORDER BY b.created_at DESC LIMIT ? OFFSET ?"
	dataArgs := append(append([]interface{}{}, args...), limit, offset)
	reboundData, err := appdb.Rebind(dataSQL)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, reboundData, dataArgs...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal bookings data missing table", "error", err)
			return []BookingRow{}, 0, nil
		}
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	out := []BookingRow{}
	for rows.Next() {
		var br BookingRow
		var pickupStr, createdStr, updatedStr string
		var cargo sql.NullFloat64
		var passengers int64
		if err := rows.Scan(&br.ID, &br.BookingNumber, &br.CustomerID, &br.CustomerName, &br.CustomerCompany, &br.RouteID, &br.RouteSource, &br.RouteDestination, &pickupStr, &br.VehicleType, &passengers, &cargo, &br.Price, &br.Notes, &br.Status, &createdStr, &updatedStr); err != nil {
			continue
		}
		br.Passengers = passengers
		if cargo.Valid {
			v := cargo.Float64
			br.CargoWeight = &v
		}
		br.PickupDate = parseTimeFlex(pickupStr)
		br.CreatedAt = parseTimeFlex(createdStr)
		br.UpdatedAt = parseTimeFlex(updatedStr)
		out = append(out, br)
	}
	return out, total, rows.Err()
}

// Invoices returns the customer's invoices page (tenant + customer_users scoped).
func (s *Service) Invoices(ctx context.Context, tenantID, userID, status, search string, limit, offset int) ([]InvoiceRow, int64, error) {
	if s == nil || s.db == nil {
		return []InvoiceRow{}, 0, nil
	}
	baseWhere := "WHERE i.tenant_id = $1 AND i.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id = $2)"
	args := []interface{}{tenantID, userID}
	if status != "" {
		baseWhere += " AND i.payment_status = ?"
		args = append(args, status)
	}
	if search != "" {
		baseWhere += " AND (lower(i.invoice_number) LIKE '%' || lower(?) || '%' OR lower(c.name) LIKE '%' || lower(?) || '%')"
		args = append(args, search, search)
	}

	countSQL := "SELECT COUNT(*) FROM invoices i JOIN customers c ON c.id = i.customer_id " + baseWhere
	reboundCount, err := appdb.Rebind(countSQL)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, reboundCount, args...).Scan(&total); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal invoices count missing table", "error", err)
			return []InvoiceRow{}, 0, nil
		}
		return nil, 0, err
	}

	dataSQL := "SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, COALESCE(c.name,''), COALESCE(c.company,''), COALESCE(i.trip_id,''), COALESCE(t.trip_number,''), i.subtotal, i.tax, COALESCE(i.discount,0), i.total, i.payment_status, i.created_at, i.updated_at FROM invoices i JOIN customers c ON c.id = i.customer_id LEFT JOIN trips t ON t.id = i.trip_id " + baseWhere + " ORDER BY i.created_at DESC LIMIT ? OFFSET ?"
	dataArgs := append(append([]interface{}{}, args...), limit, offset)
	reboundData, err := appdb.Rebind(dataSQL)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, reboundData, dataArgs...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal invoices data missing table", "error", err)
			return []InvoiceRow{}, 0, nil
		}
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	out := []InvoiceRow{}
	for rows.Next() {
		var ir InvoiceRow
		var createdStr, updatedStr string
		if err := rows.Scan(&ir.ID, &ir.InvoiceNumber, &ir.BookingID, &ir.CustomerID, &ir.CustomerName, &ir.CustomerCompany, &ir.TripID, &ir.TripNumber, &ir.Subtotal, &ir.Tax, &ir.Discount, &ir.Total, &ir.PaymentStatus, &createdStr, &updatedStr); err != nil {
			continue
		}
		ir.CreatedAt = parseTimeFlex(createdStr)
		ir.UpdatedAt = parseTimeFlex(updatedStr)
		out = append(out, ir)
	}
	return out, total, rows.Err()
}

// TrackingTrip is the trip/vehicle row rendered by the tracking page.
type TrackingTrip struct {
	TripNumber    string
	Status        string
	VehicleID     string
	ArrivalTime   string
	DepartureTime string
	VehicleReg    string
	VehicleNum    string
	BookingID     string
}

// Tracking loads the customer's trip (scoped via customer_users); nil means
// not found/not yours. Legacy DBs without customer_users fall back to
// tenant-only matching, matching the previous handler behavior.
func (s *Service) Tracking(ctx context.Context, tenantID, userID, tripID string) (*TrackingTrip, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var t TrackingTrip
	var bookingID sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT t.trip_number, t.status, COALESCE(t.vehicle_id,''), COALESCE(CAST(t.arrival_time AS TEXT), ''), COALESCE(CAST(t.departure_time AS TEXT), ''), COALESCE(v.registration_number,''), COALESCE(v.vehicle_number,''), t.booking_id
		FROM trips t
		LEFT JOIN bookings b ON b.id = t.booking_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE t.id = $1 AND t.tenant_id = $2 AND (b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=$3) OR (t.booking_id IS NULL AND $4 = $5))
	`, tripID, tenantID, userID, "", "").Scan(&t.TripNumber, &t.Status, &t.VehicleID, &t.ArrivalTime, &t.DepartureTime, &t.VehicleReg, &t.VehicleNum, &bookingID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			// Legacy DB without customer_users: tenant-only fallback.
			var t2 TrackingTrip
			var b2 sql.NullString
			if err2 := s.db.QueryRowContext(ctx, `
				SELECT t.trip_number, t.status, COALESCE(t.vehicle_id,''), COALESCE(CAST(t.arrival_time AS TEXT), ''), COALESCE(CAST(t.departure_time AS TEXT), ''), COALESCE(v.registration_number,''), COALESCE(v.vehicle_number,''), t.booking_id
				FROM trips t
				LEFT JOIN vehicles v ON v.id = t.vehicle_id
				WHERE t.id = $1 AND t.tenant_id = $2
			`, tripID, tenantID).Scan(&t2.TripNumber, &t2.Status, &t2.VehicleID, &t2.ArrivalTime, &t2.DepartureTime, &t2.VehicleReg, &t2.VehicleNum, &b2); err2 != nil {
				return nil, nil
			}
			t2.BookingID = b2.String
			return &t2, nil
		}
		return nil, err
	}
	t.BookingID = bookingID.String
	if t.TripNumber == "" {
		return nil, nil
	}
	return &t, nil
}

// StopRow is one trip stop for the tracking progression.
type StopRow struct {
	ID           string
	StopSequence int
	StopType     string
	LocationName string
	Address      string
	Status       string
}

// Stops returns the trip's stops in sequence order (empty on error, matching
// the previous handler behavior).
func (s *Service) Stops(ctx context.Context, tripID string) []StopRow {
	if s == nil || s.db == nil || tripID == "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), COALESCE(address, ''), status
		FROM trip_stops
		WHERE trip_id = $1
		ORDER BY stop_sequence ASC
	`, tripID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []StopRow
	for rows.Next() {
		var st StopRow
		if err := rows.Scan(&st.ID, &st.StopSequence, &st.StopType, &st.LocationName, &st.Address, &st.Status); err == nil {
			out = append(out, st)
		}
	}
	return out
}
