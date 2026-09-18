package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
)

// Console partials receive .Extra, but renderPage executes console.html with
// buildTemplateData(PageData) which flattens Extra to top level and drops the
// "Extra" key — so money_strip/alert_inbox get nil today.
func TestConsole_RendersMoneyStripAndInboxThroughRealPath(t *testing.T) {
	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)
	app := &App{Templates: tmpl, AuthSrv: &mockAuthSvc{}}

	pd := PageData{
		Title: "Command Center",
		Extra: map[string]interface{}{
			"InboxAlerts": []map[string]any{{
				"ID":           "al-1",
				"Title":        "CONSOLE-PROBE-ALERT-9f3",
				"Severity":     "critical",
				"SeverityRank": 1,
				"MoneyAtRisk":  4321.0,
				"CreatedAt":    "18 Sep 10:00",
				"Occurrences":  2,
			}},
			"MoneyStrip": &service.MoneyStrip{Date: "2026-09-18", Revenue: 12345, Spent: 678, Receivables: 91011},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console", nil)
	w := httptest.NewRecorder()
	app.renderPage(w, req, "console.html", pd)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "CONSOLE-PROBE-ALERT-9f3", "alert inbox must receive InboxAlerts through renderPage")
	require.Contains(t, body, "12345", "money strip must receive MoneyStrip.Revenue through renderPage")
	require.Contains(t, body, "91011", "money strip must receive MoneyStrip.Receivables through renderPage")
}

// bootConsole() must not call undefined functions — initMobileSheet() was
// removed (openMobileSheet wires lazily on select instead).
func TestConsoleJs_BootFunctionDefined(t *testing.T) {
	var src []byte
	var err error
	for _, p := range []string{"../../internal/static/js/console.js", "internal/static/js/console.js"} {
		if src, err = os.ReadFile(p); err == nil {
			break
		}
	}
	require.NoError(t, err, "console.js must be readable")
	require.Contains(t, string(src), "function openMobileSheet", "sanity: expected boot file")
	require.NotContains(t, string(src), "initMobileSheet()",
		"bootConsole must not call undefined initMobileSheet")
}
