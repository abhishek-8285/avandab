package application

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"transport-app/internal/controltower/domain"
	"transport-app/internal/eta"
	"transport-app/internal/shared"
)

// Service provides server-authoritative projections for the Dispatcher Control Tower.
type Service struct {
	db         *sql.DB
	etaService *eta.EtaService
	staleMin   time.Duration
}

// NewService constructs a Control Tower projection service.
func NewService(db *sql.DB, etaService *eta.EtaService, staleMin time.Duration) *Service {
	if staleMin <= 0 {
		staleMin = 15 * time.Minute
	}
	return &Service{
		db:         db,
		etaService: etaService,
		staleMin:   staleMin,
	}
}

// parseTimeFlex parses standard RFC3339 or SQLite datetime strings safely.
func parseTimeFlex(s string) *time.Time {
	if s == "" {
		return nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

// GetTrips returns authoritative Control Tower projections for all trips under the tenant.
func (s *Service) GetTrips(ctx context.Context, tenantID shared.TenantID, statusFilter string) ([]domain.ControlTowerTrip, error) {
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}

	query := `
		SELECT t.id, COALESCE(t.trip_number, ''), t.tenant_id, COALESCE(t.booking_id, ''),
		       t.status, COALESCE(t.start_time, ''), COALESCE(t.end_time, ''),
		       COALESCE(t.origin, ''), COALESCE(t.destination, ''),
		       COALESCE(t.driver_id, ''), COALESCE(d.first_name || ' ' || d.last_name, ''), COALESCE(d.phone, ''),
		       COALESCE(t.vehicle_id, ''), COALESCE(v.vehicle_number, ''), COALESCE(v.registration_number, '')
		FROM trips t
		LEFT JOIN drivers d ON d.id = t.driver_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE t.tenant_id = ?
	`
	args := []interface{}{tenantStr}
	if statusFilter != "" {
		query += " AND t.status = ?"
		args = append(args, statusFilter)
	}
	query += " ORDER BY t.created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []domain.ControlTowerTrip
	for rows.Next() {
		var (
			tripID, tripNumber, tTenant, bookingID, status, startTimeStr, endTimeStr string
			origin, destination, driverID, driverName, driverPhone                   string
			vehicleID, vehicleNumber, regNumber                                      string
		)
		if err := rows.Scan(
			&tripID, &tripNumber, &tTenant, &bookingID, &status,
			&startTimeStr, &endTimeStr, &origin, &destination,
			&driverID, &driverName, &driverPhone,
			&vehicleID, &vehicleNumber, &regNumber,
		); err != nil {
			return nil, err
		}

		proj, err := s.buildProjection(ctx, tripID, tripNumber, tTenant, bookingID, status,
			startTimeStr, endTimeStr, origin, destination,
			driverID, driverName, driverPhone,
			vehicleID, vehicleNumber, regNumber)
		if err != nil {
			return nil, err
		}
		result = append(result, *proj)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTrip returns the authoritative Control Tower projection for a specific trip under the tenant.
func (s *Service) GetTrip(ctx context.Context, tenantID shared.TenantID, tripID string) (*domain.ControlTowerTrip, error) {
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}

	var (
		tTripID, tripNumber, tTenant, bookingID, status, startTimeStr, endTimeStr string
		origin, destination, driverID, driverName, driverPhone                    string
		vehicleID, vehicleNumber, regNumber                                       string
	)

	query := `
		SELECT t.id, COALESCE(t.trip_number, ''), t.tenant_id, COALESCE(t.booking_id, ''),
		       t.status, COALESCE(t.start_time, ''), COALESCE(t.end_time, ''),
		       COALESCE(t.origin, ''), COALESCE(t.destination, ''),
		       COALESCE(t.driver_id, ''), COALESCE(d.first_name || ' ' || d.last_name, ''), COALESCE(d.phone, ''),
		       COALESCE(t.vehicle_id, ''), COALESCE(v.vehicle_number, ''), COALESCE(v.registration_number, '')
		FROM trips t
		LEFT JOIN drivers d ON d.id = t.driver_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE t.id = ? AND t.tenant_id = ?
	`
	err := s.db.QueryRowContext(ctx, query, tripID, tenantStr).Scan(
		&tTripID, &tripNumber, &tTenant, &bookingID, &status,
		&startTimeStr, &endTimeStr, &origin, &destination,
		&driverID, &driverName, &driverPhone,
		&vehicleID, &vehicleNumber, &regNumber,
	)
	if err != nil {
		return nil, err
	}

	return s.buildProjection(ctx, tTripID, tripNumber, tTenant, bookingID, status,
		startTimeStr, endTimeStr, origin, destination,
		driverID, driverName, driverPhone,
		vehicleID, vehicleNumber, regNumber)
}

func (s *Service) buildProjection(
	ctx context.Context,
	tripID, tripNumber, tenantID, bookingID, status string,
	startTimeStr, endTimeStr, origin, destination string,
	driverID, driverName, driverPhone string,
	vehicleID, vehicleNumber, regNumber string,
) (*domain.ControlTowerTrip, error) {
	proj := &domain.ControlTowerTrip{
		TripID:      tripID,
		TripNumber:  tripNumber,
		TenantID:    tenantID,
		BookingID:   bookingID,
		Status:      status,
		StartTime:   parseTimeFlex(startTimeStr),
		EndTime:     parseTimeFlex(endTimeStr),
		Origin:      origin,
		Destination: destination,
		Driver: domain.DriverInfo{
			ID:    driverID,
			Name:  strings.TrimSpace(driverName),
			Phone: driverPhone,
		},
		Vehicle: domain.VehicleInfo{
			ID:                 vehicleID,
			VehicleNumber:      vehicleNumber,
			RegistrationNumber: regNumber,
		},
	}

	// 1. Query Stops
	stopRows, err := s.db.QueryContext(ctx, `
		SELECT id, trip_id, stop_sequence, stop_type,
		       COALESCE(location_name, ''), COALESCE(address, ''),
		       latitude, longitude, COALESCE(geofence_radius_m, 200),
		       status, COALESCE(actual_arrival, ''), COALESCE(actual_departure, ''),
		       COALESCE(requires_pod, 0), COALESCE(requires_otp, 0),
		       COALESCE(pod_url, ''), COALESCE(signature_url, ''),
		       COALESCE(consignee_name, ''), COALESCE(consignee_phone, '')
		FROM trip_stops
		WHERE trip_id = ?
		ORDER BY stop_sequence ASC
	`, tripID)
	if err == nil {
		defer func() { _ = stopRows.Close() }()
		completedCount := 0
		var stops []domain.ControlTowerStop
		var activeStop *domain.ControlTowerStop

		for stopRows.Next() {
			var (
				sID, sTripID, sType, sLoc, sAddr, sStatus           string
				sArrStr, sDepStr, sPodUrl, sSigUrl, sCName, sCPhone string
				sSeq                                                int
				sLat, sLng, sGeo                                    float64
				sReqPod, sReqOtp                                    int
			)
			if err := stopRows.Scan(
				&sID, &sTripID, &sSeq, &sType, &sLoc, &sAddr,
				&sLat, &sLng, &sGeo, &sStatus,
				&sArrStr, &sDepStr, &sReqPod, &sReqOtp,
				&sPodUrl, &sSigUrl, &sCName, &sCPhone,
			); err == nil {
				isCompleted := sStatus == "completed" || sStatus == "skipped"
				if isCompleted {
					completedCount++
				}

				var podUrlPtr, sigUrlPtr *string
				if sPodUrl != "" {
					podUrlPtr = &sPodUrl
				}
				if sSigUrl != "" {
					sigUrlPtr = &sSigUrl
				}

				st := domain.ControlTowerStop{
					ID:              sID,
					TripID:          sTripID,
					StopSequence:    sSeq,
					StopType:        sType,
					LocationName:    sLoc,
					Address:         sAddr,
					Latitude:        sLat,
					Longitude:       sLng,
					GeofenceRadiusM: sGeo,
					Status:          sStatus,
					ActualArrival:   parseTimeFlex(sArrStr),
					ActualDeparture: parseTimeFlex(sDepStr),
					RequiresPOD:     sReqPod == 1,
					RequiresOTP:     sReqOtp == 1,
					PODSubmitted:    sPodUrl != "" || sSigUrl != "",
					PODUrl:          podUrlPtr,
					SignatureUrl:    sigUrlPtr,
					ConsigneeName:   sCName,
					ConsigneePhone:  sCPhone,
				}

				if !isCompleted && activeStop == nil {
					stCopy := st
					activeStop = &stCopy
				}
				stops = append(stops, st)
			}
		}
		proj.Stops = stops
		proj.CurrentStop = activeStop

		total := len(stops)
		progressPct := 0.0
		allDone := total > 0 && completedCount == total
		if total > 0 {
			progressPct = (float64(completedCount) / float64(total)) * 100.0
		}
		if status == "COMPLETED" {
			progressPct = 100.0
			allDone = true
			proj.CurrentStop = nil
		}
		proj.Progression = domain.ControlTowerProgression{
			TotalStops:        total,
			CompletedStops:    completedCount,
			ProgressPercent:   progressPct,
			AllStopsCompleted: allDone,
		}
	}

	// 2. Query Live Telemetry
	now := time.Now().UTC()
	markerStatus := "no_signal"
	if vehicleID != "" {
		var sLat, sLng, sSpeed, sHeading sql.NullFloat64
		var sTs sql.NullString
		_ = s.db.QueryRowContext(ctx, `
			SELECT latitude, longitude, speed, heading, timestamp
			FROM telemetry_snapshots
			WHERE vehicle_id = ?
			ORDER BY timestamp DESC LIMIT 1
		`, vehicleID).Scan(&sLat, &sLng, &sSpeed, &sHeading, &sTs)

		if sLat.Valid && sLng.Valid {
			lat := sLat.Float64
			lng := sLng.Float64
			proj.Telemetry.Latitude = &lat
			proj.Telemetry.Longitude = &lng
		}
		if sSpeed.Valid {
			spd := sSpeed.Float64
			proj.Telemetry.Speed = &spd
		}
		if sHeading.Valid {
			hdg := sHeading.Float64
			proj.Telemetry.Heading = &hdg
		}
		if sTs.Valid {
			lastT := parseTimeFlex(sTs.String)
			proj.Telemetry.LastSeenAt = lastT
			proj.SyncState.LastSyncAt = lastT

			if lastT != nil {
				age := now.Sub(*lastT)
				if age > s.staleMin {
					markerStatus = "no_signal"
					proj.SyncState.IsStale = true
				} else if proj.Telemetry.Speed != nil && *proj.Telemetry.Speed > 0 {
					markerStatus = "running"
					proj.SyncState.IsStale = false
				} else {
					markerStatus = "stopped"
					proj.SyncState.IsStale = false
				}
			}
		}
	}
	proj.Telemetry.MarkerStatus = markerStatus

	// Calculate ETA if EtaService available
	if s.etaService != nil && (status == "IN_TRANSIT" || status == "STARTED" || status == "SCHEDULED") {
		if res, err := s.etaService.Calculate(ctx, tripID); err == nil {
			minT := res.EtaMin.UTC()
			maxT := res.EtaMax.UTC()
			proj.Telemetry.EtaMin = &minT
			proj.Telemetry.EtaMax = &maxT
			proj.Telemetry.EtaMethod = res.Method
			proj.Telemetry.RemainingKM = &res.RemainingKM
		}
	}

	// 3. Query Safety & Alerts
	alertRows, err := s.db.QueryContext(ctx, `
		SELECT alert_type, COALESCE(resolved, 0), COALESCE(metadata, '')
		FROM telemetry_alerts
		WHERE trip_id = ? AND (resolved = 0 OR resolved IS NULL)
		ORDER BY created_at DESC
	`, tripID)
	if err == nil {
		defer func() { _ = alertRows.Close() }()
		activeAlerts := 0
		hasSOS := false
		var latestType string
		isDeviated := false
		var maxDevDist float64

		for alertRows.Next() {
			var aType, aMeta string
			var aRes int
			if err := alertRows.Scan(&aType, &aRes, &aMeta); err == nil {
				activeAlerts++
				if latestType == "" {
					latestType = aType
				}
				if aType == "sos" || aType == "SOS" {
					hasSOS = true
				}
				if aType == "deviation" || aType == "route_deviation" {
					isDeviated = true
				}
			}
		}
		proj.Safety = domain.ControlTowerSafety{
			HasActiveSOS:       hasSOS,
			ActiveAlertsCount:  activeAlerts,
			LatestAlertType:    latestType,
			IsDeviated:         isDeviated,
			DeviationDistanceM: maxDevDist,
		}
	}

	// 4. Query EWB
	var ewbNum, ewbStat, ewbValidStr sql.NullString
	_ = s.db.QueryRowContext(ctx, `
		SELECT eway_bill_number, status, valid_until
		FROM ewb_requests
		WHERE trip_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, tripID).Scan(&ewbNum, &ewbStat, &ewbValidStr)

	if ewbNum.Valid && ewbNum.String != "" {
		proj.EWB = &domain.ControlTowerEWB{
			EWBNumber:  ewbNum.String,
			Status:     ewbStat.String,
			ValidUntil: parseTimeFlex(ewbValidStr.String),
		}
	}

	return proj, nil
}
