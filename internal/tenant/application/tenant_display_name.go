package application

// Tenant display-name sync used by the onboarding wizard, extracted from
// internal/handlers/settings.go (handlers-end-state ADR, Phase 2). Profile
// table holds the truth; tenants.name is display only, so failures are
// best-effort at the call site.

import (
	"context"
)

// SyncTenantDisplayName updates tenants.name for the acting tenant.
func (s *TenantAdminService) SyncTenantDisplayName(ctx context.Context, tenantID, name string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, name, tenantID)
	return err
}
