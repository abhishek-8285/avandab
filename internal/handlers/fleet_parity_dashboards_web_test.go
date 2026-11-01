package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupFleetParityWebTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"admin-user:tyres:read":          true,
			"admin-user:tyres:write":         true,
			"admin-user:inventory:read":      true,
			"admin-user:inventory:write":     true,
			"admin-user:freight_audit:read":  true,
			"admin-user:freight_audit:write": true,
			"admin-user:reports:read":        true,
			"admin-user:reports:write":       true,
			"admin-user:rfq:read":            true,
			"admin-user:rfq:write":           true,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}

	app.Tyres = &TyreHandlers{App: app}
	app.Inventory = &InventoryHandlers{App: app}
	app.FreightAudit = &FreightAuditHandlers{App: app}
	app.ReportBuilder = &ReportBuilderHandlers{App: app}
	app.RFQ = &RFQHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	r.Route("/tyres", app.Tyres.Routes)
	r.Route("/inventory", app.Inventory.Routes)
	r.Route("/freight-audit", app.FreightAudit.Routes)
	r.Route("/report-builder", app.ReportBuilder.Routes)
	r.Route("/rfq", app.RFQ.Routes)

	return app, r
}

func TestFleetParityDashboardsWeb_EndToEnd(t *testing.T) {
	app, r := setupFleetParityWebTestApp(t)

	tenantID := "tenant-parity-web-1"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'Fleet Test Corp', 'fleet-test')`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, current_mileage)
		VALUES ('veh-p1', $1, 'DL01TY1001', 'DL01TY1001', 'truck', 12000, 50000)`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO facilities (id, tenant_id, name, facility_code, facility_type, address, city, state, pincode, is_active, created_at, updated_at)
		VALUES ('fac-p1', $1, 'Central Warehouse', 'C-WH1', 'hub', 'Sector 18', 'Gurugram', 'Haryana', '122001', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, tenantID)
	require.NoError(t, err)

	userHdr := func(req *http.Request) {
		req.Header.Set("X-Test-User", "admin-user")
		req.Header.Set("X-Test-Tenant", tenantID)
	}

	// ── 1. Tyres Web Dashboard & Lifecycle Forms ────────────────────────
	t.Run("Tyres_Dashboard_And_Lifecycle", func(t *testing.T) {
		// GET /tyres dashboard
		req := httptest.NewRequest("GET", "/tyres", nil)
		userHdr(req)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Tyre Management")

		// POST /tyres (create tyre)
		form := url.Values{
			"serial_number":          {"MRF-WEB-001"},
			"brand":                  {"MRF"},
			"model":                  {"Steel Muscle"},
			"size":                   {"295/80 R22.5"},
			"tyre_type":              {"RADIAL"},
			"initial_tread_depth_mm": {"15.0"},
			"min_tread_depth_mm":     {"3.0"},
			"purchase_cost":          {"22500.0"},
		}
		req = httptest.NewRequest("POST", "/tyres", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch tyre ID from database
		var tyreID string
		err := app.DB.QueryRow(`SELECT id FROM tyres WHERE tenant_id = $1 AND serial_number = 'MRF-WEB-001'`, tenantID).Scan(&tyreID)
		require.NoError(t, err)

		// POST /tyres/{id}/mount
		mountForm := url.Values{
			"vehicle_id":    {"veh-p1"},
			"axle_position": {"FRONT_LEFT"},
			"odometer_km":   {"50000"},
		}
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/mount", strings.NewReader(mountForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /tyres/{id}/inspect
		inspectForm := url.Values{
			"odometer_km":    {"55000"},
			"tread_depth_mm": {"12.5"},
			"pressure_psi":   {"115"},
			"condition":      {"GOOD"},
			"action_taken":   {"Rotated and balanced"},
		}
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/inspect", strings.NewReader(inspectForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /tyres/{id}/dismount
		dismountForm := url.Values{
			"odometer_km": {"60000"},
			"reason":      {"Scheduled retread"},
		}
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/dismount", strings.NewReader(dismountForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /tyres/{id}/retread
		retreadForm := url.Values{
			"action":             {"SEND"},
			"vendor_name":        {"National Retreaders"},
			"cost":               {"4500"},
			"new_tread_depth_mm": {"13.0"},
		}
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/retread", strings.NewReader(retreadForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Receive retread
		retreadForm.Set("action", "RECEIVE")
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/retread", strings.NewReader(retreadForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /tyres/{id}/scrap
		scrapForm := url.Values{
			"reason":          {"Casing cracked beyond retreading"},
			"recovery_amount": {"800"},
		}
		req = httptest.NewRequest("POST", "/tyres/"+tyreID+"/scrap", strings.NewReader(scrapForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
	})

	// ── 2. Parts & Spares Inventory Web Dashboard ───────────────────────
	t.Run("Inventory_Dashboard_And_Ledger", func(t *testing.T) {
		// GET /inventory dashboard
		req := httptest.NewRequest("GET", "/inventory", nil)
		userHdr(req)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Parts &amp; Spares Inventory")

		// POST /inventory/parts (create spare part)
		form := url.Values{
			"part_number":      {"BRK-PAD-W01"},
			"name":             {"Heavy Duty Brake Pad"},
			"category":         {"BRAKES"},
			"uom":              {"PIECES"},
			"cost_price":       {"3200"},
			"selling_price":    {"3800"},
			"min_stock_level":  {"5"},
			"reorder_quantity": {"10"},
			"oem_brand":        {"Bosch"},
		}
		req = httptest.NewRequest("POST", "/inventory/parts", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch part ID
		var partID string
		err := app.DB.QueryRow(`SELECT id FROM parts WHERE tenant_id = $1 AND part_number = 'BRK-PAD-W01'`, tenantID).Scan(&partID)
		require.NoError(t, err)

		// POST /inventory/stocks/adjust
		adjustForm := url.Values{
			"part_id":              {partID},
			"facility_id":          {"fac-p1"},
			"new_quantity_on_hand": {"20"},
			"reason":               {"Initial stock intake audit"},
		}
		req = httptest.NewRequest("POST", "/inventory/stocks/adjust", strings.NewReader(adjustForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /inventory/issue
		issueForm := url.Values{
			"part_id":     {partID},
			"facility_id": {"fac-p1"},
			"quantity":    {"2"},
			"vehicle_id":  {"veh-p1"},
			"notes":       {"Replaced worn pads"},
		}
		req = httptest.NewRequest("POST", "/inventory/issue", strings.NewReader(issueForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /inventory/return
		returnForm := url.Values{
			"part_id":     {partID},
			"facility_id": {"fac-p1"},
			"quantity":    {"1"},
			"vehicle_id":  {"veh-p1"},
			"notes":       {"Extra pad unused"},
		}
		req = httptest.NewRequest("POST", "/inventory/return", strings.NewReader(returnForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
	})

	// ── 3. Freight Audit Web Dashboard ──────────────────────────────────
	t.Run("FreightAudit_Dashboard_And_Workflow", func(t *testing.T) {
		// GET /freight-audit dashboard
		req := httptest.NewRequest("GET", "/freight-audit", nil)
		userHdr(req)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Freight Audit &amp; Overbilling")

		// POST /freight-audit/rates
		rateForm := url.Values{
			"contract_number":         {"RC-DEL-MUM-01"},
			"name":                    {"Delhi Mumbai 32ft MXL"},
			"billing_basis":           {"PER_KM"},
			"rate":                    {"42.0"},
			"min_charge":              {"5000.0"},
			"free_detention_hours":    {"24"},
			"detention_rate_per_hour": {"150"},
		}
		req = httptest.NewRequest("POST", "/freight-audit/rates", strings.NewReader(rateForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /freight-audit/audits
		auditForm := url.Values{
			"vendor_name":        {"Rapid Trans"},
			"vehicle_number":     {"DL01TY1001"},
			"billed_distance_km": {"550.0"},
			"gps_distance_km":    {"460.0"},
			"billed_rate":        {"50.0"},
			"billed_total":       {"27500.0"},
			"notes":              {"Detour unverified"},
		}
		req = httptest.NewRequest("POST", "/freight-audit/audits", strings.NewReader(auditForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch audit ID
		var auditID string
		err := app.DB.QueryRow(`SELECT id FROM freight_audits WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 1`, tenantID).Scan(&auditID)
		require.NoError(t, err)

		// POST /freight-audit/audits/{id}/approve-deduction
		apprForm := url.Values{
			"deduction_amount": {"4500.0"},
			"notes":            {"Approved debit note for inflated mileage"},
		}
		req = httptest.NewRequest("POST", "/freight-audit/audits/"+auditID+"/approve-deduction", strings.NewReader(apprForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /freight-audit/audits/{id}/settle
		settleForm := url.Values{
			"notes": {"Settled after deducting debit note"},
		}
		req = httptest.NewRequest("POST", "/freight-audit/audits/"+auditID+"/settle", strings.NewReader(settleForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
	})

	// ── 4. Report Builder Web Dashboard ─────────────────────────────────
	t.Run("ReportBuilder_Dashboard_And_Exec", func(t *testing.T) {
		// GET /report-builder dashboard
		req := httptest.NewRequest("GET", "/report-builder", nil)
		userHdr(req)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Custom Report Builder")

		// POST /report-builder/reports
		repForm := url.Values{
			"name":        {"Fleet Odometer Rollup"},
			"dataset":     {"VEHICLES"},
			"chart_type":  {"TABLE"},
			"description": {"Tracks total odometer per vehicle"},
		}
		req = httptest.NewRequest("POST", "/report-builder/reports", strings.NewReader(repForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch report ID
		var repID string
		err := app.DB.QueryRow(`SELECT id FROM custom_reports WHERE tenant_id = $1 AND name = 'Fleet Odometer Rollup'`, tenantID).Scan(&repID)
		require.NoError(t, err)

		// POST /report-builder/reports/{id}/run -> CSV download
		runForm := url.Values{
			"format": {"CSV"},
		}
		req = httptest.NewRequest("POST", "/report-builder/reports/"+repID+"/run", strings.NewReader(runForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/csv", rec.Header().Get("Content-Type"))

		// POST /report-builder/schedules
		schForm := url.Values{
			"report_id":  {repID},
			"name":       {"Daily 8AM Rollup"},
			"frequency":  {"DAILY"},
			"recipients": {"dispatcher@flyfleet.io"},
		}
		req = httptest.NewRequest("POST", "/report-builder/schedules", strings.NewReader(schForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
	})

	// ── 5. RFQ Lane Bidding Web Dashboard ───────────────────────────────
	t.Run("RFQ_Dashboard_And_Negotiation", func(t *testing.T) {
		// GET /rfq dashboard
		req := httptest.NewRequest("GET", "/rfq", nil)
		userHdr(req)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "RFQ Lane Bidding")

		// POST /rfq (create tender)
		now := time.Now()
		form := url.Values{
			"title":                 {"Delhi to Ahmedabad FMCG Dedicated"},
			"origin_city":           {"Delhi NCR"},
			"destination_city":      {"Ahmedabad Hub"},
			"vehicle_type_required": {"32ft MXL"},
			"cargo_weight_tons":     {"16.0"},
			"target_rate":           {"45000.0"},
			"max_acceptable_rate":   {"50000.0"},
			"cargo_type":            {"FMCG Cartons"},
			"pickup_date":           {now.Add(24 * time.Hour).Format(time.RFC3339)},
			"delivery_deadline":     {now.Add(72 * time.Hour).Format(time.RFC3339)},
			"bid_deadline":          {now.Add(12 * time.Hour).Format(time.RFC3339)},
		}
		req = httptest.NewRequest("POST", "/rfq", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch RFQ ID
		var rfqID string
		err := app.DB.QueryRow(`SELECT id FROM rfqs WHERE tenant_id = $1 AND title = 'Delhi to Ahmedabad FMCG Dedicated'`, tenantID).Scan(&rfqID)
		require.NoError(t, err)

		// POST /rfq/{id}/bids
		bidForm := url.Values{
			"carrier_name":       {"Express Haulers Ltd"},
			"bid_amount":         {"47000.0"},
			"transit_days":       {"2"},
			"vehicles_available": {"2"},
			"remarks":            {"GPS equipped fleet ready to load"},
		}
		req = httptest.NewRequest("POST", "/rfq/"+rfqID+"/bids", strings.NewReader(bidForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// Fetch bid ID
		var bidID string
		err = app.DB.QueryRow(`SELECT id FROM rfq_bids WHERE tenant_id = $1 AND rfq_id = $2`, tenantID, rfqID).Scan(&bidID)
		require.NoError(t, err)

		// POST /rfq/{id}/bids/{bidId}/counter
		counterForm := url.Values{
			"counter_rate": {"46000.0"},
			"remarks":      {"Can we close at 46k with guaranteed weekly volumes?"},
		}
		req = httptest.NewRequest("POST", "/rfq/"+rfqID+"/bids/"+bidID+"/counter", strings.NewReader(counterForm.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)

		// POST /rfq/{id}/award/{bidId}
		req = httptest.NewRequest("POST", "/rfq/"+rfqID+"/award/"+bidID, nil)
		userHdr(req)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusSeeOther, rec.Code)
	})
}
