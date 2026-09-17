package accounting

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"transport-app/internal/shared"
)

// Per-tenant accounting provider choice (00161). Reads resolve tenant row
// first, global/env config fallback. Live push only for tally; zoho /
// busy_excel / excel stay CSV-import workflow (adapters mock-only).

// AllowedProviders is the user-facing allowlist for tenant choice.
var AllowedProviders = []string{"none", "tally", "zoho", "busy_excel", "excel"}

// Setting is one tenant's accounting choice.
type Setting struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	// LivePush reports whether the provider pushes live (tally only).
	LivePush bool `json:"live_push"`
}

// ValidProvider reports whether p is a selectable provider.
func ValidProvider(p string) bool {
	switch normalizeProvider(p) {
	case "none", "tally", "zoho", "busy_excel", "excel":
		return true
	}
	return false
}

func normalizeProvider(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}

// livePushProviders are the only adapters with a real live implementation.
func livePushProvider(p string) bool {
	return normalizeProvider(p) == "tally"
}

// GetSetting returns the tenant's saved choice. Missing row → zero Setting
// (caller falls back to global config). Never errors on absence.
func GetSetting(ctx context.Context, db *sql.DB) Setting {
	var s Setting
	tid, err := shared.TenantRequired(ctx)
	if err != nil || db == nil {
		return s
	}
	_ = db.QueryRowContext(ctx,
		`SELECT provider, endpoint FROM tenant_accounting_settings WHERE tenant_id = $1`,
		string(tid)).Scan(&s.Provider, &s.Endpoint)
	s.Provider = normalizeProvider(s.Provider)
	s.LivePush = livePushProvider(s.Provider)
	return s
}

// SaveSetting stores the tenant's choice. Fails closed on missing tenant,
// unknown provider, or DB error — never silently keeps the old value.
func SaveSetting(ctx context.Context, db *sql.DB, provider, endpoint string) (Setting, error) {
	tid, err := shared.TenantRequired(ctx)
	if err != nil {
		return Setting{}, err
	}
	if db == nil {
		return Setting{}, errors.New("accounting settings: db unavailable")
	}
	provider = normalizeProvider(provider)
	if !ValidProvider(provider) {
		return Setting{}, errors.New("accounting settings: unknown provider " + provider)
	}
	endpoint = strings.TrimSpace(endpoint)
	if len(endpoint) > 512 {
		return Setting{}, errors.New("accounting settings: endpoint too long")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenant_accounting_settings (tenant_id, provider, endpoint, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (tenant_id) DO UPDATE SET
			provider = excluded.provider,
			endpoint = excluded.endpoint,
			updated_at = CURRENT_TIMESTAMP
	`, string(tid), provider, endpoint); err != nil {
		return Setting{}, err
	}
	return Setting{Provider: provider, Endpoint: endpoint, LivePush: livePushProvider(provider)}, nil
}

// ResolveConfig overlays the tenant's saved choice on the global/env config.
// No tenant in ctx (background jobs) or empty choice → fallback unchanged.
func ResolveConfig(ctx context.Context, db *sql.DB, fallback Config) Config {
	s := GetSetting(ctx, db)
	if s.Provider == "" {
		return fallback
	}
	out := fallback
	out.Provider = s.Provider
	if s.Endpoint != "" {
		out.Endpoint = s.Endpoint
	}
	return out
}

// ClientFor builds the adapter for the tenant's resolved choice.
func ClientFor(ctx context.Context, db *sql.DB, fallback Config) Client {
	return NewClient(ResolveConfig(ctx, db, fallback))
}
