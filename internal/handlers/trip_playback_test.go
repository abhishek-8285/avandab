package handlers

// Playback GPX overlay wiring: file input, local parse, dashed track layer.
import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlayback_GPXOverlayWired(t *testing.T) {
	src, err := os.ReadFile("../templates/trip_playback.html")
	require.NoError(t, err)
	body := string(src)
	require.Contains(t, body, `id="pb-gpx-file"`)
	require.Contains(t, body, `id="pb-gpx-status"`)
	require.Contains(t, body, "DOMParser")
	require.Contains(t, body, "trkpt")
	require.Contains(t, body, "dashArray")
}
