package application

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookingdomain "transport-app/internal/booking/domain"
	bookingagg "transport-app/internal/booking/domain/aggregate"
	companydomain "transport-app/internal/domain/company"
	"transport-app/internal/invoice/domain"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// ---- fakes ----

type fakeClockApp struct {
	now time.Time
}

func (c *fakeClockApp) Now() time.Time { return c.now }

type fakeIDGenApp struct {
	next int
}

func (g *fakeIDGenApp) GenerateUUID() string {
	g.next++
	return "uuid-" + string(rune('0'+g.next%10)) + "-" + string(rune('A'+g.next%26))
}

func (g *fakeIDGenApp) GenerateDisplayID(prefix string) string {
	g.next++
	return prefix + "-DISPLAY-" + string(rune('0'+g.next%10))
}

// deterministic simpler gen for tests expecting known values
type seqIDGen struct {
	counter int
}

func (s *seqIDGen) GenerateUUID() string {
	s.counter++
	return "test-uuid-" + string(rune('0'+s.counter))
}
func (s *seqIDGen) GenerateDisplayID(prefix string) string {
	s.counter++
	return prefix + "-000" + string(rune('0'+s.counter))
}

// ---- mock repos ----

type mockInvoiceRepoApp struct {
	saveErr               error
	findErr               error
	findResult            *aggregate.InvoiceAggregate
	findByBookingIDErr    error
	findByBookingIDResult *aggregate.InvoiceAggregate
	findByTripIDErr       error
	findByTripIDResult    *aggregate.InvoiceAggregate
	getReadModelErr       error
	getReadModelResult    domain.InvoiceReadModel
	searchErr             error
	searchResult          []domain.InvoiceReadModel
	searchTotal           int64
	saved                 []*aggregate.InvoiceAggregate
}

func (m *mockInvoiceRepoApp) Save(_ context.Context, inv *aggregate.InvoiceAggregate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, inv)
	return nil
}
func (m *mockInvoiceRepoApp) Find(_ context.Context, _ aggregate.InvoiceID, _ shared.TenantID) (*aggregate.InvoiceAggregate, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.findResult != nil {
		return m.findResult, nil
	}
	return nil, sql.ErrNoRows
}
func (m *mockInvoiceRepoApp) FindByBookingID(_ context.Context, _ string, _ shared.TenantID) (*aggregate.InvoiceAggregate, error) {
	if m.findByBookingIDErr != nil {
		return nil, m.findByBookingIDErr
	}
	if m.findByBookingIDResult != nil {
		return m.findByBookingIDResult, nil
	}
	return nil, sql.ErrNoRows
}
func (m *mockInvoiceRepoApp) FindByTripID(_ context.Context, _ string, _ shared.TenantID) (*aggregate.InvoiceAggregate, error) {
	if m.findByTripIDErr != nil {
		return nil, m.findByTripIDErr
	}
	if m.findByTripIDResult != nil {
		return m.findByTripIDResult, nil
	}
	return nil, sql.ErrNoRows
}
func (m *mockInvoiceRepoApp) GetReadModel(_ context.Context, _ aggregate.InvoiceID, _ shared.TenantID) (domain.InvoiceReadModel, error) {
	if m.getReadModelErr != nil {
		return domain.InvoiceReadModel{}, m.getReadModelErr
	}
	return m.getReadModelResult, nil
}
func (m *mockInvoiceRepoApp) SearchReadModels(_ context.Context, _ shared.TenantID, _, _ string, _, _ int) ([]domain.InvoiceReadModel, int64, error) {
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	return m.searchResult, m.searchTotal, nil
}

type mockBookingRepoApp struct {
	getReadModelErr    error
	getReadModelResult bookingdomain.BookingReadModel
}

func (m *mockBookingRepoApp) Save(_ context.Context, _ *bookingagg.BookingAggregate) error {
	return nil
}
func (m *mockBookingRepoApp) Find(_ context.Context, _ bookingagg.BookingID, _ shared.TenantID) (*bookingagg.BookingAggregate, error) {
	return nil, sql.ErrNoRows
}
func (m *mockBookingRepoApp) FindByNumber(_ context.Context, _ string, _ shared.TenantID) (*bookingagg.BookingAggregate, error) {
	return nil, sql.ErrNoRows
}
func (m *mockBookingRepoApp) Exists(_ context.Context, _ bookingagg.BookingID, _ shared.TenantID) (bool, error) {
	return false, nil
}
func (m *mockBookingRepoApp) Delete(_ context.Context, _ bookingagg.BookingID, _ shared.TenantID) error {
	return nil
}
func (m *mockBookingRepoApp) GetReadModel(_ context.Context, _ bookingagg.BookingID, _ shared.TenantID) (bookingdomain.BookingReadModel, error) {
	if m.getReadModelErr != nil {
		return bookingdomain.BookingReadModel{}, m.getReadModelErr
	}
	return m.getReadModelResult, nil
}
func (m *mockBookingRepoApp) SearchReadModels(_ context.Context, _ shared.TenantID, _, _ string, _, _ int) ([]bookingdomain.BookingReadModel, int64, error) {
	return nil, 0, nil
}

type mockSettingsRepoApp struct {
	getSettingsErr error
	settings       companydomain.CompanySettings
}

func (m *mockSettingsRepoApp) GetCompanySettings(_ context.Context) (companydomain.CompanySettings, error) {
	if m.getSettingsErr != nil {
		return companydomain.CompanySettings{}, m.getSettingsErr
	}
	return m.settings, nil
}

// ---- provider / tx / uow ----

type mockProviderAppInv struct {
	inv     any
	booking any
	audit   any
}

func (m *mockProviderAppInv) Bookings() any    { return m.booking }
func (m *mockProviderAppInv) Trips() any       { return nil }
func (m *mockProviderAppInv) Drivers() any     { return nil }
func (m *mockProviderAppInv) Vehicles() any    { return nil }
func (m *mockProviderAppInv) Invoices() any    { return m.inv }
func (m *mockProviderAppInv) Payments() any    { return nil }
func (m *mockProviderAppInv) AuditLogs() any   { return m.audit }
func (m *mockProviderAppInv) Maintenance() any { return nil }

type mockTxAppInv struct {
	context.Context
	prov ports.RepositoryProvider
}

func (m *mockTxAppInv) Repositories() ports.RepositoryProvider { return m.prov }

type mockUoWAppInv struct {
	inv     any
	booking any
	audit   any
	execErr error
}

func (m *mockUoWAppInv) Execute(ctx context.Context, fn func(ports.TxContext) error) error {
	if m.execErr != nil {
		return m.execErr
	}
	prov := &mockProviderAppInv{inv: m.inv, booking: m.booking, audit: m.audit}
	return fn(&mockTxAppInv{Context: ctx, prov: prov})
}

// helper to build invoice aggregate for tests
func newTestInvoice(id string, status aggregate.PaymentStatus) *aggregate.InvoiceAggregate {
	now := time.Now()
	trip := "trip-1"
	inv := aggregate.NewInvoiceAggregate(
		aggregate.InvoiceID(id),
		shared.TenantID("1"),
		"INV-001",
		"bk-1",
		"cust-1",
		&trip,
		1000,
		100,
		0,
		1100,
		status,
		now,
	)
	inv.ClearEvents()
	return inv
}

// ---- GenerateInvoice ----

func TestGenerateInvoice_MissingBookingAndTrip(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	uow := &mockUoWAppInv{inv: &mockInvoiceRepoApp{}}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "", TripID: nil, Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "booking ID is required")

	// also via GenerateInTx directly
	prov := &mockProviderAppInv{inv: &mockInvoiceRepoApp{}}
	tx := &mockTxAppInv{Context: context.Background(), prov: prov}
	_, _, err = uc.GenerateInTx(tx, GenerateInvoiceCommand{TenantID: "1"})
	require.Error(t, err)
}

func TestGenerateInvoice_RepoTypeAssertionFailure(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	uow := &mockUoWAppInv{inv: "not-a-repo"}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve invoice repository")
}

func TestGenerateInvoice_FindByBookingError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	invRepo := &mockInvoiceRepoApp{findByBookingIDErr: errors.New("db fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db fail")
}

func TestGenerateInvoice_FindByTripError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	tripID := "trip-xyz"
	invRepo := &mockInvoiceRepoApp{findByTripIDErr: errors.New("trip db fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "", TripID: &tripID, Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trip db fail")
}

func TestGenerateInvoice_ExistingPaid_ReturnsExisting(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	existing := newTestInvoice("inv-existing-paid", aggregate.PaymentStatusPaid)
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	prov := &mockProviderAppInv{inv: invRepo}
	tx := &mockTxAppInv{Context: context.Background(), prov: prov}
	id, attached, err := uc.GenerateInTx(tx, GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", LineItems: []InvoiceLineItemInput{{Description: "det", Quantity: 1, UnitPrice: 10}}})
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-existing-paid"), id)
	assert.False(t, attached)
	assert.Empty(t, invRepo.saved)
}

func TestGenerateInvoice_ExistingPartiallyPaid_ReturnsExisting(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	existing := newTestInvoice("inv-partial", aggregate.PaymentStatusPartiallyPaid)
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	// via Execute wrapper – should not attach
	id, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", LineItems: []InvoiceLineItemInput{{Description: "det", Quantity: 1, UnitPrice: 10}}})
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-partial"), id)
	assert.Empty(t, invRepo.saved)
}

func TestGenerateInvoice_ExistingPending_NoLineItems_ReturnsExisting(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &fakeIDGenApp{}
	existing := newTestInvoice("inv-pending", aggregate.PaymentStatusPending)
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	id, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-pending"), id)
	assert.Empty(t, invRepo.saved)
}

func TestGenerateInvoice_ExistingPending_WithLineItems_Attaches(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	existing := newTestInvoice("inv-pending-attach", aggregate.PaymentStatusPending)
	// ensure existing has no line items initially
	require.Empty(t, existing.LineItems)
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	ref := "det-1"
	cmd := GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk-1",
		LineItems: []InvoiceLineItemInput{
			{TripID: nil, LineType: aggregate.LineTypeDetention, Description: "Detention", Quantity: 2, UnitPrice: 50, RefID: &ref},
		},
	}
	id, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-pending-attach"), id)
	require.Len(t, invRepo.saved, 1)
	saved := invRepo.saved[0]
	// should have freight + detention = 2 lines
	assert.Len(t, saved.LineItems, 2)
	var hasFreight, hasDet bool
	for _, li := range saved.LineItems {
		if li.LineType == aggregate.LineTypeFreight {
			hasFreight = true
			assert.Equal(t, 1000.0, li.Amount) // freight amount equals original subtotal
		}
		if li.LineType == aggregate.LineTypeDetention {
			hasDet = true
			assert.Equal(t, 100.0, li.Amount)
		}
	}
	assert.True(t, hasFreight)
	assert.True(t, hasDet)
	// attached flag via GenerateInTx
	prov := &mockProviderAppInv{inv: &mockInvoiceRepoApp{findByBookingIDResult: newTestInvoice("inv-2", aggregate.PaymentStatusPending)}}
	tx := &mockTxAppInv{Context: context.Background(), prov: prov}
	uc2 := NewGenerateInvoiceUseCase(&mockUoWAppInv{inv: prov.inv}, &seqIDGen{}, clock)
	_, attached, err := uc2.GenerateInTx(tx, cmd)
	require.NoError(t, err)
	assert.True(t, attached)
}

func TestGenerateInvoice_ExistingPending_WithLineItems_Dedupe(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	existing := newTestInvoice("inv-dedupe", aggregate.PaymentStatusPending)
	ref := "det-dup"
	existing.AddLineItem(aggregate.LineItem{
		ID:          "li-1",
		TenantID:    "1",
		InvoiceID:   "inv-dedupe",
		LineType:    aggregate.LineTypeFreight,
		Description: "Freight",
		Quantity:    1,
		UnitPrice:   1000,
		Amount:      1000,
	})
	existing.AddLineItem(aggregate.LineItem{
		ID:          "li-2",
		TenantID:    "1",
		InvoiceID:   "inv-dedupe",
		LineType:    aggregate.LineTypeDetention,
		Description: "Detention",
		Quantity:    1,
		UnitPrice:   50,
		Amount:      50,
		RefID:       &ref,
	})
	existing.ClearEvents()
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	cmd := GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk-1",
		LineItems: []InvoiceLineItemInput{
			{LineType: aggregate.LineTypeDetention, Description: "Detention again", Quantity: 1, UnitPrice: 50, RefID: &ref},
			{LineType: aggregate.LineTypeDetention, Description: "New detention", Quantity: 1, UnitPrice: 30, RefID: func() *string { s := "det-new"; return &s }()},
		},
	}
	id, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-dedupe"), id)
	require.Len(t, invRepo.saved, 1)
	// original 2 + 1 new = 3, duplicate skipped
	assert.Len(t, invRepo.saved[0].LineItems, 3)
}

func TestGenerateInvoice_ExistingPending_WithLineItems_SaveError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	existing := newTestInvoice("inv-save-err", aggregate.PaymentStatusPending)
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing, saveErr: errors.New("save fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", LineItems: []InvoiceLineItemInput{{LineType: aggregate.LineTypeDetention, Description: "Det", Quantity: 1, UnitPrice: 10}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save fail")
}

func TestGenerateInvoice_ExistingPending_WithFreightAlreadyExists(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	existing := newTestInvoice("inv-freight-exists", aggregate.PaymentStatusPending)
	existing.AddLineItem(aggregate.LineItem{
		ID:          "li-freight",
		TenantID:    "1",
		InvoiceID:   "inv-freight-exists",
		LineType:    aggregate.LineTypeFreight,
		Description: "Freight",
		Amount:      1000,
	})
	existing.ClearEvents()
	invRepo := &mockInvoiceRepoApp{findByBookingIDResult: existing}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	cmd := GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", LineItems: []InvoiceLineItemInput{{LineType: aggregate.LineTypeDetention, Description: "Det", Quantity: 1, UnitPrice: 20}}}
	_, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	// should be 2 total: existing freight + new detention, no second freight
	assert.Len(t, invRepo.saved[0].LineItems, 2)
	countFreight := 0
	for _, li := range invRepo.saved[0].LineItems {
		if li.LineType == aggregate.LineTypeFreight {
			countFreight++
		}
	}
	assert.Equal(t, 1, countFreight)
}

func TestGenerateInvoice_New_DerivedPricing_GSTEnabled(t *testing.T) {
	clock := &fakeClockApp{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{
		getReadModelResult: bookingdomain.BookingReadModel{
			ID:    "bk-1",
			Price: 1000.00,
		},
	}
	settingsRepo := &mockSettingsRepoApp{
		settings: companydomain.CompanySettings{GSTEnabled: true, GSTRate: 18},
	}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	cmd := GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 999, Tax: 0, Discount: 0, Total: 999}
	id, err := uc.Execute(context.Background(), cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	require.Len(t, invRepo.saved, 1)
	saved := invRepo.saved[0]
	assert.Equal(t, 1000.0, saved.Subtotal)
	assert.Equal(t, 180.0, saved.Tax)
	assert.Equal(t, 1180.0, saved.Total)
	assert.Equal(t, 0.0, saved.Discount)
}

func TestGenerateInvoice_New_DerivedPricing_GSTDisabled(t *testing.T) {
	clock := &fakeClockApp{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{
		getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: 500},
	}
	settingsRepo := &mockSettingsRepoApp{
		settings: companydomain.CompanySettings{GSTEnabled: false, GSTRate: 18},
	}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 0, Tax: 0, Discount: 0, Total: 0})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, 500.0, invRepo.saved[0].Subtotal)
	assert.Equal(t, 0.0, invRepo.saved[0].Tax)
	assert.Equal(t, 500.0, invRepo.saved[0].Total)
}

func TestGenerateInvoice_New_DerivedPricing_NegativePriceClamped(t *testing.T) {
	clock := &fakeClockApp{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{
		getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: -100},
	}
	settingsRepo := &mockSettingsRepoApp{
		settings: companydomain.CompanySettings{GSTEnabled: true, GSTRate: 10},
	}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 0, Tax: 0, Discount: 0, Total: 0})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, 0.0, invRepo.saved[0].Subtotal)
	assert.Equal(t, 0.0, invRepo.saved[0].Tax)
	assert.Equal(t, 0.0, invRepo.saved[0].Total)
}

func TestGenerateInvoice_New_DerivedPricing_SettingsError(t *testing.T) {
	clock := &fakeClockApp{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{
		getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: 200},
	}
	settingsRepo := &mockSettingsRepoApp{getSettingsErr: errors.New("settings fail")}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 0, Tax: 0, Discount: 0, Total: 0})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	// tax 0 because settings error
	assert.Equal(t, 200.0, invRepo.saved[0].Subtotal)
	assert.Equal(t, 0.0, invRepo.saved[0].Tax)
}

func TestGenerateInvoice_New_DerivedPricing_BookingRepoTypeFailure_FallbackToValidation(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	// booking is bad type -> resolveBookingPricing returns false, so validation used
	uow := &mockUoWAppInv{inv: invRepo, booking: "bad-type", audit: &mockSettingsRepoApp{settings: companydomain.CompanySettings{GSTEnabled: true, GSTRate: 18}}}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 18, Discount: 0, Total: 118})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, 100.0, invRepo.saved[0].Subtotal)
}

func TestGenerateInvoice_New_Validation_NegativeTotal(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: -5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "total cannot be negative")
}

func TestGenerateInvoice_New_Validation_NegativeSubtotal(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: -10, Tax: 0, Discount: 0, Total: -10})
	// total negative first check wins? subtotal negative also -> total negative path
	require.Error(t, err)
}

func TestGenerateInvoice_New_Validation_NegativeTax(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: -5, Discount: 0, Total: 95})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice amounts cannot be negative")
}

func TestGenerateInvoice_New_Validation_Mismatch(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 200})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestGenerateInvoice_New_Success_NoLineItems(t *testing.T) {
	clock := &fakeClockApp{now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	trip := "trip-1"
	id, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-new", CustomerID: "cust-1", TripID: &trip, Subtotal: 200, Tax: 20, Discount: 10, Total: 210})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	require.Len(t, invRepo.saved, 1)
	saved := invRepo.saved[0]
	assert.Equal(t, "bk-new", saved.BookingID)
	assert.Equal(t, 200.0, saved.Subtotal)
	assert.Equal(t, 20.0, saved.Tax)
	assert.Equal(t, 10.0, saved.Discount)
	assert.Equal(t, 210.0, saved.Total)
	assert.Equal(t, aggregate.PaymentStatusPending, saved.PaymentStatus)
	// also check GenerateInTx attached false
	prov := &mockProviderAppInv{inv: &mockInvoiceRepoApp{}, booking: bookingRepo}
	tx := &mockTxAppInv{Context: context.Background(), prov: prov}
	uc2 := NewGenerateInvoiceUseCase(&mockUoWAppInv{inv: prov.inv, booking: bookingRepo}, &seqIDGen{}, clock)
	_, attached, err := uc2.GenerateInTx(tx, GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-new2", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.NoError(t, err)
	assert.False(t, attached)
}

func TestGenerateInvoice_New_Success_WithLineItems(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	ref := "det-2"
	id, err := uc.Execute(context.Background(), GenerateInvoiceCommand{
		TenantID: "1", BookingID: "bk-line", CustomerID: "cust-1",
		Subtotal: 500, Tax: 50, Discount: 0, Total: 550,
		LineItems: []InvoiceLineItemInput{
			{LineType: aggregate.LineTypeDetention, Description: "Detention", Quantity: 3, UnitPrice: 100, RefID: &ref},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	require.Len(t, invRepo.saved, 1)
	saved := invRepo.saved[0]
	// should have freight + detention
	assert.Len(t, saved.LineItems, 2)
	var freightAmt, detAmt float64
	for _, li := range saved.LineItems {
		if li.LineType == aggregate.LineTypeFreight {
			freightAmt = li.Amount
		}
		if li.LineType == aggregate.LineTypeDetention {
			detAmt = li.Amount
		}
	}
	assert.Equal(t, 500.0, freightAmt)
	assert.Equal(t, 300.0, detAmt)
	// subtotal recomputed = 800, total = 850
	assert.Equal(t, 800.0, saved.Subtotal)
	assert.Equal(t, 850.0, saved.Total)
}

func TestGenerateInvoice_New_SaveError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{saveErr: errors.New("save fail new")}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save fail new")
}

func TestGenerateInvoice_TripID_Path_NoBookingID(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	tripID := "trip-abc"
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	// no existing trip invoice, should create
	id, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "", TripID: &tripID, CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, &tripID, invRepo.saved[0].TripID)

	// existing trip invoice pending with no line items should return existing
	existing := newTestInvoice("inv-trip-exist", aggregate.PaymentStatusPending)
	existing.TripID = &tripID
	invRepo2 := &mockInvoiceRepoApp{findByTripIDResult: existing}
	uow2 := &mockUoWAppInv{inv: invRepo2, booking: bookingRepo}
	uc2 := NewGenerateInvoiceUseCase(uow2, idGen, clock)
	id2, err := uc2.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "", TripID: &tripID, CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-trip-exist"), id2)
	assert.Empty(t, invRepo2.saved)

	// existing paid trip invoice should return existing even with line items
	existingPaid := newTestInvoice("inv-trip-paid", aggregate.PaymentStatusPaid)
	existingPaid.TripID = &tripID
	invRepo3 := &mockInvoiceRepoApp{findByTripIDResult: existingPaid}
	uow3 := &mockUoWAppInv{inv: invRepo3}
	uc3 := NewGenerateInvoiceUseCase(uow3, idGen, clock)
	id3, err := uc3.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "", TripID: &tripID, CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110, LineItems: []InvoiceLineItemInput{{LineType: aggregate.LineTypeDetention, Description: "det", Quantity: 1, UnitPrice: 10}}})
	require.NoError(t, err)
	assert.Equal(t, aggregate.InvoiceID("inv-trip-paid"), id3)
	assert.Empty(t, invRepo3.saved)
}

func TestGenerateInvoice_Execute_UoWError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	uow := &mockUoWAppInv{execErr: errors.New("uow fail")}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow fail")
}

func TestGenerateInvoice_DerivedPricing_OverridesClientAmountsEvenIfInvalid(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: 1000}}
	settingsRepo := &mockSettingsRepoApp{settings: companydomain.CompanySettings{GSTEnabled: false}}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	// client amounts are intentionally mismatched but should be ignored because derived pricing succeeds
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 1, Tax: 999, Discount: 0, Total: 9999})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, 1000.0, invRepo.saved[0].Subtotal)
}

func TestGenerateInvoice_BookingPriceRounding(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: 123.456}}
	settingsRepo := &mockSettingsRepoApp{settings: companydomain.CompanySettings{GSTEnabled: true, GSTRate: 18}}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: settingsRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 0, Tax: 0, Discount: 0, Total: 0})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	// 123.456 rounded to 123.46 => 12346 minor, tax 18% => 2222.28 => 2222 minor => 22.22
	assert.Equal(t, 123.46, invRepo.saved[0].Subtotal)
	assert.Equal(t, 22.22, invRepo.saved[0].Tax)
	assert.Equal(t, 145.68, invRepo.saved[0].Total)
}

func TestGenerateInvoice_AuditLogsNotPricingResolver(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelResult: bookingdomain.BookingReadModel{ID: "bk-1", Price: 800}}
	// audit returns a type that does NOT implement GetCompanySettings
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo, audit: "not-a-resolver"}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 0, Tax: 0, Discount: 0, Total: 0})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, 800.0, invRepo.saved[0].Subtotal)
	assert.Equal(t, 0.0, invRepo.saved[0].Tax)
}

func TestGenerateInvoice_BookingGetError_Fallback(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: errors.New("booking not found")}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110})
	require.NoError(t, err)
	assert.Equal(t, 100.0, invRepo.saved[0].Subtotal)
}

func TestGenerateInvoice_ValidationWithinEpsilon(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	idGen := &seqIDGen{}
	invRepo := &mockInvoiceRepoApp{}
	bookingRepo := &mockBookingRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo, booking: bookingRepo}
	uc := NewGenerateInvoiceUseCase(uow, idGen, clock)
	// within epsilon 0.01 should pass
	_, err := uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110.009})
	require.NoError(t, err)
	// beyond epsilon should fail
	_, err = uc.Execute(context.Background(), GenerateInvoiceCommand{TenantID: "1", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110.02})
	require.Error(t, err)
}

// ---- GetInvoice ----

func TestGetInvoice_Success(t *testing.T) {
	now := time.Now()
	tripID := "trip-1"
	expected := domain.InvoiceReadModel{
		ID:              "inv-1",
		InvoiceNumber:   "INV-001",
		BookingID:       "bk-1",
		BookingNumber:   "BK-001",
		CustomerID:      "cust-1",
		CustomerName:    "John",
		CustomerCompany: "Acme",
		TripID:          &tripID,
		TripNumber:      "TRP-1",
		Subtotal:        1000,
		Tax:             100,
		Discount:        10,
		Total:           1090,
		PaymentStatus:   "pending",
		CGST:            50,
		SGST:            50,
		IGST:            0,
		IRN:             "irn-123",
		IRNAckNo:        "ack-123",
		IRNAckDate:      "2026-01-01",
		SignedQR:        "qr-data",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	invRepo := &mockInvoiceRepoApp{getReadModelResult: expected}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGetInvoiceUseCase(uow)
	dto, err := uc.Execute(context.Background(), GetInvoiceQuery{ID: "inv-1", TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "inv-1", dto.ID)
	assert.Equal(t, "INV-001", dto.InvoiceNumber)
	assert.Equal(t, "bk-1", dto.BookingID)
	assert.Equal(t, "BK-001", dto.BookingNumber)
	assert.Equal(t, "cust-1", dto.CustomerID)
	assert.Equal(t, "John", dto.CustomerName)
	assert.Equal(t, "Acme", dto.CustomerCompany)
	require.NotNil(t, dto.TripID)
	assert.Equal(t, "trip-1", *dto.TripID)
	assert.Equal(t, 1000.0, dto.Subtotal)
	assert.Equal(t, 100.0, dto.Tax)
	assert.Equal(t, 10.0, dto.Discount)
	assert.Equal(t, 1090.0, dto.Total)
	assert.Equal(t, "pending", dto.PaymentStatus)
	assert.Equal(t, 50.0, dto.CGST)
	assert.Equal(t, "irn-123", dto.IRN)
	assert.Equal(t, now, dto.CreatedAt)
}

func TestGetInvoice_NotFound(t *testing.T) {
	invRepo := &mockInvoiceRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGetInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), GetInvoiceQuery{ID: "none", TenantID: "1"})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetInvoice_RepoTypeAssertionFailure(t *testing.T) {
	uow := &mockUoWAppInv{inv: "bad-type"}
	uc := NewGetInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), GetInvoiceQuery{ID: "inv-1", TenantID: "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve invoice repository")
}

func TestGetInvoice_UoWError(t *testing.T) {
	uow := &mockUoWAppInv{execErr: errors.New("uow fail")}
	uc := NewGetInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), GetInvoiceQuery{ID: "inv-1", TenantID: "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow fail")
}

func TestGetInvoice_RepoError(t *testing.T) {
	invRepo := &mockInvoiceRepoApp{getReadModelErr: errors.New("db fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewGetInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), GetInvoiceQuery{ID: "inv-1", TenantID: "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db fail")
}

// ---- ListInvoices ----

func TestListInvoices_DefaultsAndSuccess(t *testing.T) {
	now := time.Now()
	tripID := "trip-1"
	rows := []domain.InvoiceReadModel{
		{ID: "inv-1", InvoiceNumber: "INV-001", BookingID: "bk-1", CustomerID: "cust-1", Subtotal: 100, Tax: 10, Discount: 0, Total: 110, PaymentStatus: "pending", CreatedAt: now, UpdatedAt: now},
		{ID: "inv-2", InvoiceNumber: "INV-002", BookingID: "bk-2", CustomerID: "cust-2", TripID: &tripID, TripNumber: "TRP-1", Subtotal: 200, Tax: 20, Discount: 5, Total: 215, PaymentStatus: "paid", CreatedAt: now, UpdatedAt: now},
	}
	invRepo := &mockInvoiceRepoApp{searchResult: rows, searchTotal: 2}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewListInvoicesUseCase(uow)
	res, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: 0, Limit: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)
	require.Len(t, res.Invoices, 2)
	assert.Equal(t, "inv-1", res.Invoices[0].ID)
	assert.Equal(t, "INV-001", res.Invoices[0].InvoiceNumber)
	assert.Equal(t, "inv-2", res.Invoices[1].ID)
	assert.Equal(t, "trip-1", *res.Invoices[1].TripID)
	assert.Equal(t, "TRP-1", res.Invoices[1].TripNumber)
}

func TestListInvoices_PaginationOffset(t *testing.T) {
	now := time.Now()
	// verify limit/page defaults produce correct db call; we can't inspect offset directly but success path with non-zero pagination should still work
	invRepo := &mockInvoiceRepoApp{searchResult: []domain.InvoiceReadModel{{ID: "inv-1", CreatedAt: now, UpdatedAt: now}}, searchTotal: 10}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewListInvoicesUseCase(uow)
	res, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: 2, Limit: 5, Search: "foo", Status: "pending"})
	require.NoError(t, err)
	assert.Equal(t, int64(10), res.Total)
	require.Len(t, res.Invoices, 1)
}

func TestListInvoices_EmptyResult(t *testing.T) {
	invRepo := &mockInvoiceRepoApp{searchResult: []domain.InvoiceReadModel{}, searchTotal: 0}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewListInvoicesUseCase(uow)
	res, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Total)
	assert.Empty(t, res.Invoices)
}

func TestListInvoices_RepoError(t *testing.T) {
	invRepo := &mockInvoiceRepoApp{searchErr: errors.New("search fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewListInvoicesUseCase(uow)
	_, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: 1, Limit: 10})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search fail")
}

func TestListInvoices_RepoTypeAssertionFailure(t *testing.T) {
	uow := &mockUoWAppInv{inv: "bad"}
	uc := NewListInvoicesUseCase(uow)
	_, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve invoice repository")
}

func TestListInvoices_UoWError(t *testing.T) {
	uow := &mockUoWAppInv{execErr: errors.New("uow fail")}
	uc := NewListInvoicesUseCase(uow)
	_, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: 1, Limit: 10})
	require.Error(t, err)
}

func TestListInvoices_NegativePageAndLimit_Defaults(t *testing.T) {
	now := time.Now()
	invRepo := &mockInvoiceRepoApp{searchResult: []domain.InvoiceReadModel{{ID: "inv-1", CreatedAt: now, UpdatedAt: now}}, searchTotal: 1}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewListInvoicesUseCase(uow)
	res, err := uc.Execute(context.Background(), ListInvoicesQuery{TenantID: "1", Page: -1, Limit: -5})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
}

// ---- Utility functions ----

func TestStateCodeFromGSTIN(t *testing.T) {
	assert.Equal(t, "27", StateCodeFromGSTIN("27ABCDE1234F1Z5"))
	assert.Equal(t, "07", StateCodeFromGSTIN("07ABCDE1234F1Z5"))
	assert.Equal(t, "", StateCodeFromGSTIN(""))
	assert.Equal(t, "", StateCodeFromGSTIN("1"))
	assert.Equal(t, "27", StateCodeFromGSTIN("27"))
	// lower case normalized to upper
	assert.Equal(t, "27", StateCodeFromGSTIN("27abc"))
}

func TestIsIntraState(t *testing.T) {
	assert.True(t, IsIntraState("27ABCDE1234F1Z5", "27XYZ1234F1Z5"))
	assert.False(t, IsIntraState("27ABCDE1234F1Z5", "29XYZ1234F1Z5"))
	assert.True(t, IsIntraState("", "29XYZ"))      // empty treated as intra
	assert.True(t, IsIntraState("27XYZ", ""))      // empty treated as intra
	assert.True(t, IsIntraState("1", "29XYZ"))     // short GSTIN empty state -> intra true
	assert.True(t, IsIntraState("27", "27"))       // same state
	assert.False(t, IsIntraState("27", "07"))      // different
	assert.True(t, IsIntraState("27abc", "27xyz")) // case uppered
}

func TestComputeLineTax_IntraState(t *testing.T) {
	cgst, sgst, igst := ComputeLineTax(1000, 18, true)
	assert.Equal(t, 90.0, cgst)
	assert.Equal(t, 90.0, sgst)
	assert.Equal(t, 0.0, igst)

	cgst, sgst, igst = ComputeLineTax(100, 18, true)
	assert.Equal(t, 9.0, cgst)
	assert.Equal(t, 9.0, sgst)
	assert.Equal(t, 0.0, igst)

	// fractional rounding
	cgst, sgst, _ = ComputeLineTax(33.33, 18, true)
	// half rate 9% => 33.33*9/100=2.9997 round2 => 3.00
	assert.Equal(t, 3.0, cgst)
	assert.Equal(t, 3.0, sgst)
}

func TestComputeLineTax_InterState(t *testing.T) {
	cgst, sgst, igst := ComputeLineTax(1000, 18, false)
	assert.Equal(t, 0.0, cgst)
	assert.Equal(t, 0.0, sgst)
	assert.Equal(t, 180.0, igst)

	_, _, igst = ComputeLineTax(33.33, 18, false)
	assert.Equal(t, 6.0, igst) // 33.33*18/100=5.9994 ->6.00
}

func TestValidateInvoiceAmounts(t *testing.T) {
	require.NoError(t, validateInvoiceAmounts(100, 18, 0, 118))
	require.NoError(t, validateInvoiceAmounts(0, 0, 0, 0))
	// epsilon allowed
	require.NoError(t, validateInvoiceAmounts(100, 18, 0, 118.005))
	require.Error(t, validateInvoiceAmounts(100, 18, 0, -1))
	assert.Contains(t, validateInvoiceAmounts(100, 18, 0, -1).Error(), "total cannot be negative")
	require.Error(t, validateInvoiceAmounts(-1, 0, 0, -1))
	require.Error(t, validateInvoiceAmounts(100, -1, 0, 99))
	require.Error(t, validateInvoiceAmounts(100, 0, -1, 99))
	require.Error(t, validateInvoiceAmounts(100, 10, 0, 200))
	assert.Contains(t, validateInvoiceAmounts(100, 10, 0, 200).Error(), "does not match")
}

func TestAttachLineItems_FreightAndDedupeAndRounding(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	inv := aggregate.NewInvoiceAggregate("inv-attach", "1", "INV-001", "bk-1", "cust-1", nil, 500, 50, 0, 550, aggregate.PaymentStatusPending, clock.Now())
	inv.ClearEvents()
	require.Empty(t, inv.LineItems)
	idGen := &seqIDGen{}
	ref1 := "ref-1"
	ref2 := "ref-2"
	cmd := GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk-1",
		LineItems: []InvoiceLineItemInput{
			{LineType: aggregate.LineTypeDetention, Description: "Det1", Quantity: 1.333, UnitPrice: 100.00, RefID: &ref1},
			{LineType: aggregate.LineTypeDetention, Description: "Det2", Quantity: 2, UnitPrice: 50, RefID: &ref2},
		},
	}
	attachLineItems(inv, cmd, idGen)
	// freight + 2 distinct detention = 3
	require.Len(t, inv.LineItems, 3)
	// check amounts rounded
	for _, li := range inv.LineItems {
		if li.LineType == aggregate.LineTypeFreight {
			assert.Equal(t, 500.0, li.Amount)
		}
		if li.RefID != nil && *li.RefID == "ref-1" {
			// 1.333*100 =133.3 rounded to 133.30?
			// RoundMoney does math.Round(v*100)/100 => 133.3
			assert.Equal(t, 133.3, li.Amount)
		}
	}
	// subtotal should be sum amounts
	expectedSubtotal := 500.0 + 133.3 + 100.0
	assert.Equal(t, expectedSubtotal, inv.Subtotal)

	// second attach with same refs should not duplicate, but new ref should be added
	newRef := "ref-3"
	cmd2 := GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk-1",
		LineItems: []InvoiceLineItemInput{
			{LineType: aggregate.LineTypeDetention, Description: "Det again", Quantity: 1, UnitPrice: 100, RefID: &ref1}, // deduped
			{LineType: aggregate.LineTypeDetention, Description: "Det new", Quantity: 1, UnitPrice: 10, RefID: &newRef},
		},
	}
	attachLineItems(inv, cmd2, idGen)
	assert.Len(t, inv.LineItems, 4) // one more (deduped ref-1 skipped)

	// duplicate within same batch is NOT deduped against each other (only against existing refs)
	dupWithin := "dup-within"
	cmd3 := GenerateInvoiceCommand{
		TenantID:  "1",
		BookingID: "bk-1",
		LineItems: []InvoiceLineItemInput{
			{LineType: aggregate.LineTypeDetention, Description: "Dup1", Quantity: 1, UnitPrice: 10, RefID: &dupWithin},
			{LineType: aggregate.LineTypeDetention, Description: "Dup2", Quantity: 1, UnitPrice: 10, RefID: &dupWithin},
		},
	}
	before := len(inv.LineItems)
	attachLineItems(inv, cmd3, idGen)
	// both dups in same batch are added because existingRefs didn't contain it beforehand
	assert.Equal(t, before+2, len(inv.LineItems))
}

func TestResolveBookingPricing(t *testing.T) {
	bookingRepo := &mockBookingRepoApp{
		getReadModelResult: bookingdomain.BookingReadModel{Price: 1000},
	}
	settingsRepo := &mockSettingsRepoApp{settings: companydomain.CompanySettings{GSTEnabled: true, GSTRate: 10}}
	prov := &mockProviderAppInv{booking: bookingRepo, audit: settingsRepo, inv: &mockInvoiceRepoApp{}}
	tx := &mockTxAppInv{Context: context.Background(), prov: prov}
	pricing, ok := resolveBookingPricing(tx, "1", "bk-1")
	require.True(t, ok)
	assert.Equal(t, 1000.0, pricing.subtotal)
	assert.Equal(t, 100.0, pricing.tax)
	assert.Equal(t, 1100.0, pricing.total)

	// GST disabled
	settingsRepo2 := &mockSettingsRepoApp{settings: companydomain.CompanySettings{GSTEnabled: false}}
	prov2 := &mockProviderAppInv{booking: bookingRepo, audit: settingsRepo2}
	tx2 := &mockTxAppInv{Context: context.Background(), prov: prov2}
	pricing2, ok := resolveBookingPricing(tx2, "1", "bk-1")
	require.True(t, ok)
	assert.Equal(t, 0.0, pricing2.tax)

	// bookings repo error -> false
	bookingErr := &mockBookingRepoApp{getReadModelErr: errors.New("not found")}
	prov3 := &mockProviderAppInv{booking: bookingErr, audit: settingsRepo}
	tx3 := &mockTxAppInv{Context: context.Background(), prov: prov3}
	_, ok = resolveBookingPricing(tx3, "1", "bk-1")
	assert.False(t, ok)

	// bookings type assertion fails -> false
	prov4 := &mockProviderAppInv{booking: "bad", audit: settingsRepo}
	tx4 := &mockTxAppInv{Context: context.Background(), prov: prov4}
	_, ok = resolveBookingPricing(tx4, "1", "bk-1")
	assert.False(t, ok)

	// audit logs not found but still true with tax 0
	prov5 := &mockProviderAppInv{booking: bookingRepo, audit: nil}
	tx5 := &mockTxAppInv{Context: context.Background(), prov: prov5}
	pricing5, ok := resolveBookingPricing(tx5, "1", "bk-1")
	require.True(t, ok)
	assert.Equal(t, 0.0, pricing5.tax)

	// negative price clamped
	negBooking := &mockBookingRepoApp{getReadModelResult: bookingdomain.BookingReadModel{Price: -50}}
	prov6 := &mockProviderAppInv{booking: negBooking, audit: settingsRepo}
	tx6 := &mockTxAppInv{Context: context.Background(), prov: prov6}
	pricing6, ok := resolveBookingPricing(tx6, "1", "bk-1")
	require.True(t, ok)
	assert.Equal(t, 0.0, pricing6.subtotal)
	assert.Equal(t, 0.0, pricing6.tax)
}

func TestRound2(t *testing.T) {
	assert.Equal(t, 1.23, round2(1.234))
	assert.Equal(t, 1.24, round2(1.235))
	assert.Equal(t, 0.0, round2(0))
	assert.Equal(t, 2.0, round2(1.999))
}

// ---- VoidInvoice ----

func TestVoidInvoice_MissingID(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	uow := &mockUoWAppInv{inv: &mockInvoiceRepoApp{}}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice ID is required")
}

func TestVoidInvoice_RepoTypeFailure(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	uow := &mockUoWAppInv{inv: "bad"}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve invoice repository")
}

func TestVoidInvoice_NotFound(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	invRepo := &mockInvoiceRepoApp{findErr: sql.ErrNoRows}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice not found")
}

func TestVoidInvoice_FindError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	invRepo := &mockInvoiceRepoApp{findErr: errors.New("db fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db fail")
}

func TestVoidInvoice_AlreadyCancelled(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	inv := newTestInvoice("inv-cancelled", aggregate.PaymentStatusPending)
	_ = inv.Void(clock.Now())
	require.Equal(t, aggregate.InvoiceStatusCancelled, inv.Status)
	invRepo := &mockInvoiceRepoApp{findResult: inv}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-cancelled"})
	require.ErrorIs(t, err, ErrInvoiceAlreadyCancelled)
}

func TestVoidInvoice_PaidCannotVoid(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	inv := newTestInvoice("inv-paid-void", aggregate.PaymentStatusPending)
	inv.Status = aggregate.InvoiceStatusPaid
	invRepo := &mockInvoiceRepoApp{findResult: inv}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-paid-void"})
	require.ErrorIs(t, err, ErrInvoiceCannotVoid)
}

func TestVoidInvoice_Success(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	inv := newTestInvoice("inv-void-ok", aggregate.PaymentStatusPending)
	// status is Outstanding by default, Void should succeed
	invRepo := &mockInvoiceRepoApp{findResult: inv}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-void-ok"})
	require.NoError(t, err)
	require.Len(t, invRepo.saved, 1)
	assert.Equal(t, aggregate.InvoiceStatusCancelled, invRepo.saved[0].Status)
	// also check via New constructor
	uc2 := NewVoidInvoiceUseCase(&mockUoWAppInv{inv: &mockInvoiceRepoApp{findResult: newTestInvoice("inv-2", aggregate.PaymentStatusPending)}}, clock)
	assert.NotNil(t, uc2)
}

func TestVoidInvoice_SaveError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	inv := newTestInvoice("inv-save-err", aggregate.PaymentStatusPending)
	invRepo := &mockInvoiceRepoApp{findResult: inv, saveErr: errors.New("save fail")}
	uow := &mockUoWAppInv{inv: invRepo}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-save-err"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "save fail")
}

func TestVoidInvoice_UoWError(t *testing.T) {
	clock := &fakeClockApp{now: time.Now()}
	uow := &mockUoWAppInv{execErr: errors.New("uow fail")}
	uc := NewVoidInvoiceUseCase(uow, clock)
	err := uc.Execute(context.Background(), VoidInvoiceCommand{TenantID: "1", ID: "inv-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uow fail")
}
