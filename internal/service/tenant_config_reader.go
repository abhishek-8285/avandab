package service

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"
)

// Per-tenant config keys carried by the settings overlay (Spec 24 §Business
// logic overlay). Verified collision-free against the fuel/scorecard/fastag/
// settlement/gst namespaces seeded by migrations 00043/00048/00049/00050/00051.
const (
	ConfigKeyBookingPrefix = "branding.booking_prefix"
	ConfigKeyTripPrefix    = "branding.trip_prefix"
	ConfigKeyInvoicePrefix = "branding.invoice_prefix"
	ConfigKeyGSTEnabled    = "billing.gst_enabled"
	ConfigKeyGSTRate       = "billing.gst_rate"
	ConfigKeyStateCode     = "billing.state_code"
)

// defaultTenantConfigTTL bounds how long a tenant's company_config snapshot
// stays fresh — same cadence as the geofence ConfigReader (30s).
const defaultTenantConfigTTL = 30 * time.Second

// tenantConfigSnapshot is one tenant's whole key/value slice of company_config
// captured at a single instant. Sharding the cache per tenant (unlike the
// geofence/fuel readers which hold ONE snapshot for whatever tenant refreshed
// last) makes concurrent multi-tenant reads independent and race-free.
type tenantConfigSnapshot struct {
	values map[string]string
	at     time.Time
}

// TenantConfigReader reads per-tenant KV overrides from company_config with a
// global-default fallback chain, sharded per tenant (safe under concurrency):
// tenant row → caller-supplied global default (from company_settings).
//
// Reads are served from a whole-tenant snapshot refreshed on miss/stale with a
// single `SELECT key,value FROM company_config WHERE tenant_id=?` (migration
// 00042 owns the table; seeds elsewhere are tenant '1' only, so every other
// tenant legitimately falls back). Plain SQL, no sqlc regen — matches the
// geofence/fuel ConfigReader precedent.
type TenantConfigReader struct {
	db    *sql.DB
	mu    sync.RWMutex
	ttl   time.Duration
	now   func() time.Time
	cache map[string]tenantConfigSnapshot // tenantID -> snapshot
}

// NewTenantConfigReader constructs a reader over the raw database. A nil DB
// yields a permanently-missing reader: all lookups miss and callers fall
// through to legacy behavior.
func NewTenantConfigReader(db *sql.DB) *TenantConfigReader {
	return &TenantConfigReader{
		db:    db,
		ttl:   defaultTenantConfigTTL,
		now:   time.Now,
		cache: make(map[string]tenantConfigSnapshot),
	}
}

// Get returns the raw stored value for (tenantID, key) and whether an
// explicit row exists. An empty cached snapshot still counts as loaded —
// absence of rows IS the tenant's config until Invalidate/TTL.
func (r *TenantConfigReader) Get(ctx context.Context, tenantID, key string) (string, bool) {
	if r == nil || r.db == nil || tenantID == "" || key == "" {
		return "", false
	}

	r.mu.RLock()
	snap, ok := r.cache[tenantID]
	fresh := ok && !snap.at.IsZero() && r.now().Sub(snap.at) <= r.ttl
	var (
		val   string
		found bool
	)
	if fresh {
		val, found = snap.values[key]
	}
	r.mu.RUnlock()
	if fresh {
		return val, found
	}

	freshSnap, err := r.refresh(ctx, tenantID)
	if err != nil {
		// Degrade gracefully: serve the stale snapshot rather than lose the
		// tenant's overrides because of a transient DB hiccup.
		r.mu.RLock()
		stale, ok := r.cache[tenantID]
		r.mu.RUnlock()
		if ok {
			val, found = stale.values[key]
			return val, found
		}
		return "", false
	}
	val, found = freshSnap.values[key]
	return val, found
}

// GetString returns the tenant's value or def when missing/blank.
func (r *TenantConfigReader) GetString(ctx context.Context, tenantID, key, def string) string {
	v, ok := r.Get(ctx, tenantID, key)
	if !ok || v == "" {
		return def
	}
	return v
}

// GetBool parses the tenant's value as a boolean, falling back to def when
// missing or unparseable. Accepts "1"/"0" leniently (form-driven config),
// matching the geofence/fuel readers.
func (r *TenantConfigReader) GetBool(ctx context.Context, tenantID, key string, def bool) bool {
	v, ok := r.Get(ctx, tenantID, key)
	if !ok || v == "" {
		return def
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	switch v {
	case "1":
		return true
	case "0":
		return false
	default:
		return def
	}
}

// GetFloat parses the tenant's value as a float, falling back to def when
// missing or unparseable.
func (r *TenantConfigReader) GetFloat(ctx context.Context, tenantID, key string, def float64) float64 {
	v, ok := r.Get(ctx, tenantID, key)
	if !ok || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// Overlay resolves key for tenantID: the tenant's row wins, otherwise the
// supplied globalDefault (typically derived from the company_settings
// singleton by the caller) flows through unchanged.
func (r *TenantConfigReader) Overlay(ctx context.Context, tenantID, key, globalDefault string) string {
	return r.GetString(ctx, tenantID, key, globalDefault)
}

// Invalidate drops the tenant's cached snapshot (future admin UI hook).
func (r *TenantConfigReader) Invalidate(tenantID string) {
	if r == nil || tenantID == "" {
		return
	}
	r.mu.Lock()
	delete(r.cache, tenantID)
	r.mu.Unlock()
}

// refresh bulk-loads the tenant's whole company_config slice into a fresh
// snapshot, double-checked under the write lock (copy of the geofence
// pattern, keyed per tenant instead of process-global).
func (r *TenantConfigReader) refresh(ctx context.Context, tenantID string) (tenantConfigSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Another goroutine may have refreshed while we waited on the lock.
	if snap, ok := r.cache[tenantID]; ok && !snap.at.IsZero() && r.now().Sub(snap.at) <= r.ttl {
		return snap, nil
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM company_config WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return tenantConfigSnapshot{}, err
	}
	defer func() { _ = rows.Close() }()

	fresh := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return tenantConfigSnapshot{}, err
		}
		fresh[k] = v
	}
	if err := rows.Err(); err != nil {
		return tenantConfigSnapshot{}, err
	}

	snap := tenantConfigSnapshot{values: fresh, at: r.now()}
	r.cache[tenantID] = snap
	return snap, nil
}

// tenantPrefix resolves a branding.* display-ID prefix through the per-tenant
// overlay: tenant row wins, else the supplied company_settings-derived
// default flows through. Nil-safe — services built without a raw DB keep
// legacy single-tenant behavior untouched.
func (s *baseService) tenantPrefix(ctx context.Context, key, fallback string) string {
	if s.tenantCfg == nil {
		return fallback
	}
	return s.tenantCfg.Overlay(ctx, tenantIDFor(ctx), key, fallback)
}

// overlayBool resolves a billing.* flag for the acting tenant over the
// company_settings-derived default. Nil-safe.
func (s *baseService) overlayBool(ctx context.Context, key string, def bool) bool {
	if s.tenantCfg == nil {
		return def
	}
	return s.tenantCfg.GetBool(ctx, tenantIDFor(ctx), key, def)
}

// overlayFloat resolves a billing.* rate for the acting tenant over the
// company_settings-derived default. Nil-safe.
func (s *baseService) overlayFloat(ctx context.Context, key string, def float64) float64 {
	if s.tenantCfg == nil {
		return def
	}
	return s.tenantCfg.GetFloat(ctx, tenantIDFor(ctx), key, def)
}
