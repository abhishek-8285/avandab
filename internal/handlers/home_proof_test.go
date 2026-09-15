package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHomeRendersHonestProof(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)
	tmpl := app.Templates.Lookup("home.html")
	require.NotNil(t, tmpl, "home.html must parse")
	var buf strings.Builder
	err := tmpl.Execute(&buf, map[string]interface{}{
		"Version":        "test",
		"Title":          "Modern Fleet & Logistics Operations",
		"SEODescription": "test",
		"CanonicalPath":  "/",
		"NoIndex":        false,
		"OGType":         "website",
	})
	require.NoError(t, err, "home.html must execute")
	body := buf.String()
	require.Contains(t, body, "No hardware")
	require.Contains(t, body, "Illustrative math")
	require.NotContains(t, body, "500+ Indian fleets")
	require.NotContains(t, body, "4.9/5")
	require.NotContains(t, body, "40+ Indian fleet operations")
}

func TestHomeHeadingHierarchy(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)
	tmpl := app.Templates.Lookup("home.html")
	require.NotNil(t, tmpl, "home.html must parse")
	var buf strings.Builder
	err := tmpl.Execute(&buf, map[string]interface{}{
		"Version": "test", "Title": "t", "SEODescription": "t",
		"CanonicalPath": "/", "NoIndex": false, "OGType": "website",
	})
	require.NoError(t, err, "home.html must execute")
	body := buf.String()
	first := func(tag string) int { return strings.Index(body, tag) }
	h1, h2, h3, h4 := first("<h1"), first("<h2"), first("<h3"), first("<h4")
	require.NotEqual(t, -1, h1, "needs one h1")
	require.NotEqual(t, -1, h2, "needs h2 sections")
	require.True(t, h1 < h2, "h1 must precede first h2")
	if h3 != -1 {
		require.True(t, h2 < h3, "no h3 before first h2 (skipped level)")
	}
	if h4 != -1 {
		require.True(t, h3 != -1 && h3 < h4, "no h4 before first h3 (skipped level)")
	}
}

func TestHomeDesignerHygiene(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)
	tmpl := app.Templates.Lookup("home.html")
	require.NotNil(t, tmpl, "home.html must parse")
	var buf strings.Builder
	err := tmpl.Execute(&buf, map[string]interface{}{
		"Version": "test", "Title": "t", "SEODescription": "t",
		"CanonicalPath": "/", "NoIndex": false, "OGType": "website",
	})
	require.NoError(t, err, "home.html must execute")
	body := buf.String()
	require.NotContains(t, body, "transition:all", "list transition properties explicitly")
	require.NotContains(t, body, "<h4>", "card titles are h3 under h2 sections")
	require.NotContains(t, body, `"Truck chahiye kal subah"`, "curly quotes, not straight")
	require.Contains(t, body, "aria-live=\"polite\"", "async calculator results need aria-live")
}
