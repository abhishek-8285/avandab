package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/config"
)

// TrackingHandlers powers the live fleet map page (Spec 04 §1.3, §7).
// Any authenticated user may view it; no permission gate (per spec).
type TrackingHandlers struct {
	*App
}

// Page renders the Leaflet tracking page. Map configuration is injected so
// the template can pick the tile provider / fallback / poll cadence without
// another round trip.
func (h *TrackingHandlers) Page(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	cfg := h.Config
	if cfg == nil {
		cfg = &config.Config{LiveMap: config.LiveMapConfig{
			MapTileProvider: "google",
			MapGoogleStyle:  "m",
			MapGL:           "IN",
			MapOSMURL:       "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
			MapPollSec:      10,
		}}
	}
	h.renderPage(w, r, "tracking.html", PageData{
		Title: "Live Tracking",
		User:  session,
		Extra: map[string]interface{}{
			"MapAssets": true,
			"MapConfig": map[string]interface{}{
				"Provider":    cfg.LiveMap.MapTileProvider,
				"GoogleStyle": cfg.LiveMap.MapGoogleStyle,
				"GL":          cfg.LiveMap.MapGL,
				"OSMUrl":      cfg.LiveMap.MapOSMURL,
				"PollSec":     cfg.LiveMap.MapPollSec,
			},
			"LiveEndpoint": "/api/v1/telemetry/live",
		},
	})
}

// Routes mounts the tracking page inside the protected web group.
func (h *TrackingHandlers) Routes(r chi.Router) {
	r.Get("/", h.Page)
}
