// Package fuel implements the fuel anomaly engine (Spec 03 §2): a single
// background loop that polls telemetry_snapshots, smooths fuel_level with a
// median window and detects refills, theft, siphon, abnormal drain and
// odometer rollbacks. It writes durable rows (fuel_events,
// driver_behaviour_events) and emits AlertEvents via the outbox inside the
// same UnitOfWork.
//
// SINGLE-INSTANCE CONSTRAINT: per-vehicle state lives in an in-memory map and
// is rebuilt at startup by replaying the last `fuel.median_window` snapshots
// per vehicle. This matches the current single-binary deployment model
// (Spec 03 §2.1, §13 item 13) — running two engine instances on the same
// database would double-process snapshots.
package fuel

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"
)

// company_config keys used by the fuel engine (Spec 03 §9).
const (
	ConfigMedianWindow         = "fuel.median_window"
	ConfigSpikeDeviationPct    = "fuel.spike_deviation_pct"
	ConfigNoiseFloorPct        = "fuel.noise_floor_pct"
	ConfigRefillThresholdL     = "fuel.refill_threshold_litres"
	ConfigTheftDropThresholdL  = "fuel.theft_drop_threshold_litres"
	ConfigSiphonDropThresholdL = "fuel.siphon_drop_threshold_litres"
	ConfigSiphonStopMinutes    = "fuel.siphon_stop_minutes"
	ConfigStopSpeedKmh         = "fuel.stop_speed_kmh"
	ConfigOdometerToleranceKm  = "fuel.odometer_tolerance_km"
	ConfigAbnormalDrainLPerKm  = "fuel.abnormal_drain_l_per_km"
	ConfigAbnormalDrainMargin  = "fuel.abnormal_drain_margin_pct"
	ConfigLevelUnit            = "fuel.level_unit"
	ConfigTickIntervalSeconds  = "fuel.tick_interval_seconds"
	ConfigGapToleranceMinutes  = "fuel.gap_tolerance_minutes"
	ConfigClaimTolerancePct    = "fuel.claim_tolerance_pct"
	ConfigClaimCrosscheckPct   = "fuel.claim_crosscheck_margin_pct"
	ConfigKmplDefault          = "fuel.kmpl_default"
	ConfigAuditEnforce         = "fuel.audit_enforce"
	ConfigWeightTheft          = "scorecard.weight.fuel_theft_suspicion"
	ConfigWeightOdoRollback    = "scorecard.weight.odometer_rollback"

	// Scorecard knobs (Spec 03 §4.2, §9). Read via the same ConfigReader;
	// weights are denormalized into driver_behaviour_events at write time.
	ConfigScorecardWindowDays = "scorecard.window_days"
	ConfigScorecardTierA      = "scorecard.tier_a"
	ConfigScorecardTierB      = "scorecard.tier_b"
	ConfigScorecardMinEvents  = "scorecard.min_events"
	ConfigScorecardFraudCap   = "scorecard.fraud_cap"
	ConfigScorecardFraudCapOn = "scorecard.fraud_cap_enabled"
	ConfigScorecardWeight     = "scorecard.weight."
	ConfigScorecardBonusA     = "scorecard.bonus_a_pct"
	ConfigScorecardBonusB     = "scorecard.bonus_b_pct"
	ConfigScorecardBonusC     = "scorecard.bonus_c_pct"
)

// Defaults used when company_config has no row for a key.
const (
	DefaultMedianWindow         = 7
	DefaultSpikeDeviationPct    = 25.0
	DefaultNoiseFloorPct        = 1.5
	DefaultRefillThresholdL     = 20.0
	DefaultTheftDropThresholdL  = 10.0
	DefaultSiphonDropThresholdL = 15.0
	DefaultSiphonStopMinutes    = 20 * time.Minute
	DefaultStopSpeedKmh         = 5.0
	DefaultOdometerToleranceKm  = 1.0
	DefaultAbnormalDrainLPerKm  = 0.6
	DefaultAbnormalDrainMargin  = 30.0
	DefaultTickInterval         = 30 * time.Second
	DefaultGapTolerance         = 30 * time.Minute
	DefaultClaimTolerancePct    = 20.0
	DefaultClaimCrosscheckPct   = 25.0
	DefaultKmpl                 = 4.0
	DefaultAuditEnforce         = false
	DefaultWeightTheft          = 25.0
	DefaultWeightOdoRollback    = 20.0

	DefaultScorecardWindowDays = 30
	DefaultScorecardTierA      = 85.0
	DefaultScorecardTierB      = 70.0
	DefaultScorecardMinEvents  = 3
	DefaultScorecardFraudCap   = 69.0
	DefaultScorecardBonusA     = 5.0
	DefaultScorecardBonusB     = 2.0
	DefaultScorecardBonusC     = 0.0
)

// cacheTTL bounds how long company_config values stay valid in memory.
const cacheTTL = 60 * time.Second

// ConfigReader reads company_config with a short-lived in-memory cache
// (Spec 03 §9 — plain SQL, no sqlc regen).
type ConfigReader struct {
	db      *sql.DB
	mu      sync.RWMutex
	cache   map[string]map[string]string // tenantID -> (key -> value)
	cacheAt map[string]time.Time         // tenantID -> timestamp
	now     func() time.Time
}

// NewConfigReader constructs a ConfigReader.
func NewConfigReader(db *sql.DB) *ConfigReader {
	return &ConfigReader{
		db:      db,
		cache:   make(map[string]map[string]string),
		cacheAt: make(map[string]time.Time),
		now:     time.Now,
	}
}

// Get returns the raw string value for a key. Returns an empty string when
// the key is not configured.
func (c *ConfigReader) Get(ctx context.Context, tenantID, key string) (string, error) {
	if err := c.refreshIfStale(ctx, tenantID); err != nil {
		return "", err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if tCache, ok := c.cache[tenantID]; ok {
		return tCache[key], nil
	}
	return "", nil
}

// GetFloat parses a key as a float, falling back to def.
func (c *ConfigReader) GetFloat(ctx context.Context, tenantID, key string, def float64) (float64, error) {
	raw, err := c.Get(ctx, tenantID, key)
	if err != nil || raw == "" {
		return def, err
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def, nil
	}
	return n, nil
}

// GetDurationSeconds parses a key as seconds, falling back to def.
func (c *ConfigReader) GetDurationSeconds(ctx context.Context, tenantID, key string, def time.Duration) (time.Duration, error) {
	return c.getDuration(ctx, tenantID, key, def, time.Second)
}

// GetDurationMinutes parses a key as minutes, falling back to def.
func (c *ConfigReader) GetDurationMinutes(ctx context.Context, tenantID, key string, def time.Duration) (time.Duration, error) {
	return c.getDuration(ctx, tenantID, key, def, time.Minute)
}

func (c *ConfigReader) getDuration(ctx context.Context, tenantID, key string, def time.Duration, unit time.Duration) (time.Duration, error) {
	raw, err := c.Get(ctx, tenantID, key)
	if err != nil || raw == "" {
		return def, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def, nil
	}
	return time.Duration(n) * unit, nil
}

// refreshIfStale reloads the tenant's config rows when the cache is older
// than cacheTTL.
func (c *ConfigReader) refreshIfStale(ctx context.Context, tenantID string) error {
	c.mu.RLock()
	lastAt, exists := c.cacheAt[tenantID]
	stale := !exists || lastAt.IsZero() || c.now().Sub(lastAt) > cacheTTL
	c.mu.RUnlock()
	if !stale {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	lastAt, exists = c.cacheAt[tenantID]
	if exists && !lastAt.IsZero() && c.now().Sub(lastAt) <= cacheTTL {
		return nil
	}

	rows, err := c.db.QueryContext(ctx,
		`SELECT key, value FROM company_config WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return err
	}
	defer rows.Close()

	fresh := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		fresh[k] = v
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.cache[tenantID] = fresh
	c.cacheAt[tenantID] = c.now()
	return nil
}
