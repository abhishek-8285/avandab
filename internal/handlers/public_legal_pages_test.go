package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ratchet: every public legal page renders 200 with its E-Commerce Amendment
// 2026 content (grievance contacts, SLAs, NCH link). Fails if a route handler
// is unwired, a template breaks, or compliance content regresses.
func TestPublicLegalPages_RenderWithComplianceContent(t *testing.T) {
	app := newRegisterTestApp(t)

	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		want    []string
	}{
		{"privacy", app.Privacy, []string{"Grievance Officer", "grievance@avandab.com", "48 hours"}},
		{"terms", app.Terms, []string{"30 days", "consumerhelpline"}},
		{"refunds", app.Refunds, []string{"30 days", "business days", "consumerhelpline"}},
		{"consumer-compliance", app.ConsumerCompliance, []string{"Self-audit certificate", "consumerhelpline", "48 hours"}},
		{"faq", app.FAQ, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+tc.name, nil)
			rr := httptest.NewRecorder()
			tc.handler(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
			for _, s := range tc.want {
				assert.Contains(t, rr.Body.String(), s)
			}
		})
	}
}

func TestPublicContactPage_RendersSLANotice(t *testing.T) {
	app := newRegisterTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/contact-us", nil)
	rr := httptest.NewRecorder()
	(&ContactHandlers{app}).Page(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	for _, s := range []string{"48 hours", "grievance@avandab.com", "consumerhelpline"} {
		assert.Contains(t, rr.Body.String(), s)
	}
}
