package handlers

// Red-proving tests ONLY for dashboard-live defects. No production edits.
//  1a. dashboard-live.js:254-261 opens /dashboard/stream when NO
//      dash-live-stamp exists (absence = classic/off-route) while script
//      loads globally via layout.html:17.
//  1b. visibilitychange handler refreshes off-route (refreshTables +
//      startDashStream with no stamp guard).
//  1c. fallback stream (__dashFallbackES) never closed on hide —
//      stopDashStream only closes __dashES.
// Static source only: no DB, no network.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardLive_NoFallbackStreamOffRoute(t *testing.T) {
	layout := readStaticForUITest(t,
		"../templates/layout.html",
		"internal/templates/layout.html",
		"layout.html",
	)
	require.Contains(t, layout, "dashboard-live.js",
		"premise: dashboard-live.js loads globally via layout.html")

	src := readStaticForUITest(t,
		"../static/js/dashboard-live.js",
		"internal/static/js/dashboard-live.js",
		"dashboard-live.js",
	)
	const marker = "if (!document.getElementById('dash-live-stamp')) {"
	idx := strings.Index(src, marker)
	require.NotEqual(t, -1, idx, "premise: no-stamp fallback branch must exist (defect location 254-261)")
	endRel := strings.Index(src[idx:], "} else {")
	require.NotEqual(t, -1, endRel, "premise: fallback if/else must exist")
	block := src[idx : idx+endRel]
	require.NotContains(t, block, "new EventSource",
		"RED: no-stamp branch opens /dashboard/stream off-route — must not open any stream without dash-live-stamp")
}

func TestDashboardLive_VisibilityHandlerStaysOffRoute(t *testing.T) {
	src := readStaticForUITest(t,
		"../static/js/dashboard-live.js",
		"internal/static/js/dashboard-live.js",
		"dashboard-live.js",
	)
	idx := strings.Index(src, "visibilitychange")
	require.NotEqual(t, -1, idx, "premise: visibilitychange handler must exist")
	end := idx + 600
	if end > len(src) {
		end = len(src)
	}
	block := src[idx:end]
	require.Contains(t, block, "dash-live-stamp",
		"RED: visibility handler refreshes off-route — must guard on dash-live-stamp before refreshTables/startDashStream")
}

func TestDashboardLive_FallbackClosedOnHide(t *testing.T) {
	src := readStaticForUITest(t,
		"../static/js/dashboard-live.js",
		"internal/static/js/dashboard-live.js",
		"dashboard-live.js",
	)
	idx := strings.Index(src, "function stopDashStream")
	require.NotEqual(t, -1, idx, "premise: stopDashStream must exist")
	fallbackRel := strings.Index(src[idx:], "// Fallback")
	require.NotEqual(t, -1, fallbackRel, "premise: fallback block must follow stopDashStream")
	block := src[idx : idx+fallbackRel]
	require.Contains(t, block, "__dashFallbackES",
		"RED: stopDashStream never closes fallback stream — must close __dashFallbackES on hide")
}
