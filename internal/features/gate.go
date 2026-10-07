package features

import (
	"net/http"

	"transport-app/internal/shared"
)

// Renderer draws the user-facing denial page. Implemented by handlers.App
// (error.html + layout.html), which also branches to JSON for API clients.
// A nil Renderer falls back to http.Error / http.NotFound, so the package stays
// usable without templates.
type Renderer func(w http.ResponseWriter, r *http.Request, status int, title, message string)

// Gate returns middleware that blocks a route family when its feature is off
// for the request's org. Add-ons get the "not enabled for your organisation"
// page (403); core features get a 404 (they are only off when the process
// disabled them).
//
// render must be the app's error renderer, not http.Error: a bare
// http.Error sends `text/plain` with no HTML at all, so a phone falls back to
// the 980px default layout viewport, scales the whole page down to fit the
// screen and renders 16px body text at roughly 7px — an unbranded, unreadable
// black page for every add-on route (/geofences, /fuel, /toll, /assistant).
func Gate(reg *Registry, key string, render Renderer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := string(shared.TenantIDFromContext(r.Context()))
			f, _ := ByKey(key)
			if tenantID == "" {
				// Fail closed: never serve another org's (or defaults')
				// features to an unresolved tenant.
				deny(w, r, f, render)
				return
			}
			if reg.Enabled(r.Context(), tenantID, key) {
				next.ServeHTTP(w, r)
				return
			}
			deny(w, r, f, render)
		})
	}
}

func deny(w http.ResponseWriter, r *http.Request, f Feature, render Renderer) {
	switch {
	case f.Tier == TierAddon:
		msg := "This add-on is not enabled for your organisation. Contact your account manager to enable " + f.Name + "."
		if render == nil {
			http.Error(w, msg, http.StatusForbidden)
			return
		}
		render(w, r, http.StatusForbidden, f.Name+" is not enabled", msg)
	case render == nil:
		http.NotFound(w, r)
	default:
		render(w, r, http.StatusNotFound, "Page Not Found",
			"The requested page is not available on this deployment.")
	}
}
