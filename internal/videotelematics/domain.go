package videotelematics

import (
	"errors"
	"math"
	"strings"
	"time"
)

// Video telematics event categories.
const (
	CategoryDMS       = "DMS"
	CategoryADAS      = "ADAS"
	CategoryManualSOS = "MANUAL_SOS"
)

// Video telematics safety incident classifications.
const (
	// Driver Monitoring System (DMS) - Cabin-facing
	EventTypeDrowsiness        = "DROWSINESS"
	EventTypeCellphone         = "CELLPHONE_DISTRACTION"
	EventTypeSeatbeltUnbuckled = "SEATBELT_UNBUCKLED"
	EventTypeSmoking           = "SMOKING"
	EventTypeYawning           = "YAWNING"
	EventTypeFaceMismatch      = "FACE_MISMATCH"
	EventTypeCameraObstruction = "CAMERA_OBSTRUCTION"

	// Advanced Driver Assistance System (ADAS) - Road-facing
	EventTypeFCW            = "FORWARD_COLLISION_WARNING"
	EventTypeLDW            = "LANE_DEPARTURE_WARNING"
	EventTypeTailgating     = "TAILGATING"
	EventTypePCW            = "PEDESTRIAN_COLLISION_WARNING"
	EventTypeHarshCornering = "HARSH_CORNERING"
	EventTypeHarshBraking   = "HARSH_BRAKING"
	EventTypePanicSOS       = "PANIC_SOS"
)

// Incident severity levels.
const (
	SeverityLow      = "LOW"
	SeverityMedium   = "MEDIUM"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"
)

// Coaching & review workflow states.
const (
	ReviewPending   = "PENDING_REVIEW"
	ReviewCoached   = "COACHED"
	ReviewDismissed = "DISMISSED"
	ReviewEscalated = "ESCALATED"
)

// Dashcam hardware operating states.
const (
	DashcamOnline  = "ONLINE"
	DashcamOffline = "OFFLINE"
	DashcamStandby = "STANDBY"
	DashcamFault   = "FAULT"
)

// Live streaming video channels.
const (
	ChannelRoadFacing  = "ROAD_FACING"
	ChannelCabinFacing = "CABIN_FACING"
	ChannelCargoFacing = "CARGO_FACING"
)

// Supported live streaming protocols.
const (
	ProtocolWebRTC = "WEBRTC"
	ProtocolHLS    = "HLS"
	ProtocolRTSP   = "RTSP"
)

// Live stream session statuses.
const (
	StreamInitiated = "INITIATED"
	StreamActive    = "ACTIVE"
	StreamEnded     = "ENDED"
	StreamFailed    = "FAILED"
)

// Driver safety risk tiers.
const (
	RiskLow      = "LOW_RISK"
	RiskModerate = "MODERATE_RISK"
	RiskHigh     = "HIGH_RISK"
	RiskCritical = "CRITICAL_RISK"
)

// DashcamProfile defines the dashcam hardware binding and active AI vision features.
type DashcamProfile struct {
	VehicleID           string    `json:"vehicle_id"`
	TenantID            string    `json:"tenant_id"`
	CameraMakeModel     string    `json:"camera_make_model"`
	CameraSerial        string    `json:"camera_serial"`
	DualFacing          bool      `json:"dual_facing"`
	ADASEnabled         bool      `json:"adas_enabled"`
	DMSEnabled          bool      `json:"dms_enabled"`
	LiveStreamSupported bool      `json:"live_stream_supported"`
	FirmwareVersion     string    `json:"firmware_version"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// VideoSafetyEvent represents an edge AI detected driver safety or collision hazard event.
type VideoSafetyEvent struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	VehicleID       string     `json:"vehicle_id"`
	VehicleNumber   *string    `json:"vehicle_number,omitempty"`
	DriverID        *string    `json:"driver_id,omitempty"`
	DriverName      *string    `json:"driver_name,omitempty"`
	TripID          *string    `json:"trip_id,omitempty"`
	Category        string     `json:"category"`
	EventType       string     `json:"event_type"`
	Severity        string     `json:"severity"`
	EventTime       time.Time  `json:"event_time"`
	SpeedKMH        float64    `json:"speed_kmh"`
	Latitude        *float64   `json:"latitude,omitempty"`
	Longitude       *float64   `json:"longitude,omitempty"`
	LocationName    string     `json:"location_name"`
	MediaClipURL    string     `json:"media_clip_url"`
	ThumbnailURL    string     `json:"thumbnail_url"`
	DurationSeconds int        `json:"duration_seconds"`
	ReviewStatus    string     `json:"review_status"`
	CoachingNote    string     `json:"coaching_note"`
	ReviewedBy      string     `json:"reviewed_by"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// LivestreamSession manages real-time remote camera viewing credentials and endpoints.
type LivestreamSession struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	VehicleID      string     `json:"vehicle_id"`
	Channel        string     `json:"channel"`
	StreamProtocol string     `json:"stream_protocol"`
	StreamURL      string     `json:"stream_url"`
	SessionToken   string     `json:"session_token"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	RequestedBy    string     `json:"requested_by"`
}

// DriverSafetyScorecard aggregates driver risk behavior over a defined operating period.
type DriverSafetyScorecard struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	DriverID           string    `json:"driver_id"`
	DriverName         *string   `json:"driver_name,omitempty"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	DistanceKM         float64   `json:"distance_km"`
	SafetyScore        float64   `json:"safety_score"`
	ADASEventCount     int       `json:"adas_event_count"`
	DMSEventCount      int       `json:"dms_event_count"`
	CriticalEventCount int       `json:"critical_event_count"`
	CoachedEventCount  int       `json:"coached_event_count"`
	RiskTier           string    `json:"risk_tier"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// VideoVehicleStatus presents dashcam hardware status and pending reviews for fleet management.
type VideoVehicleStatus struct {
	VehicleID           string             `json:"vehicle_id"`
	RegistrationNumber  string             `json:"registration_number"`
	VehicleNumber       string             `json:"vehicle_number"`
	VehicleType         string             `json:"vehicle_type"`
	Status              string             `json:"status"`
	Profile             *DashcamProfile    `json:"profile,omitempty"`
	PendingReviewsCount int                `json:"pending_reviews_count"`
	TodayEventsCount    int                `json:"today_events_count"`
	CriticalEventsCount int                `json:"critical_events_count"`
	ActiveLivestream    *LivestreamSession `json:"active_livestream,omitempty"`
}

// VideoFleetMetrics provides high-level health and safety KPIs across the dashcam fleet.
type VideoFleetMetrics struct {
	TotalCameras         int     `json:"total_cameras"`
	OnlineCameras        int     `json:"online_cameras"`
	TodayEventsCount     int     `json:"today_events_count"`
	CriticalEventsToday  int     `json:"critical_events_today"`
	PendingCoachingCount int     `json:"pending_coaching_count"`
	FleetAvgSafetyScore  float64 `json:"fleet_avg_safety_score"`
}

// EventFilter specifies filter criteria for querying video safety events.
type EventFilter struct {
	VehicleID    string `json:"vehicle_id"`
	DriverID     string `json:"driver_id"`
	Category     string `json:"category"`
	EventType    string `json:"event_type"`
	Severity     string `json:"severity"`
	ReviewStatus string `json:"review_status"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
}

// ConfigureDashcamProfileInput configures hardware serial and AI feature toggles.
type ConfigureDashcamProfileInput struct {
	CameraMakeModel     string `json:"camera_make_model"`
	CameraSerial        string `json:"camera_serial"`
	DualFacing          bool   `json:"dual_facing"`
	ADASEnabled         bool   `json:"adas_enabled"`
	DMSEnabled          bool   `json:"dms_enabled"`
	LiveStreamSupported bool   `json:"live_stream_supported"`
	FirmwareVersion     string `json:"firmware_version"`
	Status              string `json:"status"`
}

func (in *ConfigureDashcamProfileInput) Validate() error {
	if strings.TrimSpace(in.CameraSerial) == "" {
		return errors.New("camera_serial is required")
	}
	if in.CameraMakeModel == "" {
		in.CameraMakeModel = "FleetX AI Dashcam Pro"
	}
	if in.FirmwareVersion == "" {
		in.FirmwareVersion = "v2.4.0-edge-ai"
	}
	if in.Status == "" {
		// Honest connectivity: registration does not attest to a live device.
		// STANDBY until the device connects; migration 00178's schema default
		// of 'ONLINE' is a known debt (deferred, see plan row 3.12).
		in.Status = DashcamStandby
	}
	return nil
}

// IngestSafetyEventInput processes edge camera event packets and video clip uploads.
type IngestSafetyEventInput struct {
	VehicleID       string    `json:"vehicle_id"`
	DriverID        *string   `json:"driver_id,omitempty"`
	TripID          *string   `json:"trip_id,omitempty"`
	Category        string    `json:"category"`
	EventType       string    `json:"event_type"`
	Severity        string    `json:"severity"`
	EventTime       time.Time `json:"event_time"`
	SpeedKMH        float64   `json:"speed_kmh"`
	Latitude        *float64  `json:"latitude,omitempty"`
	Longitude       *float64  `json:"longitude,omitempty"`
	LocationName    string    `json:"location_name"`
	MediaClipURL    string    `json:"media_clip_url"`
	ThumbnailURL    string    `json:"thumbnail_url"`
	DurationSeconds int       `json:"duration_seconds"`
}

func (in *IngestSafetyEventInput) Validate() error {
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	if in.Category == "" {
		in.Category = CategoryDMS
	}
	if in.EventType == "" {
		in.EventType = EventTypeDrowsiness
	}
	if in.Severity == "" {
		// Default severity based on event type
		switch in.EventType {
		case EventTypeDrowsiness, EventTypeFCW, EventTypePanicSOS, EventTypeFaceMismatch:
			in.Severity = SeverityCritical
		case EventTypeCellphone, EventTypeTailgating, EventTypePCW:
			in.Severity = SeverityHigh
		case EventTypeSeatbeltUnbuckled, EventTypeHarshBraking, EventTypeHarshCornering:
			in.Severity = SeverityMedium
		default:
			in.Severity = SeverityLow
		}
	}
	if in.DurationSeconds <= 0 {
		in.DurationSeconds = 15
	}
	if in.EventTime.IsZero() {
		in.EventTime = time.Now().UTC()
	}
	return nil
}

// ReviewSafetyEventInput records manager coaching notes and updates review status.
type ReviewSafetyEventInput struct {
	ReviewStatus string `json:"review_status"`
	CoachingNote string `json:"coaching_note"`
	ReviewedBy   string `json:"reviewed_by"`
}

func (in *ReviewSafetyEventInput) Validate() error {
	if in.ReviewStatus != ReviewCoached && in.ReviewStatus != ReviewDismissed && in.ReviewStatus != ReviewEscalated {
		return errors.New("invalid review_status; must be COACHED, DISMISSED, or ESCALATED")
	}
	if in.ReviewStatus == ReviewCoached && strings.TrimSpace(in.CoachingNote) == "" {
		return errors.New("coaching_note is required when marking an event COACHED")
	}
	return nil
}

// StartLivestreamInput requests an active dual-channel video feed.
type StartLivestreamInput struct {
	VehicleID      string `json:"vehicle_id"`
	Channel        string `json:"channel"`
	StreamProtocol string `json:"stream_protocol"`
	RequestedBy    string `json:"requested_by"`
}

func (in *StartLivestreamInput) Validate() error {
	if strings.TrimSpace(in.VehicleID) == "" {
		return errors.New("vehicle_id is required")
	}
	if in.Channel == "" {
		in.Channel = ChannelRoadFacing
	}
	if in.StreamProtocol == "" {
		in.StreamProtocol = ProtocolWebRTC
	}
	return nil
}

// CalculateSafetyScore computes a 0-100 score based on ADAS/DMS events and distance.
// distanceKM must be a real reported distance — callers must refuse earlier
// (see ErrInsufficientMileage); this function only picks the penalty scale.
func CalculateSafetyScore(distanceKM float64, adasCount, dmsCount, criticalCount int) (float64, string) {
	// Penalty scale floor: one 100 km unit at least (scale minimum, not a
	// fabricated distance — the stored distance_km is always the real value).
	unitsOf100KM := distanceKM / 100.0
	if unitsOf100KM < 1.0 {
		unitsOf100KM = 1.0
	}

	// Penalties normalized per 100 km
	penalty := ((float64(adasCount) * 1.5) + (float64(dmsCount) * 3.0) + (float64(criticalCount) * 8.0)) / unitsOf100KM
	score := math.Max(0.0, math.Min(100.0, 100.0-penalty))

	score = math.Round(score*10) / 10

	var tier string
	switch {
	case score >= 85.0:
		tier = RiskLow
	case score >= 70.0:
		tier = RiskModerate
	case score >= 50.0:
		tier = RiskHigh
	default:
		tier = RiskCritical
	}

	return score, tier
}
