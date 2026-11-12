package rfq

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Acme Logistics'), ('tenant-2', 'Beta Transport');

	CREATE TABLE bookings (id TEXT PRIMARY KEY);
	CREATE TABLE stock_transfer_orders (id TEXT PRIMARY KEY);

	CREATE TABLE load_board_listings (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		origin_city TEXT NOT NULL,
		destination_city TEXT NOT NULL,
		vehicle_type_required TEXT NOT NULL,
		target_rate REAL NOT NULL,
		max_rate REAL NOT NULL,
		visibility TEXT NOT NULL DEFAULT 'PRIVATE',
		status TEXT NOT NULL DEFAULT 'OPEN',
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE load_board_bids (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		listing_id TEXT NOT NULL,
		carrier_id TEXT NOT NULL,
		carrier_name TEXT NOT NULL,
		bid_amount REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'SUBMITTED',
		submitted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		decided_at DATETIME
	);

	CREATE TABLE rfqs (
		id                    TEXT PRIMARY KEY,
		tenant_id             TEXT NOT NULL,
		rfq_number            TEXT NOT NULL,
		title                 TEXT NOT NULL,
		origin_city           TEXT NOT NULL,
		destination_city      TEXT NOT NULL,
		vehicle_type_required TEXT NOT NULL,
		cargo_type            TEXT NOT NULL DEFAULT '',
		cargo_weight_tons     REAL NOT NULL DEFAULT 0.0,
		target_rate           REAL NOT NULL DEFAULT 0.0,
		max_acceptable_rate   REAL NOT NULL DEFAULT 0.0,
		pickup_date           DATETIME NOT NULL,
		delivery_deadline     DATETIME NOT NULL,
		bid_deadline          DATETIME NOT NULL,
		status                TEXT NOT NULL DEFAULT 'OPEN',
		booking_id            TEXT,
		sto_id                TEXT,
		awarded_bid_id        TEXT,
		awarded_carrier_id    TEXT NOT NULL DEFAULT '',
		awarded_carrier_name  TEXT NOT NULL DEFAULT '',
		awarded_rate          REAL NOT NULL DEFAULT 0.0,
		awarded_at            DATETIME,
		created_by            TEXT NOT NULL DEFAULT '',
		created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE rfq_bids (
		id                 TEXT PRIMARY KEY,
		tenant_id          TEXT NOT NULL,
		rfq_id             TEXT NOT NULL,
		carrier_id         TEXT NOT NULL,
		carrier_name       TEXT NOT NULL,
		bid_amount         REAL NOT NULL,
		transit_days       INTEGER NOT NULL DEFAULT 1,
		vehicles_available INTEGER NOT NULL DEFAULT 1,
		remarks            TEXT NOT NULL DEFAULT '',
		status             TEXT NOT NULL DEFAULT 'SUBMITTED',
		submitted_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		decided_at         DATETIME,
		decided_by         TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE counter_offers (
		id             TEXT PRIMARY KEY,
		tenant_id      TEXT NOT NULL,
		entity_type    TEXT NOT NULL,
		rfq_id         TEXT,
		listing_id     TEXT,
		bid_id         TEXT NOT NULL,
		round_number   INTEGER NOT NULL DEFAULT 1,
		sender_role    TEXT NOT NULL,
		sender_id      TEXT NOT NULL DEFAULT '',
		sender_name    TEXT NOT NULL DEFAULT '',
		offered_amount REAL NOT NULL,
		remarks        TEXT NOT NULL DEFAULT '',
		status         TEXT NOT NULL DEFAULT 'PENDING',
		expires_at     DATETIME,
		created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		decided_at     DATETIME,
		decided_by     TEXT NOT NULL DEFAULT ''
	);
	`

	_, err = db.Exec(schema)
	require.NoError(t, err)
	return db
}

func TestRFQ_ValidationAndCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	now := time.Now().UTC()
	pickup := now.AddDate(0, 0, 2)
	delivery := now.AddDate(0, 0, 4)
	deadline := now.AddDate(0, 0, 1)

	// 1. Missing required field
	badIn := CreateRFQInput{
		Title: "Incomplete",
	}
	assert.Error(t, badIn.Validate())

	// 2. Delivery before pickup
	badDateIn := CreateRFQInput{
		Title:               "Bad Dates",
		OriginCity:          "Delhi",
		DestinationCity:     "Mumbai",
		VehicleTypeRequired: "32FT",
		PickupDate:          delivery,
		DeliveryDeadline:    pickup,
		BidDeadline:         deadline,
	}
	assert.Error(t, badDateIn.Validate())

	// 3. Max rate less than target rate
	badRateIn := CreateRFQInput{
		Title:               "Bad Rates",
		OriginCity:          "Delhi",
		DestinationCity:     "Mumbai",
		VehicleTypeRequired: "32FT",
		TargetRate:          50000,
		MaxAcceptableRate:   40000,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	}
	assert.Error(t, badRateIn.Validate())

	// 4. Valid RFQ Creation
	validIn := CreateRFQInput{
		Title:               "Delhi to Mumbai FMCG Lane",
		OriginCity:          "Delhi",
		DestinationCity:     "Mumbai",
		VehicleTypeRequired: "32FT_MXL",
		CargoType:           "FMCG",
		CargoWeightTons:     14.5,
		TargetRate:          42000.0,
		MaxAcceptableRate:   48000.0,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	}

	rfq1, err := svc.CreateRFQ(ctx, "tenant-1", "user-ops", validIn)
	require.NoError(t, err)
	assert.NotEmpty(t, rfq1.ID)
	assert.Contains(t, rfq1.RFQNumber, "RFQ-")
	assert.Equal(t, StatusOpen, rfq1.Status)

	// Second RFQ gets sequential number
	rfq2, err := svc.CreateRFQ(ctx, "tenant-1", "user-ops", validIn)
	require.NoError(t, err)
	assert.NotEqual(t, rfq1.RFQNumber, rfq2.RFQNumber)
}

func TestRFQ_BiddingAndCounterOfferLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	now := time.Now().UTC()
	pickup := now.AddDate(0, 0, 3)
	delivery := now.AddDate(0, 0, 5)
	deadline := now.AddDate(0, 0, 1)

	rfq, err := svc.CreateRFQ(ctx, "tenant-1", "user-ops", CreateRFQInput{
		Title:               "Pune to Bengaluru Auto Parts",
		OriginCity:          "Pune",
		DestinationCity:     "Bengaluru",
		VehicleTypeRequired: "24FT",
		TargetRate:          35000.0,
		MaxAcceptableRate:   40000.0,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         deadline,
	})
	require.NoError(t, err)

	// 1. Carrier 1 submits initial quote (₹42,000)
	bid1, err := svc.SubmitBid(ctx, "tenant-1", rfq.ID, SubmitRFQBidInput{
		CarrierID:         "c-1",
		CarrierName:       "Roadlines India",
		BidAmount:         42000.0,
		TransitDays:       2,
		VehiclesAvailable: 3,
		Remarks:           "Immediate placement",
	})
	require.NoError(t, err)
	assert.Equal(t, BidStatusSubmitted, bid1.Status)

	// 2. Carrier 2 submits initial quote (₹39,000)
	bid2, err := svc.SubmitBid(ctx, "tenant-1", rfq.ID, SubmitRFQBidInput{
		CarrierID:         "c-2",
		CarrierName:       "Super Express",
		BidAmount:         39000.0,
		TransitDays:       3,
		VehiclesAvailable: 1,
	})
	require.NoError(t, err)

	// Verify Bids listing sorted by price ASC
	bids, err := svc.ListBids(ctx, "tenant-1", rfq.ID)
	require.NoError(t, err)
	require.Len(t, bids, 2)
	assert.Equal(t, "c-2", bids[0].CarrierID, "Lowest quote first")

	// 3. Round 1 Counter-Offer: Shipper counters Carrier 1 with ₹37,000
	expHours := 6
	co1, err := svc.SubmitCounterOffer(ctx, "tenant-1", bid1.ID, "user-ops", "Ops Dispatcher", SubmitCounterOfferInput{
		EntityType:     EntityRFQ,
		RFQID:          &rfq.ID,
		SenderRole:     RoleShipper,
		OfferedAmount:  37000.0,
		Remarks:        "Target budget is 37k",
		ExpiresInHours: &expHours,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, co1.RoundNumber)
	assert.Equal(t, CounterStatusPending, co1.Status)

	// Verify Bid status updated to COUNTER_OFFERED and RFQ to UNDER_NEGOTIATION
	updatedBid1, err := svc.GetBid(ctx, "tenant-1", bid1.ID)
	require.NoError(t, err)
	assert.Equal(t, BidStatusCounterOffered, updatedBid1.Status)

	updatedRFQ, err := svc.GetRFQ(ctx, "tenant-1", rfq.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusUnderNegotiation, updatedRFQ.Status)

	// 4. Round 2 Counter-Offer: Carrier 1 counters back with ₹38,000
	co2, err := svc.SubmitCounterOffer(ctx, "tenant-1", bid1.ID, "c-1", "Roadlines Manager", SubmitCounterOfferInput{
		EntityType:     EntityRFQ,
		RFQID:          &rfq.ID,
		SenderRole:     RoleCarrier,
		OfferedAmount:  38000.0,
		Remarks:        "Final best offer ₹38,000",
		ExpiresInHours: &expHours,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, co2.RoundNumber)

	// Round 1 counter should now be SUPERSEDED
	oldCO, err := svc.repo.GetCounterOfferByID(ctx, "tenant-1", co1.ID)
	require.NoError(t, err)
	assert.Equal(t, CounterStatusSuperseded, oldCO.Status)

	// 5. Shipper accepts Carrier 1's Round 2 counter-offer (₹38,000)
	err = svc.AcceptCounterOffer(ctx, "tenant-1", co2.ID, "user-ops")
	require.NoError(t, err)

	// 6. Verify RFQ is AWARDED with agreed rate ₹38,000
	awardedRFQ, err := svc.GetRFQ(ctx, "tenant-1", rfq.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusAwarded, awardedRFQ.Status)
	assert.Equal(t, 38000.0, awardedRFQ.AwardedRate)
	assert.Equal(t, "c-1", awardedRFQ.AwardedCarrierID)
	assert.Equal(t, "Roadlines India", awardedRFQ.AwardedCarrierName)

	// Winning bid should be ACCEPTED at ₹38,000
	finalBid1, err := svc.GetBid(ctx, "tenant-1", bid1.ID)
	require.NoError(t, err)
	assert.Equal(t, BidStatusAccepted, finalBid1.Status)
	assert.Equal(t, 38000.0, finalBid1.BidAmount)

	// Competitor bid should be REJECTED
	finalBid2, err := svc.GetBid(ctx, "tenant-1", bid2.ID)
	require.NoError(t, err)
	assert.Equal(t, BidStatusRejected, finalBid2.Status)
}

func TestLoadBoard_CounterOfferIntegration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	// 1. Seed load board listing & bid
	listingID := "lbl-test-01"
	bidID := "lbb-test-01"
	now := time.Now().UTC()

	_, err := db.Exec(`INSERT INTO load_board_listings (
		id, tenant_id, origin_city, destination_city, vehicle_type_required,
		target_rate, max_rate, status, expires_at
	) VALUES (?, 'tenant-1', 'Chennai', 'Hyderabad', '32FT', 30000.0, 35000.0, 'BIDDING', ?)`,
		listingID, now.Add(24*time.Hour))
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO load_board_bids (
		id, tenant_id, listing_id, carrier_id, carrier_name, bid_amount, status
	) VALUES (?, 'tenant-1', ?, 'c-express', 'Express Fleet', 36000.0, 'SUBMITTED')`,
		bidID, listingID)
	require.NoError(t, err)

	// 2. Shipper counters load board bid at ₹32,000
	co, err := svc.SubmitCounterOffer(ctx, "tenant-1", bidID, "user-ops", "Ops Agent", SubmitCounterOfferInput{
		EntityType:    EntityLoadBoard,
		ListingID:     &listingID,
		SenderRole:    RoleShipper,
		OfferedAmount: 32000.0,
		Remarks:       "Max we can do is ₹32k for immediate dispatch",
	})
	require.NoError(t, err)
	assert.Equal(t, EntityLoadBoard, co.EntityType)
	assert.Equal(t, 32000.0, co.OfferedAmount)

	// 3. Carrier accepts counter-offer
	err = svc.AcceptCounterOffer(ctx, "tenant-1", co.ID, "carrier-agent")
	require.NoError(t, err)

	// 4. Verify load board bid is ACCEPTED at ₹32,000
	var finalBidStatus string
	var finalBidAmount float64
	err = db.QueryRow(`SELECT status, bid_amount FROM load_board_bids WHERE id = ?`, bidID).Scan(&finalBidStatus, &finalBidAmount)
	require.NoError(t, err)
	assert.Equal(t, "ACCEPTED", finalBidStatus)
	assert.Equal(t, 32000.0, finalBidAmount)

	// 5. Verify load board listing is AWARDED
	var listingStatus string
	err = db.QueryRow(`SELECT status FROM load_board_listings WHERE id = ?`, listingID).Scan(&listingStatus)
	require.NoError(t, err)
	assert.Equal(t, "AWARDED", listingStatus)
}

func TestRFQ_MultiTenantIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	now := time.Now().UTC()
	in := CreateRFQInput{
		Title:               "Tenant A Lane",
		OriginCity:          "Jaipur",
		DestinationCity:     "Ahmedabad",
		VehicleTypeRequired: "16T",
		TargetRate:          25000.0,
		MaxAcceptableRate:   28000.0,
		PickupDate:          now.AddDate(0, 0, 1),
		DeliveryDeadline:    now.AddDate(0, 0, 3),
		BidDeadline:         now.AddDate(0, 0, 1),
	}

	rfqA, err := svc.CreateRFQ(ctx, "tenant-1", "user-a", in)
	require.NoError(t, err)

	// Tenant 2 cannot see or access Tenant 1's RFQ
	_, err = svc.GetRFQ(ctx, "tenant-2", rfqA.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	listT2, totalT2, err := svc.ListRFQs(ctx, "tenant-2", ListRFQsFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, totalT2)
	assert.Empty(t, listT2)
}
