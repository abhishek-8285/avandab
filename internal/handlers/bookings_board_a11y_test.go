package handlers

// Red-proving tests ONLY for two a11y defects. No production edits.
//  1. bookings_board.html:39-50 cards are draggable <article> with text only:
//     no link/button/focusability. Default Bookings lands on board
//     (bookings_view_test.go:14-18); detail page owns lifecycle forms
//     (booking_view.html:165-180), so cards must expose a keyboard-focusable
//     link to booking detail.
//  2. layout.html:649-655 Escape path only toggles sidebar classes, while
//     complete close at :619-624 hides overlay, sets aria-expanded=false,
//     restores focus to the menu button. Escape must do the same (or call
//     the shared close).
//
// Both assertions render/read through static sources only: no DB, no network.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readLayoutForA11yTest(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../templates/layout.html",
		"internal/templates/layout.html",
	}
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			return string(b)
		}
	}
	cwd, _ := os.Getwd()
	for d := cwd; ; d = filepath.Dir(d) {
		p := filepath.Join(d, "internal", "templates", "layout.html")
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatal("could not read layout.html from candidates or walk-up")
	return ""
}

// Defect 1: each rendered board card must contain a keyboard-focusable link
// to its booking detail page. Rendered through the real template path
// (parseTemplates + buildTemplateData, exact renderPage flattening).
func TestBookingsBoard_CardHasKeyboardFocusableDetailLink(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	cols := []boardColumn{
		{Status: "pending", Cards: []boardCard{{ID: "bk-a11y-1", Number: "BK-A1", Customer: "Acme", Freight: 100}}},
		{Status: "confirmed", Cards: []boardCard{{ID: "bk-a11y-2", Number: "BK-A2", Customer: "Acme", Freight: 200}}},
		{Status: "completed", Cards: []boardCard{}},
		{Status: "cancelled", Cards: []boardCard{}},
	}
	var buf strings.Builder
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "bookings_board.html",
		buildTemplateData(PageData{Title: "Bookings Board", Extra: map[string]interface{}{"Columns": cols}})))
	out := buf.String()

	articleRe := regexp.MustCompile(`(?s)<article[^>]*data-id="([^"]+)"[^>]*>(.*?)</article>`)
	inner := map[string]string{}
	for _, m := range articleRe.FindAllStringSubmatch(out, -1) {
		inner[m[1]] = m[2]
	}
	for _, id := range []string{"bk-a11y-1", "bk-a11y-2"} {
		require.Contains(t, inner, id, "card %s must render (fixture through real path)", id)
		body := inner[id]
		require.Contains(t, body, `href="/bookings/`+id+`"`,
			"RED: board card %s has no keyboard-focusable link to booking detail "+
				"(article renders text only; detail page owns lifecycle forms)", id)
		linkRe := regexp.MustCompile(`<a[^>]*href="/bookings/` + regexp.QuoteMeta(id) + `"[^>]*>`)
		link := linkRe.FindString(body)
		require.NotEmpty(t, link, "RED: card %s detail link tag must exist", id)
		require.NotContains(t, link, `tabindex="-1"`,
			"detail link for card %s must stay keyboard-focusable", id)
	}
}

// Defect 2: Escape must perform the complete close (overlay hide +
// aria-expanded=false + focus return) or call the shared closeSidebar().
func TestSidebar_EscapePerformsCompleteClose(t *testing.T) {
	src := readLayoutForA11yTest(t)

	escIdx := strings.Index(src, "'Escape'")
	if escIdx == -1 {
		escIdx = strings.Index(src, `"Escape"`)
	}
	require.NotEqual(t, -1, escIdx, "Escape key handler must exist in layout.html")

	rest := src[escIdx:]
	end := len(rest)
	if tabIdx := strings.Index(rest, "'Tab'"); tabIdx != -1 && tabIdx < end {
		end = tabIdx
	}
	if end > 800 {
		end = 800
	}
	escapeBlock := rest[:end]

	if strings.Contains(escapeBlock, "closeSidebar") {
		return // shared complete close satisfies the requirement
	}

	require.Contains(t, escapeBlock, "overlay",
		"RED: Escape path only toggles sidebar classes — must hide overlay "+
			"(complete close at closeSidebar hides #mobile-overlay)")
	require.Contains(t, escapeBlock, "aria-expanded",
		"RED: Escape path must set aria-expanded=false on #mobile-menu-btn "+
			"(complete close does)")
	require.Contains(t, escapeBlock, ".focus()",
		"RED: Escape path must restore focus (complete close focuses menu button)")
}
