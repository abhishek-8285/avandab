package handlers

// Regression tests for the quick-navigation command palette. The palette is
// deliberately dependency-free static JS/CSS, so these assert the source
// contract the UI relies on (same approach as the other UI defect tests):
//   - the shell loads the script and exposes a real, keyboard-labelled trigger
//   - the script opens on Ctrl/Cmd+K, supports arrow/Enter/Escape, renders
//     server-supplied labels safely, and survives an HTMX body swap
//   - the dialog styles are dark-aware and honour reduced motion
// Static source only: no browser, no DB, no network.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readCommandPaletteSources(t *testing.T) (layout, js, css, tokens string) {
	t.Helper()
	layout = readStaticForUITest(t,
		"../templates/layout.html",
		"internal/templates/layout.html",
		"layout.html",
	)
	js = readStaticForUITest(t,
		"../static/js/command-palette.js",
		"internal/static/js/command-palette.js",
		"command-palette.js",
	)
	css = readStaticForUITest(t,
		"../static/css/app.css",
		"internal/static/css/app.css",
		"app.css",
	)
	tokens = readStaticForUITest(t,
		"../../src/input.css",
		"src/input.css",
		"input.css",
	)
	return
}

func TestCommandPalette_LoadedAndAccessibleTrigger(t *testing.T) {
	layout, _, _, _ := readCommandPaletteSources(t)

	require.Contains(t, layout, "/static/js/command-palette.js",
		"the global shell must load the palette script")
	require.Contains(t, layout, "data-cp-open",
		"a trigger must be wired to the palette")
	require.Contains(t, layout, `aria-keyshortcuts="Control+K Meta+K"`,
		"the shortcut must be advertised to assistive tech")
	// Without JS the trigger is an ordinary link, so a phone is never stranded.
	idx := strings.Index(layout, "data-cp-open")
	require.NotEqual(t, -1, idx)
	require.Contains(t, layout[idx-200:idx], `href="/search"`,
		"the trigger must degrade to a real link without JavaScript")
}

func TestCommandPalette_KeyboardAndSafeRendering(t *testing.T) {
	_, js, _, _ := readCommandPaletteSources(t)

	require.Contains(t, js, "e.ctrlKey || e.metaKey",
		"must open on both Ctrl+K and Cmd+K")
	require.Contains(t, js, "e.key === 'ArrowDown'", "must support ArrowDown")
	require.Contains(t, js, "e.key === 'ArrowUp'", "must support ArrowUp")
	require.Contains(t, js, "e.key === 'Enter'", "must support Enter to activate")
	require.Contains(t, js, "e.key === 'Escape'", "must support Escape to close")
	require.Contains(t, js, `role="dialog"`, "must expose a dialog role")
	require.Contains(t, js, "aria-activedescendant", "must expose the active option")
	require.Contains(t, js, "textContent = cmd.label",
		"labels must be inserted as text, never as HTML")

	// Search is the universal fallback: focus the topbar box on desktop,
	// navigate to /search on phones where that box is hidden.
	require.Contains(t, js, "global-search-input")
	require.Contains(t, js, "window.location.assign('/search')")

	// hx-boost swaps <body> and would otherwise destroy the dialog.
	require.Contains(t, js, "htmx:afterSwap")
	require.Contains(t, js, "document.contains(overlay)")
}

func TestCommandPalette_DarkAwareStylesAndMotion(t *testing.T) {
	_, _, css, tokens := readCommandPaletteSources(t)

	for _, class := range []string{
		".cp-overlay", ".cp-overlay[hidden]", ".cp-dialog",
		".cp-option", ".cp-option.is-active", ".cp-footer",
	} {
		require.Contains(t, css, class, "palette style %s must exist", class)
	}
	require.Contains(t, css, "prefers-reduced-motion: reduce",
		"the entry animation must be disabled for reduced-motion users")
	// Semantic tokens follow the Day/Night/System switch; raw colours would
	// leave the palette light-on-light in dark mode.
	for _, token := range []string{
		"var(--color-surface-container-lowest)",
		"var(--color-on-surface)",
		"var(--color-primary-container)",
		"var(--color-outline-variant)",
	} {
		require.Contains(t, css, token, "style must use semantic token %s", token)
		require.Contains(t, tokens, strings.TrimSuffix(strings.TrimPrefix(token, "var("), ")")+":",
			"premise: token %s must be defined in the theme layer", token)
	}
}
