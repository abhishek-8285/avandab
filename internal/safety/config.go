package safety

import (
	"context"
	"time"

	"transport-app/internal/fuel"
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

const (
	DefaultTickInterval       = 60 * time.Second
	DefaultSpeedLimitKmh      = 80.0
	DefaultBrakeRate          = -8.0
	DefaultAccelRate          = 8.0
	DefaultAccelMaxDTSecs     = 10.0
	DefaultMinSpeedKmh        = 20.0
	DefaultIdleMaxSpeedKmh    = 2.0
	DefaultIdleMinutes        = 5.0
	DefaultNightStartHour     = 23.0
	DefaultNightEndHour       = 5.0
	DefaultNightMinSpeedKmh   = 10.0
	DefaultNightCooldownMin   = 120.0
	DefaultHarshCooldownSec   = 120.0
	DefaultGapToleranceMin    = 30.0
	defaultSnapshotsPerSweep  = 500
	defaultWarmupReplayFrames = 10
)

type engineConfig struct {
	tickInterval     time.Duration
	speedLimitKmh    float64
	brakeRateKmhS    float64
	accelRateKmhS    float64
	accelMaxDTSecs   float64
	minSpeedKmh      float64
	idleMaxSpeedKmh  float64
	idleMinutes      float64
	nightStartHour   float64
	nightEndHour     float64
	nightMinSpeedKmh float64
	nightCooldownMin float64
	harshCooldownSec float64
	gapTolerance     time.Duration

	weightSpeeding     float64
	weightHarshBraking float64
	weightHarshAccel   float64
	weightIdling       float64
	weightNightDriving float64
}

func defaultWeights() map[string]float64 {
	return map[string]float64{
		EventSpeeding:     8,
		EventHarshBraking: 6,
		EventHarshAccel:   6,
		EventIdling:       3,
		EventNightDriving: 2,
	}
}

func (e *Engine) loadConfig(ctx context.Context) (engineConfig, error) {
	t := string(e.tenantID)
	r := e.config
	cfg := engineConfig{
		tickInterval:     DefaultTickInterval,
		speedLimitKmh:    DefaultSpeedLimitKmh,
		brakeRateKmhS:    DefaultBrakeRate,
		accelRateKmhS:    DefaultAccelRate,
		accelMaxDTSecs:   DefaultAccelMaxDTSecs,
		minSpeedKmh:      DefaultMinSpeedKmh,
		idleMaxSpeedKmh:  DefaultIdleMaxSpeedKmh,
		idleMinutes:      DefaultIdleMinutes,
		nightStartHour:   DefaultNightStartHour,
		nightEndHour:     DefaultNightEndHour,
		nightMinSpeedKmh: DefaultNightMinSpeedKmh,
		nightCooldownMin: DefaultNightCooldownMin,
		harshCooldownSec: DefaultHarshCooldownSec,
		gapTolerance:     DefaultGapToleranceMin * time.Minute,

		weightSpeeding:     defaultWeights()["speeding"],
		weightHarshBraking: defaultWeights()["harsh_braking"],
		weightHarshAccel:   defaultWeights()["harsh_accel"],
		weightIdling:       defaultWeights()["idling"],
		weightNightDriving: defaultWeights()["night_driving"],
	}
	var err error
	if cfg.tickInterval, err = r.GetDurationSeconds(ctx, t, ConfigTickInterval, DefaultTickInterval); err != nil {
		return cfg, err
	}
	floatCfg := []struct {
		key  string
		def  float64
		dest *float64
	}{
		{ConfigSpeedLimitKmh, DefaultSpeedLimitKmh, &cfg.speedLimitKmh},
		{ConfigBrakeRate, DefaultBrakeRate, &cfg.brakeRateKmhS},
		{ConfigAccelRate, DefaultAccelRate, &cfg.accelRateKmhS},
		{ConfigAccelMaxDTSecs, DefaultAccelMaxDTSecs, &cfg.accelMaxDTSecs},
		{ConfigMinSpeedKmh, DefaultMinSpeedKmh, &cfg.minSpeedKmh},
		{ConfigIdleMaxSpeedKmh, DefaultIdleMaxSpeedKmh, &cfg.idleMaxSpeedKmh},
		{ConfigIdleMinutes, DefaultIdleMinutes, &cfg.idleMinutes},
		{ConfigNightStartHour, DefaultNightStartHour, &cfg.nightStartHour},
		{ConfigNightEndHour, DefaultNightEndHour, &cfg.nightEndHour},
		{ConfigNightMinSpeedKmh, DefaultNightMinSpeedKmh, &cfg.nightMinSpeedKmh},
		{ConfigNightCooldownMin, DefaultNightCooldownMin, &cfg.nightCooldownMin},
		{ConfigHarshCooldownSec, DefaultHarshCooldownSec, &cfg.harshCooldownSec},
	}
	for _, fc := range floatCfg {
		v, ferr := r.GetFloat(ctx, t, fc.key, fc.def)
		if ferr != nil {
			return cfg, ferr
		}
		*fc.dest = v
	}
	if v, ferr := r.GetFloat(ctx, t, ConfigGapToleranceMin, DefaultGapToleranceMin); ferr == nil && v > 0 {
		cfg.gapTolerance = time.Duration(v * float64(time.Minute))
	}
	if v, ferr := r.GetFloat(ctx, t, fuel.ConfigScorecardWeight+"speeding", cfg.weightSpeeding); ferr == nil {
		cfg.weightSpeeding = v
	}
	if v, ferr := r.GetFloat(ctx, t, fuel.ConfigScorecardWeight+"harsh_braking", cfg.weightHarshBraking); ferr == nil {
		cfg.weightHarshBraking = v
	}
	if v, ferr := r.GetFloat(ctx, t, fuel.ConfigScorecardWeight+"harsh_accel", cfg.weightHarshAccel); ferr == nil {
		cfg.weightHarshAccel = v
	}
	if v, ferr := r.GetFloat(ctx, t, fuel.ConfigScorecardWeight+"idling", cfg.weightIdling); ferr == nil {
		cfg.weightIdling = v
	}
	if v, ferr := r.GetFloat(ctx, t, fuel.ConfigScorecardWeight+"night_driving", cfg.weightNightDriving); ferr == nil {
		cfg.weightNightDriving = v
	}
	return cfg, nil
}
