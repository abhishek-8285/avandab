package sto_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/sto"
)

func newSTOTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_sto_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSTOLifecycleAndLoadBoardBidding(t *testing.T) {
	db := newSTOTestDB(t)
	repo := sto.NewSQLRepository(db)
	svc := sto.NewService(repo, db)
	ctx := context.Background()

	tenantA := "tenant-sto-a"
	tenantB := "tenant-sto-b"

	_, err := db.Exec(`
		INSERT OR IGNORE INTO tenants (id, name, slug) VALUES 
		('tenant-sto-a', 'Alpha Logistics', 'alpha-sto'),
		('tenant-sto-b', 'Beta Freight', 'beta-sto');
	`)
	require.NoError(t, err)

	// Create Facilities in Pune and Mumbai
	facPuneID := "fac-pune-01"
	facMumbaiID := "fac-mum-01"
	_, err = db.Exec(`
		INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type, city, latitude, longitude)
		VALUES 
		($1, $2, 'PUN-PLANT', 'Pune Manufacturing Plant', 'depot', 'Pune', 18.5204, 73.8567),
		($3, $2, 'BOM-RDC', 'Mumbai Central RDC', 'hub', 'Mumbai', 19.0760, 72.8777)`,
		facPuneID, tenantA, facMumbaiID)
	require.NoError(t, err)

	// 1. Validation failure: Same origin and dest facility
	_, err = svc.CreateSTO(ctx, tenantA, sto.CreateSTOInput{
		OriginFacilityID:      facPuneID,
		DestinationFacilityID: facPuneID,
		MaterialCode:          "MAT-01",
		Quantity:              10,
		UOM:                   "MT",
		RequiredDeliveryDate:  "2026-09-20",
	})
	assert.Error(t, err)

	// 2. Validation failure: Non-positive quantity
	_, err = svc.CreateSTO(ctx, tenantA, sto.CreateSTOInput{
		OriginFacilityID:      facPuneID,
		DestinationFacilityID: facMumbaiID,
		MaterialCode:          "MAT-01",
		Quantity:              -5,
		UOM:                   "MT",
		RequiredDeliveryDate:  "2026-09-20",
	})
	assert.Error(t, err)

	// 3. Create Valid STO in DRAFT
	createdSTO, err := svc.CreateSTO(ctx, tenantA, sto.CreateSTOInput{
		OriginFacilityID:      facPuneID,
		DestinationFacilityID: facMumbaiID,
		MaterialCode:          "MAT-AUTO-ENGINE",
		MaterialDescription:   "Engine Powertrain Components",
		Quantity:              24.5,
		UOM:                   "MT",
		RequiredDeliveryDate:  "2026-09-25",
		Notes:                 "Handle with temperature control",
		CreatedBy:             "usr-planner-01",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, createdSTO.ID)
	assert.True(t, strings.HasPrefix(createdSTO.STONumber, "STO-"))
	assert.Equal(t, sto.StatusDRAFT, createdSTO.Status)
	assert.Equal(t, "Pune Manufacturing Plant", createdSTO.OriginFacilityName)
	assert.Equal(t, "Mumbai Central RDC", createdSTO.DestFacilityName)

	// 4. Cannot post DRAFT STO directly to loadboard
	_, err = svc.PostToLoadBoard(ctx, tenantA, createdSTO.ID, sto.PostToLoadBoardInput{
		VehicleTypeRequired: "truck",
		TargetRate:          22000,
		MaxRate:             25000,
	})
	assert.Error(t, err, "DRAFT STO must be released before posting")

	// 5. Release STO
	releasedSTO, err := svc.ReleaseSTO(ctx, tenantA, createdSTO.ID)
	require.NoError(t, err)
	assert.Equal(t, sto.StatusRELEASED, releasedSTO.Status)

	// 6. Post STO to Load Board
	listing, err := svc.PostToLoadBoard(ctx, tenantA, releasedSTO.ID, sto.PostToLoadBoardInput{
		VehicleTypeRequired: "truck",
		TargetRate:          22000,
		MaxRate:             26000,
		Visibility:          sto.VisibilityPRIVATE,
		ExpiresHours:        72,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, listing.ID)
	assert.Equal(t, "Pune", listing.OriginCity)
	assert.Equal(t, "Mumbai", listing.DestinationCity)
	assert.Equal(t, sto.LBStatusOPEN, listing.Status)
	assert.Equal(t, 22000.0, listing.TargetRate)
	assert.Equal(t, 26000.0, listing.MaxRate)

	// STO status is now POSTED
	updatedSTO, err := svc.GetSTO(ctx, tenantA, createdSTO.ID)
	require.NoError(t, err)
	assert.Equal(t, sto.StatusPOSTED, updatedSTO.Status)

	// 7. Submit Bids from Carriers
	bid1, err := svc.SubmitBid(ctx, tenantA, listing.ID, sto.SubmitBidInput{
		CarrierID:   "carr-western",
		CarrierName: "Western Freightways",
		BidAmount:   23500,
		Remarks:     "Available for immediate placement",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bid1.ID)
	assert.Equal(t, sto.BidStatusSUBMITTED, bid1.Status)

	bid2, err := svc.SubmitBid(ctx, tenantA, listing.ID, sto.SubmitBidInput{
		CarrierID:   "carr-maharashtra",
		CarrierName: "Maharashtra Surface Transport",
		BidAmount:   22000, // lower bid
		Remarks:     "GPS enabled reefer fleet",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bid2.ID)

	// Listing status moves to BIDDING
	refreshedListing, err := svc.GetListing(ctx, tenantA, listing.ID)
	require.NoError(t, err)
	assert.Equal(t, sto.LBStatusBIDDING, refreshedListing.Status)
	assert.Equal(t, 2, refreshedListing.BidsCount)

	// 8. Award Bid (Accept Bid 2)
	awardResult, err := svc.AwardBid(ctx, tenantA, listing.ID, bid2.ID)
	require.NoError(t, err)
	assert.Equal(t, listing.ID, awardResult.ListingID)
	assert.Equal(t, bid2.ID, awardResult.WinningBid.ID)
	assert.Equal(t, sto.BidStatusACCEPTED, awardResult.WinningBid.Status)
	assert.NotNil(t, awardResult.TripID)

	// Competing Bid 1 is REJECTED
	bids, err := svc.ListBids(ctx, tenantA, listing.ID)
	require.NoError(t, err)
	require.Len(t, bids, 2)
	for _, b := range bids {
		if b.ID == bid2.ID {
			assert.Equal(t, sto.BidStatusACCEPTED, b.Status)
		} else {
			assert.Equal(t, sto.BidStatusREJECTED, b.Status)
		}
	}

	// Listing is now AWARDED
	awardedListing, err := svc.GetListing(ctx, tenantA, listing.ID)
	require.NoError(t, err)
	assert.Equal(t, sto.LBStatusAWARDED, awardedListing.Status)

	// STO is now ASSIGNED
	assignedSTO, err := svc.GetSTO(ctx, tenantA, createdSTO.ID)
	require.NoError(t, err)
	assert.Equal(t, sto.StatusASSIGNED, assignedSTO.Status)

	// 9. Acceptance Criteria 3: Concurrent / subsequent award fails closed
	_, err = svc.AwardBid(ctx, tenantA, listing.ID, bid1.ID)
	assert.Error(t, err, "Already awarded listing must reject further awards")

	// 10. Multi-Tenant Isolation
	// Tenant B cannot access Tenant A's STO or private listing
	_, err = svc.GetSTO(ctx, tenantB, createdSTO.ID)
	assert.Error(t, err)

	_, err = svc.GetListing(ctx, tenantB, listing.ID)
	assert.Error(t, err)
}
