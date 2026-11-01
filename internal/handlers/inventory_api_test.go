package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"transport-app/internal/inventory"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupInventoryTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:inventory:read":   true,
			"admin-user:inventory:write":  true,
			"viewer-user:inventory:read":  true,
			"viewer-user:inventory:write": false,
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

	app.Inventory = &InventoryHandlers{App: app}

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

	app.Inventory.RegisterAPIRoutes(r)
	return app, r
}

func TestInventoryAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupInventoryTestApp(t)

	tenantA := "tenant-inv-a"
	tenantB := "tenant-inv-b"

	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'Tenant A', 'tenant-a'), ($2, 'Tenant B', 'tenant-b')`, tenantA, tenantB)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type) VALUES
		('fac-delhi', $1, 'DEP-DEL-01', 'Delhi Workshop', 'workshop'),
		('fac-mumbai', $1, 'DEP-MUM-01', 'Mumbai Hub', 'depot'),
		('fac-blr', $2, 'DEP-BLR-01', 'Bangalore Hub', 'depot')`, tenantA, tenantB)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity)
		VALUES ('veh-inv-1', $1, 'DL01IN0001', 'DL01IN0001', 'truck', 12000)`, tenantA)
	require.NoError(t, err)

	// 1. Viewer cannot create part (RBAC inventory:write required)
	bodyCreate := `{
		"part_number": "FLT-FUEL-HD",
		"name": "Heavy Duty Fuel Filter Water Separator",
		"category": "FILTERS",
		"uom": "PIECES",
		"min_stock_level": 5.0,
		"reorder_quantity": 20.0,
		"cost_price": 750.0,
		"selling_price": 950.0,
		"oem_brand": "Fleetguard"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/parts", bytes.NewBufferString(bodyCreate))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 2. Admin creates part in Tenant A
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/parts", bytes.NewBufferString(bodyCreate))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var createResp struct {
		Part inventory.Part `json:"part"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	part1 := createResp.Part
	assert.NotEmpty(t, part1.ID)
	assert.Equal(t, "FLT-FUEL-HD", part1.PartNumber)
	assert.Equal(t, 750.0, part1.CostPrice)

	// Admin creates a second part
	bodyCreate2 := `{
		"part_number": "BRK-LINING-RR",
		"name": "Rear Brake Lining Set",
		"category": "BRAKES",
		"uom": "SETS",
		"cost_price": 2200.0
	}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/parts", bytes.NewBufferString(bodyCreate2))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var createResp2 struct {
		Part inventory.Part `json:"part"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp2))
	part2 := createResp2.Part

	// 3. List parts (Tenant A sees 2 parts, Tenant B sees 0)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/inventory/parts?category=FILTERS", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Parts []inventory.Part `json:"parts"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 1, listResp.Total)
	assert.Equal(t, part1.ID, listResp.Parts[0].ID)

	// Cross-tenant: Tenant B sees 0 parts
	req = httptest.NewRequest(http.MethodGet, "/api/v1/inventory/parts", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Total)

	// 4. Get and Update Part
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/inventory/parts/%s", part1.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	updatePayload := `{"name": "Heavy Duty Fuel Filter Water Separator (Gen 2)"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/inventory/parts/%s", part1.ID), bytes.NewBufferString(updatePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 5. Create Purchase Bill (GRN) with 2 items for Delhi Workshop
	billPayload := fmt.Sprintf(`{
		"bill_number": "BILL-FLEET-2026-001",
		"vendor_name": "Fleet Spares India Ltd",
		"vendor_gstin": "07AABCF1234F1Z1",
		"facility_id": "fac-delhi",
		"items": [
			{"part_id": "%s", "quantity": 30.0, "unit_cost": 750.0, "tax_rate": 18.0},
			{"part_id": "%s", "quantity": 10.0, "unit_cost": 2200.0, "tax_rate": 18.0}
		]
	}`, part1.ID, part2.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/bills", bytes.NewBufferString(billPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var billResp struct {
		Bill inventory.PartPurchaseBill `json:"bill"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &billResp))
	bill := billResp.Bill
	assert.Equal(t, inventory.BillStatusDraft, bill.Status)
	assert.Len(t, bill.Items, 2)

	// 6. Receive Purchase Bill (credits stock at Delhi Workshop)
	receivePayload := `{"received_by": "Ravi Workshop Storekeeper"}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/inventory/bills/%s/receive", bill.ID), bytes.NewBufferString(receivePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var recBillResp struct {
		Bill inventory.PartPurchaseBill `json:"bill"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recBillResp))
	assert.Equal(t, inventory.BillStatusReceived, recBillResp.Bill.Status)
	assert.Equal(t, "Ravi Workshop Storekeeper", recBillResp.Bill.ReceivedBy)

	// 7. Verify stock levels at Delhi Workshop
	req = httptest.NewRequest(http.MethodGet, "/api/v1/inventory/stocks?facility_id=fac-delhi", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var stockResp struct {
		Stocks []inventory.PartStock `json:"stocks"`
		Total  int                   `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &stockResp))
	assert.Equal(t, 2, stockResp.Total)

	// 8. Issue 5 fuel filters to veh-inv-1
	issuePayload := fmt.Sprintf(`{
		"part_id": "%s",
		"facility_id": "fac-delhi",
		"quantity": 5.0,
		"vehicle_id": "veh-inv-1",
		"performed_by": "Ravi Workshop Storekeeper",
		"notes": "PM Service A"
	}`, part1.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/issue", bytes.NewBufferString(issuePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var issueResp struct {
		Transaction inventory.PartTransaction `json:"transaction"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &issueResp))
	assert.Equal(t, inventory.TxWorkOrderIssue, issueResp.Transaction.TransactionType)
	assert.Equal(t, 25.0, issueResp.Transaction.BalanceAfter)

	// Attempting to issue more than remaining stock (e.g. 50 filters when 25 available)
	overflowPayload := fmt.Sprintf(`{
		"part_id": "%s",
		"facility_id": "fac-delhi",
		"quantity": 50.0
	}`, part1.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/issue", bytes.NewBufferString(overflowPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// 9. Return 1 unused filter back to stock
	returnPayload := fmt.Sprintf(`{
		"part_id": "%s",
		"facility_id": "fac-delhi",
		"quantity": 1.0,
		"vehicle_id": "veh-inv-1",
		"notes": "Unused return"
	}`, part1.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/return", bytes.NewBufferString(returnPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 10. Cycle count adjustment
	adjustPayload := fmt.Sprintf(`{
		"part_id": "%s",
		"facility_id": "fac-delhi",
		"new_quantity_on_hand": 30.0,
		"reason": "Recounted shelf"
	}`, part1.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/stocks/adjust", bytes.NewBufferString(adjustPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 11. Inter-facility transfer (Delhi -> Mumbai)
	transferPayload := fmt.Sprintf(`{
		"part_id": "%s",
		"from_facility_id": "fac-delhi",
		"to_facility_id": "fac-mumbai",
		"quantity": 10.0,
		"sto_id": "sto-transfer-01"
	}`, part1.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inventory/transfer", bytes.NewBufferString(transferPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 12. Check transaction audit trail
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/inventory/transactions?part_id=%s", part1.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var txListResp struct {
		Transactions []inventory.PartTransaction `json:"transactions"`
		Total        int                         `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &txListResp))
	assert.GreaterOrEqual(t, txListResp.Total, 4) // Purchase receipt, Issue, Return, Adjust, STO Out

	// 13. Inventory summary
	req = httptest.NewRequest(http.MethodGet, "/api/v1/inventory/summary", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var sumResp struct {
		Summary inventory.InventorySummary `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sumResp))
	assert.Equal(t, 2, sumResp.Summary.TotalParts)
	assert.Greater(t, sumResp.Summary.TotalValuation, 0.0)
	assert.Equal(t, 2, sumResp.Summary.ActiveFacilityCount) // Delhi and Mumbai
}
