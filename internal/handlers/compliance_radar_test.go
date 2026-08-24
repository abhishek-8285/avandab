package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	channels "transport-app/internal/alerts/channels"
	alertpipeline "transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestComplianceRadar_Endpoint — Spec 22 §2.8: radar route gated by
// compliance:read middleware at mount, tenant-scoped payload from
// context, and honest 501 when the service was never attached.
func TestComplianceRadar_Endpoint(t *testing.T) {
	db := newInvoiceLineTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})

	radarSvc := service.NewComplianceRadarService(db, alertpipeline.NewEngine(
		alertsqlite.NewAlertRepository(db), map[string]channels.Provider{}, nil), nil)

	h := NewComplianceHandlers(app, nil)
	h.AttachRadar(radarSvc)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			next.ServeHTTP(w, req.WithContext(tctx))
		})
	}).Get("/api/compliance/radar", h.Radar)

	fetch := func() (int, map[string]any) {
		req := withSession(httptest.NewRequest(http.MethodGet, "/api/compliance/radar", nil), "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return w.Code, out
	}

	code, out := fetch()
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, out["expiring_soon"])
	assert.Empty(t, out["ewaybill_expiring"])

	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type,
		capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('veh-r', 'MH-12-RR', 'MH12RR', 'truck', 10,
		date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, expiry_date, status)
		VALUES ('vdoc-r', 'veh-r', 'insurance', '/f/r.pdf', date('now','+25 days'), 'verified')`)
	require.NoError(t, err)

	code, out = fetch()
	require.Equal(t, http.StatusOK, code)
	soon, ok := out["expiring_soon"].([]any)
	require.True(t, ok)
	require.Len(t, soon, 1)
	entry := soon[0].(map[string]any)
	assert.Equal(t, "veh-r", entry["id"])
	assert.Equal(t, "insurance", entry["kind"])
	days := entry["days_left"].(float64)
	assert.GreaterOrEqual(t, days, 23.0)
	assert.LessOrEqual(t, days, 26.0)

	// Nil-attached handler degrades honestly.
	h2 := NewComplianceHandlers(app, nil)
	r2 := chi.NewRouter()
	r2.Get("/api/compliance/radar", h2.Radar)
	w3 := httptest.NewRecorder()
	r2.ServeHTTP(w3, withSession(httptest.NewRequest(http.MethodGet, "/api/compliance/radar", nil), "user-1", "admin"))
	assert.Equal(t, http.StatusNotImplemented, w3.Code)
}
