package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"transport-app/internal/auth"
	"transport-app/internal/cache"
	"transport-app/internal/shared"
)

// UserTenantLookup resolves a user's tenant id and the tenant's lifecycle
// status. status "active" grants access; any other value is rejected.
type UserTenantLookup func(ctx context.Context, userID string) (tenantID string, status string, err error)

// userTenantCacheTTL bounds staleness of suspended/active flips: an org that
// gets suspended takes at most this long to stop serving requests.
const userTenantCacheTTL = 60 * time.Second

// TenantForUserResolver adapts a UserTenantLookup into a TenantResolver with
// a short-lived cache in front of the SQL lookup. Lookup errors are returned
// to the caller (middleware decides reject-vs-fallback); users whose row has
// no tenant are treated as lookup failures — users.tenant_id is NOT NULL.
func TenantForUserResolver(lookup UserTenantLookup, c cache.Cache) TenantResolver {
	return func(ctx context.Context, userID string) (shared.TenantID, error) {
		key := "usertenant:" + userID
		if c != nil {
			if val, ok, err := c.Get(ctx, key); err == nil && ok {
				parts := strings.Split(string(val), "\x00")
				if len(parts) == 2 && parts[0] != "" {
					if parts[1] != "active" {
						return "", auth.ErrTenantSuspended
					}
					return shared.TenantID(parts[0]), nil
				}
				// Malformed payload: fall through to the live lookup.
			}
		}

		tenantID, status, err := lookup(ctx, userID)
		if err != nil {
			return "", err
		}
		if tenantID == "" {
			return "", fmt.Errorf("tenant resolver: user %q has no tenant", userID)
		}

		if c != nil {
			payload := tenantID + "\x00" + status
			_ = c.Set(ctx, key, []byte(payload), userTenantCacheTTL)
		}

		if status != "active" {
			return "", auth.ErrTenantSuspended
		}
		return shared.TenantID(tenantID), nil
	}
}
