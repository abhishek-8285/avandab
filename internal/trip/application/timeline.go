package application

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"transport-app/internal/eta"
)

// MilestoneType defines the taxonomy of tracking milestones (Spec 20 §2).
type MilestoneType string

const (
	MilestoneOrderCreated        MilestoneType = "ORDER_CREATED"
	MilestoneVehicleDispatched   MilestoneType = "VEHICLE_DISPATCHED"
	MilestoneStopArrived         MilestoneType = "STOP_ARRIVED"
	MilestoneInTransitCheckpoint MilestoneType = "IN_TRANSIT_CHECKPOINT"
	MilestoneOutForDelivery      MilestoneType = "OUT_FOR_DELIVERY"
	MilestoneDeliveredPOD        MilestoneType = "DELIVERED_POD"
)

// MilestoneDTO represents a single milestone on the public tracking timeline.
type MilestoneDTO struct {
	ID        string  `json:"id"`
	Type      string  `json:"type,omitempty"`
	Title     string  `json:"title"`
	Location  string  `json:"location,omitempty"`
	Timestamp *string `json:"timestamp"` // ISO8601 string or null
	Status    string  `json:"status"`    // "completed", "in_progress", "pending"
}

// TripTimelineDTO represents the complete timeline response for public share links.
type TripTimelineDTO struct {
	TripID       string         `json:"trip_id"`
	TripNumber   string         `json:"trip_number,omitempty"`
	Status       string         `json:"status"`
	ETA          *string        `json:"eta"`
	VehicleLabel string         `json:"vehicle_label,omitempty"`
	DriverName   string         `json:"driver_name,omitempty"`
	DriverPhone  string         `json:"driver_phone,omitempty"`
	Milestones   []MilestoneDTO `json:"milestones"`
	Polyline     [][]float64    `json:"polyline,omitempty"`
}

// TimelineUseCase coordinates dynamic derivation of customer tracking timelines.
type TimelineUseCase struct {
	db         *sql.DB
	etaService *eta.EtaService
}

// NewTimelineUseCase creates a new TimelineUseCase.
func NewTimelineUseCase(db *sql.DB, etaSvc *eta.EtaService) *TimelineUseCase {
	return &TimelineUseCase{
		db:         db,
		etaService: etaSvc,
	}
}

// MaskPhoneNumber masks a phone number for public CX display (Spec 20 §2.2).
// Example: "+91 9876543210" -> "+91 98*** **210".
func MaskPhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	clean := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '+' {
			return r
		}
		return -1
	}, phone)

	if strings.HasPrefix(clean, "+") && len(clean) >= 12 {
		countryAndFirst := clean[:5]
		lastThree := clean[len(clean)-3:]
		return countryAndFirst[:3] + " " + countryAndFirst[3:] + "*** **" + lastThree
	}
	if len(clean) >= 10 {
		firstTwo := clean[:2]
		lastThree := clean[len(clean)-3:]
		return firstTwo + "*** **" + lastThree
	}
	if len(clean) > 4 {
		return clean[:2] + "****" + clean[len(clean)-2:]
	}
	return "***"
}

// HaversineDistanceMeters calculates the great-circle distance between two points in meters.
func HaversineDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // Earth radius in meters
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	deltaPhi := (lat2 - lat1) * math.Pi / 180
	deltaLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*
			math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// BuildTimeline generates the milestone progression and sanitized CX data for a trip.
func (uc *TimelineUseCase) BuildTimeline(ctx context.Context, tripID, tenantID string) (*TripTimelineDTO, error) {
	// 1. Fetch core trip details
	var (
		tNumber, tStatus, routeID               string
		bookingID, driverID, vehicleID          sql.NullString
		startedAt, reachedPickupAt, inTransitAt sql.NullTime
		deliveredAt, completedAt, createdAt     sql.NullTime
		arrivalTime, departureTime              sql.NullTime
		podScanValue, podOTP                    sql.NullString
		rSource, rDest                          sql.NullString
		rDistance                               sql.NullFloat64
		vReg, vNum                              sql.NullString
		dName, dPhone                           sql.NullString
		bCreatedAt                              sql.NullTime
	)

	query := `
		SELECT t.trip_number, t.status, t.route_id, t.booking_id, t.driver_id, t.vehicle_id,
		       t.started_at, t.reached_pickup_at, t.in_transit_at, t.delivered_at, t.completed_at,
		       t.created_at, t.arrival_time, t.departure_time,
		       t.pod_scan_value, t.pod_otp,
		       r.source, r.destination, r.distance,
		       v.registration_number, v.vehicle_number,
		       TRIM(COALESCE(d.first_name, '') || ' ' || COALESCE(d.last_name, '')), d.phone,
		       b.created_at
		FROM trips t
		LEFT JOIN routes r ON r.id = t.route_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN drivers d ON d.id = t.driver_id
		LEFT JOIN bookings b ON b.id = t.booking_id
		WHERE t.id = $1`

	var err error
	if tenantID != "" {
		query += ` AND t.tenant_id = $2`
		err = uc.db.QueryRowContext(ctx, query, tripID, tenantID).Scan(
			&tNumber, &tStatus, &routeID, &bookingID, &driverID, &vehicleID,
			&startedAt, &reachedPickupAt, &inTransitAt, &deliveredAt, &completedAt,
			&createdAt, &arrivalTime, &departureTime,
			&podScanValue, &podOTP,
			&rSource, &rDest, &rDistance,
			&vReg, &vNum,
			&dName, &dPhone,
			&bCreatedAt,
		)
	} else {
		err = uc.db.QueryRowContext(ctx, query, tripID).Scan(
			&tNumber, &tStatus, &routeID, &bookingID, &driverID, &vehicleID,
			&startedAt, &reachedPickupAt, &inTransitAt, &deliveredAt, &completedAt,
			&createdAt, &arrivalTime, &departureTime,
			&podScanValue, &podOTP,
			&rSource, &rDest, &rDistance,
			&vReg, &vNum,
			&dName, &dPhone,
			&bCreatedAt,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("trip lookup failed: %w", err)
	}

	// 2. Fetch latest telemetry snapshot for vehicle
	var curLat, curLng *float64
	var lastTelemetryTime *time.Time
	if vehicleID.Valid && vehicleID.String != "" {
		var sLat, sLng sql.NullFloat64
		var sTs sql.NullTime
		_ = uc.db.QueryRowContext(ctx, `
			SELECT latitude, longitude, timestamp
			FROM telemetry_snapshots
			WHERE vehicle_id = $1 AND latitude IS NOT NULL AND longitude IS NOT NULL
			ORDER BY timestamp DESC LIMIT 1`, vehicleID.String).Scan(&sLat, &sLng, &sTs)
		if sLat.Valid && sLng.Valid {
			latVal := sLat.Float64
			lngVal := sLng.Float64
			curLat = &latVal
			curLng = &lngVal
			if sTs.Valid {
				tUTC := sTs.Time.UTC()
				lastTelemetryTime = &tUTC
			}
		}
	}

	// 3. Fetch polyline coordinates
	var polyline [][]float64
	pRows, pErr := uc.db.QueryContext(ctx, `
		SELECT latitude, longitude
		FROM telemetry_positions
		WHERE trip_id = $1
		ORDER BY device_time ASC
		LIMIT 500`, tripID)
	if pErr == nil {
		defer func() { _ = pRows.Close() }()
		for pRows.Next() {
			var pLat, pLng float64
			if err := pRows.Scan(&pLat, &pLng); err == nil {
				polyline = append(polyline, []float64{pLat, pLng})
			}
		}
	}
	if len(polyline) == 0 && vehicleID.Valid && startedAt.Valid {
		// Fallback to vehicle coordinates since trip started
		vRows, vErr := uc.db.QueryContext(ctx, `
			SELECT latitude, longitude
			FROM telemetry_positions
			WHERE vehicle_id = $1 AND device_time >= $2
			ORDER BY device_time ASC
			LIMIT 500`, vehicleID.String, startedAt.Time)
		if vErr == nil {
			defer func() { _ = vRows.Close() }()
			for vRows.Next() {
				var pLat, pLng float64
				if err := vRows.Scan(&pLat, &pLng); err == nil {
					polyline = append(polyline, []float64{pLat, pLng})
				}
			}
		}
	}
	if len(polyline) == 0 && curLat != nil && curLng != nil {
		polyline = append(polyline, []float64{*curLat, *curLng})
	}

	// 4. Fetch trip stops
	type stopItem struct {
		id            string
		seq           int
		stopType      string
		locationName  string
		address       string
		lat, lng      sql.NullFloat64
		actualArrival sql.NullTime
		status        string
		podVerifiedAt sql.NullTime
	}
	var stops []stopItem
	sRows, sErr := uc.db.QueryContext(ctx, `
		SELECT id, stop_sequence, stop_type, location_name, address, latitude, longitude,
		       actual_arrival, status, pod_verified_at
		FROM trip_stops
		WHERE trip_id = $1
		ORDER BY stop_sequence ASC`, tripID)
	if sErr == nil {
		defer func() { _ = sRows.Close() }()
		for sRows.Next() {
			var st stopItem
			if err := sRows.Scan(&st.id, &st.seq, &st.stopType, &st.locationName, &st.address,
				&st.lat, &st.lng, &st.actualArrival, &st.status, &st.podVerifiedAt); err == nil {
				stops = append(stops, st)
			}
		}
	}

	// 5. Fetch FASTag toll crossings
	type tollItem struct {
		id        string
		plazaName string
		timestamp time.Time
	}
	var tolls []tollItem
	tRows, tErr := uc.db.QueryContext(ctx, `
		SELECT id, plaza_name, txn_timestamp
		FROM fastag_transactions
		WHERE trip_id = $1 AND status = 'SUCCESS'
		ORDER BY txn_timestamp ASC`, tripID)
	if tErr == nil {
		defer func() { _ = tRows.Close() }()
		for tRows.Next() {
			var tItem tollItem
			var pName sql.NullString
			var tTime time.Time
			if err := tRows.Scan(&tItem.id, &pName, &tTime); err == nil {
				tItem.plazaName = pName.String
				if tItem.plazaName == "" {
					tItem.plazaName = "FASTag Toll Plaza"
				}
				tItem.timestamp = tTime.UTC()
				tolls = append(tolls, tItem)
			}
		}
	}

	// 6. Fetch Geofence transit events
	type geoEventItem struct {
		id        string
		name      string
		eventType string
		zoneKind  string
		createdAt time.Time
	}
	var geoEvents []geoEventItem
	gRows, gErr := uc.db.QueryContext(ctx, `
		SELECT ge.id, COALESCE(g.name, ge.zone_kind, 'Checkpoint'), ge.event_type, COALESCE(ge.zone_kind, ''), ge.created_at
		FROM geofence_events ge
		LEFT JOIN geofences g ON g.id = ge.geofence_id
		WHERE ge.trip_id = $1 AND ge.event_type IN ('entering', 'inside', 'leaving')
		ORDER BY ge.created_at ASC`, tripID)
	if gErr == nil {
		defer func() { _ = gRows.Close() }()
		for gRows.Next() {
			var gItem geoEventItem
			var cTime time.Time
			if err := gRows.Scan(&gItem.id, &gItem.name, &gItem.eventType, &gItem.zoneKind, &cTime); err == nil {
				gItem.createdAt = cTime.UTC()
				geoEvents = append(geoEvents, gItem)
			}
		}
	}

	// 7. Calculate ETA
	var etaStr *string
	if uc.etaService != nil {
		if res, err := uc.etaService.Calculate(ctx, tripID); err == nil {
			val := res.EtaMax.UTC().Format(time.RFC3339)
			etaStr = &val
		}
	}
	if etaStr == nil && arrivalTime.Valid {
		val := arrivalTime.Time.UTC().Format(time.RFC3339)
		etaStr = &val
	}

	// 8. Vehicle Label
	vehLabel := "—"
	if vReg.Valid && vReg.String != "" {
		vehLabel = vReg.String
	} else if vNum.Valid && vNum.String != "" {
		vehLabel = vNum.String
	}

	originName := rSource.String
	if originName == "" {
		originName = "Origin Depot"
	}
	destName := rDest.String
	if destName == "" {
		destName = "Destination Hub"
	}

	// 9. Derive Milestones
	var milestones []MilestoneDTO
	mIndex := 1

	// Milestone 1: ORDER_CREATED
	var orderCreatedTime *string
	if bCreatedAt.Valid {
		val := bCreatedAt.Time.UTC().Format(time.RFC3339)
		orderCreatedTime = &val
	} else if createdAt.Valid {
		val := createdAt.Time.UTC().Format(time.RFC3339)
		orderCreatedTime = &val
	}
	milestones = append(milestones, MilestoneDTO{
		ID:        fmt.Sprintf("m%d", mIndex),
		Type:      string(MilestoneOrderCreated),
		Title:     "Order Created & Confirmed",
		Location:  originName,
		Timestamp: orderCreatedTime,
		Status:    "completed",
	})
	mIndex++

	// Milestone 2: VEHICLE_DISPATCHED
	var dispatchTime *string
	dispatchStatus := "pending"
	if startedAt.Valid {
		val := startedAt.Time.UTC().Format(time.RFC3339)
		dispatchTime = &val
		dispatchStatus = "completed"
	} else if tStatus == "assigned" || tStatus == "scheduled" {
		dispatchStatus = "in_progress"
	}

	milestones = append(milestones, MilestoneDTO{
		ID:        fmt.Sprintf("m%d", mIndex),
		Type:      string(MilestoneVehicleDispatched),
		Title:     fmt.Sprintf("Dispatched from %s", originName),
		Location:  originName,
		Timestamp: dispatchTime,
		Status:    dispatchStatus,
	})
	mIndex++

	// Milestone 3: Intermediate Stops (from trip_stops)
	var finalDropStop *stopItem
	for i := range stops {
		st := &stops[i]
		if st.stopType == "drop" && i == len(stops)-1 {
			finalDropStop = st
			continue
		}

		mStatus := "pending"
		var stTime *string
		if st.actualArrival.Valid {
			val := st.actualArrival.Time.UTC().Format(time.RFC3339)
			stTime = &val
			mStatus = "completed"
		} else if st.status == "en_route" || st.status == "arrived" || st.status == "servicing" {
			mStatus = "in_progress"
		} else if dispatchStatus == "completed" && i == 0 {
			mStatus = "in_progress"
		}

		title := fmt.Sprintf("Stop reached: %s", st.locationName)
		if st.stopType == "pickup" {
			title = fmt.Sprintf("Pickup arrived: %s", st.locationName)
		} else if st.stopType == "waypoint" || st.stopType == "hub_transit" {
			title = fmt.Sprintf("Transit checkpoint: %s", st.locationName)
		}

		milestones = append(milestones, MilestoneDTO{
			ID:        fmt.Sprintf("m%d", mIndex),
			Type:      string(MilestoneStopArrived),
			Title:     title,
			Location:  st.locationName,
			Timestamp: stTime,
			Status:    mStatus,
		})
		mIndex++
	}

	// Milestone 4: FASTag Toll Checkpoints
	for _, toll := range tolls {
		tVal := toll.timestamp.Format(time.RFC3339)
		milestones = append(milestones, MilestoneDTO{
			ID:        fmt.Sprintf("m%d", mIndex),
			Type:      string(MilestoneInTransitCheckpoint),
			Title:     fmt.Sprintf("Toll Plaza Crossed (%s)", toll.plazaName),
			Location:  toll.plazaName,
			Timestamp: &tVal,
			Status:    "completed",
		})
		mIndex++
	}

	// Geofence Transit Checkpoints
	for _, geo := range geoEvents {
		if geo.zoneKind == "drop" {
			continue // handled in out for delivery / arrival
		}
		gVal := geo.createdAt.Format(time.RFC3339)
		milestones = append(milestones, MilestoneDTO{
			ID:        fmt.Sprintf("m%d", mIndex),
			Type:      string(MilestoneInTransitCheckpoint),
			Title:     fmt.Sprintf("Geofence Checkpoint: %s", geo.name),
			Location:  geo.name,
			Timestamp: &gVal,
			Status:    "completed",
		})
		mIndex++
	}

	// Milestone 5: OUT_FOR_DELIVERY
	outForDeliveryStatus := "pending"
	var outForDeliveryTime *string
	isNearDrop := false

	// Check 5km destination proximity
	if curLat != nil && curLng != nil && finalDropStop != nil && finalDropStop.lat.Valid && finalDropStop.lng.Valid {
		dist := HaversineDistanceMeters(*curLat, *curLng, finalDropStop.lat.Float64, finalDropStop.lng.Float64)
		if dist <= 5000.0 {
			isNearDrop = true
		}
	}

	if tStatus == "completed" || tStatus == "delivered" {
		outForDeliveryStatus = "completed"
		if deliveredAt.Valid {
			val := deliveredAt.Time.UTC().Format(time.RFC3339)
			outForDeliveryTime = &val
		} else if completedAt.Valid {
			val := completedAt.Time.UTC().Format(time.RFC3339)
			outForDeliveryTime = &val
		}
	} else if tStatus == "reached_pickup" || tStatus == "in_transit" {
		if isNearDrop {
			outForDeliveryStatus = "in_progress"
			if lastTelemetryTime != nil {
				val := lastTelemetryTime.Format(time.RFC3339)
				outForDeliveryTime = &val
			}
		}
	}

	milestones = append(milestones, MilestoneDTO{
		ID:        fmt.Sprintf("m%d", mIndex),
		Type:      string(MilestoneOutForDelivery),
		Title:     fmt.Sprintf("Out for Delivery to %s", destName),
		Location:  destName,
		Timestamp: outForDeliveryTime,
		Status:    outForDeliveryStatus,
	})
	mIndex++

	// Milestone 6: DELIVERED_POD
	deliveredStatus := "pending"
	var deliveredTime *string
	isPODVerified := false

	if podScanValue.Valid && podScanValue.String != "" {
		isPODVerified = true
	}
	if finalDropStop != nil && finalDropStop.podVerifiedAt.Valid {
		isPODVerified = true
	}

	deliveredTitle := fmt.Sprintf("Arrival & Delivery at %s", destName)
	if tStatus == "completed" || tStatus == "delivered" || completedAt.Valid {
		deliveredStatus = "completed"
		if completedAt.Valid {
			val := completedAt.Time.UTC().Format(time.RFC3339)
			deliveredTime = &val
		} else if deliveredAt.Valid {
			val := deliveredAt.Time.UTC().Format(time.RFC3339)
			deliveredTime = &val
		}
		if isPODVerified {
			deliveredTitle = fmt.Sprintf("Delivered & e-POD Verified at %s", destName)
		} else {
			deliveredTitle = fmt.Sprintf("Delivered at %s", destName)
		}
	}

	milestones = append(milestones, MilestoneDTO{
		ID:        fmt.Sprintf("m%d", mIndex),
		Type:      string(MilestoneDeliveredPOD),
		Title:     deliveredTitle,
		Location:  destName,
		Timestamp: deliveredTime,
		Status:    deliveredStatus,
	})

	// 10. Assemble Response DTO
	dto := &TripTimelineDTO{
		TripID:       tripID,
		TripNumber:   tNumber,
		Status:       tStatus,
		ETA:          etaStr,
		VehicleLabel: vehLabel,
		DriverName:   dName.String,
		DriverPhone:  MaskPhoneNumber(dPhone.String),
		Milestones:   milestones,
		Polyline:     polyline,
	}

	return dto, nil
}
