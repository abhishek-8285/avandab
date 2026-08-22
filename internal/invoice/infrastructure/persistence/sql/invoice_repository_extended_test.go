package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

func setupInvoiceDBExtended(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "-", "_")
	safeName = strings.ReplaceAll(safeName, "#", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(invoiceSchema)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO customers (id, name, company) VALUES ('cust-1', 'Acme', 'Acme Corp'), ('cust-2', 'Beta LLC', 'Beta')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO bookings (id, booking_number) VALUES ('bk-1', 'BK-0001'), ('bk-2', 'BK-0002')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number) VALUES ('trip-1', 'TR-0001'), ('trip-2', 'TR-0002')`)
	require.NoError(t, err)
	return dbConn
}

func newInvoiceAggWithTrip(id, tenantID, invNum, bookingID, customerID string, tripID *string, subtotal, tax, discount, total float64, status aggregate.PaymentStatus, now time.Time) *aggregate.InvoiceAggregate {
	return aggregate.NewInvoiceAggregate(
		aggregate.InvoiceID(id),
		shared.TenantID(tenantID),
		invNum,
		bookingID,
		customerID,
		tripID,
		subtotal, tax, discount, total,
		status,
		now,
	)
}

// ---------------------------------------------------------------------------
// GetReadModel
// ---------------------------------------------------------------------------

func TestInvoiceRepository_GetReadModel_Success(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	agg := newInvoiceAggWithTrip("inv-rm-1", "t1", "INV-RM-001", "bk-1", "cust-1", nil, 1000, 100, 0, 1100, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "inv-rm-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "inv-rm-1", rm.ID)
	assert.Equal(t, "INV-RM-001", rm.InvoiceNumber)
	assert.Equal(t, "bk-1", rm.BookingID)
	assert.Equal(t, "BK-0001", rm.BookingNumber)
	assert.Equal(t, "cust-1", rm.CustomerID)
	assert.Equal(t, "Acme", rm.CustomerName)
	assert.Equal(t, "Acme Corp", rm.CustomerCompany)
	assert.Nil(t, rm.TripID)
	assert.Equal(t, "", rm.TripNumber)
	assert.Equal(t, 1000.0, rm.Subtotal)
	assert.Equal(t, 100.0, rm.Tax)
	assert.Equal(t, 1100.0, rm.Total)
	assert.Equal(t, "pending", rm.PaymentStatus)
	assert.False(t, rm.CreatedAt.IsZero())
}

func TestInvoiceRepository_GetReadModel_WithTripAndGST(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	tripID := "trip-1"
	agg := newInvoiceAggWithTrip("inv-rm-2", "t1", "INV-RM-002", "bk-1", "cust-1", &tripID, 2000, 180, 0, 2180, aggregate.PaymentStatusPending, now)
	agg.Cgst = 90
	agg.Sgst = 90
	agg.Igst = 0
	irn := "irn-123"
	ackNo := "ack-1"
	ackDate := "2026-08-20"
	qr := "qr-data"
	agg.IRN = &irn
	agg.IRNAckNo = &ackNo
	agg.IRNAckDate = &ackDate
	agg.SignedQR = &qr
	ewb := "ewb-999"
	agg.EwbNumber = &ewb
	require.NoError(t, repo.Save(ctx, agg))

	rm, err := repo.GetReadModel(ctx, "inv-rm-2", "t1")
	require.NoError(t, err)
	require.NotNil(t, rm.TripID)
	assert.Equal(t, "trip-1", *rm.TripID)
	assert.Equal(t, "TR-0001", rm.TripNumber)
	assert.InDelta(t, 90.0, rm.CGST, 0.001)
	assert.InDelta(t, 90.0, rm.SGST, 0.001)
	assert.InDelta(t, 0.0, rm.IGST, 0.001)
	assert.Equal(t, "irn-123", rm.IRN)
	assert.Equal(t, "ack-1", rm.IRNAckNo)
	assert.Equal(t, "2026-08-20", rm.IRNAckDate)
	assert.Equal(t, "qr-data", rm.SignedQR)
}

func TestInvoiceRepository_GetReadModel_NotFound(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	_, err := repo.GetReadModel(ctx, "nope", "t1")
	require.Error(t, err)
}

func TestInvoiceRepository_GetReadModel_TenantIsolation(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-rm-3", "t1", "INV-RM-003", "bk-1", "cust-1", nil, 500, 0, 0, 500, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.GetReadModel(ctx, "inv-rm-3", "t2")
	require.Error(t, err)
	rm, err := repo.GetReadModel(ctx, "inv-rm-3", "t1")
	require.NoError(t, err)
	assert.Equal(t, "INV-RM-003", rm.InvoiceNumber)
}

func TestInvoiceRepository_GetReadModel_ErrorClosedDB(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-rm-4", "t1", "INV-RM-004", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.GetReadModel(ctx, "inv-rm-4", "t1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SearchReadModels
// ---------------------------------------------------------------------------

func TestInvoiceRepository_SearchReadModels_Pagination(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		id := "inv-s-" + string(rune('0'+i))
		num := "INV-S-00" + string(rune('0'+i))
		agg := newInvoiceAggWithTrip(id, "t1", num, "bk-1", "cust-1", nil, float64(100*i), 0, 0, float64(100*i), aggregate.PaymentStatusPending, now.Add(time.Duration(i)*time.Minute))
		require.NoError(t, repo.Save(ctx, agg))
	}
	// Create one for other tenant
	aggOther := newInvoiceAggWithTrip("inv-s-other", "t2", "INV-S-OTHER", "bk-1", "cust-1", nil, 999, 0, 0, 999, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, aggOther))

	models, total, err := repo.SearchReadModels(ctx, "t1", "", "", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, models, 2)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, models, 1)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)

	models, total, err = repo.SearchReadModels(ctx, "t2", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, models, 1)
	assert.Equal(t, "inv-s-other", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 0)
}

func TestInvoiceRepository_SearchReadModels_FilterByInvoiceNumber(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg1 := newInvoiceAggWithTrip("inv-q-1", "t1", "INV-SEARCH-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	agg2 := newInvoiceAggWithTrip("inv-q-2", "t1", "INV-SEARCH-002", "bk-1", "cust-1", nil, 200, 0, 0, 200, aggregate.PaymentStatusPending, now)
	agg3 := newInvoiceAggWithTrip("inv-q-3", "t1", "INV-OTHER-999", "bk-1", "cust-1", nil, 300, 0, 0, 300, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	models, total, err := repo.SearchReadModels(ctx, "t1", "SEARCH-001", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	assert.Equal(t, "inv-q-1", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "INV-SEARCH", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, models, 2)

	models, total, err = repo.SearchReadModels(ctx, "t1", "NONEXISTENTXYZ", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, models, 0)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)
}

func TestInvoiceRepository_SearchReadModels_FilterByCustomerName(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg1 := newInvoiceAggWithTrip("inv-cust-1", "t1", "INV-CUST-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	agg2 := newInvoiceAggWithTrip("inv-cust-2", "t1", "INV-CUST-002", "bk-1", "cust-2", nil, 200, 0, 0, 200, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))

	models, total, err := repo.SearchReadModels(ctx, "t1", "Acme", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "inv-cust-1", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "Beta", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "inv-cust-2", models[0].ID)
}

func TestInvoiceRepository_SearchReadModels_FilterByStatus(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg1 := newInvoiceAggWithTrip("inv-st-1", "t1", "INV-ST-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	agg2 := newInvoiceAggWithTrip("inv-st-2", "t1", "INV-ST-002", "bk-1", "cust-1", nil, 200, 0, 0, 200, aggregate.PaymentStatusPaid, now)
	agg3 := newInvoiceAggWithTrip("inv-st-3", "t1", "INV-ST-003", "bk-1", "cust-1", nil, 300, 0, 0, 300, aggregate.PaymentStatusPartiallyPaid, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	models, total, err := repo.SearchReadModels(ctx, "t1", "", "pending", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	assert.Equal(t, "pending", models[0].PaymentStatus)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "paid", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "inv-st-2", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models, 3)

	models, total, err = repo.SearchReadModels(ctx, "t1", "INV-ST", "pending", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, models, 1)

	models, total, err = repo.SearchReadModels(ctx, "t1", "", "nonexistent_status", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, models, 0)
}

func TestInvoiceRepository_SearchReadModels_ErrorClosedDB(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, _, err := repo.SearchReadModels(ctx, "t1", "", "", 10, 0)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// FindByTripID
// ---------------------------------------------------------------------------

func TestInvoiceRepository_FindByTripID_Success(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	tripID := "trip-1"
	agg := newInvoiceAggWithTrip("inv-trip-1", "t1", "INV-TRIP-001", "bk-1", "cust-1", &tripID, 500, 0, 0, 500, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.FindByTripID(ctx, "trip-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "inv-trip-1", string(found.ID))
	assert.Equal(t, "INV-TRIP-001", found.InvoiceNumber)
	require.NotNil(t, found.TripID)
	assert.Equal(t, "trip-1", *found.TripID)
}

func TestInvoiceRepository_FindByTripID_NotFound(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	_, err := repo.FindByTripID(ctx, "nope", "t1")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestInvoiceRepository_FindByTripID_TenantIsolation(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	tripID := "trip-1"
	agg := newInvoiceAggWithTrip("inv-trip-2", "t1", "INV-TRIP-002", "bk-1", "cust-1", &tripID, 400, 0, 0, 400, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.FindByTripID(ctx, "trip-1", "t2")
	assert.ErrorIs(t, err, sql.ErrNoRows)
	found, err := repo.FindByTripID(ctx, "trip-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "inv-trip-2", string(found.ID))
}

func TestInvoiceRepository_FindByTripID_ErrorClosedDB(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	tripID := "trip-1"
	agg := newInvoiceAggWithTrip("inv-trip-3", "t1", "INV-TRIP-003", "bk-1", "cust-1", &tripID, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.FindByTripID(ctx, "trip-1", "t1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Save with line items and extended fields
// ---------------------------------------------------------------------------

func TestInvoiceRepository_Save_WithLineItems(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-li-1", "t1", "INV-LI-001", "bk-1", "cust-1", nil, 1000, 0, 0, 1000, aggregate.PaymentStatusPending, now)
	hsn := "9965"
	unit := "HRS"
	ref := "det-1"
	trip2 := "trip-1"
	agg.AddLineItem(aggregate.LineItem{
		ID:           "li-1",
		TenantID:     "t1",
		InvoiceID:    "inv-li-1",
		TripID:       &trip2,
		LineType:     aggregate.LineTypeFreight,
		HSNSACCode:   &hsn,
		Description:  "Freight",
		Unit:         &unit,
		Quantity:     1,
		UnitPrice:    1000,
		Rate:         1000,
		TaxableValue: 1000,
		CgstRate:     9,
		SgstRate:     9,
		IgstRate:     0,
		CgstAmount:   90,
		SgstAmount:   90,
		IgstAmount:   0,
		Amount:       1000,
		Total:        1180,
		RefID:        &ref,
	})
	agg.AddLineItem(aggregate.LineItem{
		ID:          "li-2",
		TenantID:    "t1",
		InvoiceID:   "inv-li-1",
		LineType:    aggregate.LineTypeDetention,
		Description: "Detention",
		Quantity:    2,
		UnitPrice:   100,
		Amount:      200,
		RefID:       nil,
	})
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "inv-li-1", "t1")
	require.NoError(t, err)
	require.Len(t, found.LineItems, 2)
	// Check first line item fields persisted correctly
	li1 := found.LineItems[0]
	assert.Equal(t, "li-1", li1.ID)
	assert.Equal(t, aggregate.LineTypeFreight, li1.LineType)
	require.NotNil(t, li1.TripID)
	assert.Equal(t, "trip-1", *li1.TripID)
	require.NotNil(t, li1.HSNSACCode)
	assert.Equal(t, "9965", *li1.HSNSACCode)
	require.NotNil(t, li1.Unit)
	assert.Equal(t, "HRS", *li1.Unit)
	assert.InDelta(t, 1.0, li1.Quantity, 0.001)
	assert.InDelta(t, 1000.0, li1.UnitPrice, 0.001)
	assert.InDelta(t, 90.0, li1.CgstAmount, 0.001)
	assert.InDelta(t, 90.0, li1.SgstAmount, 0.001)
	require.NotNil(t, li1.RefID)
	assert.Equal(t, "det-1", *li1.RefID)
	// Second line null fields
	li2 := found.LineItems[1]
	assert.Equal(t, "li-2", li2.ID)
	assert.Nil(t, li2.TripID)
	assert.Nil(t, li2.HSNSACCode)
	assert.Nil(t, li2.Unit)
	assert.Nil(t, li2.RefID)
	assert.Equal(t, aggregate.LineTypeDetention, li2.LineType)
}

func TestInvoiceRepository_Save_WithExtendedFieldsAndDueDate(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	due := now.Add(30 * 24 * time.Hour)
	tripID := "trip-2"
	agg := newInvoiceAggWithTrip("inv-ext-1", "t1", "INV-EXT-001", "bk-2", "cust-2", &tripID, 5000, 450, 100, 5350, aggregate.PaymentStatusPending, now)
	agg.DueDate = &due
	agg.Cgst = 225
	agg.Sgst = 225
	agg.Igst = 0
	agg.PaidAmount = 0
	agg.Status = aggregate.InvoiceStatusIssued
	irn := "irn-ext-1"
	ackNo := "ack-ext-1"
	ackDate := "2026-08-20"
	qr := "signed-qr-data"
	ewb := "ewb-ext-1"
	agg.IRN = &irn
	agg.IRNAckNo = &ackNo
	agg.IRNAckDate = &ackDate
	agg.SignedQR = &qr
	agg.EwbNumber = &ewb
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.Find(ctx, "inv-ext-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "INV-EXT-001", found.InvoiceNumber)
	require.NotNil(t, found.TripID)
	assert.Equal(t, "trip-2", *found.TripID)
	assert.InDelta(t, 225.0, found.Cgst, 0.001)
	assert.InDelta(t, 225.0, found.Sgst, 0.001)
	require.NotNil(t, found.DueDate)
	assert.WithinDuration(t, due, *found.DueDate, time.Second)
	require.NotNil(t, found.IRN)
	assert.Equal(t, "irn-ext-1", *found.IRN)
	require.NotNil(t, found.IRNAckNo)
	assert.Equal(t, "ack-ext-1", *found.IRNAckNo)
	require.NotNil(t, found.IRNAckDate)
	assert.Equal(t, "2026-08-20", *found.IRNAckDate)
	require.NotNil(t, found.SignedQR)
	assert.Equal(t, "signed-qr-data", *found.SignedQR)
	require.NotNil(t, found.EwbNumber)
	assert.Equal(t, "ewb-ext-1", *found.EwbNumber)
	assert.Equal(t, aggregate.InvoiceStatusIssued, found.Status)

	// Update path: modify extended fields and save again (tests updateInvoiceFullSQL)
	newDue := due.Add(10 * 24 * time.Hour)
	found.DueDate = &newDue
	found.Cgst = 250
	found.Sgst = 250
	newIrn := "irn-ext-2"
	found.IRN = &newIrn
	found.PaidAmount = 1000
	found.PaymentStatus = aggregate.PaymentStatusPartiallyPaid
	require.NoError(t, repo.Save(ctx, found))
	assert.Equal(t, int64(2), found.Version)
	found2, err := repo.Find(ctx, "inv-ext-1", "t1")
	require.NoError(t, err)
	assert.InDelta(t, 250.0, found2.Cgst, 0.001)
	require.NotNil(t, found2.IRN)
	assert.Equal(t, "irn-ext-2", *found2.IRN)
	assert.InDelta(t, 1000.0, found2.PaidAmount, 0.001)
	assert.WithinDuration(t, newDue, *found2.DueDate, time.Second)
}

func TestInvoiceRepository_Save_PersistLineItems_Replace(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-rep-1", "t1", "INV-REP-001", "bk-1", "cust-1", nil, 1000, 0, 0, 1000, aggregate.PaymentStatusPending, now)
	agg.AddLineItem(aggregate.LineItem{ID: "li-a", TenantID: "t1", InvoiceID: "inv-rep-1", LineType: aggregate.LineTypeFreight, Description: "Freight A", Amount: 1000})
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "inv-rep-1", "t1")
	require.NoError(t, err)
	require.Len(t, found.LineItems, 1)
	// Replace line items
	found.LineItems = []aggregate.LineItem{
		{ID: "li-b", TenantID: "t1", InvoiceID: "inv-rep-1", LineType: aggregate.LineTypeDetention, Description: "Detention B", Amount: 200},
		{ID: "li-c", TenantID: "t1", InvoiceID: "inv-rep-1", LineType: aggregate.LineTypeAccessorial, Description: "Accessorial C", Amount: 300},
	}
	found.RecomputeTotals()
	require.NoError(t, repo.Save(ctx, found))
	found2, err := repo.Find(ctx, "inv-rep-1", "t1")
	require.NoError(t, err)
	require.Len(t, found2.LineItems, 2)
	assert.Equal(t, "li-b", found2.LineItems[0].ID)
	assert.Equal(t, "li-c", found2.LineItems[1].ID)
	assert.InDelta(t, 500.0, found2.Subtotal, 0.001)
}

func TestInvoiceRepository_Save_NullStringHelpers(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	// Test with nil TripID, nil IRN etc to exercise nullStringPtr nil branch
	agg := newInvoiceAggWithTrip("inv-null-1", "t1", "INV-NULL-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	agg.IRN = nil
	agg.EwbNumber = nil
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "inv-null-1", "t1")
	require.NoError(t, err)
	assert.Nil(t, found.TripID)
	assert.Nil(t, found.IRN)
	assert.Nil(t, found.EwbNumber)
	// Now with valid TripID and IRN to exercise valid branch
	tripID := "trip-1"
	irn := "irn-valid"
	agg2 := newInvoiceAggWithTrip("inv-null-2", "t1", "INV-NULL-002", "bk-1", "cust-1", &tripID, 200, 0, 0, 200, aggregate.PaymentStatusPending, now)
	agg2.IRN = &irn
	require.NoError(t, repo.Save(ctx, agg2))
	found2, err := repo.Find(ctx, "inv-null-2", "t1")
	require.NoError(t, err)
	require.NotNil(t, found2.TripID)
	assert.Equal(t, "trip-1", *found2.TripID)
	require.NotNil(t, found2.IRN)
	assert.Equal(t, "irn-valid", *found2.IRN)
}

func TestInvoiceRepository_Q_WithTx(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repoI := NewInvoiceRepository(dbConn)
	repo, ok := repoI.(*invoiceRepository)
	require.True(t, ok)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-tx-1", "t1", "INV-TX-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))

	tx, err := dbConn.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	txCtx := repository.WithTxInContext(ctx, tx)

	qTx := repo.Q(txCtx)
	require.NotNil(t, qTx)
	qNoTx := repo.Q(ctx)
	require.NotNil(t, qNoTx)
	assert.NotEqual(t, qTx, qNoTx)

	found, err := repo.Find(txCtx, "inv-tx-1", "t1")
	require.NoError(t, err)
	assert.Equal(t, "INV-TX-001", found.InvoiceNumber)

	// Save inside transaction should use tx-bound exec
	found.ClearEvents()
	found.PaidAmount = 50
	found.PaymentStatus = aggregate.PaymentStatusPartiallyPaid
	require.NoError(t, repo.Save(txCtx, found))
	_ = tx.Rollback()
	// After rollback, original should still be visible with 0 paid
	found2, err := repo.Find(ctx, "inv-tx-1", "t1")
	require.NoError(t, err)
	assert.InDelta(t, 0.0, found2.PaidAmount, 0.001)

	// Commit path
	tx2, err := dbConn.Begin()
	require.NoError(t, err)
	txCtx2 := repository.WithTxInContext(ctx, tx2)
	found3, err := repo.Find(txCtx2, "inv-tx-1", "t1")
	require.NoError(t, err)
	found3.ClearEvents()
	found3.PaidAmount = 100
	found3.PaymentStatus = aggregate.PaymentStatusPaid
	found3.Status = aggregate.InvoiceStatusPaid
	require.NoError(t, repo.Save(txCtx2, found3))
	require.NoError(t, tx2.Commit())
	found4, err := repo.Find(ctx, "inv-tx-1", "t1")
	require.NoError(t, err)
	assert.InDelta(t, 100.0, found4.PaidAmount, 0.001)
	assert.Equal(t, aggregate.PaymentStatusPaid, found4.PaymentStatus)
}

func TestInvoiceRepository_Save_ErrorClosedDB(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-err-1", "t1", "INV-ERR-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	_ = dbConn.Close()
	err := repo.Save(ctx, agg)
	require.Error(t, err)
}

func TestInvoiceRepository_Find_ErrorClosedDB_Extended(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg := newInvoiceAggWithTrip("inv-err-2", "t1", "INV-ERR-002", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.Find(ctx, "inv-err-2", "t1")
	require.Error(t, err)
}

func TestInvoiceRepository_Find_StatusEmptyDefaultsToOutstanding(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	// Directly insert invoice with empty status to test fallback in findInvoiceBySQL
	_, err := dbConn.Exec(`INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, tax, discount, total, payment_status, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"inv-empty-status", "INV-EMPTY-001", "bk-1", "cust-1", 100.0, 0.0, 0.0, 100.0, "pending", "", "t1", 1)
	require.NoError(t, err)
	found, err := repo.Find(ctx, "inv-empty-status", "t1")
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceStatusOutstanding, found.Status)
}

func TestInvoiceRepository_SearchReadModels_TenantIsolationWithQuery(t *testing.T) {
	dbConn := setupInvoiceDBExtended(t)
	repo := NewInvoiceRepository(dbConn)
	ctx := context.Background()
	now := time.Now()
	agg1 := newInvoiceAggWithTrip("inv-ti-1", "t1", "INV-TI-001", "bk-1", "cust-1", nil, 100, 0, 0, 100, aggregate.PaymentStatusPending, now)
	agg2 := newInvoiceAggWithTrip("inv-ti-2", "t2", "INV-TI-002", "bk-1", "cust-1", nil, 200, 0, 0, 200, aggregate.PaymentStatusPending, now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))

	models, total, err := repo.SearchReadModels(ctx, "t1", "INV-TI", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "inv-ti-1", models[0].ID)

	models, total, err = repo.SearchReadModels(ctx, "t2", "INV-TI", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "inv-ti-2", models[0].ID)
}
