package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/freightaudit"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupFreightAuditTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:freight_audit:read":   true,
			"admin-user:freight_audit:write":  true,
			"viewer-user:freight_audit:read":  true,
			"viewer-user:freight_audit:write": false,
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

	app.FreightAudit = &FreightAuditHandlers{App: app}

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

	app.FreightAudit.RegisterAPIRoutes(r)
	return app, r
}

func TestFreightAuditAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupFreightAuditTestApp(t)
	defer app.DB.Close()

	tenantA := "tenant-fa-a"
	tenantB := "tenant-fa-b"

	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Tenant A', 'tenant-a'), (?, 'Tenant B', 'tenant-b')`, tenantA, tenantB)
	require.NoError(t, err)

	// 1. RBAC Gating: unauthenticated or forbidden user cannot write rate card
	ratePayload := freightaudit.CreateRateCardInput{
		ContractNumber:       "RC-MUM-DEL-01",
		Name:                 "Mumbai to Delhi Linehaul 32FT",
		VehicleType:          "32FT_MXL",
		BillingBasis:         freightaudit.BillingBasisPerKM,
		Rate:                 42.0,
		MinCharge:            4000.0,
		FreeDetentionHours:   2.0,
		DetentionRatePerHour: 300.0,
	}
	body, _ := json.Marshal(ratePayload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/rates", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "viewer-user") // viewer has read only
	req.Header.Set("X-Test-Tenant", tenantA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 2. Admin creates Rate Card in Tenant A
	req = httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/rates", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var createdCard freightaudit.RateCard
	require.NoError(t, json.NewDecoder(w.Body).Decode(&createdCard))
	assert.Equal(t, "RC-MUM-DEL-01", createdCard.ContractNumber)
	assert.Equal(t, 42.0, createdCard.Rate)

	// 3. Get Rate Card
	req = httptest.NewRequest(http.MethodGet, "/api/v1/freight-audit/rates/"+createdCard.ID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 4. Update Rate Card
	newRate := 43.5
	updatePayload := freightaudit.UpdateRateCardInput{Rate: &newRate}
	body, _ = json.Marshal(updatePayload)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/freight-audit/rates/"+createdCard.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 5. Create Freight Audit with Discrepancy
	// Billed: 1,500 km @ ₹45/km + ₹2,000 tolls = ₹69,500
	// Audited: GPS 1,420 km @ ₹43.5/km (from rate card) = ₹61,770 + FASTag tolls ₹1,500 = ₹63,270
	// Overbilling: ₹6,230
	gpsDist := 1420.0
	auditedTolls := 1500.0
	auditPayload := freightaudit.CreateAuditInput{
		RateCardID:         &createdCard.ID,
		VendorName:         "SuperFast Express",
		VehicleNumber:      "MH04CD5678",
		BilledDistanceKM:   1500.0,
		BilledRate:         45.0,
		BilledBaseFreight:  67500.0,
		BilledTollCharges:  2000.0,
		BilledTotal:        69500.0,
		GPSDistanceKM:      &gpsDist,
		AuditedTollCharges: &auditedTolls,
		Notes:              "Weekly carrier bill audit",
	}
	body, _ = json.Marshal(auditPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/audits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var createdAudit freightaudit.FreightAudit
	require.NoError(t, json.NewDecoder(w.Body).Decode(&createdAudit))
	assert.Equal(t, freightaudit.AuditStatusDiscrepancyFlagged, createdAudit.Status)
	assert.Equal(t, 6230.0, createdAudit.OverbillingAmount)
	assert.Equal(t, 80.0, createdAudit.DistanceVarianceKM)
	assert.Equal(t, 500.0, createdAudit.TollVariance)

	// 6. Get Freight Audit with Deductions
	req = httptest.NewRequest(http.MethodGet, "/api/v1/freight-audit/audits/"+createdAudit.ID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var fetchedAudit freightaudit.FreightAudit
	require.NoError(t, json.NewDecoder(w.Body).Decode(&fetchedAudit))
	assert.Len(t, fetchedAudit.Deductions, 3) // distance inflation, rate overcharge, unverified toll

	// 7. Approve Deduction (Generates Debit Note)
	deductPayload := freightaudit.ApproveDeductionInput{
		Notes: "Approved deduction for distance variance and unverified tolls",
	}
	body, _ = json.Marshal(deductPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/audits/"+createdAudit.ID+"/approve-deduction", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var approvedAudit freightaudit.FreightAudit
	require.NoError(t, json.NewDecoder(w.Body).Decode(&approvedAudit))
	assert.Equal(t, freightaudit.AuditStatusDeductionApproved, approvedAudit.Status)
	assert.Equal(t, 6230.0, approvedAudit.DeductionAmount)
	assert.Equal(t, 63270.0, approvedAudit.PayableAmount)
	assert.Contains(t, approvedAudit.DebitNoteNumber, "DN-")

	// 8. Dispute by Carrier
	disputePayload := freightaudit.DisputeAuditInput{
		Reason: "Toll plaza fastag scanner was down; driver paid cash receipt attached",
	}
	body, _ = json.Marshal(disputePayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/audits/"+createdAudit.ID+"/dispute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 9. Final Settle
	settlePayload := freightaudit.SettleAuditInput{
		Notes: "Cash receipt verified, finalized deduction",
	}
	body, _ = json.Marshal(settlePayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/freight-audit/audits/"+createdAudit.ID+"/settle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 10. Summary Metrics
	req = httptest.NewRequest(http.MethodGet, "/api/v1/freight-audit/summary", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var summary freightaudit.AuditSummary
	require.NoError(t, json.NewDecoder(w.Body).Decode(&summary))
	assert.Equal(t, 1, summary.TotalAudits)
	assert.Equal(t, 69500.0, summary.TotalBilledAmount)
	assert.Equal(t, 63270.0, summary.TotalAuditedAmount)
	assert.Equal(t, 6230.0, summary.TotalOverbillingAmount)
	assert.Equal(t, 1, summary.SettledCount)

	// 11. Tenant Isolation Check: Tenant B cannot access Tenant A audit
	req = httptest.NewRequest(http.MethodGet, "/api/v1/freight-audit/audits/"+createdAudit.ID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
