package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/experiments"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	vehicleagg "transport-app/internal/vehicle/domain/aggregate"
)

func TestOnboardPage_AccessControl(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	// 1. Unauthenticated -> Redirect to /dashboard
	req := httptest.NewRequest(http.MethodGet, "/company/onboard", nil)
	rr := httptest.NewRecorder()
	sh.OnboardPage(rr, req)
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/dashboard", rr.Header().Get("Location"))

	// 2. Authenticated non-admin (e.g. driver) -> Redirect to /dashboard
	req = httptest.NewRequest(http.MethodGet, "/company/onboard", nil)
	sessionDriver := &auth.SessionData{UserID: "driver-1", Role: "driver"}
	ctx := context.WithValue(req.Context(), auth.ContextUser, sessionDriver)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()
	sh.OnboardPage(rr, req)
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/dashboard", rr.Header().Get("Location"))

	// 3. Authenticated admin -> Renders OK (200)
	req = httptest.NewRequest(http.MethodGet, "/company/onboard", nil)
	sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	ctx = context.WithValue(req.Context(), auth.ContextUser, sessionAdmin)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()
	sh.OnboardPage(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Fleet Setup Wizard")
	assert.Contains(t, rr.Body.String(), "Company Identity &amp; Compliance Details")
}

func TestSaveOnboard_CompanyOnly_RedirectsToDashboard(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	form := url.Values{}
	form.Set("company_name", "Apex Speed Cargo")
	form.Set("email", "ops@apexspeed.test")
	form.Set("phone", "+91 98765 00000")
	form.Set("gst_number", "27ABCDE1234F1Z5")
	form.Set("currency", "INR")
	form.Set("timezone", "Asia/Kolkata")
	form.Set("address", "100 Highway Logistics Park")

	req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	ctx := context.WithValue(req.Context(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	sh.SaveOnboard(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/dashboard", rr.Header().Get("Location"))

	// Verify flash cookie is set
	flashFound := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "flash_success" && strings.Contains(c.Value, "Onboarding completed") {
			flashFound = true
			break
		}
	}
	assert.True(t, flashFound, "flash_success cookie should be set upon completion")

	// Verify settings saved
	settings, err := app.Services.Settings.GetSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Apex Speed Cargo", settings.CompanyName)
	assert.Equal(t, "INR", settings.Currency)
	assert.Equal(t, "27ABCDE1234F1Z5", *settings.GSTNumber)
}

func TestSaveOnboard_WithFirstVehicleAndDriver_ProvisionsAndRedirectsToTracking(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	form := url.Values{}
	// Step 1: Company
	form.Set("company_name", "Global Movers Express")
	form.Set("email", "contact@globalmovers.test")
	form.Set("phone", "+91 98111 22334")
	form.Set("address", "Plot 42, Transport Nagar")
	form.Set("gst_number", "07AAAAA0000A1Z5")
	form.Set("currency", "INR")

	// Step 2: First Vehicle
	form.Set("vehicle_registration", "DL01AB9999")
	form.Set("vehicle_model", "Tata 407 LPT")
	form.Set("vehicle_type", string(vehicleagg.VehicleTypeMiniTruck))
	form.Set("fuel_type", "diesel")
	form.Set("capacity", "3000")

	// Step 3: First Driver
	form.Set("driver_name", "Suresh Sharma")
	form.Set("driver_phone", "+91 99887 76655")
	form.Set("driver_license", "DL-0420110099887")
	form.Set("experience", "6")

	req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sessionAdmin := &auth.SessionData{UserID: "admin-2", Role: "admin"}
	ctx := context.WithValue(req.Context(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	sh.SaveOnboard(rr, req)

	// Since vehicle was provided, redirection goes to /tracking
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/tracking", rr.Header().Get("Location"))

	// Verify vehicle exists in database
	var vehicleCount int
	err := app.DB.QueryRow(`SELECT count(*) FROM vehicles WHERE registration_number = 'DL01AB9999'`).Scan(&vehicleCount)
	require.NoError(t, err)
	assert.Equal(t, 1, vehicleCount, "vehicle DL01AB9999 should be provisioned")

	// Verify driver exists in database
	var driverCount int
	err = app.DB.QueryRow(`SELECT count(*) FROM drivers WHERE phone = '+91 99887 76655'`).Scan(&driverCount)
	require.NoError(t, err)
	assert.Equal(t, 1, driverCount, "driver Suresh Sharma should be provisioned")
}

func TestSaveOnboard_MandatoryFieldValidations(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	tests := []struct {
		name          string
		companyName   string
		email         string
		phone         string
		address       string
		gst           string
		pan           string
		expectedTitle string
	}{
		{
			name:          "missing company_name",
			companyName:   "",
			email:         "ops@test.com",
			phone:         "+91 98765 43210",
			address:       "123 Main St",
			expectedTitle: "Company Name Required",
		},
		{
			name:          "missing address",
			companyName:   "Acme Logistics",
			email:         "ops@test.com",
			phone:         "+91 98765 43210",
			address:       "",
			expectedTitle: "Address Required",
		},
		{
			name:          "missing phone",
			companyName:   "Acme Logistics",
			email:         "ops@test.com",
			phone:         "",
			address:       "123 Main St",
			expectedTitle: "Phone Number Required",
		},
		{
			name:          "missing email",
			companyName:   "Acme Logistics",
			email:         "",
			phone:         "+91 98765 43210",
			address:       "123 Main St",
			expectedTitle: "Email Required",
		},
		{
			name:          "invalid GST format",
			companyName:   "Acme Logistics",
			email:         "ops@test.com",
			phone:         "+91 98765 43210",
			address:       "123 Main St",
			gst:           "INVALID-GSTIN-123",
			expectedTitle: "Invalid GST Number",
		},
		{
			name:          "invalid PAN format",
			companyName:   "Acme Logistics",
			email:         "ops@test.com",
			phone:         "+91 98765 43210",
			address:       "123 Main St",
			pan:           "INVALIDPAN99",
			expectedTitle: "Invalid PAN Number",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			if tc.companyName != "" {
				form.Set("company_name", tc.companyName)
			}
			if tc.email != "" {
				form.Set("email", tc.email)
			}
			if tc.phone != "" {
				form.Set("phone", tc.phone)
			}
			if tc.address != "" {
				form.Set("address", tc.address)
			}
			if tc.gst != "" {
				form.Set("gst_number", tc.gst)
			}
			if tc.pan != "" {
				form.Set("pan_number", tc.pan)
			}

			req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
			ctx := context.WithValue(req.Context(), auth.ContextUser, sessionAdmin)
			ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			sh.SaveOnboard(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Contains(t, rr.Body.String(), tc.expectedTitle)
		})
	}
}

func TestSaveOnboard_3TierClassification(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	ctx := context.WithValue(context.Background(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))

	// 1. Tier 1: GST Registered Enterprise (15-char GSTIN, auto-extracts PAN)
	t.Run("Tier 1: GST Enterprise auto-extracts PAN", func(t *testing.T) {
		form := url.Values{}
		form.Set("company_name", "National Logistics Ltd")
		form.Set("email", "admin@nationallogistics.test")
		form.Set("phone", "+91 98765 11111")
		form.Set("address", "Expressway Hub, Sector 10")
		form.Set("gst_number", "27ABCDE1234F1Z5") // PAN is ABCDE1234F

		req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		sh.SaveOnboard(rr, req)
		assert.Equal(t, http.StatusSeeOther, rr.Code)

		settings, err := app.Services.Settings.GetSettings(ctx)
		require.NoError(t, err)
		assert.Equal(t, "National Logistics Ltd", settings.CompanyName)
		assert.Equal(t, "27ABCDE1234F1Z5", *settings.GSTNumber)
		require.NotNil(t, settings.PanNumber)
		assert.Equal(t, "ABCDE1234F", *settings.PanNumber, "PAN should be automatically extracted from GSTIN")
		assert.Equal(t, 1, settings.TaxTier(), "Should classify as Tier 1")
		assert.Equal(t, "TAX INVOICE", settings.LegalInvoiceTitle())
	})

	// 2. Tier 2: Non-GST with PAN (Section 31(3)(c) Bill of Supply & Sec 194C(6) TDS declaration)
	t.Run("Tier 2: Non-GST with PAN", func(t *testing.T) {
		form := url.Values{}
		form.Set("company_name", "Kisan Roadlines")
		form.Set("email", "kisan@roadlines.test")
		form.Set("phone", "+91 98765 22222")
		form.Set("address", "Mandi Gate No 2, Indore")
		form.Set("gst_number", "")
		form.Set("pan_number", "BKZPK9876L")

		req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		sh.SaveOnboard(rr, req)
		assert.Equal(t, http.StatusSeeOther, rr.Code)

		settings, err := app.Services.Settings.GetSettings(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Kisan Roadlines", settings.CompanyName)
		assert.Empty(t, *settings.GSTNumber)
		require.NotNil(t, settings.PanNumber)
		assert.Equal(t, "BKZPK9876L", *settings.PanNumber)
		assert.Equal(t, 2, settings.TaxTier(), "Should classify as Tier 2")
		assert.Equal(t, "BILL OF SUPPLY / FREIGHT BILL", settings.LegalInvoiceTitle())
	})

	// 3. Tier 3: Micro / Unorganized Transporter (No GST & No PAN, Rule 54(3) Bilty)
	t.Run("Tier 3: Micro Transporter without GST or PAN", func(t *testing.T) {
		form := url.Values{}
		form.Set("company_name", "Raju Tempo Service")
		form.Set("email", "raju@tempo.test")
		form.Set("phone", "+91 98765 33333")
		form.Set("address", "Near Railway Station, Patna")
		form.Set("gst_number", "")
		form.Set("pan_number", "")

		req := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		sh.SaveOnboard(rr, req)
		assert.Equal(t, http.StatusSeeOther, rr.Code)

		settings, err := app.Services.Settings.GetSettings(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Raju Tempo Service", settings.CompanyName)
		assert.Empty(t, *settings.GSTNumber)
		assert.Empty(t, *settings.PanNumber)
		assert.Equal(t, 3, settings.TaxTier(), "Should classify as Tier 3")
		assert.Equal(t, "CONSIGNMENT FREIGHT BILL", settings.LegalInvoiceTitle())
	})
}

func TestDashboard_ComplianceRouteGuarding(t *testing.T) {
	app := newRegisterTestApp(t)
	dh := &DashboardHandlers{App: app}
	app.Dashboard = dh
	app.Experiments = experiments.NewRecorder(app.DB)
	sh := &SettingsHandlers{App: app}

	// 1. Initial / unconfigured state -> Accessing dashboard redirects to /company/onboard
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	ctx := context.WithValue(req.Context(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	dh.Index(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/company/onboard", rr.Header().Get("Location"))

	// Check flash error cookie
	var flashErrCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "flash_error" {
			flashErrCookie = c
			break
		}
	}
	require.NotNil(t, flashErrCookie)
	assert.Contains(t, flashErrCookie.Value, "Please complete mandatory company compliance details to unlock fleet operations.")

	// 2. Submit valid Step 1 compliance
	form := url.Values{}
	form.Set("company_name", "Fleet Masters Corp")
	form.Set("email", "ops@fleetmasters.test")
	form.Set("phone", "+91 98765 11223")
	form.Set("address", "Sector 18, Transport Hub")
	form.Set("gst_number", "27ABCDE1234F1Z5")

	saveReq := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveReq = saveReq.WithContext(ctx)

	saveRR := httptest.NewRecorder()
	sh.SaveOnboard(saveRR, saveReq)
	assert.Equal(t, http.StatusSeeOther, saveRR.Code)

	// 3. Now dashboard renders 200 OK because Step 1 compliance is complete
	dashReq := httptest.NewRequest(http.MethodGet, "/dashboard", nil).WithContext(ctx)
	dashRR := httptest.NewRecorder()
	dh.Index(dashRR, dashReq)

	assert.Equal(t, http.StatusOK, dashRR.Code)
	assert.Contains(t, dashRR.Body.String(), "Dashboard")
}

func TestOperationalRoutes_ComplianceGate(t *testing.T) {
	app := newRegisterTestApp(t)
	sh := &SettingsHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
			ctx := context.WithValue(r.Context(), auth.ContextUser, sessionAdmin)
			ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.RequireCompanyCompliance(app.Services.Settings))

	routes := []string{
		"/dashboard",
		"/tracking",
		"/bookings",
		"/trips",
		"/vehicles",
		"/drivers",
		"/maintenance",
		"/geofences",
		"/telemetry",
		"/customers",
		"/routes",
		"/invoices",
		"/payments",
		"/kharcha",
		"/settlements",
	}

	for _, route := range routes {
		r.Get(route, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("unlocked: " + route))
		})
	}

	// 1. Initial / unconfigured state: all operational routes must 303 Redirect to /company/onboard
	for _, route := range routes {
		t.Run("locked: "+route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusSeeOther, rr.Code, "Route %s should redirect when company compliance is incomplete", route)
			assert.Equal(t, "/company/onboard", rr.Header().Get("Location"))

			var flashErrCookie *http.Cookie
			for _, c := range rr.Result().Cookies() {
				if c.Name == "flash_error" {
					flashErrCookie = c
					break
				}
			}
			require.NotNil(t, flashErrCookie, "Route %s should set flash_error cookie", route)
			assert.Contains(t, flashErrCookie.Value, "Please complete mandatory company compliance details to unlock fleet operations.")
		})
	}

	// 2. Submit Step 1 compliance details
	sessionAdmin := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	ctx := context.WithValue(context.Background(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-1"))

	form := url.Values{}
	form.Set("company_name", "Avandab Fleet Ops Pvt Ltd")
	form.Set("email", "ops@avandabfleet.com")
	form.Set("phone", "+91 98765 43210")
	form.Set("address", "Plot 101, Cargo Complex, Sector 20")
	form.Set("gst_number", "27ABCDE1234F1Z5")

	saveReq := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveReq = saveReq.WithContext(ctx)

	saveRR := httptest.NewRecorder()
	sh.SaveOnboard(saveRR, saveReq)
	assert.Equal(t, http.StatusSeeOther, saveRR.Code)

	// 3. Verify all operational routes unlock immediately (200 OK)
	for _, route := range routes {
		t.Run("unlocked: "+route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Route %s should be unlocked with 200 OK after compliance completed", route)
			assert.Equal(t, "unlocked: "+route, rr.Body.String())
		})
	}
}

func TestOperationalRoutes_ComplianceGate_NonGST_Unlocks(t *testing.T) {
	app := newRegisterTestApp(t)
	// Tenant-scoped settings (migration 00125) enforce the tenants FK on
	// write — register the synthetic tenant like every other tenant-aware test.
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-non-gst', 'Non GST Fleet', 'non-gst-fleet')`)
	require.NoError(t, err)
	sh := &SettingsHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionAdmin := &auth.SessionData{UserID: "admin-non-gst", Role: "admin"}
			ctx := context.WithValue(r.Context(), auth.ContextUser, sessionAdmin)
			ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-non-gst"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.RequireCompanyCompliance(app.Services.Settings))

	routes := []string{
		"/dashboard",
		"/tracking",
		"/bookings",
		"/trips",
		"/vehicles",
		"/drivers",
		"/maintenance",
		"/geofences",
		"/telemetry",
		"/customers",
		"/routes",
		"/invoices",
		"/payments",
		"/kharcha",
		"/settlements",
	}

	for _, route := range routes {
		r.Get(route, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("unlocked: " + route))
		})
	}

	// 1. Submit Step 1 compliance WITHOUT GSTIN (Non-GST / RCM fleet owner)
	sessionAdmin := &auth.SessionData{UserID: "admin-non-gst", Role: "admin"}
	ctx := context.WithValue(context.Background(), auth.ContextUser, sessionAdmin)
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID("tenant-non-gst"))

	form := url.Values{}
	form.Set("company_name", "Singh Roadways")
	form.Set("email", "singh@roadways.local")
	form.Set("phone", "+91 98111 22233")
	form.Set("address", "Shop 4, Transport Nagar, Ludhiana")
	form.Set("gst_number", "") // Empty GSTIN - non-GST / RCM mode

	saveReq := httptest.NewRequest(http.MethodPost, "/company/onboard", strings.NewReader(form.Encode()))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveReq = saveReq.WithContext(ctx)

	saveRR := httptest.NewRecorder()
	sh.SaveOnboard(saveRR, saveReq)
	assert.Equal(t, http.StatusSeeOther, saveRR.Code)

	// 2. Verify all operational routes unlock immediately (200 OK)
	for _, route := range routes {
		t.Run("non_gst_unlocked: "+route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Route %s should unlock under Non-GST mode", route)
			assert.Equal(t, "unlocked: "+route, rr.Body.String())
		})
	}
}
