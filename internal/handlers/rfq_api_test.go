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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/rfq"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupRFQTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:rfq:read":         true,
			"admin-user:rfq:write":        true,
			"admin-user:loadboard:read":   true,
			"admin-user:loadboard:write":  true,
			"viewer-user:rfq:read":        true,
			"viewer-user:rfq:write":       false,
			"viewer-user:loadboard:read":  true,
			"viewer-user:loadboard:write": false,
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

	app.RFQ = &RFQHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			userName := req.Header.Get("X-Test-UserName")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Name:   userName,
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	app.RFQ.RegisterAPIRoutes(r)
	return app, r
}

func TestRFQAPI_RBAC(t *testing.T) {
	app, r := setupRFQTestApp(t)
	defer app.DB.Close()

	// 1. Anonymous GET -> 401
	req := httptest.NewRequest("GET", "/api/v1/rfq", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// 2. Anonymous POST -> 401
	req = httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewBufferString(`{}`))
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// 3. Viewer GET -> 200
	req = httptest.NewRequest("GET", "/api/v1/rfq", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 4. Viewer POST -> 403 Forbidden
	req = httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewBufferString(`{"origin_city":"Mumbai"}`))
	req.Header.Set("X-Test-User", "viewer-user")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRFQAPI_FullNegotiationAndAwardLifecycle(t *testing.T) {
	app, r := setupRFQTestApp(t)
	defer app.DB.Close()

	tenantID := "tenant-rfq-1"
	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Test Logistics', 'test-logistics')`, tenantID)
	require.NoError(t, err)

	now := time.Now().UTC()
	pickup := now.Add(48 * time.Hour)
	delivery := now.Add(96 * time.Hour)
	deadline := now.Add(24 * time.Hour)

	// Step 1: Create RFQ
	createPayload := rfq.CreateRFQInput{
		Title:               "Mumbai to Delhi Container 32FT",
		OriginCity:          "Mumbai",
		DestinationCity:     "Delhi",
		VehicleTypeRequired: "32FT Container",
		CargoType:           "Electronics",
		CargoWeightTons:     14.5,
		TargetRate:          45000,
		MaxAcceptableRate:   52000,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	}
	body, _ := json.Marshal(createPayload)

	req := httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-UserName", "Dispatcher Alice")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	var createdRFQ rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &createdRFQ))
	assert.NotEmpty(t, createdRFQ.ID)
	assert.Contains(t, createdRFQ.RFQNumber, "RFQ-")
	assert.Equal(t, rfq.StatusOpen, createdRFQ.Status)
	assert.Equal(t, "Mumbai", createdRFQ.OriginCity)

	rfqID := createdRFQ.ID

	// Step 2: List RFQs with filter
	req = httptest.NewRequest("GET", "/api/v1/rfq?status=OPEN&origin_city=Mumbai", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var listResp struct {
		RFQs  []rfq.RFQ `json:"rfqs"`
		Total int       `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listResp))
	assert.Equal(t, 1, listResp.Total)
	assert.Equal(t, rfqID, listResp.RFQs[0].ID)

	// Step 3: Get single RFQ
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfqID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var fetchedRFQ rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &fetchedRFQ))
	assert.Equal(t, rfqID, fetchedRFQ.ID)

	// Step 4: Submit Bid 1 (FastLogistics)
	bid1Payload := rfq.SubmitRFQBidInput{
		CarrierID:         "carrier-fast",
		CarrierName:       "FastLogistics Ltd",
		BidAmount:         49000,
		TransitDays:       3,
		VehiclesAvailable: 2,
		Remarks:           "Immediate placement possible",
	}
	body, _ = json.Marshal(bid1Payload)
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfqID+"/bids", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var bid1 rfq.RFQBid
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bid1))
	assert.Equal(t, 49000.0, bid1.BidAmount)
	assert.Equal(t, rfq.BidStatusSubmitted, bid1.Status)

	// Step 5: Submit Bid 2 (BlueDart)
	bid2Payload := rfq.SubmitRFQBidInput{
		CarrierID:         "carrier-blue",
		CarrierName:       "BlueDart Transport",
		BidAmount:         51000,
		TransitDays:       4,
		VehiclesAvailable: 1,
	}
	body, _ = json.Marshal(bid2Payload)
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfqID+"/bids", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var bid2 rfq.RFQBid
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bid2))
	assert.Equal(t, 51000.0, bid2.BidAmount)

	// Step 6: List Bids
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfqID+"/bids", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var bidsResp struct {
		Bids []rfq.RFQBid `json:"bids"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bidsResp))
	assert.Len(t, bidsResp.Bids, 2)

	// Step 7: Shipper initiates Counter-Offer on Bid 1
	counter1Payload := rfq.SubmitCounterOfferInput{
		SenderRole:    rfq.RoleShipper,
		OfferedAmount: 47000,
		Remarks:       "Can you do 47k for prompt morning loading?",
	}
	body, _ = json.Marshal(counter1Payload)
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfqID+"/bids/"+bid1.ID+"/counter", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-UserName", "Shipper Ops")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var co1 rfq.CounterOffer
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &co1))
	assert.Equal(t, 1, co1.RoundNumber)
	assert.Equal(t, 47000.0, co1.OfferedAmount)
	assert.Equal(t, rfq.CounterStatusPending, co1.Status)

	// Step 8: Carrier counters back (Round 2)
	counter2Payload := rfq.SubmitCounterOfferInput{
		SenderRole:    rfq.RoleCarrier,
		OfferedAmount: 48000,
		Remarks:       "Best rate we can offer is 48k",
	}
	body, _ = json.Marshal(counter2Payload)
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfqID+"/bids/"+bid1.ID+"/counter", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-UserName", "FastLogistics Dispatch")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var co2 rfq.CounterOffer
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &co2))
	assert.Equal(t, 2, co2.RoundNumber)
	assert.Equal(t, 48000.0, co2.OfferedAmount)
	assert.Equal(t, rfq.CounterStatusPending, co2.Status)

	// Step 9: List Counter-Offers on Bid 1
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfqID+"/bids/"+bid1.ID+"/counters", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var countersResp struct {
		CounterOffers []rfq.CounterOffer `json:"counter_offers"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &countersResp))
	assert.Len(t, countersResp.CounterOffers, 2)
	// First round superseded
	assert.Equal(t, rfq.CounterStatusSuperseded, countersResp.CounterOffers[0].Status)
	assert.Equal(t, rfq.CounterStatusPending, countersResp.CounterOffers[1].Status)

	// Step 10: Shipper accepts Counter-Offer (Round 2)
	req = httptest.NewRequest("POST", "/api/v1/rfq/counters/"+co2.ID+"/accept", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// Step 11: Verify RFQ status is now AWARDED
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfqID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var awardedRFQ rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &awardedRFQ))
	assert.Equal(t, rfq.StatusAwarded, awardedRFQ.Status)
	assert.Equal(t, 48000.0, awardedRFQ.AwardedRate)
	assert.Equal(t, "FastLogistics Ltd", awardedRFQ.AwardedCarrierName)

	// Step 12: Verify winning bid is ACCEPTED with agreed rate, competitor bid is REJECTED
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfqID+"/bids", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &bidsResp))
	for _, b := range bidsResp.Bids {
		if b.ID == bid1.ID {
			assert.Equal(t, rfq.BidStatusAccepted, b.Status)
			assert.Equal(t, 48000.0, b.BidAmount)
		} else if b.ID == bid2.ID {
			assert.Equal(t, rfq.BidStatusRejected, b.Status)
		}
	}

	// Step 13: Submitting another bid on awarded RFQ returns 409 Conflict
	newBidPayload := rfq.SubmitRFQBidInput{
		CarrierID:   "carrier-late",
		CarrierName: "Late Transport",
		BidAmount:   46000,
	}
	body, _ = json.Marshal(newBidPayload)
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfqID+"/bids", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestRFQAPI_CloseAndCancel(t *testing.T) {
	app, r := setupRFQTestApp(t)
	defer app.DB.Close()

	tenantID := "tenant-rfq-actions"
	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Tenant Actions', 'tenant-actions')`, tenantID)
	require.NoError(t, err)

	now := time.Now().UTC()
	pickup := now.Add(24 * time.Hour)
	delivery := now.Add(48 * time.Hour)
	deadline := now.Add(12 * time.Hour)

	// Create RFQ 1 to close
	body, _ := json.Marshal(rfq.CreateRFQInput{
		Title:               "Close Test RFQ",
		OriginCity:          "Pune",
		DestinationCity:     "Surat",
		VehicleTypeRequired: "Open 20FT",
		TargetRate:          25000,
		MaxAcceptableRate:   28000,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	})
	req := httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var rfq1 rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rfq1))

	// Close RFQ 1
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfq1.ID+"/close", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfq1.ID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rfq1))
	assert.Equal(t, rfq.StatusClosed, rfq1.Status)

	// Create RFQ 2 to cancel
	body, _ = json.Marshal(rfq.CreateRFQInput{
		Title:               "Cancel Test RFQ",
		OriginCity:          "Bengaluru",
		DestinationCity:     "Chennai",
		VehicleTypeRequired: "Container 24FT",
		TargetRate:          30000,
		MaxAcceptableRate:   35000,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	})
	req = httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var rfq2 rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rfq2))

	// Cancel RFQ 2
	req = httptest.NewRequest("POST", "/api/v1/rfq/"+rfq2.ID+"/cancel", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	req = httptest.NewRequest("GET", "/api/v1/rfq/"+rfq2.ID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &rfq2))
	assert.Equal(t, rfq.StatusCancelled, rfq2.Status)
}

func TestRFQAPI_MultiTenantIsolation(t *testing.T) {
	app, r := setupRFQTestApp(t)
	defer app.DB.Close()

	tenantA := "tenant-iso-a"
	tenantB := "tenant-iso-b"
	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Tenant A', 'tenant-a'), (?, 'Tenant B', 'tenant-b')`, tenantA, tenantB)
	require.NoError(t, err)

	now := time.Now().UTC()
	pickup := now.Add(24 * time.Hour)
	delivery := now.Add(48 * time.Hour)
	deadline := now.Add(12 * time.Hour)

	// Create on Tenant A
	body, _ := json.Marshal(rfq.CreateRFQInput{
		Title:               "Tenant A Secret RFQ",
		OriginCity:          "Kolkata",
		DestinationCity:     "Patna",
		VehicleTypeRequired: "16T Truck",
		TargetRate:          22000,
		MaxAcceptableRate:   26000,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	})
	req := httptest.NewRequest("POST", "/api/v1/rfq", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)
	var createdRFQ rfq.RFQ
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &createdRFQ))

	// Tenant B tries to get it -> 404
	req = httptest.NewRequest("GET", "/api/v1/rfq/"+createdRFQ.ID, nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	// Tenant B lists RFQs -> 0
	req = httptest.NewRequest("GET", "/api/v1/rfq", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var listResp struct {
		RFQs  []rfq.RFQ `json:"rfqs"`
		Total int       `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Total)
}

func TestLoadBoardCounterOfferAPI(t *testing.T) {
	app, r := setupRFQTestApp(t)
	defer app.DB.Close()

	tenantID := "tenant-lb-counter"
	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Tenant LB', 'tenant-lb')`, tenantID)
	require.NoError(t, err)

	// Insert customer, route and booking for load board listing
	_, err = app.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES ('cust-1', 'Test Customer', '9876543210')`)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rte-lb-1', 'Nagpur', 'Hyderabad', 500, 10, 30000)`)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, tenant_id) VALUES ('bkg-lb-1', 'BKG-LB-001', 'cust-1', datetime('now'), 'rte-lb-1', '32FT', 35000, ?)`, tenantID)
	require.NoError(t, err)

	listingID := "lbl-test-101"
	_, err = app.DB.Exec(`INSERT INTO load_board_listings (id, tenant_id, booking_id, origin_city, destination_city, vehicle_type_required, target_rate, max_rate, expires_at) VALUES (?, ?, 'bkg-lb-1', 'Nagpur', 'Hyderabad', '32FT', 32000, 36000, datetime('now', '+2 days'))`, listingID, tenantID)
	require.NoError(t, err)

	bidID := "lbb-test-501"
	_, err = app.DB.Exec(`INSERT INTO load_board_bids (id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount, status) VALUES (?, ?, ?, 'carrier-apex', 'Apex Logistics', 34500, 'SUBMITTED')`, bidID, tenantID, listingID)
	require.NoError(t, err)

	// Step 1: Submit counter offer on load board bid
	counterPayload := rfq.SubmitCounterOfferInput{
		SenderRole:    rfq.RoleShipper,
		OfferedAmount: 33000,
		Remarks:       "Can offer 33,000 all inclusive",
	}
	body, _ := json.Marshal(counterPayload)
	req := httptest.NewRequest("POST", "/api/v1/loadboard/listings/"+listingID+"/bids/"+bidID+"/counter", bytes.NewReader(body))
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-UserName", "Broker Alice")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)

	var co rfq.CounterOffer
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &co))
	assert.Equal(t, rfq.EntityLoadBoard, co.EntityType)
	assert.Equal(t, 33000.0, co.OfferedAmount)
	assert.Equal(t, rfq.CounterStatusPending, co.Status)

	// Step 2: List counter offers on load board bid
	req = httptest.NewRequest("GET", "/api/v1/loadboard/listings/"+listingID+"/bids/"+bidID+"/counters", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var countersResp struct {
		CounterOffers []rfq.CounterOffer `json:"counter_offers"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &countersResp))
	assert.Len(t, countersResp.CounterOffers, 1)

	// Step 3: Accept counter offer on load board
	req = httptest.NewRequest("POST", "/api/v1/loadboard/counters/"+co.ID+"/accept", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// Verify DB state for listing and bid
	var listingStatus string
	err = app.DB.QueryRow(`SELECT status FROM load_board_listings WHERE id = ?`, listingID).Scan(&listingStatus)
	require.NoError(t, err)
	assert.Equal(t, "AWARDED", listingStatus)

	var bidStatus string
	var bidAmount float64
	err = app.DB.QueryRow(`SELECT status, bid_amount FROM load_board_bids WHERE id = ?`, bidID).Scan(&bidStatus, &bidAmount)
	require.NoError(t, err)
	assert.Equal(t, "ACCEPTED", bidStatus)
	assert.Equal(t, 33000.0, bidAmount)
}
