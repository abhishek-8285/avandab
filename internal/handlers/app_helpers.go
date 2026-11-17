package handlers

import (
	"log/slog"
	"net/http"
)

// Small template/format helpers split out of app.go to keep the god-file
// under its ratchet cap.

// templateFailure logs the template execution error server-side and answers
// a generic 500 so internal template paths/errors never reach the client.
func templateFailure(w http.ResponseWriter, templateName string, err error) {
	slog.Error("template execution failed", "template", templateName, "error", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

// abbrString truncates s to at most max runes, appending an ellipsis. Rune
// slicing keeps multi-byte scripts (Hindi) intact; the previous byte slice
// could split a UTF-8 sequence and render replacement characters.
func abbrString(s string, max int) string {
	if max < 0 {
		max = 0
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max]) + "..."
	}
	return string(r[:max-3]) + "..."
}

// maxMultipartBodyBytes bounds request bodies for handlers that call
// ParseMultipartForm. ParseMultipartForm's maxMemory argument is only the
// in-memory spill threshold, so without MaxBytesReader an oversized upload
// still exhausts temp disk (gosec G120).
const maxMultipartBodyBytes = 12 << 20
