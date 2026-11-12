package handlers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressiveDisclosureKeepsSummaryActionsClickable is the regression
// guard for commit 109beab: desktop CSS disables pointer events on
// details.vsec > summary so sections cannot be collapsed, but pointer-events
// is inherited — that made the trip view "full ledger" link (rendered inside
// the kharcha summary) dead at >=768px. Interactive descendants must opt back
// in. Fails before the layout.html rule is present.
func TestProgressiveDisclosureKeepsSummaryActionsClickable(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	src, err := os.ReadFile(filepath.Join("internal", "templates", "layout.html"))
	require.NoError(t, err)
	layout := string(src)

	require.Contains(t, layout, "details.vsec > summary { cursor: default; pointer-events: none; }",
		"desktop summary must stay inert so sections never collapse")
	assert.Contains(t, layout, "details.vsec > summary a",
		"interactive children inside a summary must re-enable pointer events")
	assert.Contains(t, layout, "pointer-events: auto; cursor: pointer;",
		"the re-enable rule must actually restore pointer events")
}
