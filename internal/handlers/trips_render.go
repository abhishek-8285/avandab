package handlers

// Standalone (login-free) template rendering for the e-POD page, split out
// of trips.go to keep the god-file under its ratchet cap.

import (
	"log/slog"
	"net/http"
)

func (h *TripHandlers) renderStandalone(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if h.App == nil || h.App.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	if m, ok := data.(map[string]interface{}); ok {
		m["Lang"] = langOf(r)
	}
	tmpl := h.App.templatesFor(r).Lookup(name)
	if tmpl == nil {
		slog.Error("template not found", "template", name)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("template execution failed", "template", name, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
