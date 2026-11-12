package application

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/eta"
)

// ErrConsoleNotFound is returned when the tenant-scoped vehicle does not exist.
var ErrConsoleNotFound = errors.New("console: not found")

// ConsoleReadModel owns the Command Center read queries (fleet strip and
// vehicle context panel). HTTP handlers keep caching/JSON/HTTP concerns
// (docs/tech-specs/handlers-end-state-adr.md, Phase 1).
type ConsoleReadModel struct {
	db  *sql.DB
	eta *eta.EtaService
	now func() time.Time
}

// NewConsoleReadModel builds the Command Center read model.
func NewConsoleReadModel(db *sql.DB, etaSvc *eta.EtaService) *ConsoleReadModel {
	return &ConsoleReadModel{db: db, eta: etaSvc, now: time.Now}
}

// FleetItem is one color-coded card in the fleet strip.
type FleetItem struct {
	ID        string   `json:"id"`
	Number    string   `json:"number"`
	Status    string   `json:"status"`
	Lat       *float64 `json:"lat,omitempty"`
	Lng       *float64 `json:"lng,omitempty"`
	SpeedKmph *float64 `json:"speed_kmph,omitempty"`
	At        string   `json:"at,omitempty"`
}

// Fleet returns every vehicle with its latest position (single query, no N+1).
func (m *ConsoleReadModel) Fleet(ctx context.Context, tenantID string) ([]FleetItem, error) {
	if m == nil || m.db == nil {
		return []FleetItem{}, nil
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT v.id, v.vehicle_number, v.status,
		       p.latitude, p.longitude, p.speed, p.device_time
		FROM vehicles v
		LEFT JOIN vehicle_latest_position p ON p.vehicle_id = v.id
		WHERE v.tenant_id = $1
		ORDER BY v.vehicle_number
		LIMIT 500`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := []FleetItem{}
	for rows.Next() {
		var it FleetItem
		var lat, lng, speed sql.NullFloat64
		var at sql.NullTime
		if err := rows.Scan(&it.ID, &it.Number, &it.Status, &lat, &lng, &speed, &at); err != nil {
			return nil, err
		}
		if lat.Valid && lng.Valid {
			it.Lat, it.Lng = &lat.Float64, &lng.Float64
		}
		if speed.Valid {
			it.SpeedKmph = &speed.Float64
		}
		if at.Valid {
			it.At = at.Time.UTC().Format(time.RFC3339)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// VehicleContextResponse is the Spec 22 §2.3 wire shape.
type VehicleContextResponse struct {
	Vehicle        VehicleCtx    `json:"vehicle"`
	Position       *PositionCtx  `json:"position,omitempty"`
	Trip           *TripCtx      `json:"trip,omitempty"`
	Driver         *DriverCtx    `json:"driver,omitempty"`
	PnlKmToday     float64       `json:"pnl_km_today"`
	KharchaPending []KharchaItem `json:"kharcha_pending"`
	EwayBill       *EwayCtx      `json:"eway_bill,omitempty"`
	FastagBalance  *float64      `json:"fastag_balance"`
	DocsExpiring   []DocItem     `json:"docs_expiring"`
}

type VehicleCtx struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Status string `json:"status"`
}

type PositionCtx struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	SpeedKmph float64 `json:"speed_kmph"`
	At        string  `json:"at"`
}

type TripCtx struct {
	ID    string     `json:"id"`
	Route string     `json:"route"`
	EtaAt *time.Time `json:"eta_at,omitempty"`
}

type DriverCtx struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Phone *string `json:"phone,omitempty"`
}

type KharchaItem struct {
	ID       string  `json:"id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
}

type EwayCtx struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

type DocItem struct {
	Kind      string `json:"kind"`
	ExpiresOn string `json:"expires_on"`
}

// VehicleContext assembles the vehicle context panel from real tenant-scoped
// tables (no N+1). Unknown vehicle for the tenant returns ErrConsoleNotFound.
func (m *ConsoleReadModel) VehicleContext(ctx context.Context, tenantID, vehicleID string) (*VehicleContextResponse, error) {
	if m == nil || m.db == nil {
		return nil, ErrConsoleNotFound
	}
	resp := &VehicleContextResponse{KharchaPending: []KharchaItem{}, DocsExpiring: []DocItem{}}

	// 1. Vehicle (existence + tenancy gate for everything below).
	err := m.db.QueryRowContext(ctx, `
		SELECT id, vehicle_number, status FROM vehicles WHERE id = $1 AND tenant_id = $2`,
		vehicleID, tenantID).Scan(&resp.Vehicle.ID, &resp.Vehicle.Number, &resp.Vehicle.Status)
	if err == sql.ErrNoRows {
		return nil, ErrConsoleNotFound
	}
	if err != nil {
		return nil, err
	}

	// 2. Latest position.
	var lat, lng, speed sql.NullFloat64
	var deviceTime sql.NullTime
	posErr := m.db.QueryRowContext(ctx, `
		SELECT latitude, longitude, speed, device_time
		FROM vehicle_latest_position WHERE vehicle_id = $1 AND tenant_id = $2`,
		vehicleID, tenantID).Scan(&lat, &lng, &speed, &deviceTime)
	if posErr == nil && lat.Valid && lng.Valid {
		p := &PositionCtx{Lat: lat.Float64, Lng: lng.Float64}
		if speed.Valid {
			p.SpeedKmph = speed.Float64
		}
		if deviceTime.Valid {
			p.At = deviceTime.Time.UTC().Format(time.RFC3339)
		}
		resp.Position = p
	}

	// 3. Active trip + route.
	var tripID, driverID sql.NullString
	var routeLabel string
	var distance float64
	tripErr := m.db.QueryRowContext(ctx, `
		SELECT t.id, t.driver_id, COALESCE(r.source,'') || '→' || COALESCE(r.destination,''), COALESCE(r.distance,0)
		FROM trips t JOIN routes r ON r.id = t.route_id
		WHERE t.vehicle_id = $1 AND t.tenant_id = $2
		  AND t.status IN ('assigned','started','reached_pickup','in_transit')
		ORDER BY t.departure_time DESC LIMIT 1`,
		vehicleID, tenantID).Scan(&tripID, &driverID, &routeLabel, &distance)
	hasTrip := tripErr == nil && tripID.Valid
	if hasTrip {
		t := &TripCtx{ID: tripID.String, Route: routeLabel}
		if m.eta != nil {
			if res, etaErr := m.eta.Calculate(ctx, tripID.String); etaErr == nil {
				arrival := res.ArrivalAt
				t.EtaAt = &arrival
			}
		}
		resp.Trip = t
	}

	// 4. Driver on the active trip.
	if hasTrip && driverID.Valid && driverID.String != "" {
		var d DriverCtx
		var phone sql.NullString
		dErr := m.db.QueryRowContext(ctx, `
			SELECT id,
			       COALESCE(first_name,'') || ' ' || COALESCE(last_name,''),
			       phone
			FROM drivers WHERE id = $1`, driverID.String).Scan(&d.ID, &d.Name, &phone)
		if dErr == nil {
			if phone.Valid && phone.String != "" {
				d.Phone = &phone.String
			}
			resp.Driver = &d
		}
	}

	// 5. P&L per km today: live margin over route length.
	if hasTrip && distance > 0 {
		var margin float64
		if mErr := m.db.QueryRowContext(ctx,
			`SELECT estimated_margin FROM trips WHERE id = $1`, tripID.String).Scan(&margin); mErr == nil {
			resp.PnlKmToday = margin / distance
		}
	}

	// 6. Pending kharcha on this vehicle's trips.
	kRows, kErr := m.db.QueryContext(ctx, `
		SELECT de.id, de.amount, COALESCE(de.category, de.expense_type, '')
		FROM driver_expenses de JOIN trips t ON de.trip_id = t.id
		WHERE t.vehicle_id = $1 AND t.tenant_id = $2 AND de.status = 'pending'
		ORDER BY de.created_at DESC LIMIT 10`, vehicleID, tenantID)
	if kErr == nil {
		defer func() { _ = kRows.Close() }()
		for kRows.Next() {
			var it KharchaItem
			if err := kRows.Scan(&it.ID, &it.Amount, &it.Category); err == nil {
				resp.KharchaPending = append(resp.KharchaPending, it)
			}
		}
	}

	// 7. Active e-way bill on the active trip.
	if hasTrip {
		var e EwayCtx
		var validUntil time.Time
		eErr := m.db.QueryRowContext(ctx, `
			SELECT eb.id, eb.valid_until FROM eway_bills eb
			WHERE eb.trip_id = $1 AND eb.status = 'active'
			ORDER BY eb.valid_until ASC LIMIT 1`, tripID.String).
			Scan(&e.ID, &validUntil)
		if eErr == nil {
			e.ExpiresAt = validUntil.UTC().Format(time.RFC3339)
			resp.EwayBill = &e
		}
	}

	// 8. FASTag balance (sum across tags fitted to the vehicle).
	var balance float64
	bErr := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(balance),0) FROM fastag_tags
		WHERE vehicle_id = $1 AND tenant_id = $2`, vehicleID, tenantID).Scan(&balance)
	if bErr == nil {
		resp.FastagBalance = &balance
	}

	// 9. Documents expiring within 30 days.
	dRows, dErr := m.db.QueryContext(ctx, `
		SELECT doc_type, expiry_date FROM vehicle_documents
		WHERE vehicle_id = $1 AND expiry_date IS NOT NULL
		  AND substr(CAST(expiry_date AS TEXT), 1, 10) <= $2
		ORDER BY expiry_date ASC LIMIT 5`, vehicleID, m.now().UTC().AddDate(0, 0, 30).Format("2006-01-02"))
	if dErr == nil {
		defer func() { _ = dRows.Close() }()
		for dRows.Next() {
			var it DocItem
			var expiry sql.NullTime
			if err := dRows.Scan(&it.Kind, &expiry); err == nil && expiry.Valid {
				it.ExpiresOn = expiry.Time.Format("2006-01-02")
				resp.DocsExpiring = append(resp.DocsExpiring, it)
			}
		}
	}

	return resp, nil
}
