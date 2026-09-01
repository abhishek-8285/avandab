package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/eta"
	"transport-app/internal/shared"
	"transport-app/internal/telemetry/providers"
)

// GPSLogPayload is a single GPS log uploaded in batch by the mobile app.
// Provider-parity fields (migration 00117) are optional: older app versions
// keep working — the phone already knows speed/heading/battery for free and
// just wasn't sending them. Motion/Valid are nullable so "absent" is
// distinguishable from "false".
type GPSLogPayload struct {
	ID        int64   `json:"id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timestamp string  `json:"timestamp"`
	// AccuracyM is horizontal accuracy in metres (optional; mobile sends it
	// when the platform reports it).
	AccuracyM float64 `json:"accuracy_m,omitempty"`
	// Speed in km/h and Heading in degrees (0-360), from the location API.
	Speed   float64 `json:"speed,omitempty"`
	Heading float64 `json:"heading,omitempty"`
	// Device/fix health signals. BatteryLevel is the phone battery percent;
	// Satellites is GNSS satellite count (when the platform exposes it).
	BatteryLevel *float64 `json:"battery_level,omitempty"`
	Satellites   *int     `json:"satellites,omitempty"`
	Motion       *bool    `json:"motion,omitempty"`
}

// SyncBatchRequest is the mobile-app sync payload. DeviceID is the synthetic
// device IMEI (app-<uuid>) registered in telemetry_devices (Decision D3).
type SyncBatchRequest struct {
	DeviceID string          `json:"device_id"`
	DriverID string          `json:"driver_id,omitempty"`
	Logs     []GPSLogPayload `json:"logs"`
}

// SyncBatchResponse acknowledges a sync batch. SyncedIDs contains the offline
// log IDs that were accepted into the pipeline.
type SyncBatchResponse struct {
	Success     bool    `json:"success"`
	SyncedCount int     `json:"synced_count"`
	SyncedIDs   []int64 `json:"synced_ids"`
	ServerTime  string  `json:"server_time"`
}

// TelemetrySnapshotPayload is a full state snapshot from the mobile app.
type TelemetrySnapshotPayload struct {
	ID        string  `json:"id,omitempty"`
	TripID    string  `json:"trip_id"`
	VehicleID string  `json:"vehicle_id"`
	Timestamp string  `json:"timestamp"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Speed     float64 `json:"speed"`
	FuelLevel float64 `json:"fuel_level"`
	Odometer  float64 `json:"odometer"`
}

// RegisterTelemetryRoutes mounts the sync + snapshots endpoints plus the live
// tracking feed (Spec 04 §7). These live inside the RequireAPIAuth group
// (mobile app sends a Bearer token) and use absolute paths to preserve the
// /api/v1/telemetry/ prefix.
func RegisterTelemetryRoutes(r chi.Router, ing *Ingestor, db *sql.DB, staleMin time.Duration, etaSvc ...*eta.EtaService) {
	r.Post("/api/v1/telemetry/sync", HandleTelemetrySync(ing))
	r.Post("/api/v1/telemetry/snapshots", HandleTelemetrySnapshots(ing))
	r.Get("/api/v1/telemetry/live", LiveHandler(db, staleMin, etaSvc...))
	r.Get("/api/v1/telemetry/geofences", GeofencesHandler(db))
	r.Get("/api/v1/telemetry/history", HistoryHandler(db))
	r.Get("/api/v1/telemetry/playback", PlaybackHandler(db))
	r.Get("/api/v1/trips/{id}/playback", PlaybackHandler(db))
	r.Get("/api/v1/trips/{id}/summary", TripSummaryHandler(db))
}

// RegisterGeocodeRoute mounts the reverse-geocode proxy next to the live
// feed (Spec 04 §7). Separate so callers without NOMINATIM_URL configured
// can skip it entirely — the handler then reports 503 anyway.
func RegisterGeocodeRoute(r chi.Router, nominatimURL string) {
	r.Get("/api/v1/telemetry/reverse_geocode", ReverseGeocodeHandler(nominatimURL))
}

// ensureSyntheticDevice completes Decision D3: driver phones have no IMEI, so
// the driver_id itself becomes the device identity. The ingest pipeline
// quarantines unknown IMEIs, so a sync from an unregistered driver would be
// silently dropped. We auto-provision an active `mobile_app` device on first
// sync so the frame is accepted; admin tooling can still retire it later.
func (ing *Ingestor) ensureSyntheticDevice(ctx context.Context, imei string) {
	if imei == "" {
		return
	}
	if existing, err := ing.deviceStore.GetByIMEI(ctx, imei); err == nil && existing != nil {
		return
	}
	now := time.Now()
	_ = ing.deviceStore.InsertDevice(ctx, Device{
		ID:          uuid.NewString(),
		TenantID:    string(shared.DefaultTenant),
		IMEI:        imei,
		DeviceType:  "mobile_app",
		Status:      "active",
		ActivatedAt: &now,
	})
}

// HandleTelemetrySync processes a batch of GPS logs from the mobile app.
// Each log becomes a RawFrame routed through the canonical pipeline. Only
// frames that were Accepted (including deduped replays) contribute their
// offline log ID to synced_ids.
func HandleTelemetrySync(ing *Ingestor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req SyncBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"success": "false", "error": "Invalid request payload"})
			return
		}

		syncedIDs := make([]int64, 0, len(req.Logs))
		for _, logItem := range req.Logs {
			ts, err := time.Parse(time.RFC3339, logItem.Timestamp)
			if err != nil {
				continue // skip unparseable timestamps
			}

			// If no DeviceID was provided, fall back to DriverID-derived
			// synthetic device (Decision D3), or just ack without device.
			imei := req.DeviceID
			if imei == "" {
				imei = req.DriverID
				ing.ensureSyntheticDevice(r.Context(), imei)
			}

			frame := providers.RawFrame{
				IMEI:          imei,
				Latitude:      logItem.Latitude,
				Longitude:     logItem.Longitude,
				Speed:         logItem.Speed,
				Heading:       logItem.Heading,
				Provider:      "own",
				ProviderMsgID: "sync:" + strconv.FormatInt(logItem.ID, 10),
				RawPayload:    []byte(`{"source":"sync_batch"}`),
				DeviceTime:    ts,
				BatteryLevel:  logItem.BatteryLevel,
				Satellites:    logItem.Satellites,
				Motion:        logItem.Motion,
			}

			result, err := ing.IngestRawFrame(r.Context(), frame)
			if err != nil {
				continue
			}
			if result.Accepted {
				syncedIDs = append(syncedIDs, logItem.ID)
			}
		}

		resp := SyncBatchResponse{
			Success:     true,
			SyncedCount: len(syncedIDs),
			SyncedIDs:   syncedIDs,
			ServerTime:  time.Now().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// HandleTelemetrySnapshots processes a state snapshot from the mobile app.
// Routes through the pipeline (no direct INSERT OR REPLACE) per Spec 01 §7
// Modify list. The device is resolved by vehicle_id.
func HandleTelemetrySnapshots(ing *Ingestor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var snap TelemetrySnapshotPayload
		if err := json.NewDecoder(r.Body).Decode(&snap); err != nil || snap.VehicleID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"success": "false", "error": "Invalid snapshot payload"})
			return
		}

		// Resolve the device by vehicle_id so the pipeline can look it up.
		var imei string
		if d, err := ing.deviceStore.GetByVehicleID(r.Context(), snap.VehicleID); err == nil && d != nil {
			imei = d.IMEI
		}

		at, err := time.Parse(time.RFC3339, snap.Timestamp)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"success": "false", "error": "Invalid timestamp"})
			return
		}

		speed := snap.Speed
		fuel := snap.FuelLevel
		odo := snap.Odometer
		frame := providers.RawFrame{
			IMEI:          imei,
			TripID:        snap.TripID,
			Latitude:      snap.Latitude,
			Longitude:     snap.Longitude,
			Speed:         speed,
			Provider:      "own",
			ProviderMsgID: "snap:" + snap.ID,
			RawPayload:    []byte(`{"source":"snapshot"}`),
			DeviceTime:    at,
			FuelLevel:     &fuel,
			Odometer:      &odo,
		}

		result, err := ing.IngestRawFrame(r.Context(), frame)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"success": "false", "error": "pipeline failed"})
			return
		}

		sid := snap.ID
		if sid == "" {
			sid = uuid.NewString() // Decision D5: new rows use UUIDs
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     result.Accepted,
			"quarantined": result.Quarantined,
			"snapshot_id": sid,
			"server_time": time.Now().Format(time.RFC3339),
		})
	}
}
