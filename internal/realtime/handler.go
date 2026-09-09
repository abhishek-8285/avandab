package realtime

import (
	"encoding/json"
	"net/http"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// StreamHandler returns an http.HandlerFunc for SSE streaming (Spec 04 §1.2, §7).
// Sets SSE headers, flushes headers immediately, and exits on client disconnect
// or slow consumer drop. Optional ?trip_id= / ?vehicle_id= query filters.
// If sseEnabled is provided and false, returns HTTP 503 Service Unavailable.
func StreamHandler(h Broadcaster, sseEnabled ...bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(sseEnabled) > 0 && !sseEnabled[0] {
			http.Error(w, `{"error":"SSE streaming is disabled"}`, http.StatusServiceUnavailable)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Optional filter based on query params
		tripID := r.URL.Query().Get("trip_id")
		vehicleID := r.URL.Query().Get("vehicle_id")

		// Tenant isolation: the caller tenant comes from the auth layer
		// (RequireAPIAuth stamps it on the context). Events carrying a
		// tenant_id are delivered only to that tenant — without this, any
		// authenticated user sees every org's live positions and SOS
		// locations. Events without a tenant stamp (trip bus events —
		// TODO: stamp tenant at publish) pass through as before.
		// No caller tenant (tests, mounts outside the auth group):
		// legacy passthrough, trip/vehicle filters still apply.
		callerTenant := string(shared.TenantIDFromContext(r.Context()))
		f := func(e events.Event) bool {
			if callerTenant != "" {
				if tid, ok := eventTenant(e); ok && tid != callerTenant {
					return false
				}
			}
			if tripID == "" && vehicleID == "" {
				return true
			}
			m := eventPayloadMap(e)
			if m == nil {
				return false
			}
			if tripID != "" {
				tid, ok := m["trip_id"].(string)
				if !ok || tid != tripID {
					return false
				}
			}
			if vehicleID != "" {
				vid, ok := m["vehicle_id"].(string)
				if !ok || vid != vehicleID {
					return false
				}
			}
			return true
		}

		ch, unsub := h.Subscribe(r.Context(), f)
		defer unsub()

		// Flush headers immediately
		flusher.Flush()

		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				// SSE keep-alive comment to prevent reverse proxies (Cloudflare/Caddy) from timing out idle connections
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
				flusher.Flush()
			case frame, ok := <-ch:
				if !ok {
					return // channel closed (slow consumer dropped or hub shutdown)
				}
				if _, err := w.Write(frame); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// eventPayloadMap normalizes an event payload to a string map for filter
// matching. Struct payloads (SOSEvent, AlertEvent) marshal via their json
// tags; map payloads pass through.
func eventPayloadMap(e events.Event) map[string]interface{} {
	if e.Payload == nil {
		return nil
	}
	if m, ok := e.Payload.(map[string]interface{}); ok {
		return m
	}
	b, err := json.Marshal(e.Payload)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// eventTenant extracts the tenant stamp. Struct tags use "tenant_id";
// unstamped payloads (trip bus events) report ok=false.
func eventTenant(e events.Event) (string, bool) {
	m := eventPayloadMap(e)
	if m == nil {
		return "", false
	}
	if tid, ok := m["tenant_id"].(string); ok && tid != "" {
		return tid, true
	}
	if tid, ok := m["TenantID"].(string); ok && tid != "" {
		return tid, true
	}
	return "", false
}
