// Package features is the per-org feature-flag registry. Every product feature
// registers here (one Catalog entry); orgs (tenants) can be granted or revoked
// features at runtime via the feature_flags table. Core features are on for
// every org; addon features are off until granted — the pricing surface.
//
// Resolution order for Enabled(org, key):
//  1. feature_flags row for the org (explicit grant/revoke) — always wins
//  2. env flag when set (true enables process-wide, false disables) — the
//     operator's monetization lever: unset-or-false + per-org grants
//  3. EnvDefaultOn when the env flag is unset (mirrors internal/config
//     defaults so existing deployments keep working)
//  4. catalog tier default (core → on, addon → off)
package features

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Tier classifies a feature commercially.
type Tier string

const (
	TierCore  Tier = "core"  // included for every org
	TierAddon Tier = "addon" // premium: off until granted per org
)

// Feature is one catalog entry. Adding a product feature = adding a line here
// plus gating its routes/workers with the Registry.
type Feature struct {
	Key          string // stable slug, e.g. "telemetry"
	Name         string
	Description  string
	Category     string // grouping in the admin UI
	Tier         Tier
	EnvFlag      string // optional process-wide switch, e.g. "TELEMETRY_ENABLED"
	EnvDefaultOn bool   // value when EnvFlag is unset; mirrors internal/config defaults
}

// Catalog is the single registry of flaggable features. Order = UI order.
var Catalog = []Feature{
	// Operations
	{Key: "telemetry", Name: "GPS Telemetry & Live Tracking", Category: "Operations", Tier: TierAddon,
		Description: "Device registry, MQTT/REST ingestion, live map, geofences, fuel-sensor analytics.", EnvFlag: "TELEMETRY_ENABLED", EnvDefaultOn: true},
	{Key: "geofences", Name: "Geofences & Detention Billing", Category: "Operations", Tier: TierAddon,
		Description: "Zone dwell detection, breach alerts, detention charge lines.", EnvFlag: "TELEMETRY_ENABLED", EnvDefaultOn: true},
	{Key: "fuel_audit", Name: "Fuel Audit & KMPL Reports", Category: "Operations", Tier: TierAddon,
		Description: "Refill/theft/siphon detection, KMPL efficiency reports.", EnvFlag: "TELEMETRY_ENABLED", EnvDefaultOn: true},
	{Key: "share_links", Name: "Public Trip Share Links", Category: "Operations", Tier: TierCore,
		Description: "PIN-protected public tracking pages for customers."},
	{Key: "customer_portal", Name: "Customer Portal", Category: "Operations", Tier: TierCore,
		Description: "Self-service shipment, invoice and tracking portal for customers."},

	// Commerce & Finance
	{Key: "fastag", Name: "FASTag Toll Reconciliation", Category: "Commerce & Finance", Tier: TierAddon,
		Description: "Tag wallets, toll-transaction reconciliation, auto-kharcha.", EnvFlag: "INTEGRATION_FASTAG_ENABLED"},
	{Key: "ewaybill", Name: "GST e-Way Bills", Category: "Commerce & Finance", Tier: TierCore,
		Description: "Part-A/Part-B lifecycle, expiry monitor, ₹50k auto-generate.", EnvFlag: "INTEGRATION_EWAYBILL_ENABLED"},
	{Key: "gst_einvoice", Name: "GST e-Invoice (IRN)", Category: "Commerce & Finance", Tier: TierAddon,
		Description: "IRN generation, signed QR, per-line CGST/SGST/IGST.", EnvFlag: "INTEGRATION_GSTN_ENABLED"},
	{Key: "accounting_sync", Name: "Accounting Sync (Tally/Zoho)", Category: "Commerce & Finance", Tier: TierAddon,
		Description: "Invoice/settlement export to accounting systems.", EnvFlag: "INTEGRATION_ACCOUNTING_ENABLED"},
	{Key: "razorpay", Name: "Online Payments (Razorpay)", Category: "Commerce & Finance", Tier: TierCore,
		Description: "Checkout, webhooks, refunds."},
	{Key: "settlements", Name: "Driver Settlements & TDS", Category: "Commerce & Finance", Tier: TierCore,
		Description: "Per-km/fixed/commission payouts with 194C TDS."},

	// Insights
	{Key: "scorecard", Name: "Driver Safety Scorecard", Category: "Insights", Tier: TierAddon,
		Description: "Weighted behaviour scoring, tiers A/B/C, settlement bonus hooks.", EnvFlag: "TELEMETRY_ENABLED", EnvDefaultOn: true},
	{Key: "pnl", Name: "Trip P&L Engine", Category: "Insights", Tier: TierAddon,
		Description: "Per-trip margin with fuel-cost modelling and snapshots."},
	{Key: "experiments", Name: "A/B Experiments", Category: "Insights", Tier: TierCore,
		Description: "Bucketed variants with sticky assignment and conversion tracking."},

	// Command Center (Spec 22)
	{Key: "command_center", Name: "Owner Command Center", Category: "Command Center", Tier: TierAddon,
		Description: "One-screen console: money strip, fleet context, ranked alerts.", EnvFlag: "COMMAND_CENTER_ENABLED"},
	{Key: "bookings_board", Name: "Bookings Kanban Board", Category: "Command Center", Tier: TierAddon,
		Description: "Drag-and-drop booking status board with live sync.", EnvFlag: "BOOKINGS_BOARD_ENABLED"},
	{Key: "driver_money", Name: "Driver Paisa Tab", Category: "Command Center", Tier: TierCore,
		Description: "Driver balance transparency, settlements history, advance requests.", EnvFlag: "DRIVER_MONEY_ENABLED", EnvDefaultOn: true},
	{Key: "alert_inbox", Name: "Ranked Alert Inbox", Category: "Command Center", Tier: TierAddon,
		Description: "Severity-ranked alert inbox with ack/snooze on the console.", EnvFlag: "ALERT_INBOX_ENABLED"},

	// Intelligence
	{Key: "agent", Name: "AI Ops Assistant", Category: "Intelligence", Tier: TierAddon,
		Description: "Conversational ops agent with approval-gated actions.", EnvFlag: "AGENT_ENABLED"},
	{Key: "rag", Name: "Knowledge Search (RAG)", Category: "Intelligence", Tier: TierAddon,
		Description: "Document embeddings and semantic knowledge search.", EnvFlag: "RAG_ENABLED"},
	{Key: "founder", Name: "Founder Signals & Digest", Category: "Intelligence", Tier: TierCore,
		Description: "Multi-channel founder alerts and daily digest."},

	// Platform
	{Key: "pwa", Name: "Progressive Web App", Category: "Platform", Tier: TierCore,
		Description: "Installable web app with offline shell.", EnvFlag: "PWA_ENABLED"},
}

func ByKey(key string) (Feature, bool) {
	for _, f := range Catalog {
		if f.Key == key {
			return f, true
		}
	}
	return Feature{}, false
}

// SnapshotEntry is what handlers/templates see for one feature in one org.
type SnapshotEntry struct {
	Feature
	Enabled bool
}

// Registry answers Enabled queries with a per-org DB cache (60s TTL) so route
// gating costs nothing per request.
type Registry struct {
	db        *sql.DB
	envLookup func(string) string // indirection for tests; nil = os.Getenv

	mu    sync.Mutex
	cache map[string]cachedOrg // tenantID → flags
}

type cachedOrg struct {
	flags   map[string]bool // feature → explicitly set value
	fetched time.Time
}

// NewRegistry builds a registry. envLookup defaults to os.Getenv.
func NewRegistry(db *sql.DB, envLookup func(string) string) *Registry {
	if envLookup == nil {
		envLookup = osGetenv
	}
	return &Registry{db: db, envLookup: envLookup, cache: map[string]cachedOrg{}}
}

const cacheTTL = 60 * time.Second

// Enabled resolves a feature for one org. Unknown keys are off.
func (reg *Registry) Enabled(ctx context.Context, tenantID, key string) bool {
	f, ok := ByKey(key)
	if !ok {
		return false
	}
	flags := reg.flagsFor(ctx, tenantID)
	if v, explicit := flags[key]; explicit {
		return v
	}
	if v, decided := envState(reg.envLookup, f); decided {
		return v
	}
	return f.Tier == TierCore
}

// envState resolves the process-wide switch for a feature: (value, decided).
func envState(lookup func(string) string, f Feature) (bool, bool) {
	if f.EnvFlag == "" {
		return false, false
	}
	switch lookup(f.EnvFlag) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	if f.EnvDefaultOn {
		return true, true
	}
	return false, false
}

// Snapshot returns the full catalog resolved for one org (admin UI + layout).
func (reg *Registry) Snapshot(ctx context.Context, tenantID string) []SnapshotEntry {
	flags := reg.flagsFor(ctx, tenantID)
	out := make([]SnapshotEntry, 0, len(Catalog))
	for _, f := range Catalog {
		e := SnapshotEntry{Feature: f}
		if v, explicit := flags[f.Key]; explicit {
			e.Enabled = v
		} else if v, decided := envState(reg.envLookup, f); decided {
			e.Enabled = v
		} else {
			e.Enabled = f.Tier == TierCore
		}
		out = append(out, e)
	}
	return out
}

// Set grants/revokes a feature for one org (audit-logged by the caller).
// Empty updatedBy is allowed for migrations/seeds.
func (reg *Registry) Set(ctx context.Context, tenantID, key string, enabled bool, updatedBy string) error {
	if _, ok := ByKey(key); !ok {
		return ErrUnknownFeature
	}
	_, err := reg.db.ExecContext(ctx, `
INSERT INTO feature_flags (tenant_id, feature, enabled, updated_by, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(tenant_id, feature) DO UPDATE SET
	enabled = excluded.enabled, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		tenantID, key, enabled, updatedBy, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	reg.invalidate(tenantID)
	return nil
}

func (reg *Registry) invalidate(tenantID string) {
	reg.mu.Lock()
	delete(reg.cache, tenantID)
	reg.mu.Unlock()
}

func (reg *Registry) flagsFor(ctx context.Context, tenantID string) map[string]bool {
	reg.mu.Lock()
	c, ok := reg.cache[tenantID]
	if ok && time.Since(c.fetched) < (cacheTTL) {
		reg.mu.Unlock()
		return c.flags
	}
	reg.mu.Unlock()

	flags := map[string]bool{}
	if reg.db != nil {
		rows, err := reg.db.QueryContext(ctx,
			`SELECT feature, enabled FROM feature_flags WHERE tenant_id = ?`, tenantID)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var k string
				var v bool
				if rows.Scan(&k, &v) == nil {
					flags[k] = v
				}
			}
		}
	}

	reg.mu.Lock()
	reg.cache[tenantID] = cachedOrg{flags: flags, fetched: time.Now()}
	reg.mu.Unlock()
	return flags
}
