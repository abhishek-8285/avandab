package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// TestComplianceDashboard_TenantIsolation: the dashboard aggregates must
// never leak another org's fleet. Pre-fix every query below was global —
// tenant-b's blocked driver + pending docs showed up in tenant-a's view.
func TestComplianceDashboard_TenantIsolation(t *testing.T) {
	db := newInvoiceLineTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	h := NewComplianceHandlers(app, nil)

	for _, tn := range []string{"tenant-a", "tenant-b"} {
		_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES (?, ?, ?)`, tn, tn, tn)
		require.NoError(t, err)
	}

	// tenant-a: 2 drivers (1 blocked), 1 vehicle, 1+1 pending docs.
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, tenant_id, first_name, last_name, phone, status, blocked, blocked_reason)
		VALUES ('drv-a1','drv-a1','tenant-a','A','One','111','available',0,''), ('drv-a2','drv-a2','tenant-a','A','Two','112','available',1,'license expired')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, status, blocked)
		VALUES ('veh-a1','tenant-a','AA-01','AA01','truck',1000,'available',0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_documents (id, driver_id, doc_type, file_url, status)
		VALUES ('ddoc-a1','drv-a1','dl','/f/a.pdf','pending_review')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, status)
		VALUES ('vdoc-a1','veh-a1','insurance','/f/va.pdf','pending_review')`)
	require.NoError(t, err)

	// tenant-b: 1 blocked driver + 1 pending doc (must stay invisible to A).
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, tenant_id, first_name, last_name, phone, status, blocked, blocked_reason)
		VALUES ('drv-b1','drv-b1','tenant-b','B','One','221','available',1,'permit expired')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_documents (id, driver_id, doc_type, file_url, status)
		VALUES ('ddoc-b1','drv-b1','dl','/f/b.pdf','pending_review')`)
	require.NoError(t, err)

	fetch := func(tenant string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/compliance/dashboard", nil)
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
		w := httptest.NewRecorder()
		h.DashboardJSON(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}

	outA := fetch("tenant-a")
	assert.Equal(t, float64(2), outA["drivers"].(map[string]any)["total"])
	assert.Equal(t, float64(1), outA["drivers"].(map[string]any)["blocked"])
	blocked := outA["blocked_drivers"].([]any)
	require.Len(t, blocked, 1)
	assert.Equal(t, "drv-a2", blocked[0].(map[string]any)["id"])
	assert.Equal(t, float64(2), outA["documents_pending"], "only A's docs")

	// Bootstrap keeps the legacy global view (single-tenant + platform).
	outBoot := fetch("1")
	assert.Equal(t, float64(3), outBoot["drivers"].(map[string]any)["total"])
	assert.Equal(t, float64(3), outBoot["documents_pending"])
}
