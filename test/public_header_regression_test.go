package test

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test for mobile header showing both hamburger icons (☰✕).
// Root cause: unlayered `svg{display:inline-block}` in app.css beats
// Tailwind v4's layered `.hidden{display:none}` on every SVG, so
// `class="... hidden"` never hides an SVG. The display rule must not
// match `.hidden` SVGs.
func TestSvgHiddenNotOverriddenByAppCSS(t *testing.T) {
	data, err := os.ReadFile("../internal/static/css/app.css")
	require.NoError(t, err)
	css := string(data)

	// No bare `svg { ... display ... }` rule may exist: it would be unlayered
	// and override the layered Tailwind `.hidden` utility on all SVGs.
	bareSvgDisplay := regexp.MustCompile(`(?m)^\s*svg\s*\{[^}]*display\s*:`)
	assert.NotRegexp(t, bareSvgDisplay, css,
		"bare svg{display} rule overrides Tailwind .hidden on SVGs; scope it as svg:not(.hidden)")

	assert.Contains(t, css, "svg:not(.hidden)",
		"app.css must scope the SVG display guarantee so .hidden keeps working")
}

// The mobile login label span was malformed (`<span class="sm:hidden"{{t ...}}/span>`),
// producing invalid HTML in the public header.
func TestPublicHeaderLoginSpanWellFormed(t *testing.T) {
	data, err := os.ReadFile("../internal/templates/partials/public_header.html")
	require.NoError(t, err)
	header := string(data)

	assert.NotContains(t, header, `{{t "nav.login"}}/span`,
		"public_header.html contains malformed nav.login span")
	assert.Contains(t, header, `<span class="sm:hidden">{{t "nav.login"}}</span>`,
		"public_header.html must render mobile login label with well-formed span")

	// Hamburger close icon must keep the hidden utility class (relies on fix above).
	assert.Contains(t, header, `id="hamburger-icon-close"`)
	assert.Contains(t, header, `id="hamburger-icon-open"`)
}
