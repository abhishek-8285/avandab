package handlers

// Red-proving tests ONLY for two UI defects. No production edits.
//  1. layout.html shared submit guard ignores e.defaultPrevented, disables
//     the submit button on a cancelled submit, never restores on failure.
//  2a. bookings_board.html cards write data-status from wrong scope
//     ($.Status = template root, always empty) instead of the column.
//  2b. bookings_board.js clears shared `dragged` on dragend while the async
//     fetch rejection path dereferences it (use-after-clear TypeError).
//
// Card assertions render through the real templates (parseTemplates +
// buildTemplateData, the exact renderPage flattening path). JS/guard
// behavior is asserted via static source (no browser, no DB, no network).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readStaticForUITest(t *testing.T, candidates ...string) string {
	t.Helper()
	var errs []string
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err == nil {
			return string(b)
		}
		errs = append(errs, c+": "+err.Error())
	}
	// Also try resolving from repo root via cwd walk-up.
	cwd, _ := os.Getwd()
	for d := cwd; ; d = filepath.Dir(d) {
		for _, c := range candidates {
			base := filepath.Base(c)
			var p string
			if strings.Contains(c, "templates") {
				p = filepath.Join(d, "internal", "templates", base)
			} else {
				p = filepath.Join(d, "internal", "static", "js", base)
			}
			if b, err := os.ReadFile(p); err == nil {
				return string(b)
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatalf("could not read static file (tried %v): %s", candidates, strings.Join(errs, "; "))
	return ""
}

// Defect 1: cancelled submits must not disable the button, and a failed
// submit must restore it. The shared guard currently does neither.
func TestLayoutSubmitGuard_RespectsCancelledSubmit(t *testing.T) {
	src := readStaticForUITest(t,
		"../templates/layout.html",
		"internal/templates/layout.html",
		"layout.html",
	)
	idx := strings.Index(src, "function initFormGuards")
	require.NotEqual(t, -1, idx, "initFormGuards block must exist in layout.html")
	guard := src[idx:]
	if end := strings.Index(guard, "initFormGuards();"); end != -1 {
		guard = guard[:end]
	}

	require.Contains(t, guard, "defaultPrevented",
		"RED: guard ignores e.defaultPrevented — a submit cancelled by the "+
			"data-confirm handler (or htmx validation) still disables the button")
	require.Contains(t, guard, "disabled = false",
		"RED: guard never restores the button on failure — no disabled=false "+
			"path after storing originalHtml")
}

// Defect 2a: each card's data-status must equal its column lane. Rendered
// through the real template path (buildTemplateData flattens Extra, exactly
// like renderPage).
func TestBookingsBoard_CardStatusMatchesColumn(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	cols := []boardColumn{
		{Status: "pending", Cards: []boardCard{{ID: "bk-pend", Number: "BK-P", Customer: "Acme", Freight: 100}}},
		{Status: "confirmed", Cards: []boardCard{{ID: "bk-conf", Number: "BK-C", Customer: "Acme", Freight: 200}}},
		{Status: "completed", Cards: []boardCard{}},
		{Status: "cancelled", Cards: []boardCard{}},
	}
	var buf strings.Builder
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "bookings_board.html",
		buildTemplateData(PageData{Title: "Bookings Board", Extra: map[string]interface{}{"Columns": cols}})))
	out := buf.String()

	re := regexp.MustCompile(`data-id="([^"]+)"[^>]*?data-status="([^"]*)"`)
	got := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		got[m[1]] = m[2]
	}
	require.Contains(t, got, "bk-pend", "pending card must render")
	require.Contains(t, got, "bk-conf", "confirmed card must render")
	require.Equal(t, "pending", got["bk-pend"],
		"RED: card bk-pend must carry its column lane (template writes $.Status = root scope)")
	require.Equal(t, "confirmed", got["bk-conf"],
		"RED: card bk-conf must carry its column lane (template writes $.Status = root scope)")
}

// Defect 2b: dragend clears shared `dragged` while the async fetch rejection
// path dereferences it. Static source assertion only (no browser).
func TestBookingsBoardJs_DragStateSurvivesAsyncRejection(t *testing.T) {
	src := readStaticForUITest(t,
		"../static/js/bookings_board.js",
		"internal/static/js/bookings_board.js",
		"bookings_board.js",
	)

	require.Contains(t, src, "dragged = null",
		"premise: dragend clears the shared dragged reference")
	fetchIdx := strings.Index(src, "fetch(tpl")
	require.NotEqual(t, -1, fetchIdx, "board JS must POST via fetch(tpl...)")
	fetchBlock := src[fetchIdx:]

	require.NotContains(t, fetchBlock, "dragged.style",
		"RED: async then/catch dereferences shared `dragged` after dragend "+
			"may have nulled it — must use a captured local + null-guard")
}
