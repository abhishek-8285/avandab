package deviation

import (
	"context"
	"time"

	"transport-app/internal/fuel"
)

// Policy configuration keys for company_config overrides.
const (
	PolicyMaxDistanceMeters = "deviation.max_distance_meters"
	PolicyReturnDistanceM   = "deviation.return_distance_meters"
	PolicySustainedDuration = "deviation.sustained_duration_seconds"
	PolicyMinStreakCount    = "deviation.min_streak_count"
	PolicyMaxAccuracyMeters = "deviation.max_gps_accuracy_meters"
	PolicyMaxSpeedJumpKmh   = "deviation.max_feasible_speed_jump_kmh"
	PolicyMinMovingSpeedKmh = "deviation.min_moving_speed_kmh"
	PolicyCooldownMinutes   = "deviation.cooldown_minutes"
)

// Default thresholds for the GPS deviation engine.
const (
	DefaultMaxDistanceMeters = 5000.0 // 5.0 km from planned route
	DefaultReturnDistanceM   = 1500.0 // 1.5 km (hysteresis to return to ON_ROUTE)
	DefaultSustainedDuration = 60.0   // 60 seconds of sustained deviation
	DefaultMinStreakCount    = 2      // Minimum 2 consecutive deviated GPS fixes
	DefaultMaxAccuracyMeters = 50.0   // Reject noisy GPS fixes with accuracy > 50m
	DefaultMaxSpeedJumpKmh   = 180.0  // Reject impossible teleportation jumps > 180 km/h
	DefaultMinMovingSpeedKmh = 3.0    // Ignore stationary vehicle drift near hubs
	DefaultCooldownDuration  = 30 * time.Minute
	DefaultPollInterval      = 30 * time.Second
)

// DeviationPolicy defines the thresholds and defense gates for route deviation detection.
type DeviationPolicy struct {
	TenantID             string
	MaxDistanceMeters    float64
	ReturnDistanceMeters float64
	SustainedDurationSec float64
	MinStreakCount       int
	MaxAccuracyMeters    float64
	MaxFeasibleSpeedKmh  float64
	MinMovingSpeedKmh    float64
	CooldownDuration     time.Duration
}

// DefaultDeviationPolicy returns standard production defaults for a tenant.
func DefaultDeviationPolicy(tenantID string) DeviationPolicy {
	if tenantID == "" {
		tenantID = "1"
	}
	return DeviationPolicy{
		TenantID:             tenantID,
		MaxDistanceMeters:    DefaultMaxDistanceMeters,
		ReturnDistanceMeters: DefaultReturnDistanceM,
		SustainedDurationSec: DefaultSustainedDuration,
		MinStreakCount:       DefaultMinStreakCount,
		MaxAccuracyMeters:    DefaultMaxAccuracyMeters,
		MaxFeasibleSpeedKmh:  DefaultMaxSpeedJumpKmh,
		MinMovingSpeedKmh:    DefaultMinMovingSpeedKmh,
		CooldownDuration:     DefaultCooldownDuration,
	}
}

// LoadDeviationPolicy reads tenant-specific policy thresholds from company_config.
func LoadDeviationPolicy(ctx context.Context, tenantID string, r *fuel.ConfigReader) (DeviationPolicy, error) {
	p := DefaultDeviationPolicy(tenantID)
	if r == nil {
		return p, nil
	}

	if v, err := r.GetFloat(ctx, tenantID, PolicyMaxDistanceMeters, p.MaxDistanceMeters); err == nil && v > 0 {
		p.MaxDistanceMeters = v
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicyReturnDistanceM, p.ReturnDistanceMeters); err == nil && v > 0 {
		p.ReturnDistanceMeters = v
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicySustainedDuration, p.SustainedDurationSec); err == nil && v > 0 {
		p.SustainedDurationSec = v
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicyMinStreakCount, float64(p.MinStreakCount)); err == nil && v > 0 {
		p.MinStreakCount = int(v)
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicyMaxAccuracyMeters, p.MaxAccuracyMeters); err == nil && v > 0 {
		p.MaxAccuracyMeters = v
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicyMaxSpeedJumpKmh, p.MaxFeasibleSpeedKmh); err == nil && v > 0 {
		p.MaxFeasibleSpeedKmh = v
	}
	if v, err := r.GetFloat(ctx, tenantID, PolicyMinMovingSpeedKmh, p.MinMovingSpeedKmh); err == nil && v >= 0 {
		p.MinMovingSpeedKmh = v
	}
	if v, err := r.GetDurationMinutes(ctx, tenantID, PolicyCooldownMinutes, p.CooldownDuration); err == nil && v > 0 {
		p.CooldownDuration = v
	}

	return p, nil
}
