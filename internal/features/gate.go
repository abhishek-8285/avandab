package features

import (
	"net/http"

	"transport-app/internal/shared"
)

// Gate returns middleware that blocks a route family when its feature is off
// for the request's org. Add-ons get the branded upsell page (403); core
// features get a plain 404 (they are only off when the process disabled them).
func Gate(reg *Registry, key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := string(shared.TenantIDFromContext(r.Context()))
			f, _ := ByKey(key)
			if tenantID == "" {
				// Fail closed: never serve another org's (or defaults')
				// features to an unresolved tenant.
				deny(w, r, f)
				return
			}
			if reg.Enabled(r.Context(), tenantID, key) {
				next.ServeHTTP(w, r)
				return
			}
			deny(w, r, f)
		})
	}
}

func deny(w http.ResponseWriter, r *http.Request, f Feature) {
	if f.Tier == TierAddon {
		http.Error(w,
			"This add-on is not enabled for your organisation. Contact your account manager to enable "+f.Name+".",
			http.StatusForbidden)
		return
	}
	http.NotFound(w, r)
}
