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
		{"pricing", app.Pricing, []string{"Professional TMS", "Enterprise 3PL", "99.9%", "Service Level Agreement"}},
		{"status", app.Status, []string{"All Systems Operational", "99.9%", "API", "Telemetry"}},
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

// TestPricingPage_NoUnverifiableClaims is the truth ratchet for /pricing:
// availability, recovery, support and connector claims must match what the
// platform actually provides. Changing wording is fine; re-introducing one of
// these phrases without implementing the capability is not (AGENTS.md prime
// directive: zero false promises).
func TestPricingPage_NoUnverifiableClaims(t *testing.T) {
	app := newRegisterTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/pricing", nil)
	rr := httptest.NewRecorder()
	app.Pricing(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()

	forbidden := []string{
		"guaranteed 99.9%",              // availability is a target, not a guarantee
		"SAP / Oracle",                  // no SAP/Oracle connectors exist
		"Continuous WAL replication",    // no replication is configured
		"Automated failover",            // single-node deployment, manual restore
		"pager duty",                    // no 24/7 on-call rotation
		"automated compliance rails",    // provider adapters are contract-gated
		"dedicated TAM",                 // no assigned technical account manager
		"certified secure data centers", // deployment region is tenant-configured
	}
	for _, phrase := range forbidden {
		assert.NotContains(t, body, phrase, "unverifiable claim on /pricing: %q", phrase)
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
