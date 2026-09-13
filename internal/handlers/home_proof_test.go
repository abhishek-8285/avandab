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
