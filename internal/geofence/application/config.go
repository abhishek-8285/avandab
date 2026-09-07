package application

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"
)

// company_config keys used by the geofence engine (Spec 02 §3, §5, §6).
const (
	ConfigDwellDebounceSeconds = "geofence.dwell_debounce_seconds"
	ConfigBufferMetres         = "geofence.buffer_metres"
	ConfigHysteresisMetres     = "geofence.hysteresis_metres"
	ConfigPollIntervalSeconds  = "geofence.poll_interval_seconds"
	ConfigAutoReachPickup      = "geofence.auto_reach_pickup"
	ConfigAutoStartTransit     = "geofence.auto_start_transit"
	ConfigDetentionFreeSeconds = "geofence.detention_free_seconds"
	ConfigDetentionRatePerHour = "geofence.detention_rate_per_hour"
)

// Defaults used when company_config has no row for a key.
const (
	DefaultDwellDebounce    = 60 * time.Second
	DefaultBufferMetres     = 20.0
	DefaultHysteresisMetres = 25.0
	DefaultPollInterval     = 10 * time.Second
	DefaultDetentionFree    = 30 * time.Minute
	DefaultDetentionRate    = 0.0
)

// cacheTTL is how long the in-memory cache stays valid before a DB refresh.
const cacheTTL = 30 * time.Second

// ConfigReader reads company_config with a short-lived in-memory cache.
// Writes go through the same table (1G admin UI); the TTL bounds staleness.
type ConfigReader struct {
	db      *sql.DB
	mu      sync.RWMutex
	cache   map[string]string // key -> value
	cacheAt time.Time
	now     func() time.Time
}

// NewConfigReader constructs a ConfigReader.
func NewConfigReader(db *sql.DB) *ConfigReader {
	return &ConfigReader{
		db:    db,
		cache: make(map[string]string),
		now:   time.Now,
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
	return c.cache[key], nil
}

// GetDurationSeconds parses a key as seconds, falling back to def.
func (c *ConfigReader) GetDurationSeconds(ctx context.Context, tenantID, key string, def time.Duration) (time.Duration, error) {
	raw, err := c.Get(ctx, tenantID, key)
	if err != nil || raw == "" {
		return def, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def, nil
	}
	return time.Duration(n) * time.Second, nil
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

// GetBool parses a key as a boolean (1/true/yes), falling back to def.
func (c *ConfigReader) GetBool(ctx context.Context, tenantID, key string, def bool) (bool, error) {
	raw, err := c.Get(ctx, tenantID, key)
	if err != nil || raw == "" {
		return def, err
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		// Accept "1"/"0" as a lenient parse (common in form-driven config).
		switch raw {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
		return def, nil
	}
	return b, nil
}

// refreshIfStale reloads the tenant's config rows when the cache is older
// than cacheTTL.
func (c *ConfigReader) refreshIfStale(ctx context.Context, tenantID string) error {
	c.mu.RLock()
	stale := c.cacheAt.IsZero() || c.now().Sub(c.cacheAt) > cacheTTL
	c.mu.RUnlock()
	if !stale {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check under the write lock (another goroutine may have refreshed).
	if !c.cacheAt.IsZero() && c.now().Sub(c.cacheAt) <= cacheTTL {
		return nil
	}

	rows, err := c.db.QueryContext(ctx,
		`SELECT key, value FROM company_config WHERE tenant_id = $1`, tenantID)
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
	c.cache = fresh
	c.cacheAt = c.now()
	return nil
}
