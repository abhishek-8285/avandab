package inventory_test

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

	"transport-app/internal/inventory"
)

func newTestInventoryDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_inv_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	// Seed test tenants, facilities, and vehicle
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-a', 'Tenant A', 'tenant-a'),
		('tenant-b', 'Tenant B', 'tenant-b')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT OR IGNORE INTO facilities (id, tenant_id, facility_code, name, facility_type) VALUES
		('fac-depot-1', 'tenant-a', 'DEP-DEL-01', 'Delhi Main Workshop', 'workshop'),
		('fac-depot-2', 'tenant-a', 'DEP-MUM-01', 'Mumbai Hub Store', 'depot'),
		('fac-depot-b', 'tenant-b', 'DEP-BLR-01', 'Bangalore Store', 'depot')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		('veh-1', 'tenant-a', 'DL-01-AA-1111', 'V-001', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 50000.0)`)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInventory_PartsCRUDAndTenantIsolation(t *testing.T) {
	db := newTestInventoryDB(t)
	repo := inventory.NewSQLRepository(db)
	svc := inventory.NewService(repo)
	ctx := context.Background()

	// 1. Create part in tenant-a
	costPrice := 450.0
	sellingPrice := 650.0
	minStock := 10.0
	reorderQty := 25.0
	p1, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber:       "FLT-OIL-01",
		Name:             "Heavy Duty Engine Oil Filter",
		Category:         inventory.CategoryFilters,
		Description:      "Spin-on oil filter for 28-32T trucks",
		UOM:              inventory.UOMPieces,
		MinStockLevel:    &minStock,
		ReorderQuantity:  &reorderQty,
		CostPrice:        costPrice,
		SellingPrice:     sellingPrice,
		OEMBrand:         "Fleetguard",
		CompatibleModels: "Tata Signa 4825",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, p1.ID)
	assert.Equal(t, "FLT-OIL-01", p1.PartNumber)
	assert.Equal(t, inventory.CategoryFilters, p1.Category)
	assert.Equal(t, 450.0, p1.CostPrice)
	assert.True(t, p1.IsActive)

	// 2. Duplicate part number in same tenant must fail
	_, err = svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber: "FLT-OIL-01",
		Name:       "Duplicate Filter",
		Category:   inventory.CategoryFilters,
	})
	assert.ErrorIs(t, err, inventory.ErrDuplicatePartNumber)

	// Same part number in tenant-b must succeed (tenant isolation)
	pB, err := svc.CreatePart(ctx, "tenant-b", inventory.CreatePartInput{
		PartNumber: "FLT-OIL-01",
		Name:       "Tenant B Filter",
		Category:   inventory.CategoryFilters,
	})
	require.NoError(t, err)
	assert.Equal(t, "tenant-b", pB.TenantID)

	// 3. Update Part
	newCost := 480.0
	updated, err := svc.UpdatePart(ctx, "tenant-a", p1.ID, inventory.UpdatePartInput{
		CostPrice: &newCost,
	})
	require.NoError(t, err)
	assert.Equal(t, 480.0, updated.CostPrice)

	// 4. List Parts with filtering
	list, total, err := svc.ListParts(ctx, inventory.PartFilter{
		TenantID: "tenant-a",
		Category: "FILTERS",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, p1.ID, list[0].ID)

	// Tenant B list sees only its parts
	listB, totalB, err := svc.ListParts(ctx, inventory.PartFilter{
		TenantID: "tenant-b",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, totalB)
	assert.Equal(t, pB.ID, listB[0].ID)
}

func TestInventory_PurchaseBillLifecycleAndStockLedger(t *testing.T) {
	db := newTestInventoryDB(t)
	repo := inventory.NewSQLRepository(db)
	svc := inventory.NewService(repo)
	ctx := context.Background()

	// Create 2 parts
	p1, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber: "BRK-PAD-01",
		Name:       "Front Brake Pad Set",
		Category:   inventory.CategoryBrakes,
		CostPrice:  1200.0,
	})
	require.NoError(t, err)

	p2, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber: "OIL-ENG-15W40",
		Name:       "Engine Oil 15W-40 20L Drum",
		Category:   inventory.CategoryFluids,
		UOM:        inventory.UOMLitres,
		CostPrice:  3500.0,
	})
	require.NoError(t, err)

	// 1. Create Purchase Bill (GRN) in DRAFT
	bill, err := svc.CreatePurchaseBill(ctx, "tenant-a", inventory.CreatePurchaseBillInput{
		BillNumber:  "INV-BOSCH-2026-001",
		VendorName:  "Bosch Spares Depot",
		VendorGSTIN: "07AAAAA0000A1Z5",
		FacilityID:  "fac-depot-1",
		Notes:       "Quarterly scheduled stock replenishment",
		Items: []inventory.PurchaseBillItemInput{
			{PartID: p1.ID, Quantity: 20.0, UnitCost: 1150.0, TaxRate: 18.0},
			{PartID: p2.ID, Quantity: 10.0, UnitCost: 3400.0, TaxRate: 18.0},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, bill.ID)
	assert.Equal(t, inventory.BillStatusDraft, bill.Status)
	assert.Len(t, bill.Items, 2)
	assert.Greater(t, bill.TotalAmount, 50000.0)

	// Stock before receiving must be 0
	stocksBefore, _, err := svc.ListStocks(ctx, inventory.StockFilter{
		TenantID:   "tenant-a",
		FacilityID: "fac-depot-1",
	})
	require.NoError(t, err)
	assert.Empty(t, stocksBefore)

	// 2. Receive Purchase Bill
	receivedBill, err := svc.ReceivePurchaseBill(ctx, "tenant-a", bill.ID, "Ramesh Store Manager")
	require.NoError(t, err)
	assert.Equal(t, inventory.BillStatusReceived, receivedBill.Status)
	assert.Equal(t, "Ramesh Store Manager", receivedBill.ReceivedBy)
	assert.NotNil(t, receivedBill.ReceivedDate)

	// Re-receiving an already received bill must fail
	_, err = svc.ReceivePurchaseBill(ctx, "tenant-a", bill.ID, "Ramesh")
	assert.ErrorIs(t, err, inventory.ErrBillAlreadyProcessed)

	// 3. Stock after receiving must reflect quantities
	stocksAfter, _, err := svc.ListStocks(ctx, inventory.StockFilter{
		TenantID:   "tenant-a",
		FacilityID: "fac-depot-1",
	})
	require.NoError(t, err)
	assert.Len(t, stocksAfter, 2)

	stockMap := make(map[string]float64)
	for _, s := range stocksAfter {
		stockMap[s.PartID] = s.QuantityOnHand
	}
	assert.Equal(t, 20.0, stockMap[p1.ID])
	assert.Equal(t, 10.0, stockMap[p2.ID])

	// 4. Verify transaction ledger entries
	txs, totalTxs, err := svc.ListTransactions(ctx, inventory.TransactionFilter{
		TenantID:        "tenant-a",
		TransactionType: string(inventory.TxPurchaseReceipt),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, totalTxs)
	assert.Equal(t, 20.0, txs[0].Quantity)
}

func TestInventory_IssueReturnAndStockAdjustment(t *testing.T) {
	db := newTestInventoryDB(t)
	repo := inventory.NewSQLRepository(db)
	svc := inventory.NewService(repo)
	ctx := context.Background()

	minStock := 5.0
	p, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber:    "ALT-BELT-01",
		Name:          "Alternator Serpentine Belt",
		Category:      inventory.CategoryEngine,
		CostPrice:     850.0,
		MinStockLevel: &minStock,
	})
	require.NoError(t, err)

	// Receive 10 belts
	bill, err := svc.CreatePurchaseBill(ctx, "tenant-a", inventory.CreatePurchaseBillInput{
		BillNumber: "BILL-BELT-01",
		VendorName: "Gates Rubber",
		FacilityID: "fac-depot-1",
		Items: []inventory.PurchaseBillItemInput{
			{PartID: p.ID, Quantity: 10.0, UnitCost: 850.0, TaxRate: 18.0},
		},
	})
	require.NoError(t, err)
	_, err = svc.ReceivePurchaseBill(ctx, "tenant-a", bill.ID, "Workshop Lead")
	require.NoError(t, err)

	// 1. Issue 3 belts to veh-1
	issueTx, err := svc.IssuePart(ctx, "tenant-a", inventory.IssuePartInput{
		PartID:      p.ID,
		FacilityID:  "fac-depot-1",
		Quantity:    3.0,
		VehicleID:   "veh-1",
		WorkOrderID: "wo-101",
		PerformedBy: "Suresh Mech",
		Notes:       "Replaced worn alternator belt during 50k service",
	})
	require.NoError(t, err)
	assert.Equal(t, inventory.TxWorkOrderIssue, issueTx.TransactionType)
	assert.Equal(t, 7.0, issueTx.BalanceAfter)

	// 2. Attempt to issue more than remaining stock (7 available, trying 10) must fail
	_, err = svc.IssuePart(ctx, "tenant-a", inventory.IssuePartInput{
		PartID:     p.ID,
		FacilityID: "fac-depot-1",
		Quantity:   10.0,
	})
	assert.ErrorIs(t, err, inventory.ErrInsufficientStock)

	// 3. Return 1 belt from wo-101 (only 2 were used)
	returnTx, err := svc.ReturnPart(ctx, "tenant-a", inventory.ReturnPartInput{
		PartID:      p.ID,
		FacilityID:  "fac-depot-1",
		Quantity:    1.0,
		VehicleID:   "veh-1",
		WorkOrderID: "wo-101",
		PerformedBy: "Suresh Mech",
		Notes:       "Spare belt unneeded, returning to shelf",
	})
	require.NoError(t, err)
	assert.Equal(t, inventory.TxWorkOrderReturn, returnTx.TransactionType)
	assert.Equal(t, 8.0, returnTx.BalanceAfter)

	// 4. Physical cycle count: adjust stock from 8 to 9 (found 1 extra belt)
	adjTx, err := svc.AdjustStock(ctx, "tenant-a", inventory.AdjustStockInput{
		PartID:            p.ID,
		FacilityID:        "fac-depot-1",
		NewQuantityOnHand: 9.0,
		Reason:            "Annual physical inventory variance",
		PerformedBy:       "Audit Inspector",
	})
	require.NoError(t, err)
	assert.Equal(t, inventory.TxAdjustmentIn, adjTx.TransactionType)
	assert.Equal(t, 1.0, adjTx.Quantity)
	assert.Equal(t, 9.0, adjTx.BalanceAfter)

	// Adjust downwards (damaged 2 belts written off)
	adjOutTx, err := svc.AdjustStock(ctx, "tenant-a", inventory.AdjustStockInput{
		PartID:            p.ID,
		FacilityID:        "fac-depot-1",
		NewQuantityOnHand: 7.0,
		Reason:            "Damaged during warehouse transit",
		PerformedBy:       "Audit Inspector",
	})
	require.NoError(t, err)
	assert.Equal(t, inventory.TxAdjustmentOut, adjOutTx.TransactionType)
	assert.Equal(t, 2.0, adjOutTx.Quantity)
	assert.Equal(t, 7.0, adjOutTx.BalanceAfter)
}

func TestInventory_InterStoreSTOTransfer(t *testing.T) {
	db := newTestInventoryDB(t)
	repo := inventory.NewSQLRepository(db)
	svc := inventory.NewService(repo)
	ctx := context.Background()

	p, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber: "CLU-PLT-01",
		Name:       "Heavy Duty Clutch Plate 430mm",
		Category:   inventory.CategoryTransmission,
		CostPrice:  5500.0,
	})
	require.NoError(t, err)

	// Stock 15 units at Delhi Main Workshop (fac-depot-1)
	_, err = svc.AdjustStock(ctx, "tenant-a", inventory.AdjustStockInput{
		PartID:            p.ID,
		FacilityID:        "fac-depot-1",
		NewQuantityOnHand: 15.0,
		Reason:            "Opening stock",
	})
	require.NoError(t, err)

	// Transfer 5 units from Delhi (fac-depot-1) to Mumbai (fac-depot-2) under STO-2026-0042
	outTx, inTx, err := svc.TransferStock(ctx, "tenant-a", inventory.TransferStockInput{
		PartID:         p.ID,
		FromFacilityID: "fac-depot-1",
		ToFacilityID:   "fac-depot-2",
		Quantity:       5.0,
		STOID:          "sto-2026-0042",
		PerformedBy:    "Logistics Officer",
		Notes:          "Emergency inter-depot transfer for breakdown vehicle",
	})
	require.NoError(t, err)

	assert.Equal(t, inventory.TxSTOOut, outTx.TransactionType)
	assert.Equal(t, 10.0, outTx.BalanceAfter)

	assert.Equal(t, inventory.TxSTOIn, inTx.TransactionType)
	assert.Equal(t, 5.0, inTx.BalanceAfter)

	// Check Mumbai store stock
	mumStock, _, err := svc.ListStocks(ctx, inventory.StockFilter{
		TenantID:   "tenant-a",
		FacilityID: "fac-depot-2",
	})
	require.NoError(t, err)
	require.Len(t, mumStock, 1)
	assert.Equal(t, 5.0, mumStock[0].QuantityOnHand)
}

func TestInventory_SummaryAndValuation(t *testing.T) {
	db := newTestInventoryDB(t)
	repo := inventory.NewSQLRepository(db)
	svc := inventory.NewService(repo)
	ctx := context.Background()

	minStock := 10.0
	p1, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber:    "LUB-GRS-01",
		Name:          "Chassis Grease EP2 18kg",
		Category:      inventory.CategoryFluids,
		CostPrice:     2000.0,
		MinStockLevel: &minStock,
	})
	require.NoError(t, err)

	// Stock 4 units (below min_stock 10 -> Low stock!)
	_, err = svc.AdjustStock(ctx, "tenant-a", inventory.AdjustStockInput{
		PartID:            p1.ID,
		FacilityID:        "fac-depot-1",
		NewQuantityOnHand: 4.0,
		Reason:            "Opening stock",
	})
	require.NoError(t, err)

	p2, err := svc.CreatePart(ctx, "tenant-a", inventory.CreatePartInput{
		PartNumber:    "BLB-HL-24V",
		Name:          "Halogen Headlamp Bulb 24V 70W",
		Category:      inventory.CategoryElectrical,
		CostPrice:     150.0,
		MinStockLevel: &minStock,
	})
	require.NoError(t, err)

	// Stock 50 units (healthy stock)
	_, err = svc.AdjustStock(ctx, "tenant-a", inventory.AdjustStockInput{
		PartID:            p2.ID,
		FacilityID:        "fac-depot-1",
		NewQuantityOnHand: 50.0,
		Reason:            "Opening stock",
	})
	require.NoError(t, err)

	summary, err := svc.GetSummary(ctx, "tenant-a")
	require.NoError(t, err)

	assert.Equal(t, 2, summary.TotalParts)
	// Valuation: (4 * 2000) + (50 * 150) = 8000 + 7500 = 15500
	assert.Equal(t, 15500.0, summary.TotalValuation)
	assert.Equal(t, 54.0, summary.TotalStockedUnits)
	assert.Equal(t, 1, summary.LowStockCount) // p1 is low stock
}
