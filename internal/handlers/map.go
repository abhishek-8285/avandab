package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/telemetry"
)

// MapHandlers powers the live fleet map page and SSE stream (Spec 12 §2.2, §4.3).
type MapHandlers struct {
	*App
	liveStore *telemetry.LiveStore
}

func NewMapHandlers(app *App, liveStore *telemetry.LiveStore) *MapHandlers {
	return &MapHandlers{App: app, liveStore: liveStore}
}

func (h *MapHandlers) Page(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "map.html", PageData{
		Title: "Live Fleet Map",
		User:  session,
		Extra: map[string]interface{}{
			"MapAssets": true,
		},
	})
}

func (h *MapHandlers) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Prime immediately
	h.writeMapFrame(w, flusher, r.Context())

	for {
		select {
		case <-r.Context().Done():
			return // Client disconnected
		case <-ticker.C:
			h.writeMapFrame(w, flusher, r.Context())
		}
	}
}

func (h *MapHandlers) writeMapFrame(w http.ResponseWriter, f http.Flusher, ctx context.Context) {
	// Fail closed: never stream the bootstrap org's fleet to an unresolved
	// viewer (route sits behind auth which always sets tenant).
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		return
	}

	var vehicles []telemetry.LiveVehicle
	var err error
	if h.liveStore != nil {
		vehicles, err = h.liveStore.Live(ctx, tenantID, "", time.Now())
	}
	if err != nil || vehicles == nil {
		vehicles = []telemetry.LiveVehicle{}
	}

	jsonBytes, err := json.Marshal(vehicles)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: datastar-merge-signals\ndata: {\"vehicles\":%s}\n\n", jsonBytes)
	f.Flush()
}
