package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BackhaulMatchDTO represents a candidate backhaul booking match.
type BackhaulMatchDTO struct {
	BookingID          string    `json:"booking_id"`
	BookingNumber      string    `json:"booking_number"`
	CustomerID         string    `json:"customer_id"`
	CustomerName       string    `json:"customer_name,omitempty"`
	PickupAddress      string    `json:"pickup_address"`
	PickupLat          float64   `json:"pickup_lat"`
	PickupLng          float64   `json:"pickup_lng"`
	DeliveryAddress    string    `json:"delivery_address"`
	DeliveryLat        float64   `json:"delivery_lat"`
	DeliveryLng        float64   `json:"delivery_lng"`
	PickupTime         time.Time `json:"pickup_time"`
	CargoWeight        float64   `json:"cargo_weight"`
	VehicleType        string    `json:"vehicle_type"`
	Price              float64   `json:"price"`
	DeadheadKm         float64   `json:"deadhead_km"`
	PickupDistanceKm   float64   `json:"pickup_distance_km"`
	DropoffDistanceKm  float64   `json:"dropoff_distance_km"`
	FreightMargin      float64   `json:"freight_margin"`
	CorridorSavingsPct float64   `json:"corridor_savings_pct,omitempty"`
}

// BackhaulOfferRequest represents a request to create a backhaul dispatch offer.
type BackhaulOfferRequest struct {
	TripID      string  `json:"trip_id"`
	BookingID   string  `json:"booking_id"`
	OfferedRate float64 `json:"offered_rate"`
}

// BackhaulOfferResponse represents the created dispatch offer.
type BackhaulOfferResponse struct {
	OfferID     string    `json:"offer_id"`
	TripID      string    `json:"trip_id"`
	BookingID   string    `json:"booking_id"`
	DriverID    string    `json:"driver_id"`
	VehicleID   string    `json:"vehicle_id"`
	OfferedRate float64   `json:"offered_rate"`
	Status      string    `json:"status"`
	OfferedAt   time.Time `json:"offered_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// BackhaulService executes return-corridor matching and dispatch offer creation.
type BackhaulService struct {
	db *sql.DB
}

// NewBackhaulService creates a new BackhaulService.
func NewBackhaulService(db *sql.DB) *BackhaulService {
	return &BackhaulService{db: db}
}

// HaversineDistanceKm calculates the great-circle distance between two decimal degree points.
func HaversineDistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	a := math.Sin(dLat/2.0)*math.Sin(dLat/2.0) +
		math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*
			math.Sin(dLon/2.0)*math.Sin(dLon/2.0)
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return math.Round(r*c*100.0) / 100.0
}

type tripMetadata struct {
	id          string
	status      string
	driverID    *string
	vehicleID   *string
	routeID     string
	bookingID   *string
	arrivalTime *time.Time
}

// FindBackhaulMatches evaluates candidate unassigned bookings for a trip nearing or at delivery.
func (s *BackhaulService) FindBackhaulMatches(ctx context.Context, tenantID, tripID string, radiusKm float64) ([]BackhaulMatchDTO, error) {
	if s.db == nil {
		return nil, errors.New("database connection unavailable")
	}
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if tripID == "" {
		return nil, errors.New("trip_id is required")
	}

	// Radius defaults and clamping
	if radiusKm <= 0 {
		radiusKm = 50.0
	} else if radiusKm > 150.0 {
		radiusKm = 150.0
	}

	// 1. Fetch Trip
	var meta tripMetadata
	var driverID, vehicleID, bookingID sql.NullString
	var arrivalTime sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, status, driver_id, vehicle_id, route_id, booking_id, arrival_time
		FROM trips
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, tripID).Scan(
		&meta.id, &meta.status, &driverID, &vehicleID, &meta.routeID, &bookingID, &arrivalTime,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("trip not found: %s", tripID)
		}
		return nil, fmt.Errorf("failed fetching trip: %w", err)
	}
	if driverID.Valid {
		meta.driverID = &driverID.String
	}
	if vehicleID.Valid {
		meta.vehicleID = &vehicleID.String
	}
	if bookingID.Valid {
		meta.bookingID = &bookingID.String
	}
	if arrivalTime.Valid {
		meta.arrivalTime = &arrivalTime.Time
	}

	// Trigger Check: trip must not be draft or cancelled.
	if meta.status == "draft" || meta.status == "cancelled" {
		return []BackhaulMatchDTO{}, nil
	}

	// If vehicle is missing, cannot match
	if meta.vehicleID == nil || *meta.vehicleID == "" {
		return []BackhaulMatchDTO{}, nil
	}

	// 2. Safety & Maintenance Block Checks (Spec 19 §2.3 & §6.3)
	var vehicleType string
	var vehicleCapacity int
	var vehicleStatus string
	var vehicleBlocked int
	var facilityID sql.NullString

	err = s.db.QueryRowContext(ctx, `
		SELECT vehicle_type, capacity, status, blocked, facility_id
		FROM vehicles
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, *meta.vehicleID).Scan(
		&vehicleType, &vehicleCapacity, &vehicleStatus, &vehicleBlocked, &facilityID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []BackhaulMatchDTO{}, nil
		}
		return nil, fmt.Errorf("failed fetching vehicle: %w", err)
	}

	// Compliance block or blocked/inactive/maintenance status
	if vehicleBlocked == 1 || vehicleStatus == "blocked" || vehicleStatus == "maintenance" || vehicleStatus == "inactive" {
		return []BackhaulMatchDTO{}, nil
	}

	// Pending maintenance work orders check (Spec 19 §2.3)
	var openWorkOrders int
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM work_orders
		WHERE tenant_id = $1 AND vehicle_id = $2 AND status IN ('open', 'assigned', 'in_progress', 'on_hold')`,
		tenantID, *meta.vehicleID).Scan(&openWorkOrders)
	if err == nil && openWorkOrders > 0 {
		return []BackhaulMatchDTO{}, nil
	}

	// 3. Resolve Destination Coordinates (lat_d, lon_d)
	destLat, destLon, destFound := s.resolveTripDestination(ctx, tenantID, meta)
	if !destFound {
		return nil, errors.New("cannot resolve trip destination coordinates for backhaul matching")
	}

	// 4. Resolve Vehicle Base / Home Coordinates (lat_b, lon_b)
	baseLat, baseLon, baseFound := s.resolveVehicleBase(ctx, tenantID, meta, facilityID)

	// 5. Query candidate unassigned bookings in same tenant
	query := `
		SELECT 
			b.id, b.booking_number, b.customer_id, b.route_id, b.vehicle_type,
			COALESCE(b.cargo_weight, 0.0), b.price, b.status, b.pickup_date,
			COALESCE(c.name, ''),
			COALESCE(cbd.pickup_address, ''),
			COALESCE(cbd.pickup_lat, 0.0),
			COALESCE(cbd.pickup_lng, 0.0),
			COALESCE(cbd.delivery_address, ''),
			COALESCE(cbd.delivery_lat, 0.0),
			COALESCE(cbd.delivery_lng, 0.0),
			cbd.scheduled_at,
			COALESCE(rl.source_lat, 0.0),
			COALESCE(rl.source_lng, 0.0),
			COALESCE(rl.dest_lat, 0.0),
			COALESCE(rl.dest_lng, 0.0),
			COALESCE(r.source, ''),
			COALESCE(r.destination, '')
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id AND c.tenant_id = b.tenant_id
		LEFT JOIN customer_booking_details cbd ON cbd.booking_id = b.id AND cbd.tenant_id = b.tenant_id
		LEFT JOIN routes r ON r.id = b.route_id
		LEFT JOIN route_locations rl ON rl.route_id = b.route_id
		WHERE b.tenant_id = $1
		  AND b.status IN ('pending', 'confirmed')
		  AND NOT EXISTS (
			  SELECT 1 FROM trips t WHERE t.booking_id = b.id AND t.tenant_id = b.tenant_id AND t.status NOT IN ('cancelled')
		  )
		  AND NOT EXISTS (
			  SELECT 1 FROM dispatch_offers dof WHERE dof.tenant_id = b.tenant_id AND dof.booking_id = b.id AND dof.status = 'accepted'
		  )`

	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed querying candidate bookings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	now := time.Now()
	var matches []BackhaulMatchDTO

	for rows.Next() {
		var (
			bID, bNumber, custID, routeID, bVehType, bStatus, custName string
			cbdPickAddr, cbdDelivAddr, rSource, rDest                  string
			cargoWeight, price                                         float64
			pickupDate                                                 time.Time
			cbdPickLat, cbdPickLng, cbdDelivLat, cbdDelivLng           float64
			schedAt                                                    sql.NullTime
			rlSourceLat, rlSourceLng, rlDestLat, rlDestLng             float64
		)

		if err := rows.Scan(
			&bID, &bNumber, &custID, &routeID, &bVehType,
			&cargoWeight, &price, &bStatus, &pickupDate,
			&custName,
			&cbdPickAddr, &cbdPickLat, &cbdPickLng,
			&cbdDelivAddr, &cbdDelivLat, &cbdDelivLng,
			&schedAt,
			&rlSourceLat, &rlSourceLng, &rlDestLat, &rlDestLng,
			&rSource, &rDest,
		); err != nil {
			return nil, fmt.Errorf("scan candidate booking: %w", err)
		}

		// Resolve pickup coordinates
		pLat, pLng := cbdPickLat, cbdPickLng
		if pLat == 0 && pLng == 0 {
			pLat, pLng = rlSourceLat, rlSourceLng
		}
		if pLat == 0 && pLng == 0 {
			continue // cannot geolocate candidate pickup
		}

		// Resolve delivery coordinates
		dLat, dLng := cbdDelivLat, cbdDelivLng
		if dLat == 0 && dLng == 0 {
			dLat, dLng = rlDestLat, rlDestLng
		}

		// 1. Spatial Filter: deadhead distance <= radiusKm
		deadheadKm := HaversineDistanceKm(destLat, destLon, pLat, pLng)
		if deadheadKm > radiusKm {
			continue
		}

		// 2. Temporal Filter: T_now - 2h <= T_pickup <= T_now + 18h
		pickupTime := pickupDate
		if schedAt.Valid && !schedAt.Time.IsZero() {
			pickupTime = schedAt.Time
		}
		if !pickupTime.IsZero() {
			if pickupTime.Before(now.Add(-2 * time.Hour)) {
				continue
			}
			if pickupTime.After(now.Add(18 * time.Hour)) {
				continue
			}
		}

		// 3. Payload Capacity & Body Type Constraints
		if vehicleCapacity > 0 && cargoWeight > 0 {
			if cargoWeight > float64(vehicleCapacity) {
				continue
			}
		}
		if bVehType != "" && vehicleType != "" {
			if !strings.EqualFold(bVehType, vehicleType) {
				continue
			}
		}

		// 4. Homeward Corridor Alignment
		var dropoffDistKm float64
		var corridorSavingsPct float64
		if baseFound && dLat != 0 && dLng != 0 {
			dropoffDistKm = HaversineDistanceKm(dLat, dLng, baseLat, baseLon)
			pickupDistToBase := HaversineDistanceKm(pLat, pLng, baseLat, baseLon)
			// Candidate dropoff must bring vehicle closer to base than pickup point
			if dropoffDistKm > pickupDistToBase {
				continue
			}

			directBaseKm := HaversineDistanceKm(destLat, destLon, baseLat, baseLon)
			if directBaseKm > 0 {
				savingsRatio := (1.0 - (deadheadKm / directBaseKm)) * 100.0
				if savingsRatio < 0 {
					savingsRatio = 0
				} else if savingsRatio > 100 {
					savingsRatio = 100
				}
				corridorSavingsPct = math.Round(savingsRatio*10.0) / 10.0
			}
		}

		// 5. Margin estimation: price minus fuel for deadhead (standard ₹25/km)
		freightMargin := price - (deadheadKm * 25.0)

		pickAddr := cbdPickAddr
		if pickAddr == "" {
			pickAddr = rSource
		}
		delivAddr := cbdDelivAddr
		if delivAddr == "" {
			delivAddr = rDest
		}

		matches = append(matches, BackhaulMatchDTO{
			BookingID:          bID,
			BookingNumber:      bNumber,
			CustomerID:         custID,
			CustomerName:       custName,
			PickupAddress:      pickAddr,
			PickupLat:          pLat,
			PickupLng:          pLng,
			DeliveryAddress:    delivAddr,
			DeliveryLat:        dLat,
			DeliveryLng:        dLng,
			PickupTime:         pickupTime,
			CargoWeight:        cargoWeight,
			VehicleType:        bVehType,
			Price:              price,
			DeadheadKm:         deadheadKm,
			PickupDistanceKm:   deadheadKm,
			DropoffDistanceKm:  dropoffDistKm,
			FreightMargin:      math.Round(freightMargin*100.0) / 100.0,
			CorridorSavingsPct: corridorSavingsPct,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate iteration error: %w", err)
	}

	// 6. Deterministic Sorting: lowest deadhead km, highest margin, tie-breaker booking ID
	sort.Slice(matches, func(i, j int) bool {
		if math.Abs(matches[i].DeadheadKm-matches[j].DeadheadKm) > 0.05 {
			return matches[i].DeadheadKm < matches[j].DeadheadKm
		}
		if math.Abs(matches[i].FreightMargin-matches[j].FreightMargin) > 0.05 {
			return matches[i].FreightMargin > matches[j].FreightMargin
		}
		return matches[i].BookingID < matches[j].BookingID
	})

	if matches == nil {
		matches = []BackhaulMatchDTO{}
	}
	return matches, nil
}

// CreateBackhaulOffer creates a dispatch offer for a driver on a completing trip to accept a return booking.
func (s *BackhaulService) CreateBackhaulOffer(ctx context.Context, tenantID string, req BackhaulOfferRequest) (*BackhaulOfferResponse, error) {
	if s.db == nil {
		return nil, errors.New("database connection unavailable")
	}
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if req.TripID == "" {
		return nil, errors.New("trip_id is required")
	}
	if req.BookingID == "" {
		return nil, errors.New("booking_id is required")
	}

	// 1. Verify trip and assigned driver & vehicle
	var driverID, vehicleID sql.NullString
	var tripStatus string
	err := s.db.QueryRowContext(ctx, `
		SELECT status, driver_id, vehicle_id
		FROM trips
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, req.TripID).Scan(&tripStatus, &driverID, &vehicleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("trip not found: %s", req.TripID)
		}
		return nil, fmt.Errorf("failed fetching trip: %w", err)
	}

	if !driverID.Valid || driverID.String == "" || !vehicleID.Valid || vehicleID.String == "" {
		return nil, errors.New("trip has no assigned driver or vehicle for dispatch offer")
	}

	// 2. Safety & Maintenance checks on vehicle
	var vStatus string
	var vBlocked int
	err = s.db.QueryRowContext(ctx, `
		SELECT status, blocked FROM vehicles
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, vehicleID.String).Scan(&vStatus, &vBlocked)
	if err != nil {
		return nil, fmt.Errorf("failed fetching vehicle status: %w", err)
	}
	if vBlocked == 1 || vStatus == "blocked" || vStatus == "maintenance" || vStatus == "inactive" {
		return nil, errors.New("cannot create offer: vehicle is blocked or under maintenance")
	}

	var openWOCount int
	_ = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM work_orders
		WHERE tenant_id = $1 AND vehicle_id = $2 AND status IN ('open', 'assigned', 'in_progress', 'on_hold')`,
		tenantID, vehicleID.String).Scan(&openWOCount)
	if openWOCount > 0 {
		return nil, errors.New("cannot create offer: vehicle has open maintenance work orders")
	}

	// 3. Verify candidate booking
	var bookingStatus string
	var bookingPrice float64
	err = s.db.QueryRowContext(ctx, `
		SELECT status, price FROM bookings
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, req.BookingID).Scan(&bookingStatus, &bookingPrice)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("booking not found: %s", req.BookingID)
		}
		return nil, fmt.Errorf("failed fetching booking: %w", err)
	}

	if bookingStatus != "pending" && bookingStatus != "confirmed" {
		return nil, fmt.Errorf("booking is not available (status: %s)", bookingStatus)
	}

	// 4. Ensure booking hasn't been accepted already
	var acceptedOffers int
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dispatch_offers
		WHERE tenant_id = $1 AND booking_id = $2 AND status = 'accepted'`,
		tenantID, req.BookingID).Scan(&acceptedOffers)
	if err == nil && acceptedOffers > 0 {
		return nil, errors.New("booking has already been accepted by another driver")
	}

	// 5. Default offered rate to booking price if zero
	offeredRate := req.OfferedRate
	if offeredRate <= 0 {
		offeredRate = bookingPrice
	}

	// 6. Check if an active offer already exists for this booking and driver
	var existingOfferID string
	var existingOfferedAt, existingExpiresAt time.Time
	err = s.db.QueryRowContext(ctx, `
		SELECT id, offered_at, expires_at FROM dispatch_offers
		WHERE tenant_id = $1 AND booking_id = $2 AND driver_id = $3 AND status = 'offered' AND expires_at > $4`,
		tenantID, req.BookingID, driverID.String, time.Now()).Scan(&existingOfferID, &existingOfferedAt, &existingExpiresAt)
	if err == nil && existingOfferID != "" {
		return &BackhaulOfferResponse{
			OfferID:     existingOfferID,
			TripID:      req.TripID,
			BookingID:   req.BookingID,
			DriverID:    driverID.String,
			VehicleID:   vehicleID.String,
			OfferedRate: offeredRate,
			Status:      "offered",
			OfferedAt:   existingOfferedAt,
			ExpiresAt:   existingExpiresAt,
		}, nil
	}

	// 7. Insert new dispatch offer (00109 schema)
	offerID := uuid.NewString()
	now := time.Now()
	expiresAt := now.Add(30 * time.Minute)

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dispatch_offers (id, tenant_id, booking_id, driver_id, vehicle_id, status, offered_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'offered', $6, $7)`,
		offerID, tenantID, req.BookingID, driverID.String, vehicleID.String, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed creating dispatch offer: %w", err)
	}

	return &BackhaulOfferResponse{
		OfferID:     offerID,
		TripID:      req.TripID,
		BookingID:   req.BookingID,
		DriverID:    driverID.String,
		VehicleID:   vehicleID.String,
		OfferedRate: offeredRate,
		Status:      "offered",
		OfferedAt:   now,
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *BackhaulService) resolveTripDestination(ctx context.Context, tenantID string, meta tripMetadata) (float64, float64, bool) {
	// 1. From trip_stops final stop
	var stopLat, stopLng sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT latitude, longitude
		FROM trip_stops
		WHERE tenant_id = $1 AND trip_id = $2 AND latitude IS NOT NULL AND longitude IS NOT NULL AND (latitude != 0 OR longitude != 0)
		ORDER BY stop_sequence DESC LIMIT 1`,
		tenantID, meta.id).Scan(&stopLat, &stopLng)
	if err == nil && stopLat.Valid && stopLng.Valid && (stopLat.Float64 != 0 || stopLng.Float64 != 0) {
		return stopLat.Float64, stopLng.Float64, true
	}

	// 2. From route_locations
	if meta.routeID != "" {
		var rDestLat, rDestLng sql.NullFloat64
		err = s.db.QueryRowContext(ctx, `
			SELECT dest_lat, dest_lng
			FROM route_locations
			WHERE route_id = $1 AND dest_lat IS NOT NULL AND dest_lng IS NOT NULL AND (dest_lat != 0 OR dest_lng != 0)`,
			meta.routeID).Scan(&rDestLat, &rDestLng)
		if err == nil && rDestLat.Valid && rDestLng.Valid && (rDestLat.Float64 != 0 || rDestLng.Float64 != 0) {
			return rDestLat.Float64, rDestLng.Float64, true
		}
	}

	// 3. From customer_booking_details of trip's initial booking
	if meta.bookingID != nil && *meta.bookingID != "" {
		var cbdLat, cbdLng sql.NullFloat64
		err = s.db.QueryRowContext(ctx, `
			SELECT delivery_lat, delivery_lng
			FROM customer_booking_details
			WHERE tenant_id = $1 AND booking_id = $2 AND delivery_lat IS NOT NULL AND delivery_lng IS NOT NULL AND (delivery_lat != 0 OR delivery_lng != 0)`,
			tenantID, *meta.bookingID).Scan(&cbdLat, &cbdLng)
		if err == nil && cbdLat.Valid && cbdLng.Valid && (cbdLat.Float64 != 0 || cbdLng.Float64 != 0) {
			return cbdLat.Float64, cbdLng.Float64, true
		}
	}

	// 4. From trips.pod_lat, trips.pod_lng
	var podLat, podLng sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT pod_lat, pod_lng FROM trips
		WHERE tenant_id = $1 AND id = $2 AND pod_lat IS NOT NULL AND pod_lng IS NOT NULL AND (pod_lat != 0 OR pod_lng != 0)`,
		tenantID, meta.id).Scan(&podLat, &podLng)
	if err == nil && podLat.Valid && podLng.Valid && (podLat.Float64 != 0 || podLng.Float64 != 0) {
		return podLat.Float64, podLng.Float64, true
	}

	return 0, 0, false
}

func (s *BackhaulService) resolveVehicleBase(ctx context.Context, tenantID string, meta tripMetadata, facilityID sql.NullString) (float64, float64, bool) {
	// 1. From vehicle facility
	if facilityID.Valid && facilityID.String != "" {
		var fLat, fLng sql.NullFloat64
		err := s.db.QueryRowContext(ctx, `
			SELECT latitude, longitude FROM facilities
			WHERE tenant_id = $1 AND id = $2 AND latitude IS NOT NULL AND longitude IS NOT NULL AND (latitude != 0 OR longitude != 0)`,
			tenantID, facilityID.String).Scan(&fLat, &fLng)
		if err == nil && fLat.Valid && fLng.Valid && (fLat.Float64 != 0 || fLng.Float64 != 0) {
			return fLat.Float64, fLng.Float64, true
		}
	}

	// 2. From trip first stop
	var stopLat, stopLng sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT latitude, longitude
		FROM trip_stops
		WHERE tenant_id = $1 AND trip_id = $2 AND latitude IS NOT NULL AND longitude IS NOT NULL AND (latitude != 0 OR longitude != 0)
		ORDER BY stop_sequence ASC LIMIT 1`,
		tenantID, meta.id).Scan(&stopLat, &stopLng)
	if err == nil && stopLat.Valid && stopLng.Valid && (stopLat.Float64 != 0 || stopLng.Float64 != 0) {
		return stopLat.Float64, stopLng.Float64, true
	}

	// 3. From route_locations source
	if meta.routeID != "" {
		var rSourceLat, rSourceLng sql.NullFloat64
		err = s.db.QueryRowContext(ctx, `
			SELECT source_lat, source_lng
			FROM route_locations
			WHERE route_id = $1 AND source_lat IS NOT NULL AND source_lng IS NOT NULL AND (source_lat != 0 OR source_lng != 0)`,
			meta.routeID).Scan(&rSourceLat, &rSourceLng)
		if err == nil && rSourceLat.Valid && rSourceLng.Valid && (rSourceLat.Float64 != 0 || rSourceLng.Float64 != 0) {
			return rSourceLat.Float64, rSourceLng.Float64, true
		}
	}

	// 4. From customer_booking_details pickup of trip's initial booking
	if meta.bookingID != nil && *meta.bookingID != "" {
		var cbdLat, cbdLng sql.NullFloat64
		err = s.db.QueryRowContext(ctx, `
			SELECT pickup_lat, pickup_lng
			FROM customer_booking_details
			WHERE tenant_id = $1 AND booking_id = $2 AND pickup_lat IS NOT NULL AND pickup_lng IS NOT NULL AND (pickup_lat != 0 OR pickup_lng != 0)`,
			tenantID, *meta.bookingID).Scan(&cbdLat, &cbdLng)
		if err == nil && cbdLat.Valid && cbdLng.Valid && (cbdLat.Float64 != 0 || cbdLng.Float64 != 0) {
			return cbdLat.Float64, cbdLng.Float64, true
		}
	}

	return 0, 0, false
}
