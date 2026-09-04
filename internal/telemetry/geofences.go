package telemetry

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"transport-app/internal/geofence/domain"
	geofencerepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	"transport-app/internal/shared"
)

// GeofenceZone is one active zone for the tracking map overlay (Spec 04 §7).
// Polygon vertices are [lat, lng] pairs, matching the persisted format.
type GeofenceZone struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Shape     string         `json:"shape"`
	CenterLat float64        `json:"center_lat"`
	CenterLng float64        `json:"center_lng"`
	RadiusM   float64        `json:"radius_m"`
	Polygon   []domain.Point `json:"polygon,omitempty"`
}

// GeofencesHandler serves GET /api/v1/telemetry/geofences — JSON array of
// active geofence zones for the tenant, used by the tracking map overlay.
// Mounted inside RequireAPIAuth (tenant from request context).
func GeofencesHandler(db *sql.DB) http.HandlerFunc {
	repo := geofencerepo.NewGeofenceRepository(db)
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := string(shared.TenantIDFromContext(r.Context()))
		if tenantID == "" {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		zones, err := repo.ListActiveByTenant(r.Context(), tenantID)
		if err != nil {
			http.Error(w, `{"error":"geofence query failed"}`, http.StatusInternalServerError)
			return
		}
		out := make([]GeofenceZone, 0, len(zones))
		for _, z := range zones {
			out = append(out, GeofenceZone{
				ID:        z.ID,
				Name:      z.Name,
				Kind:      z.Kind,
				Shape:     z.Shape,
				CenterLat: z.CenterLat,
				CenterLng: z.CenterLng,
				RadiusM:   z.RadiusM,
				Polygon:   z.Polygon,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(out)
	}
}
