package videotelematics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service provides high-level business logic for video telematics and AI dashcams.
type Service struct {
	repo           Repository
	mediaServerURL string
}

// NewService creates a new video telematics service. An empty mediaServerURL
// keeps live streaming fail-closed (P0 "stop fake data"): StartLivestream
// returns an error and creates no session row until a real media server is
// configured (Plan P3 item 3.12).
func NewService(repo Repository, mediaServerURL string) *Service {
	return &Service{repo: repo, mediaServerURL: strings.TrimSpace(mediaServerURL)}
}

// ConfigureProfile configures or updates the dashcam profile and edge AI features for a vehicle.
func (s *Service) ConfigureProfile(ctx context.Context, tenantID, vehicleID string, in ConfigureDashcamProfileInput) (*DashcamProfile, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	profile := &DashcamProfile{
		VehicleID:           vehicleID,
		TenantID:            tenantID,
		CameraMakeModel:     in.CameraMakeModel,
		CameraSerial:        in.CameraSerial,
		DualFacing:          in.DualFacing,
		ADASEnabled:         in.ADASEnabled,
		DMSEnabled:          in.DMSEnabled,
		LiveStreamSupported: in.LiveStreamSupported,
		FirmwareVersion:     in.FirmwareVersion,
		Status:              in.Status,
	}

	if err := s.repo.UpsertDashcamProfile(ctx, profile); err != nil {
		return nil, err
	}

	return s.repo.GetDashcamProfile(ctx, tenantID, vehicleID)
}

// GetProfile retrieves the dashcam configuration profile for a vehicle.
func (s *Service) GetProfile(ctx context.Context, tenantID, vehicleID string) (*DashcamProfile, error) {
	return s.repo.GetDashcamProfile(ctx, tenantID, vehicleID)
}

// ListDashcamVehicles lists all vehicles equipped with dashcams, along with today's incident metrics.
func (s *Service) ListDashcamVehicles(ctx context.Context, tenantID string) ([]VideoVehicleStatus, error) {
	return s.repo.ListDashcamVehicles(ctx, tenantID)
}

// IngestSafetyEvent handles edge AI dashcam incident uploads (ADAS/DMS) and triggers safety workflows.
func (s *Service) IngestSafetyEvent(ctx context.Context, tenantID string, in IngestSafetyEventInput) (*VideoSafetyEvent, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	eventID := uuid.NewString()
	eventTime := in.EventTime
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}

	// Media evidence is stored only as received from the device (P0 "stop
	// fake data"): no clip upload means empty URLs — UI shows "evidence
	// pending" and the scorecard never rests on fabricated media.
	event := &VideoSafetyEvent{
		ID:              eventID,
		TenantID:        tenantID,
		VehicleID:       in.VehicleID,
		DriverID:        in.DriverID,
		TripID:          in.TripID,
		Category:        in.Category,
		EventType:       in.EventType,
		Severity:        in.Severity,
		EventTime:       eventTime,
		SpeedKMH:        in.SpeedKMH,
		Latitude:        in.Latitude,
		Longitude:       in.Longitude,
		LocationName:    in.LocationName,
		MediaClipURL:    in.MediaClipURL,
		ThumbnailURL:    in.ThumbnailURL,
		DurationSeconds: in.DurationSeconds,
		ReviewStatus:    ReviewPending,
	}

	if err := s.repo.CreateSafetyEvent(ctx, event); err != nil {
		return nil, err
	}

	// Update driver safety scorecard in the background if driver is associated
	if in.DriverID != nil && strings.TrimSpace(*in.DriverID) != "" {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -30)
		_, _ = s.CalculateAndSaveDriverScorecard(ctx, tenantID, *in.DriverID, start, now, 0)
	}

	return s.repo.GetSafetyEvent(ctx, tenantID, eventID)
}

// GetSafetyEvent returns details of a single safety event.
func (s *Service) GetSafetyEvent(ctx context.Context, tenantID, eventID string) (*VideoSafetyEvent, error) {
	return s.repo.GetSafetyEvent(ctx, tenantID, eventID)
}

// ListSafetyEvents queries safety events matching optional filters.
func (s *Service) ListSafetyEvents(ctx context.Context, tenantID string, filter EventFilter) ([]VideoSafetyEvent, int, error) {
	return s.repo.ListSafetyEvents(ctx, tenantID, filter)
}

// ReviewSafetyEvent updates safety event review status (COACHED, DISMISSED, ESCALATED) with coaching notes.
func (s *Service) ReviewSafetyEvent(ctx context.Context, tenantID, eventID string, in ReviewSafetyEventInput) (*VideoSafetyEvent, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := s.repo.UpdateSafetyEventReview(ctx, tenantID, eventID, in.ReviewStatus, in.CoachingNote, in.ReviewedBy, now); err != nil {
		return nil, err
	}

	event, err := s.repo.GetSafetyEvent(ctx, tenantID, eventID)
	if err != nil {
		return nil, err
	}

	// Refresh scorecard if driver was coached
	if event != nil && event.DriverID != nil && strings.TrimSpace(*event.DriverID) != "" {
		start := now.AddDate(0, 0, -30)
		_, _ = s.CalculateAndSaveDriverScorecard(ctx, tenantID, *event.DriverID, start, now, 0)
	}

	return event, nil
}

// StartLivestream brokers a live video stream session against the configured
// media server. It fails closed when no media server is configured — no
// fabricated URLs, no session row.
func (s *Service) StartLivestream(ctx context.Context, tenantID string, in StartLivestreamInput) (*LivestreamSession, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	if s.mediaServerURL == "" {
		return nil, errors.New("livestream media server is not configured; set VIDEO_MEDIA_SERVER_URL")
	}

	// Terminate any existing pending/active stream for this vehicle
	if existing, _ := s.repo.GetActiveLivestreamByVehicle(ctx, tenantID, in.VehicleID); existing != nil {
		_ = s.repo.EndLivestreamSession(ctx, tenantID, existing.ID, time.Now().UTC())
	}

	sessionToken := uuid.NewString()
	channelSlug := strings.ToLower(in.Channel)
	var streamURL string

	switch in.StreamProtocol {
	case ProtocolHLS:
		streamURL = fmt.Sprintf("%s/hls/%s/%s/%s/live.m3u8?token=%s", s.mediaServerURL, tenantID, in.VehicleID, channelSlug, sessionToken)
	case ProtocolRTSP:
		streamURL = fmt.Sprintf("%s/%s/%s/%s?token=%s", s.mediaServerURL, tenantID, in.VehicleID, channelSlug, sessionToken)
	default:
		streamURL = fmt.Sprintf("%s/streams/%s/%s/%s?token=%s", s.mediaServerURL, tenantID, in.VehicleID, channelSlug, sessionToken)
	}

	session := &LivestreamSession{
		ID:             uuid.NewString(),
		TenantID:       tenantID,
		VehicleID:      in.VehicleID,
		Channel:        in.Channel,
		StreamProtocol: in.StreamProtocol,
		StreamURL:      streamURL,
		SessionToken:   sessionToken,
		// INITIATED until an external force/confirm call flips it to ACTIVE —
		// sessions are never fabricated as already-streaming (Plan P3 item 3.12).
		Status:      StreamInitiated,
		StartedAt:   time.Now().UTC(),
		RequestedBy: in.RequestedBy,
	}

	if err := s.repo.CreateLivestreamSession(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

// EndLivestream concludes an active video streaming session.
func (s *Service) EndLivestream(ctx context.Context, tenantID, sessionID string) error {
	return s.repo.EndLivestreamSession(ctx, tenantID, sessionID, time.Now().UTC())
}

// CalculateAndSaveDriverScorecard aggregates safety events and recalculates a 0-100 score.
func (s *Service) CalculateAndSaveDriverScorecard(ctx context.Context, tenantID, driverID string, periodStart, periodEnd time.Time, distanceKM float64) (*DriverSafetyScorecard, error) {
	if periodStart.IsZero() {
		periodStart = time.Now().UTC().AddDate(0, 0, -30)
	}
	if periodEnd.IsZero() {
		periodEnd = time.Now().UTC()
	}

	existing, _ := s.repo.GetDriverScorecard(ctx, tenantID, driverID)
	if distanceKM <= 0 {
		if existing != nil && existing.DistanceKM > 0 {
			distanceKM = existing.DistanceKM
		} else {
			distanceKM = 1200.0 // 1,200 km default monthly baseline
		}
	}

	adasCount, dmsCount, critCount, coachedCount, err := s.repo.GetDriverEventCounts(ctx, tenantID, driverID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	score, tier := CalculateSafetyScore(distanceKM, adasCount, dmsCount, critCount)

	scorecardID := uuid.NewString()
	if existing != nil {
		scorecardID = existing.ID
	}

	card := &DriverSafetyScorecard{
		ID:                 scorecardID,
		TenantID:           tenantID,
		DriverID:           driverID,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		DistanceKM:         distanceKM,
		SafetyScore:        score,
		ADASEventCount:     adasCount,
		DMSEventCount:      dmsCount,
		CriticalEventCount: critCount,
		CoachedEventCount:  coachedCount,
		RiskTier:           tier,
	}

	if err := s.repo.UpsertSafetyScorecard(ctx, card); err != nil {
		return nil, err
	}

	return s.repo.GetDriverScorecard(ctx, tenantID, driverID)
}

// GetDriverScorecard retrieves the latest scorecard for a driver.
func (s *Service) GetDriverScorecard(ctx context.Context, tenantID, driverID string) (*DriverSafetyScorecard, error) {
	return s.repo.GetDriverScorecard(ctx, tenantID, driverID)
}

// ListDriverScorecards returns driver scorecards for the fleet leaderboard.
func (s *Service) ListDriverScorecards(ctx context.Context, tenantID string, limit, offset int) ([]DriverSafetyScorecard, int, error) {
	return s.repo.ListDriverScorecards(ctx, tenantID, limit, offset)
}

// GetFleetMetrics returns aggregate video telematics health and safety metrics.
func (s *Service) GetFleetMetrics(ctx context.Context, tenantID string) (*VideoFleetMetrics, error) {
	return s.repo.GetFleetMetrics(ctx, tenantID)
}
