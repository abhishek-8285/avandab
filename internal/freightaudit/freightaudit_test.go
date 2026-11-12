package freightaudit_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/freightaudit"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_fa_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "../../db/migrations", 173))

	// Seed basic tenant and dependencies
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-fa-1', 'Freight Tenant 1', 't-fa-1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-fa-2', 'Freight Tenant 2', 't-fa-2')`)
	require.NoError(t, err)

	// Seed a route
	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('route-del-mum', 'Delhi', 'Mumbai', 1400.0, 24.0, 42.0)`)
	require.NoError(t, err)

	return db
}

func TestFreightAudit_RateCardsCRUDAndTenantIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := freightaudit.NewRepository(db)
	svc := freightaudit.NewService(repo)
	ctx := context.Background()

	// 1. Create Rate Card in Tenant 1
	rc1, err := svc.CreateRateCard(ctx, "t-fa-1", freightaudit.CreateRateCardInput{
		ContractNumber:       "RC-DEL-MUM-01",
		Name:                 "Delhi Mumbai 32FT MXL Rate",
		RouteID:              strPtr("route-del-mum"),
		VehicleType:          "32FT_MXL",
		BillingBasis:         freightaudit.BillingBasisPerKM,
		Rate:                 42.50,
		MinCharge:            5000.0,
		FreeDetentionHours:   2.0,
		DetentionRatePerHour: 250.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "RC-DEL-MUM-01", rc1.ContractNumber)
	assert.Equal(t, 42.50, rc1.Rate)
	assert.True(t, rc1.IsActive)

	// 2. Tenant Isolation Check: Tenant 2 cannot see Tenant 1's rate card
	_, err = svc.GetRateCard(ctx, "t-fa-2", rc1.ID)
	assert.ErrorIs(t, err, freightaudit.ErrNotFound)

	// 3. Update Rate Card
	newRate := 44.0
	updated, err := svc.UpdateRateCard(ctx, "t-fa-1", rc1.ID, freightaudit.UpdateRateCardInput{
		Rate: &newRate,
	})
	require.NoError(t, err)
	assert.Equal(t, 44.0, updated.Rate)

	// 4. List Rate Cards
	list, total, err := svc.ListRateCards(ctx, "t-fa-1", freightaudit.RateCardFilter{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, list, 1)
}

func TestFreightAudit_ThreeWayAuditCalculation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := freightaudit.NewRepository(db)
	svc := freightaudit.NewService(repo)
	ctx := context.Background()

	// Create Rate Card
	rc, err := svc.CreateRateCard(ctx, "t-fa-1", freightaudit.CreateRateCardInput{
		ContractNumber:       "RC-PUN-BLR-01",
		Name:                 "Pune Bangalore 20FT Rate",
		BillingBasis:         freightaudit.BillingBasisPerKM,
		Rate:                 40.0,
		MinCharge:            3000.0,
		FreeDetentionHours:   2.0,
		DetentionRatePerHour: 200.0,
	})
	require.NoError(t, err)

	// Audit with Discrepancy:
	// Carrier billed 900 km @ ₹45/km + ₹1,500 tolls + ₹600 detention = ₹42,600
	// Ground truth: GPS 840 km (60 km inflation), Rate ₹40/km (₹5 rate inflation),
	// FASTag verified tolls ₹1,100 (₹400 overbilled), Detention 3h (1h excess @ ₹200 = ₹200)
	// Audited Base: 840 * 40 = ₹33,600
	// Audited Total: 33,600 + 1,100 + 200 = ₹34,900
	// Overbilling Amount: 42,600 - 34,900 = ₹7,700
	gpsDist := 840.0
	actualWeight := 12000.0
	auditedRate := 40.0
	auditedTolls := 1100.0
	auditedDetentionHours := 3.0

	audit, err := svc.CreateAudit(ctx, "t-fa-1", freightaudit.CreateAuditInput{
		RateCardID:             &rc.ID,
		VendorName:             "Apex Roadways",
		VehicleNumber:          "MH12AB9999",
		BillingBasis:           &rc.BillingBasis,
		BilledDistanceKM:       900.0,
		BilledWeightKG:         12000.0,
		BilledRate:             45.0,
		BilledBaseFreight:      40500.0,
		BilledTollCharges:      1500.0,
		BilledDetentionCharges: 600.0,
		BilledTotal:            42600.0,
		GPSDistanceKM:          &gpsDist,
		ActualWeightKG:         &actualWeight,
		AuditedRate:            &auditedRate,
		AuditedTollCharges:     &auditedTolls,
		AuditedDetentionHours:  &auditedDetentionHours,
	})
	require.NoError(t, err)

	assert.Equal(t, freightaudit.AuditStatusDiscrepancyFlagged, audit.Status)
	assert.Equal(t, 7700.0, audit.OverbillingAmount)
	assert.InDelta(t, 18.07, audit.VariancePct, 0.05)
	assert.Equal(t, 60.0, audit.DistanceVarianceKM)
	assert.Equal(t, 5.0, audit.RateVariance)
	assert.Equal(t, 400.0, audit.TollVariance)

	// Verify generated deduction line items
	require.NotEmpty(t, audit.Deductions)
	var categories []freightaudit.DeductionCategory
	for _, d := range audit.Deductions {
		categories = append(categories, d.Category)
	}
	assert.Contains(t, categories, freightaudit.DeductionCategoryDistanceInflation)
	assert.Contains(t, categories, freightaudit.DeductionCategoryRateOvercharge)
	assert.Contains(t, categories, freightaudit.DeductionCategoryUnverifiedToll)
}

func TestFreightAudit_ApprovalDisputeAndSettlementLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := freightaudit.NewRepository(db)
	svc := freightaudit.NewService(repo)
	ctx := context.Background()

	// Create audit with overbilling
	gpsDist := 500.0
	auditedRate := 30.0
	audit, err := svc.CreateAudit(ctx, "t-fa-1", freightaudit.CreateAuditInput{
		VendorName:        "National Cargo",
		VehicleNumber:     "DL01XY7890",
		BilledDistanceKM:  550.0,
		BilledRate:        30.0,
		BilledBaseFreight: 16500.0,
		BilledTotal:       16500.0,
		GPSDistanceKM:     &gpsDist,
		AuditedRate:       &auditedRate,
	})
	require.NoError(t, err)
	assert.Equal(t, 1500.0, audit.OverbillingAmount)

	// 1. Approve Deduction
	approved, err := svc.ApproveDeduction(ctx, "t-fa-1", audit.ID, "auditor@corp.com", freightaudit.ApproveDeductionInput{
		Notes: "GPS discrepancy of 50 km approved for full debit note deduction",
	})
	require.NoError(t, err)
	assert.Equal(t, freightaudit.AuditStatusDeductionApproved, approved.Status)
	assert.Equal(t, 1500.0, approved.DeductionAmount)
	assert.Equal(t, 15000.0, approved.PayableAmount)
	assert.Contains(t, approved.DebitNoteNumber, "DN-")
	assert.Equal(t, "auditor@corp.com", *approved.ApprovedBy)

	// 2. Carrier disputes
	disputed, err := svc.DisputeAudit(ctx, "t-fa-1", audit.ID, "carrier", freightaudit.DisputeAuditInput{
		Reason: "Driver took approved bypass route due to highway landslide",
	})
	require.NoError(t, err)
	assert.Equal(t, freightaudit.AuditStatusDisputed, disputed.Status)
	assert.Equal(t, "Driver took approved bypass route due to highway landslide", disputed.DisputeReason)

	// 3. Final Settlement
	settled, err := svc.SettleAudit(ctx, "t-fa-1", audit.ID, "finance_manager", freightaudit.SettleAuditInput{
		Notes: "Settled with agreed compromise deduction",
	})
	require.NoError(t, err)
	assert.Equal(t, freightaudit.AuditStatusSettled, settled.Status)

	// 4. Verify Summary KPIs
	summary, err := svc.GetSummary(ctx, "t-fa-1")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.TotalAudits)
	assert.Equal(t, 16500.0, summary.TotalBilledAmount)
	assert.Equal(t, 15000.0, summary.TotalAuditedAmount)
	assert.Equal(t, 1500.0, summary.TotalOverbillingAmount)
	assert.Equal(t, 1, summary.SettledCount)
}

func strPtr(s string) *string {
	return &s
}

func TestFreightAudit_EnumsAndValidation(t *testing.T) {
	// 1. BillingBasis.IsValid
	assert.True(t, freightaudit.BillingBasisPerKM.IsValid())
	assert.True(t, freightaudit.BillingBasisPerKG.IsValid())
	assert.True(t, freightaudit.BillingBasisPerTon.IsValid())
	assert.True(t, freightaudit.BillingBasisPerTonKM.IsValid())
	assert.True(t, freightaudit.BillingBasisFlat.IsValid())
	assert.False(t, freightaudit.BillingBasis("UNKNOWN").IsValid())

	// 2. AuditStatus.IsValid
	assert.True(t, freightaudit.AuditStatusPending.IsValid())
	assert.True(t, freightaudit.AuditStatusVerified.IsValid())
	assert.True(t, freightaudit.AuditStatusDiscrepancyFlagged.IsValid())
	assert.True(t, freightaudit.AuditStatusDeductionApproved.IsValid())
	assert.True(t, freightaudit.AuditStatusDisputed.IsValid())
	assert.True(t, freightaudit.AuditStatusSettled.IsValid())
	assert.False(t, freightaudit.AuditStatus("UNKNOWN").IsValid())

	// 3. CreateRateCardInput.Validate
	inValidRC := freightaudit.CreateRateCardInput{
		ContractNumber: "RC-001",
		Name:           "Valid RC",
		BillingBasis:   freightaudit.BillingBasisPerKM,
		Rate:           45.0,
		MinCharge:      500.0,
	}
	assert.NoError(t, inValidRC.Validate())

	emptyContract := inValidRC
	emptyContract.ContractNumber = ""
	assert.Error(t, emptyContract.Validate())

	emptyName := inValidRC
	emptyName.Name = ""
	assert.Error(t, emptyName.Validate())

	badBasis := inValidRC
	badBasis.BillingBasis = "HOURLY"
	assert.Error(t, badBasis.Validate())

	negRate := inValidRC
	negRate.Rate = -10
	assert.Error(t, negRate.Validate())

	negMinCharge := inValidRC
	negMinCharge.MinCharge = -50
	assert.Error(t, negMinCharge.Validate())

	// 4. CreateAuditInput.Validate
	inValidAudit := freightaudit.CreateAuditInput{
		BilledTotal: 1000.0,
	}
	assert.NoError(t, inValidAudit.Validate())

	negTotal := inValidAudit
	negTotal.BilledTotal = -10.0
	assert.Error(t, negTotal.Validate())
}

func TestFreightAudit_FindMatchingAndListAudits(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := freightaudit.NewRepository(db)
	svc := freightaudit.NewService(repo)
	ctx := context.Background()

	// 1. Create a Rate Card with route and vehicle
	rc, err := svc.CreateRateCard(ctx, "t-fa-1", freightaudit.CreateRateCardInput{
		ContractNumber: "RC-MATCH-01",
		Name:           "Route Specific Rate",
		RouteID:        strPtr("route-del-mum"),
		VehicleType:    "32FT",
		BillingBasis:   freightaudit.BillingBasisPerKM,
		Rate:           50.0,
	})
	require.NoError(t, err)

	// 2. FindMatchingRateCard
	matched, err := repo.FindMatchingRateCard(ctx, "t-fa-1", "route-del-mum", "32FT")
	require.NoError(t, err)
	assert.Equal(t, rc.ID, matched.ID)

	// Not found in tenant 2
	_, err = repo.FindMatchingRateCard(ctx, "t-fa-2", "route-del-mum", "32FT")
	assert.Error(t, err)

	// 3. UpdateRateCard
	newRate := 55.0
	updated, err := svc.UpdateRateCard(ctx, "t-fa-1", rc.ID, freightaudit.UpdateRateCardInput{
		Rate:     &newRate,
		IsActive: boolPtr(true),
	})
	require.NoError(t, err)
	assert.Equal(t, 55.0, updated.Rate)

	// 4. Create Audits and ListAudits
	_, err = svc.CreateAudit(ctx, "t-fa-1", freightaudit.CreateAuditInput{
		VendorName:        "Fast Delivery",
		VehicleNumber:     "DL01A1000",
		BilledTotal:       5000.0,
		BilledDistanceKM:  100.0,
		BilledRate:        50.0,
		BilledBaseFreight: 5000.0,
	})
	require.NoError(t, err)

	audits, total, err := svc.ListAudits(ctx, "t-fa-1", freightaudit.AuditFilter{
		Search: "Fast Delivery",
		Limit:  10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, audits, 1)

	// 5. GetTripAuditData
	// Seed trip, vehicle, booking
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry) 
		VALUES ('v-fa-1', 't-fa-1', 'DL01A1000', 'DL01A1000', 'truck', 10, '2028-01-01', '2028-01-01', '2028-01-01')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, status, vehicle_id, route_id, departure_time, start_odometer, close_odometer) 
		VALUES ('tr-fa-1', 't-fa-1', 'TRIP-100', 'completed', 'v-fa-1', 'route-del-mum', datetime('now'), 1000.0, 1500.0)`)
	require.NoError(t, err)

	tripData, err := repo.GetTripAuditData(ctx, "t-fa-1", "tr-fa-1")
	require.NoError(t, err)
	assert.Equal(t, "tr-fa-1", tripData.TripID)
	assert.Equal(t, "DL01A1000", tripData.VehicleNumber)
	assert.Equal(t, 1000.0, *tripData.StartOdometer)
	assert.Equal(t, 1500.0, *tripData.CloseOdometer)
}

func boolPtr(b bool) *bool {
	return &b
}
