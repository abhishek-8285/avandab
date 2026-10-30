package videotelematics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Repository defines data access operations for video telematics.
type Repository interface {
	UpsertDashcamProfile(ctx context.Context, p *DashcamProfile) error
	GetDashcamProfile(ctx context.Context, tenantID, vehicleID string) (*DashcamProfile, error)
	ListDashcamVehicles(ctx context.Context, tenantID string) ([]VideoVehicleStatus, error)

	CreateSafetyEvent(ctx context.Context, event *VideoSafetyEvent) error
	GetSafetyEvent(ctx context.Context, tenantID, eventID string) (*VideoSafetyEvent, error)
	ListSafetyEvents(ctx context.Context, tenantID string, filter EventFilter) ([]VideoSafetyEvent, int, error)
	UpdateSafetyEventReview(ctx context.Context, tenantID, eventID, reviewStatus, coachingNote, reviewedBy string, reviewedAt time.Time) error

	CreateLivestreamSession(ctx context.Context, session *LivestreamSession) error
	GetLivestreamSession(ctx context.Context, tenantID, sessionID string) (*LivestreamSession, error)
	GetActiveLivestreamByVehicle(ctx context.Context, tenantID, vehicleID string) (*LivestreamSession, error)
	EndLivestreamSession(ctx context.Context, tenantID, sessionID string, endedAt time.Time) error
	ActivateLivestreamSession(ctx context.Context, tenantID, sessionID string) error

	UpsertSafetyScorecard(ctx context.Context, card *DriverSafetyScorecard) error
	GetDriverScorecard(ctx context.Context, tenantID, driverID string) (*DriverSafetyScorecard, error)
	ListDriverScorecards(ctx context.Context, tenantID string, limit, offset int) ([]DriverSafetyScorecard, int, error)

	GetFleetMetrics(ctx context.Context, tenantID string) (*VideoFleetMetrics, error)
	GetDriverEventCounts(ctx context.Context, tenantID, driverID string, start, end time.Time) (adasCount, dmsCount, criticalCount, coachedCount int, err error)
}

// SQLRepository implements Repository using standard sql.DB.
type SQLRepository struct {
	db *sql.DB
}

// NewSQLRepository creates a new video telematics SQL repository.
func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) UpsertDashcamProfile(ctx context.Context, p *DashcamProfile) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	dualFacing := 0
	if p.DualFacing {
		dualFacing = 1
	}
	adasEnabled := 0
	if p.ADASEnabled {
		adasEnabled = 1
	}
	dmsEnabled := 0
	if p.DMSEnabled {
		dmsEnabled = 1
	}
	liveStreamSupported := 0
	if p.LiveStreamSupported {
		liveStreamSupported = 1
	}

	query := `INSERT INTO vehicle_dashcam_profiles (
		vehicle_id, tenant_id, camera_make_model, camera_serial,
		dual_facing, adas_enabled, dms_enabled, live_stream_supported,
		firmware_version, status, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT (vehicle_id) DO UPDATE SET
		camera_make_model = EXCLUDED.camera_make_model,
		camera_serial = EXCLUDED.camera_serial,
		dual_facing = EXCLUDED.dual_facing,
		adas_enabled = EXCLUDED.adas_enabled,
		dms_enabled = EXCLUDED.dms_enabled,
		live_stream_supported = EXCLUDED.live_stream_supported,
		firmware_version = EXCLUDED.firmware_version,
		status = EXCLUDED.status,
		updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, query,
		p.VehicleID, p.TenantID, p.CameraMakeModel, p.CameraSerial,
		dualFacing, adasEnabled, dmsEnabled, liveStreamSupported,
		p.FirmwareVersion, p.Status, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetDashcamProfile(ctx context.Context, tenantID, vehicleID string) (*DashcamProfile, error) {
	query := `SELECT
		vehicle_id, tenant_id, camera_make_model, camera_serial,
		dual_facing, adas_enabled, dms_enabled, live_stream_supported,
		firmware_version, status, created_at, updated_at
	FROM vehicle_dashcam_profiles
	WHERE tenant_id = $1 AND vehicle_id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var p DashcamProfile
	var dualFacing, adasEnabled, dmsEnabled, liveStreamSupported int

	err := row.Scan(
		&p.VehicleID, &p.TenantID, &p.CameraMakeModel, &p.CameraSerial,
		&dualFacing, &adasEnabled, &dmsEnabled, &liveStreamSupported,
		&p.FirmwareVersion, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	p.DualFacing = dualFacing == 1
	p.ADASEnabled = adasEnabled == 1
	p.DMSEnabled = dmsEnabled == 1
	p.LiveStreamSupported = liveStreamSupported == 1
	return &p, nil
}

func (r *SQLRepository) ListDashcamVehicles(ctx context.Context, tenantID string) ([]VideoVehicleStatus, error) {
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	query := `SELECT
		v.id, v.registration_number, v.vehicle_number, v.vehicle_type, v.status,
		p.camera_make_model, p.camera_serial, p.dual_facing, p.adas_enabled, p.dms_enabled,
		p.live_stream_supported, p.firmware_version, p.status, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM video_safety_events e WHERE e.vehicle_id = v.id AND e.tenant_id = v.tenant_id AND e.review_status = 'PENDING_REVIEW') AS pending_reviews,
		(SELECT COUNT(*) FROM video_safety_events e WHERE e.vehicle_id = v.id AND e.tenant_id = v.tenant_id AND e.event_time >= $2) AS today_events,
		(SELECT COUNT(*) FROM video_safety_events e WHERE e.vehicle_id = v.id AND e.tenant_id = v.tenant_id AND e.severity = 'CRITICAL' AND e.event_time >= $2) AS critical_events
	FROM vehicles v
	INNER JOIN vehicle_dashcam_profiles p ON v.id = p.vehicle_id AND v.tenant_id = p.tenant_id
	WHERE v.tenant_id = $1
	ORDER BY v.vehicle_number ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, todayStart)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []VideoVehicleStatus
	for rows.Next() {
		var (
			v                                                     VideoVehicleStatus
			pMake, pSerial, pFirmware, pStatus                    string
			pDual, pADAS, pDMS, pLive                             int
			pCreated, pUpdated                                    time.Time
			pendingReviews, todayEventsCount, criticalEventsCount int
		)

		err := rows.Scan(
			&v.VehicleID, &v.RegistrationNumber, &v.VehicleNumber, &v.VehicleType, &v.Status,
			&pMake, &pSerial, &pDual, &pADAS, &pDMS,
			&pLive, &pFirmware, &pStatus, &pCreated, &pUpdated,
			&pendingReviews, &todayEventsCount, &criticalEventsCount,
		)
		if err != nil {
			return nil, err
		}

		v.Profile = &DashcamProfile{
			VehicleID:           v.VehicleID,
			TenantID:            tenantID,
			CameraMakeModel:     pMake,
			CameraSerial:        pSerial,
			DualFacing:          pDual == 1,
			ADASEnabled:         pADAS == 1,
			DMSEnabled:          pDMS == 1,
			LiveStreamSupported: pLive == 1,
			FirmwareVersion:     pFirmware,
			Status:              pStatus,
			CreatedAt:           pCreated,
			UpdatedAt:           pUpdated,
		}
		v.PendingReviewsCount = pendingReviews
		v.TodayEventsCount = todayEventsCount
		v.CriticalEventsCount = criticalEventsCount

		results = append(results, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Attach active livestream sessions if any
	activeStreamQuery := `SELECT
		id, tenant_id, vehicle_id, channel, stream_protocol,
		stream_url, session_token, status, started_at, requested_by
	FROM video_livestream_sessions
	WHERE tenant_id = $1 AND status IN ('INITIATED', 'ACTIVE')`

	streamRows, err := r.db.QueryContext(ctx, activeStreamQuery, tenantID)
	if err == nil {
		defer func() { _ = streamRows.Close() }()
		activeStreams := make(map[string]*LivestreamSession)
		for streamRows.Next() {
			var s LivestreamSession
			if err := streamRows.Scan(
				&s.ID, &s.TenantID, &s.VehicleID, &s.Channel, &s.StreamProtocol,
				&s.StreamURL, &s.SessionToken, &s.Status, &s.StartedAt, &s.RequestedBy,
			); err == nil {
				activeStreams[s.VehicleID] = &s
			}
		}
		for i := range results {
			if s, ok := activeStreams[results[i].VehicleID]; ok {
				results[i].ActiveLivestream = s
			}
		}
	}

	return results, nil
}

func (r *SQLRepository) CreateSafetyEvent(ctx context.Context, e *VideoSafetyEvent) error {
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.EventTime.IsZero() {
		e.EventTime = now
	}
	if e.ReviewStatus == "" {
		e.ReviewStatus = ReviewPending
	}

	query := `INSERT INTO video_safety_events (
		id, tenant_id, vehicle_id, driver_id, trip_id,
		category, event_type, severity, event_time,
		speed_kmh, latitude, longitude, location_name,
		media_clip_url, thumbnail_url, duration_seconds,
		review_status, coaching_note, reviewed_by, reviewed_at,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`

	_, err := r.db.ExecContext(ctx, query,
		e.ID, e.TenantID, e.VehicleID, e.DriverID, e.TripID,
		e.Category, e.EventType, e.Severity, e.EventTime,
		e.SpeedKMH, e.Latitude, e.Longitude, e.LocationName,
		e.MediaClipURL, e.ThumbnailURL, e.DurationSeconds,
		e.ReviewStatus, e.CoachingNote, e.ReviewedBy, e.ReviewedAt,
		e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetSafetyEvent(ctx context.Context, tenantID, eventID string) (*VideoSafetyEvent, error) {
	query := `SELECT
		e.id, e.tenant_id, e.vehicle_id, v.vehicle_number, e.driver_id, (d.first_name || ' ' || d.last_name) AS driver_name,
		e.trip_id, e.category, e.event_type, e.severity, e.event_time,
		e.speed_kmh, e.latitude, e.longitude, e.location_name,
		e.media_clip_url, e.thumbnail_url, e.duration_seconds,
		e.review_status, e.coaching_note, e.reviewed_by, e.reviewed_at,
		e.created_at, e.updated_at
	FROM video_safety_events e
	LEFT JOIN vehicles v ON e.vehicle_id = v.id AND e.tenant_id = v.tenant_id
	LEFT JOIN drivers d ON e.driver_id = d.id AND e.tenant_id = d.tenant_id
	WHERE e.tenant_id = $1 AND e.id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, eventID)
	var e VideoSafetyEvent
	err := row.Scan(
		&e.ID, &e.TenantID, &e.VehicleID, &e.VehicleNumber, &e.DriverID, &e.DriverName,
		&e.TripID, &e.Category, &e.EventType, &e.Severity, &e.EventTime,
		&e.SpeedKMH, &e.Latitude, &e.Longitude, &e.LocationName,
		&e.MediaClipURL, &e.ThumbnailURL, &e.DurationSeconds,
		&e.ReviewStatus, &e.CoachingNote, &e.ReviewedBy, &e.ReviewedAt,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *SQLRepository) ListSafetyEvents(ctx context.Context, tenantID string, filter EventFilter) ([]VideoSafetyEvent, int, error) {
	var conditions []string
	var args []any
	paramIdx := 1

	conditions = append(conditions, fmt.Sprintf("e.tenant_id = $%d", paramIdx))
	args = append(args, tenantID)
	paramIdx++

	if filter.VehicleID != "" {
		conditions = append(conditions, fmt.Sprintf("e.vehicle_id = $%d", paramIdx))
		args = append(args, filter.VehicleID)
		paramIdx++
	}
	if filter.DriverID != "" {
		conditions = append(conditions, fmt.Sprintf("e.driver_id = $%d", paramIdx))
		args = append(args, filter.DriverID)
		paramIdx++
	}
	if filter.Category != "" {
		conditions = append(conditions, fmt.Sprintf("e.category = $%d", paramIdx))
		args = append(args, filter.Category)
		paramIdx++
	}
	if filter.EventType != "" {
		conditions = append(conditions, fmt.Sprintf("e.event_type = $%d", paramIdx))
		args = append(args, filter.EventType)
		paramIdx++
	}
	if filter.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("e.severity = $%d", paramIdx))
		args = append(args, filter.Severity)
		paramIdx++
	}
	if filter.ReviewStatus != "" {
		conditions = append(conditions, fmt.Sprintf("e.review_status = $%d", paramIdx))
		args = append(args, filter.ReviewStatus)
		paramIdx++
	}

	whereClause := strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM video_safety_events e WHERE %s`, whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`SELECT
		e.id, e.tenant_id, e.vehicle_id, v.vehicle_number, e.driver_id, (d.first_name || ' ' || d.last_name) AS driver_name,
		e.trip_id, e.category, e.event_type, e.severity, e.event_time,
		e.speed_kmh, e.latitude, e.longitude, e.location_name,
		e.media_clip_url, e.thumbnail_url, e.duration_seconds,
		e.review_status, e.coaching_note, e.reviewed_by, e.reviewed_at,
		e.created_at, e.updated_at
	FROM video_safety_events e
	LEFT JOIN vehicles v ON e.vehicle_id = v.id AND e.tenant_id = v.tenant_id
	LEFT JOIN drivers d ON e.driver_id = d.id AND e.tenant_id = d.tenant_id
	WHERE %s
	ORDER BY e.event_time DESC
	LIMIT $%d OFFSET $%d`, whereClause, paramIdx, paramIdx+1)

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var events []VideoSafetyEvent
	for rows.Next() {
		var e VideoSafetyEvent
		err := rows.Scan(
			&e.ID, &e.TenantID, &e.VehicleID, &e.VehicleNumber, &e.DriverID, &e.DriverName,
			&e.TripID, &e.Category, &e.EventType, &e.Severity, &e.EventTime,
			&e.SpeedKMH, &e.Latitude, &e.Longitude, &e.LocationName,
			&e.MediaClipURL, &e.ThumbnailURL, &e.DurationSeconds,
			&e.ReviewStatus, &e.CoachingNote, &e.ReviewedBy, &e.ReviewedAt,
			&e.CreatedAt, &e.UpdatedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}

	return events, total, rows.Err()
}

func (r *SQLRepository) UpdateSafetyEventReview(ctx context.Context, tenantID, eventID, reviewStatus, coachingNote, reviewedBy string, reviewedAt time.Time) error {
	now := time.Now().UTC()
	query := `UPDATE video_safety_events
	SET review_status = $3,
		coaching_note = $4,
		reviewed_by = $5,
		reviewed_at = $6,
		updated_at = $7
	WHERE tenant_id = $1 AND id = $2`

	res, err := r.db.ExecContext(ctx, query, tenantID, eventID, reviewStatus, coachingNote, reviewedBy, reviewedAt, now)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return errors.New("safety event not found")
	}
	return nil
}

func (r *SQLRepository) CreateLivestreamSession(ctx context.Context, s *LivestreamSession) error {
	now := time.Now().UTC()
	if s.StartedAt.IsZero() {
		s.StartedAt = now
	}
	if s.Status == "" {
		s.Status = StreamInitiated
	}

	query := `INSERT INTO video_livestream_sessions (
		id, tenant_id, vehicle_id, channel, stream_protocol,
		stream_url, session_token, status, started_at, requested_by
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.TenantID, s.VehicleID, s.Channel, s.StreamProtocol,
		s.StreamURL, s.SessionToken, s.Status, s.StartedAt, s.RequestedBy,
	)
	return err
}

func (r *SQLRepository) GetLivestreamSession(ctx context.Context, tenantID, sessionID string) (*LivestreamSession, error) {
	query := `SELECT
		id, tenant_id, vehicle_id, channel, stream_protocol,
		stream_url, session_token, status, started_at, ended_at, requested_by
	FROM video_livestream_sessions
	WHERE tenant_id = $1 AND id = $2`

	row := r.db.QueryRowContext(ctx, query, tenantID, sessionID)
	var s LivestreamSession
	err := row.Scan(
		&s.ID, &s.TenantID, &s.VehicleID, &s.Channel, &s.StreamProtocol,
		&s.StreamURL, &s.SessionToken, &s.Status, &s.StartedAt, &s.EndedAt, &s.RequestedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SQLRepository) GetActiveLivestreamByVehicle(ctx context.Context, tenantID, vehicleID string) (*LivestreamSession, error) {
	query := `SELECT
		id, tenant_id, vehicle_id, channel, stream_protocol,
		stream_url, session_token, status, started_at, ended_at, requested_by
	FROM video_livestream_sessions
	WHERE tenant_id = $1 AND vehicle_id = $2 AND status IN ('INITIATED', 'ACTIVE')
	ORDER BY started_at DESC
	LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, tenantID, vehicleID)
	var s LivestreamSession
	err := row.Scan(
		&s.ID, &s.TenantID, &s.VehicleID, &s.Channel, &s.StreamProtocol,
		&s.StreamURL, &s.SessionToken, &s.Status, &s.StartedAt, &s.EndedAt, &s.RequestedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SQLRepository) EndLivestreamSession(ctx context.Context, tenantID, sessionID string, endedAt time.Time) error {
	query := `UPDATE video_livestream_sessions
	SET status = 'ENDED',
		ended_at = $3
	WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, sessionID, endedAt)
	return err
}

// ActivateLivestreamSession marks an INITIATED livestream session ACTIVE. It
// refuses to touch sessions in any other state (no rows affected → error).
func (r *SQLRepository) ActivateLivestreamSession(ctx context.Context, tenantID, sessionID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE video_livestream_sessions
	SET status = 'ACTIVE'
	WHERE tenant_id = $1 AND id = $2 AND status = 'INITIATED'`, tenantID, sessionID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("livestream session not found or not INITIATED")
	}
	return nil
}

func (r *SQLRepository) UpsertSafetyScorecard(ctx context.Context, card *DriverSafetyScorecard) error {
	now := time.Now().UTC()
	if card.CreatedAt.IsZero() {
		card.CreatedAt = now
	}
	card.UpdatedAt = now

	query := `INSERT INTO driver_safety_scorecards (
		id, tenant_id, driver_id, period_start, period_end,
		distance_km, safety_score, adas_event_count, dms_event_count,
		critical_event_count, coached_event_count, risk_tier,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	ON CONFLICT (id) DO UPDATE SET
		period_start = EXCLUDED.period_start,
		period_end = EXCLUDED.period_end,
		distance_km = EXCLUDED.distance_km,
		safety_score = EXCLUDED.safety_score,
		adas_event_count = EXCLUDED.adas_event_count,
		dms_event_count = EXCLUDED.dms_event_count,
		critical_event_count = EXCLUDED.critical_event_count,
		coached_event_count = EXCLUDED.coached_event_count,
		risk_tier = EXCLUDED.risk_tier,
		updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, query,
		card.ID, card.TenantID, card.DriverID, card.PeriodStart, card.PeriodEnd,
		card.DistanceKM, card.SafetyScore, card.ADASEventCount, card.DMSEventCount,
		card.CriticalEventCount, card.CoachedEventCount, card.RiskTier,
		card.CreatedAt, card.UpdatedAt,
	)
	return err
}

func (r *SQLRepository) GetDriverScorecard(ctx context.Context, tenantID, driverID string) (*DriverSafetyScorecard, error) {
	query := `SELECT
		s.id, s.tenant_id, s.driver_id, (d.first_name || ' ' || d.last_name) AS driver_name, s.period_start, s.period_end,
		s.distance_km, s.safety_score, s.adas_event_count, s.dms_event_count,
		s.critical_event_count, s.coached_event_count, s.risk_tier,
		s.created_at, s.updated_at
	FROM driver_safety_scorecards s
	LEFT JOIN drivers d ON s.driver_id = d.id AND s.tenant_id = d.tenant_id
	WHERE s.tenant_id = $1 AND s.driver_id = $2
	ORDER BY s.period_end DESC
	LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, tenantID, driverID)
	var s DriverSafetyScorecard
	err := row.Scan(
		&s.ID, &s.TenantID, &s.DriverID, &s.DriverName, &s.PeriodStart, &s.PeriodEnd,
		&s.DistanceKM, &s.SafetyScore, &s.ADASEventCount, &s.DMSEventCount,
		&s.CriticalEventCount, &s.CoachedEventCount, &s.RiskTier,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SQLRepository) ListDriverScorecards(ctx context.Context, tenantID string, limit, offset int) ([]DriverSafetyScorecard, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	countQuery := `SELECT COUNT(*) FROM driver_safety_scorecards WHERE tenant_id = $1`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT
		s.id, s.tenant_id, s.driver_id, (d.first_name || ' ' || d.last_name) AS driver_name, s.period_start, s.period_end,
		s.distance_km, s.safety_score, s.adas_event_count, s.dms_event_count,
		s.critical_event_count, s.coached_event_count, s.risk_tier,
		s.created_at, s.updated_at
	FROM driver_safety_scorecards s
	LEFT JOIN drivers d ON s.driver_id = d.id AND s.tenant_id = d.tenant_id
	WHERE s.tenant_id = $1
	ORDER BY s.safety_score ASC, s.period_end DESC
	LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var scorecards []DriverSafetyScorecard
	for rows.Next() {
		var s DriverSafetyScorecard
		err := rows.Scan(
			&s.ID, &s.TenantID, &s.DriverID, &s.DriverName, &s.PeriodStart, &s.PeriodEnd,
			&s.DistanceKM, &s.SafetyScore, &s.ADASEventCount, &s.DMSEventCount,
			&s.CriticalEventCount, &s.CoachedEventCount, &s.RiskTier,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		scorecards = append(scorecards, s)
	}

	return scorecards, total, rows.Err()
}

func (r *SQLRepository) GetFleetMetrics(ctx context.Context, tenantID string) (*VideoFleetMetrics, error) {
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	metrics := &VideoFleetMetrics{}

	query := `SELECT
		(SELECT COUNT(DISTINCT vehicle_id) FROM vehicle_dashcam_profiles WHERE tenant_id = $1),
		(SELECT COUNT(*) FROM vehicle_dashcam_profiles WHERE tenant_id = $1 AND status = 'ONLINE'),
		(SELECT COUNT(*) FROM video_safety_events WHERE tenant_id = $1 AND event_time >= $2),
		(SELECT COUNT(*) FROM video_safety_events WHERE tenant_id = $1 AND severity = 'CRITICAL' AND event_time >= $2),
		(SELECT COUNT(*) FROM video_safety_events WHERE tenant_id = $1 AND review_status = 'PENDING_REVIEW'),
		COALESCE((SELECT AVG(safety_score) FROM driver_safety_scorecards WHERE tenant_id = $1), 100.0)`

	err := r.db.QueryRowContext(ctx, query, tenantID, todayStart).Scan(
		&metrics.TotalCameras,
		&metrics.OnlineCameras,
		&metrics.TodayEventsCount,
		&metrics.CriticalEventsToday,
		&metrics.PendingCoachingCount,
		&metrics.FleetAvgSafetyScore,
	)
	if err != nil {
		return nil, err
	}
	return metrics, nil
}

func (r *SQLRepository) GetDriverEventCounts(ctx context.Context, tenantID, driverID string, start, end time.Time) (adasCount, dmsCount, criticalCount, coachedCount int, err error) {
	query := `SELECT
		COALESCE(SUM(CASE WHEN category = 'ADAS' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN category = 'DMS' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN severity = 'CRITICAL' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN review_status = 'COACHED' THEN 1 ELSE 0 END), 0)
	FROM video_safety_events
	WHERE tenant_id = $1 AND driver_id = $2 AND event_time >= $3 AND event_time <= $4`

	err = r.db.QueryRowContext(ctx, query, tenantID, driverID, start, end).Scan(
		&adasCount, &dmsCount, &criticalCount, &coachedCount,
	)
	return
}
