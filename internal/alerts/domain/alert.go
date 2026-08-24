package domain

import (
	"time"
)

// Severity levels for alerts (Spec 05 §2)
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
	SeverityBlocker  = "blocker"
)

// Status values for canonical alerts
const (
	StatusOpen         = "open"
	StatusAcknowledged = "acknowledged"
	StatusResolved     = "resolved"
	StatusEscalated    = "escalated"
	StatusClosed       = "closed"
)

// Alert Sources (Spec 05 §2)
const (
	SourceTelemetry  = "telemetry"
	SourceGeofence   = "geofence"
	SourceFuel       = "fuel"
	SourceCompliance = "compliance"
	SourceEWayBill   = "ewaybill"
	SourceSOS        = "sos"
)

// 13 Canonical Telemetry / Alert Types (Spec 05 §12, 00059)
const (
	AlertTypeNightDriving         = "night_driving"
	AlertTypeRestrictedZone       = "restricted_zone"
	AlertTypeUnauthorizedMovement = "unauthorized_movement"
	AlertTypeOffHoursUse          = "off_hours_use"
	AlertTypeRefill               = "refill"
	AlertTypeTheftSuspicion       = "theft_suspicion"
	AlertTypeAbnormalDrain        = "abnormal_drain"
	AlertTypeSiphonConfirmed      = "siphon_confirmed"
	AlertTypeOdometerRollback     = "odometer_rollback"
	AlertTypeSpeeding             = "speeding"
	AlertTypeTempBreach           = "temp_breach"
	AlertTypeGPSDeviation         = "gps_deviation"
	AlertTypeGeofenceBreach       = "geofence_breach"
	AlertTypeEmergencySOS         = "emergency_sos"
	AlertTypeComplianceBlocked    = "compliance_blocked"
	AlertTypeEWayBillExpiring     = "ewaybill_expiring"
)

// AlertSource defines a producer source in the system.
type AlertSource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// Rule defines an alert matching, threshold, and routing rule.
type Rule struct {
	ID                 string    `json:"id"`
	Source             string    `json:"source"`
	AlertType          string    `json:"alert_type"`
	Name               string    `json:"name"`
	Severity           string    `json:"severity"`
	Threshold          *float64  `json:"threshold,omitempty"`
	ThresholdUnit      *string   `json:"threshold_unit,omitempty"`
	DedupKeyExpr       string    `json:"dedup_key_expr"`
	CooldownSeconds    int       `json:"cooldown_seconds"`
	StormWindowSeconds int       `json:"storm_window_seconds"`
	StormBatchMin      int       `json:"storm_batch_min"`
	ChannelRouting     string    `json:"channel_routing"` // JSON
	EscalationSchedule *string   `json:"escalation_schedule,omitempty"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// RuleOverride allows tuning thresholds/cooldowns per vehicle/entity.
type RuleOverride struct {
	ID              string    `json:"id"`
	RuleID          string    `json:"rule_id"`
	EntityID        *string   `json:"entity_id,omitempty"`
	Severity        *string   `json:"severity,omitempty"`
	Threshold       *float64  `json:"threshold,omitempty"`
	CooldownSeconds *int      `json:"cooldown_seconds,omitempty"`
	Channels        *string   `json:"channels,omitempty"`
	IsActive        bool      `json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
}

// Alert is the canonical operational alert item stored in `alerts` table.
type Alert struct {
	ID               string     `json:"id"`
	RuleID           *string    `json:"rule_id,omitempty"`
	Source           string     `json:"source"`
	AlertType        string     `json:"alert_type"`
	Severity         string     `json:"severity"`
	Status           string     `json:"status"`
	DedupKey         string     `json:"dedup_key"`
	TenantID         string     `json:"tenant_id"`
	AckStatus        string     `json:"ack_status"`
	SeverityRank     int        `json:"severity_rank"`
	MoneyAtRisk      float64    `json:"money_at_risk"`
	SnoozedUntil     *time.Time `json:"snoozed_until,omitempty"`
	EntityType       *string    `json:"entity_type,omitempty"`
	EntityID         *string    `json:"entity_id,omitempty"`
	UserID           *string    `json:"user_id,omitempty"`
	Title            string     `json:"title"`
	Message          string     `json:"message"`
	Occurrences      int        `json:"occurrences"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	NextEscalationAt *time.Time `json:"next_escalation_at,omitempty"`
	EscalationStep   int        `json:"escalation_step"`
	Latitude         *float64   `json:"latitude,omitempty"`
	Longitude        *float64   `json:"longitude,omitempty"`
	Metadata         string     `json:"metadata,omitempty"` // JSON string
	AckedBy          *string    `json:"acked_by,omitempty"`
	AckedAt          *time.Time `json:"acked_at,omitempty"`
	ResolvedBy       *string    `json:"resolved_by,omitempty"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Inbox ack lifecycle values (Spec 22 §3, column alerts.ack_status).
const (
	AckStatusOpen     = "open"
	AckStatusSnoozed  = "snoozed"
	AckStatusAcked    = "acked"
	AckStatusResolved = "resolved"
)

// Severity ranks 1-5 for the ranked inbox (Spec 22 §5.1). Lower rank sorts
// first. Emit-time default mapping from legacy severity strings; producers
// may pass an explicit rank via the event payload ("severity_rank").
// Fine-grained money/waste classification (rank 3) arrives with the
// kharcha/fuel emitters (Spec 22 S8/S9).
const (
	RankCritical = 1
	RankUrgent   = 2
	RankMoney    = 3
	RankWaste    = 4
	RankInfo     = 5
)

// SeverityToRank maps legacy pipeline severities to inbox ranks.
func SeverityToRank(severity string) int {
	switch severity {
	case SeverityBlocker:
		return RankCritical
	case SeverityCritical:
		return RankUrgent
	case SeverityWarning:
		return RankWaste
	default:
		return RankInfo
	}
}

// NotificationPreference defines per-user channel opt-ins.
type NotificationPreference struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Channel     string    `json:"channel"`
	Enabled     bool      `json:"enabled"`
	MinSeverity string    `json:"min_severity"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
