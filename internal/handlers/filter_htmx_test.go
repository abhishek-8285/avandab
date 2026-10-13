package handlers

// Ratchet for the list-filter htmx contract: pages without a #list-table
// swap target (alerts, ops-alerts, experiments, settlements, telemetry)
// render filter_bar/pagination with NoHtmx=true so chips degrade to plain
// GET links instead of dead htmx swaps. Fails on pre-fix templates where
// hx-get was unconditional.

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderPartial(t *testing.T, name string, data map[string]interface{}) string {
	t.Helper()
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	tpl := tmpl.Lookup(name)
	require.NotNilf(t, tpl, "template %s not found", name)
	var buf bytes.Buffer
	require.NoErrorf(t, tpl.Execute(&buf, data), "template %s failed to render", name)
	return buf.String()
}

func TestFilterBarNoHtmxStripsSwapAttrs(t *testing.T) {
	chips := []map[string]interface{}{
		{"Label": "All", "Value": ""},
		{"Label": "Draft", "Value": "draft"},
	}
	out := renderPartial(t, "filter_bar.html", map[string]interface{}{
		"Action": "/alerts", "StatusFilter": "draft", "Chips": chips, "NoHtmx": true,
	})
	assert.NotContains(t, out, "hx-get=", "NoHtmx chips must not carry htmx attrs")
	assert.NotContains(t, out, "hx-target=", "NoHtmx chips must not carry htmx attrs")
	assert.NotContains(t, out, "hx-include=", "NoHtmx chips must not carry htmx attrs")
	assert.Contains(t, out, "/alerts?status=draft", "plain href fallback must survive")
	assert.Contains(t, out, "border-primary bg-primary", "server-side active chip must still highlight")
}

func TestFilterBarDefaultKeepsHtmx(t *testing.T) {
	chips := []map[string]interface{}{
		{"Label": "All", "Value": ""},
		{"Label": "Draft", "Value": "draft"},
	}
	out := renderPartial(t, "filter_bar.html", map[string]interface{}{
		"Action": "/trips", "StatusFilter": "draft", "Chips": chips,
	})
	assert.Contains(t, out, `hx-target="#list-table"`, "SPA pages must keep partial-swap target")
}

func TestPaginationNoHtmxStripsSwapAttrs(t *testing.T) {
	data := map[string]interface{}{
		"Pagination": PaginationData{Page: 1, PerPage: 20, Total: 45, TotalPages: 3, HasPrev: false, HasNext: true, BasePath: "/telemetry/devices"},
		"NoHtmx":     true,
	}
	out := renderPartial(t, "pagination.html", data)
	assert.NotContains(t, out, "hx-get=", "NoHtmx pagination must not carry htmx attrs")
	assert.NotContains(t, out, "hx-target=", "NoHtmx pagination must not carry htmx attrs")
	assert.Contains(t, out, "/telemetry/devices?page=2", "plain href fallback must survive")
}
