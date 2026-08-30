package safety

import (
	"context"
	"time"

	"transport-app/internal/fuel"
)

// Policy configuration keys for company_config overrides.
const (
	PolicySpeedLimitKmh    = "safety.speed_limit_kmh"
	PolicyBrakeRate        = "safety.harsh_brake_rate_kmh_s"
	PolicyAccelRate        = "safety.harsh_accel_rate_kmh_s"
	PolicyAccelMaxDTSecs   = "safety.accel_max_dt_seconds"
	PolicyMinSpeedKmh      = "safety.min_speed_kmh"
	PolicyIdleMaxSpeedKmh  = "safety.idle_max_speed_kmh"
	PolicyIdleMinutes      = "safety.idle_minutes"
	PolicyNightStartHour   = "safety.night_start_hour"
	PolicyNightEndHour     = "safety.night_end_hour"
	PolicyNightMinSpeedKmh = "safety.night_min_speed_kmh"
	PolicyNightCooldownMin = "safety.night_cooldown_minutes"
	PolicyHarshCooldownSec = "safety.harsh_cooldown_seconds"
	PolicyGapToleranceMin  = "safety.gap_tolerance_minutes"
	PolicyMaxAccuracyM     = "safety.max_gps_accuracy_meters"
	PolicyMaxSpeedJumpKmh  = "safety.max_feasible_speed_jump_kmh"
)

// Default thresholds for the motion safety engine.
const (
	DefaultSpeedLimitKmh      = 80.0
	DefaultBrakeRate          = -8.0 // km/h per second (~ -2.22 m/s²)
	DefaultAccelRate          = 8.0  // km/h per second (~ 2.22 m/s²)
	DefaultAccelMaxDTSecs     = 10.0
	DefaultMinSpeedKmh        = 20.0
	DefaultIdleMaxSpeedKmh    = 2.0
	DefaultIdleMinutes        = 5.0
	DefaultNightStartHour     = 23.0 // 11 PM
	DefaultNightEndHour       = 5.0  // 5 AM
	DefaultNightMinSpeedKmh   = 10.0
	DefaultNightCooldownMin   = 120.0
	DefaultHarshCooldownSec   = 120.0
	DefaultGapToleranceMin    = 30.0
	DefaultMaxAccuracyMeters  = 50.0  // Reject GPS noise with accuracy > 50m
	DefaultMaxSpeedJumpKmh    = 180.0 // Reject impossible GPS teleportation jumps > 180 km/h
	DefaultTickInterval       = 60 * time.Second
	defaultSnapshotsPerSweep  = 500
	defaultWarmupReplayFrames = 10
)

const (
	ConfigTickInterval     = "safety.tick_interval_seconds"
	ConfigSpeedLimitKmh    = "safety.speed_limit_kmh"
	ConfigBrakeRate        = "safety.harsh_brake_rate_kmh_s"
	ConfigAccelRate        = "safety.harsh_accel_rate_kmh_s"
	ConfigAccelMaxDTSecs   = "safety.accel_max_dt_seconds"
	ConfigMinSpeedKmh      = "safety.min_speed_kmh"
	ConfigIdleMaxSpeedKmh  = "safety.idle_max_speed_kmh"
	ConfigIdleMinutes      = "safety.idle_minutes"
	ConfigNightStartHour   = "safety.night_start_hour"
	ConfigNightEndHour     = "safety.night_end_hour"
	ConfigNightMinSpeedKmh = "safety.night_min_speed_kmh"
	ConfigNightCooldownMin = "safety.night_cooldown_minutes"
	ConfigHarshCooldownSec = "safety.harsh_cooldown_seconds"
	ConfigGapToleranceMin  = "safety.gap_tolerance_minutes"
)

// SafetyPolicy defines the thresholds and defense gates for motion safety detection.
type SafetyPolicy struct {
	TenantID            string
	SpeedLimitKmh       float64
	BrakeRateKmhS       float64
	AccelRateKmhS       float64
	AccelMaxDTSecs      float64
	MinSpeedKmh         float64
	IdleMaxSpeedKmh     float64
	IdleMinutes         float64
	NightStartHour      float64
	NightEndHour        float64
	NightMinSpeedKmh    float64
	NightCooldownMin    float64
	HarshCooldownSec    float64
	GapTolerance        time.Duration
	MaxAccuracyMeters   float64
	MaxFeasibleSpeedKmh float64

	Weights map[string]float64
}

// DefaultSafetyPolicy returns the baseline safety policy.
func DefaultSafetyPolicy(tenantID string) SafetyPolicy {
	if tenantID == "" {
		tenantID = "1"
	}
	return SafetyPolicy{
		TenantID:            tenantID,
		SpeedLimitKmh:       DefaultSpeedLimitKmh,
		BrakeRateKmhS:       DefaultBrakeRate,
		AccelRateKmhS:       DefaultAccelRate,
		AccelMaxDTSecs:      DefaultAccelMaxDTSecs,
		MinSpeedKmh:         DefaultMinSpeedKmh,
		IdleMaxSpeedKmh:     DefaultIdleMaxSpeedKmh,
		IdleMinutes:         DefaultIdleMinutes,
		NightStartHour:      DefaultNightStartHour,
		NightEndHour:        DefaultNightEndHour,
		NightMinSpeedKmh:    DefaultNightMinSpeedKmh,
		NightCooldownMin:    DefaultNightCooldownMin,
		HarshCooldownSec:    DefaultHarshCooldownSec,
		GapTolerance:        DefaultGapToleranceMin * time.Minute,
		MaxAccuracyMeters:   DefaultMaxAccuracyMeters,
		MaxFeasibleSpeedKmh: DefaultMaxSpeedJumpKmh,
		Weights: map[string]float64{
			EventSpeeding:     8.0,
			EventHarshBraking: 6.0,
			EventHarshAccel:   6.0,
			EventIdling:       3.0,
			EventNightDriving: 2.0,
		},
	}
}

// LoadSafetyPolicy loads tenant-specific overrides from company_config.
func LoadSafetyPolicy(ctx context.Context, tenantID string, r *fuel.ConfigReader) (SafetyPolicy, error) {
	p := DefaultSafetyPolicy(tenantID)
	if r == nil {
		return p, nil
	}

	floatKeys := []struct {
		key  string
		def  float64
		dest *float64
	}{
		{PolicySpeedLimitKmh, DefaultSpeedLimitKmh, &p.SpeedLimitKmh},
		{PolicyBrakeRate, DefaultBrakeRate, &p.BrakeRateKmhS},
		{PolicyAccelRate, DefaultAccelRate, &p.AccelRateKmhS},
		{PolicyAccelMaxDTSecs, DefaultAccelMaxDTSecs, &p.AccelMaxDTSecs},
		{PolicyMinSpeedKmh, DefaultMinSpeedKmh, &p.MinSpeedKmh},
		{PolicyIdleMaxSpeedKmh, DefaultIdleMaxSpeedKmh, &p.IdleMaxSpeedKmh},
		{PolicyIdleMinutes, DefaultIdleMinutes, &p.IdleMinutes},
		{PolicyNightStartHour, DefaultNightStartHour, &p.NightStartHour},
		{PolicyNightEndHour, DefaultNightEndHour, &p.NightEndHour},
		{PolicyNightMinSpeedKmh, DefaultNightMinSpeedKmh, &p.NightMinSpeedKmh},
		{PolicyNightCooldownMin, DefaultNightCooldownMin, &p.NightCooldownMin},
		{PolicyHarshCooldownSec, DefaultHarshCooldownSec, &p.HarshCooldownSec},
		{PolicyMaxAccuracyM, DefaultMaxAccuracyMeters, &p.MaxAccuracyMeters},
		{PolicyMaxSpeedJumpKmh, DefaultMaxSpeedJumpKmh, &p.MaxFeasibleSpeedKmh},
	}

	for _, fk := range floatKeys {
		v, err := r.GetFloat(ctx, tenantID, fk.key, fk.def)
		if err != nil {
			return p, err
		}
		*fk.dest = v
	}

	if v, err := r.GetFloat(ctx, tenantID, PolicyGapToleranceMin, DefaultGapToleranceMin); err == nil && v > 0 {
		p.GapTolerance = time.Duration(v * float64(time.Minute))
	}

	for evType := range p.Weights {
		if v, err := r.GetFloat(ctx, tenantID, fuel.ConfigScorecardWeight+evType, p.Weights[evType]); err == nil && v > 0 {
			p.Weights[evType] = v
		}
	}

	return p, nil
}
