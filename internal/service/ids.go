package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// generateID creates a new UUID string for entity primary keys.
func generateID() string {
	return uuid.NewString()
}

// generateDisplayID creates a human-readable ID with a prefix.
// Format: {prefix}-{short-uuid}
func generateDisplayID(prefix string) string {
	id := uuid.NewString()
	return fmt.Sprintf("%s-%s", prefix, id[:8])
}

// generateDriverID creates a driver display ID.
func generateDriverID(prefix string) string {
	return generateDisplayID(prefix)
}

// generateBookingNumber creates a booking number from company settings.
func (s *baseService) generateBookingNumber(ctx context.Context) string {
	settings, err := s.store.GetCompanySettings(ctx)
	if err != nil {
		return fmt.Sprintf("%s-%s", "BK", uuid.NewString()[:8])
	}
	return generateDisplayID(settings.BookingPrefix)
}

// generateTripNumber creates a trip number from company settings.
func (s *baseService) generateTripNumber(ctx context.Context) string {
	settings, err := s.store.GetCompanySettings(ctx)
	if err != nil {
		return fmt.Sprintf("%s-%s", "TR", uuid.NewString()[:8])
	}
	return generateDisplayID(settings.TripPrefix)
}

// generateInvoiceNumber creates a GST-compliant sequential invoice number:
// {prefix}/{financial-year}/{seq:04d}, e.g. "INV/2026-27/0001" — exactly 16
// characters with the default prefix, within the GST ≤16-char limit. Sequence
// allocation is atomic per tenant per financial year. If company settings are
// unavailable it still tries sequentially with the default prefix; only when
// sequence allocation itself fails does it fall back to the legacy random
// scheme, logging loudly because that number is NOT gap-free.
func (s *baseService) generateInvoiceNumber(ctx context.Context) string {
	prefix := "INV"
	if settings, err := s.store.GetCompanySettings(ctx); err != nil {
		s.log.Warn("invoice prefix unavailable, using default", "error", err)
	} else if trimmed := strings.TrimSpace(settings.InvoicePrefix); trimmed != "" {
		prefix = trimmed
	}

	number, err := s.store.NextInvoiceNumber(ctx, tenantIDFor(ctx), prefix)
	if err != nil {
		s.log.Error("invoice sequence allocation failed, falling back to random number (NOT gap-free)", "error", err)
		return fmt.Sprintf("%s-%s", prefix, uuid.NewString()[:8])
	}
	return number
}

// sanitizeName capitalizes the first letter of a name.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// ValidateRequired checks that required string fields are non-empty.
func ValidateRequired(fields map[string]string) error {
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

// RoleFromID converts a role ID to a domain.Role.
func RoleFromID(store Store, ctx context.Context, roleID int64) (domain.Role, error) {
	return store.GetRoleByID(ctx, roleID)
}

// tenantIDFor derives the acting tenant from the request context, falling
// back to the bootstrap default for system-initiated writes. Never hardcode
// a tenant literal at call sites (AGENTS.md Prohibition #4).
func tenantIDFor(ctx context.Context) string {
	if id := shared.TenantIDFromContext(ctx); id != "" {
		return string(id)
	}
	return string(shared.DefaultTenant)
}
