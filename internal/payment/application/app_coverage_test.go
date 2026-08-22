package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoiceDomain "transport-app/internal/invoice/domain"
	invoiceAgg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/payment/razorpay"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

// ---- mock repos for isolated tests ----

type mockPayRepoApp struct {
	saveErr              error
	findErr              error
	findResult           *paymentagg.PaymentAggregate
	getReadModelErr      error
	getReadModelResult   domain.PaymentReadModel
	getByInvoiceErr      error
	getByInvoiceResult   []domain.PaymentReadModel
	searchErr            error
	searchResult         []domain.PaymentReadModel
	searchTotal          int64
	existsRazorpayErr    error
	existsRazorpayResult paymentagg.PaymentID
	setRazorpayErr       error
	saved                []*paymentagg.PaymentAggregate
}

func (m *mockPayRepoApp) Save(_ context.Context, p *paymentagg.PaymentAggregate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, p)
	return nil
}
func (m *mockPayRepoApp) Find(_ context.Context, _ paymentagg.PaymentID, _ shared.TenantID) (*paymentagg.PaymentAggregate, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.findResult != nil {
		return m.findResult, nil
	}
	return nil, sql.ErrNoRows
}
func (m *mockPayRepoApp) FindByReference(_ context.Context, _ string, _ shared.TenantID) (paymentagg.PaymentID, error) {
	return "", sql.ErrNoRows
}
func (m *mockPayRepoApp) GetReadModel(_ context.Context, _ paymentagg.PaymentID, _ shared.TenantID) (domain.PaymentReadModel, error) {
	if m.getReadModelErr != nil {
		return domain.PaymentReadModel{}, m.getReadModelErr
	}
	return m.getReadModelResult, nil
}
func (m *mockPayRepoApp) GetPaymentsByInvoice(_ context.Context, _ string, _ shared.TenantID) ([]domain.PaymentReadModel, error) {
	if m.getByInvoiceErr != nil {
		return nil, m.getByInvoiceErr
	}
	return m.getByInvoiceResult, nil
}
func (m *mockPayRepoApp) SearchReadModels(_ context.Context, _ shared.TenantID, _ string, _, _ int) ([]domain.PaymentReadModel, int64, error) {
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	return m.searchResult, m.searchTotal, nil
}
func (m *mockPayRepoApp) SetRazorpayFields(_ context.Context, _ paymentagg.PaymentID, _ shared.TenantID, _, _, _ string) error {
	return m.setRazorpayErr
}
func (m *mockPayRepoApp) ExistsRazorpayPayment(_ context.Context, _ shared.TenantID, _ string) (paymentagg.PaymentID, error) {
	if m.existsRazorpayErr != nil {
		return "", m.existsRazorpayErr
	}
	if m.existsRazorpayResult != "" {
		return m.existsRazorpayResult, nil
	}
	return "", sql.ErrNoRows
}
func (m *mockPayRepoApp) ExistsWebhookEvent(_ context.Context, _ shared.TenantID, _ string) (paymentagg.PaymentID, error) {
	return "", sql.ErrNoRows
}
func (m *mockPayRepoApp) SetWebhookEventID(_ context.Context, _ paymentagg.PaymentID, _ shared.TenantID, _ string) error {
	return nil
}

type mockInvRepoApp struct {
	findErr    error
	findResult *invoiceAgg.InvoiceAggregate
	saveErr    error
}

func (m *mockInvRepoApp) Save(_ context.Context, inv *invoiceAgg.InvoiceAggregate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.findResult = inv
	return nil
}
func (m *mockInvRepoApp) Find(_ context.Context, _ invoiceAgg.InvoiceID, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.findResult != nil {
		return m.findResult, nil
	}
	return nil, sql.ErrNoRows
}
func (m *mockInvRepoApp) FindByBookingID(_ context.Context, _ string, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	return nil, sql.ErrNoRows
}
func (m *mockInvRepoApp) FindByTripID(_ context.Context, _ string, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	return nil, sql.ErrNoRows
}
func (m *mockInvRepoApp) GetReadModel(_ context.Context, _ invoiceAgg.InvoiceID, _ shared.TenantID) (invoiceDomain.InvoiceReadModel, error) {
	return invoiceDomain.InvoiceReadModel{}, nil
}
func (m *mockInvRepoApp) SearchReadModels(_ context.Context, _ shared.TenantID, _, _ string, _, _ int) ([]invoiceDomain.InvoiceReadModel, int64, error) {
	return nil, 0, nil
}

type mockProviderApp struct {
	pay any
	inv any
}

func (m *mockProviderApp) Bookings() any    { return nil }
func (m *mockProviderApp) Trips() any       { return nil }
func (m *mockProviderApp) Drivers() any     { return nil }
func (m *mockProviderApp) Vehicles() any    { return nil }
func (m *mockProviderApp) Invoices() any    { return m.inv }
func (m *mockProviderApp) Payments() any    { return m.pay }
func (m *mockProviderApp) AuditLogs() any   { return nil }
func (m *mockProviderApp) Maintenance() any { return nil }

type mockTxApp struct {
	context.Context
	prov ports.RepositoryProvider
}

func (m *mockTxApp) Repositories() ports.RepositoryProvider { return m.prov }

type mockUoWApp struct {
	payRepo any
	invRepo any
	execErr error
}

func (m *mockUoWApp) Execute(ctx context.Context, fn func(ports.TxContext) error) error {
	if m.execErr != nil {
		return m.execErr
	}
	prov := &mockProviderApp{pay: m.payRepo, inv: m.invRepo}
	return fn(&mockTxApp{Context: ctx, prov: prov})
}

// ---- GetPayment ----

func TestGetPayment_Success_App(t *testing.T) {
	now := time.Now()
	ref := "REF-GET"
	rem := "note"
	expected := domain.PaymentReadModel{
		ID:            "pay-1",
		InvoiceID:     "inv-1",
		InvoiceNumber: "INV-0001",
		PaymentDate:   now,
		Amount:        123.45,
		Method:        "cash",
		Reference:     &ref,
		Remarks:       &rem,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	payRepo := &mockPayRepoApp{getReadModelResult: expected}
	uow := &mockUoWApp{payRepo: payRepo, invRepo: &mockInvRepoApp{}}
	uc := NewGetPaymentUseCase(uow)
	dto, err := uc.Execute(context.Background(), GetPaymentQuery{ID: "pay-1", TenantID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "pay-1", dto.ID)
	assert.Equal(t, "inv-1", dto.InvoiceID)
	assert.Equal(t, "INV-0001", dto.InvoiceNumber)
	assert.Equal(t, 123.45, dto.Amount)
	require.NotNil(t, dto.Reference)
	assert.Equal(t, "REF-GET", *dto.Reference)
}

func TestGetPayment_NotFound_App(t *testing.T) {
	payRepo := &mockPayRepoApp{getReadModelErr: sql.ErrNoRows}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewGetPaymentUseCase(uow)
	_, err := uc.Execute(context.Background(), GetPaymentQuery{ID: "none", TenantID: "1"})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetPayment_RepoTypeAssertionFailure_App(t *testing.T) {
	uow := &mockUoWApp{payRepo: "not-a-repo"}
	uc := NewGetPaymentUseCase(uow)
	_, err := uc.Execute(context.Background(), GetPaymentQuery{ID: "pay-1", TenantID: "1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve payment repository")
}

func TestGetPayment_UoWError_App(t *testing.T) {
	uow := &mockUoWApp{execErr: errors.New("uow fail")}
	uc := NewGetPaymentUseCase(uow)
	_, err := uc.Execute(context.Background(), GetPaymentQuery{ID: "pay-1", TenantID: "1"})
	require.Error(t, err)
}

// ---- ListPayments ----

func TestListPayments_DefaultsAndSuccess_App(t *testing.T) {
	now := time.Now()
	ref := "REF-LIST"
	rows := []domain.PaymentReadModel{
		{ID: "pay-1", InvoiceID: "inv-1", InvoiceNumber: "INV-1", PaymentDate: now, Amount: 100, Method: "cash", Reference: &ref, CreatedAt: now, UpdatedAt: now},
		{ID: "pay-2", InvoiceID: "inv-2", InvoiceNumber: "INV-2", PaymentDate: now, Amount: 200, Method: "upi", CreatedAt: now, UpdatedAt: now},
	}
	payRepo := &mockPayRepoApp{searchResult: rows, searchTotal: 2}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewListPaymentsUseCase(uow)
	res, err := uc.Execute(context.Background(), ListPaymentsQuery{TenantID: "1", Page: 0, Limit: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.Total)
	require.Len(t, res.Payments, 2)
	assert.Equal(t, "pay-1", res.Payments[0].ID)
}

func TestListPayments_FilterByMethod_App(t *testing.T) {
	now := time.Now()
	rows := []domain.PaymentReadModel{{ID: "pay-upi", InvoiceID: "inv-1", Method: "upi", Amount: 50, PaymentDate: now, CreatedAt: now, UpdatedAt: now}}
	payRepo := &mockPayRepoApp{searchResult: rows, searchTotal: 1}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewListPaymentsUseCase(uow)
	res, err := uc.Execute(context.Background(), ListPaymentsQuery{TenantID: "1", Method: "upi", Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Total)
	assert.Equal(t, "upi", res.Payments[0].Method)
}

func TestListPayments_RepoError_App(t *testing.T) {
	payRepo := &mockPayRepoApp{searchErr: errors.New("search fail")}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewListPaymentsUseCase(uow)
	_, err := uc.Execute(context.Background(), ListPaymentsQuery{TenantID: "1", Page: 1, Limit: 10})
	require.Error(t, err)
}

func TestListPayments_RepoTypeAssertionFailure_App(t *testing.T) {
	uow := &mockUoWApp{payRepo: "bad"}
	uc := NewListPaymentsUseCase(uow)
	_, err := uc.Execute(context.Background(), ListPaymentsQuery{TenantID: "1"})
	require.Error(t, err)
}

// ---- ListPaymentsByInvoice ----

func TestListPaymentsByInvoice_Success_App(t *testing.T) {
	now := time.Now()
	rows := []domain.PaymentReadModel{
		{ID: "pay-1", InvoiceID: "inv-1", InvoiceNumber: "INV-1", Amount: 100, PaymentDate: now, Method: "cash", CreatedAt: now, UpdatedAt: now},
	}
	payRepo := &mockPayRepoApp{getByInvoiceResult: rows}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewListPaymentsByInvoiceUseCase(uow)
	dtos, err := uc.Execute(context.Background(), ListPaymentsByInvoiceQuery{TenantID: "1", InvoiceID: "inv-1"})
	require.NoError(t, err)
	require.Len(t, dtos, 1)
	assert.Equal(t, "pay-1", dtos[0].ID)
}

func TestListPaymentsByInvoice_EmptyInvoiceID_App(t *testing.T) {
	uow := &mockUoWApp{payRepo: &mockPayRepoApp{}}
	uc := NewListPaymentsByInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), ListPaymentsByInvoiceQuery{TenantID: "1", InvoiceID: ""})
	require.Error(t, err)
}

func TestListPaymentsByInvoice_RepoError_App(t *testing.T) {
	payRepo := &mockPayRepoApp{getByInvoiceErr: errors.New("fail")}
	uow := &mockUoWApp{payRepo: payRepo}
	uc := NewListPaymentsByInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), ListPaymentsByInvoiceQuery{TenantID: "1", InvoiceID: "inv-1"})
	require.Error(t, err)
}

func TestListPaymentsByInvoice_RepoTypeAssertionFailure_App(t *testing.T) {
	uow := &mockUoWApp{payRepo: "bad"}
	uc := NewListPaymentsByInvoiceUseCase(uow)
	_, err := uc.Execute(context.Background(), ListPaymentsByInvoiceQuery{TenantID: "1", InvoiceID: "inv-1"})
	require.Error(t, err)
}

// ---- RecordPayment validation ----

func TestRecordPayment_ValidationErrors_App(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	idGen := &fakeIDGen{}
	uow := newFakeUnitOfWork()
	uc := NewRecordPaymentUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: "", Amount: 100})
	require.Error(t, err)
	_, err = uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: "inv-1", Amount: 0})
	require.Error(t, err)
	_, err = uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: "inv-1", Amount: -10})
	require.Error(t, err)
}

func TestRecordPayment_InvoiceNotFound_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &fakeIDGen{}
	uow := newFakeUnitOfWork()
	uc := NewRecordPaymentUseCase(uow, idGen, clock)
	_, err := uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: "inv-missing", PaymentDate: clock.Now(), Amount: 100, Method: paymentagg.PaymentMethodCash})
	require.ErrorIs(t, err, ErrInvoiceNotFound)
}

func TestRecordPayment_Success_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &fakeIDGen{}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	uc := NewRecordPaymentUseCase(uow, idGen, clock)
	ref := "REF-REC"
	id, err := uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: string(invID), PaymentDate: clock.Now(), Amount: 400, Method: paymentagg.PaymentMethodUPI, Reference: &ref})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	inv, err := uow.repos.invoices.Find(context.Background(), invID, "1")
	require.NoError(t, err)
	assert.Equal(t, 400.0, inv.PaidAmount)
}

func TestRecordPayment_WithRazorpayFields_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &fakeIDGen{}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	uc := NewRecordPaymentUseCase(uow, idGen, clock)
	ref := "pay_razor_999"
	id, err := uc.Execute(context.Background(), RecordPaymentCommand{
		TenantID: "1", InvoiceID: string(invID), PaymentDate: clock.Now(), Amount: 1000, Method: paymentagg.PaymentMethodRazorpay, Reference: &ref,
		RazorpayOrderID: "order_123", RazorpayPaymentID: "pay_razor_999", RazorpaySignature: "sig",
	})
	require.NoError(t, err)
	exists, err := uow.repos.payments.ExistsRazorpayPayment(context.Background(), "1", "pay_razor_999")
	require.NoError(t, err)
	assert.Equal(t, id, exists)
}

func TestRecordPayment_InvoiceRepoTypeFailure_App(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	idGen := &fakeIDGen{}
	// UoW where Invoices returns wrong type
	badProv := &mockProviderApp{inv: "bad-invoice-type", pay: &mockPayRepoApp{}}
	badUoW := &mockUoWAppWithProv{prov: badProv}
	uc := NewRecordPaymentUseCase(badUoW, idGen, clock)
	_, err := uc.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: "inv-1", PaymentDate: clock.Now(), Amount: 100, Method: paymentagg.PaymentMethodCash})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve invoice repository")
}

type mockUoWAppWithProv struct {
	prov ports.RepositoryProvider
}

func (m *mockUoWAppWithProv) Execute(ctx context.Context, fn func(ports.TxContext) error) error {
	return fn(&mockTxApp{Context: ctx, prov: m.prov})
}

// ---- Reverse additional ----

func TestReversePayment_NotFound_App(t *testing.T) {
	ctx, _, _, reverseUC, _, _, _ := setupWebhookUnitTest(t)
	_, err := reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: "nonexistent", Reason: "refund"})
	require.ErrorIs(t, err, ErrPaymentNotFound)
}

func TestReversePayment_InvoiceNotFound_App(t *testing.T) {
	ctx, uow, _, reverseUC, _, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	payID := paymentagg.PaymentID("pay-orig-inv-missing-2")
	original := paymentagg.NewPaymentAggregate(payID, "1", string(invID), clock.Now(), 400, paymentagg.PaymentMethodCash, nil, nil, clock.Now())
	require.NoError(t, uow.repos.payments.Save(ctx, original))
	uow.repos.invoices.mu.Lock()
	delete(uow.repos.invoices.byID, invID)
	uow.repos.invoices.mu.Unlock()
	_, err := reverseUC.Execute(ctx, ReversePaymentCommand{TenantID: "1", OriginalPayID: payID, Reason: "refund"})
	require.ErrorIs(t, err, ErrInvoiceNotFound)
}

// ---- Razorpay order ----

func TestCreateRazorpayOrder_NotConfigured_App(t *testing.T) {
	uow := newFakeUnitOfWork()
	uc := NewCreateRazorpayOrderUseCase(uow, &fakeOrderCreatorApp{err: razorpay.ErrNotConfigured}, "")
	_, err := uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: "inv-1"})
	require.ErrorIs(t, err, ErrRazorpayNotConfigured)
}

func TestCreateRazorpayOrder_InvoiceNotFound_App(t *testing.T) {
	uow := newFakeUnitOfWork()
	uc := NewCreateRazorpayOrderUseCase(uow, &fakeOrderCreatorApp{}, "key_id")
	_, err := uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: "missing"})
	require.ErrorIs(t, err, ErrInvoiceNotFound)
}

func TestCreateRazorpayOrder_AlreadySettled_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &fakeIDGen{}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	recordUC := NewRecordPaymentUseCase(uow, idGen, clock)
	_, err := recordUC.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: string(invID), PaymentDate: clock.Now(), Amount: 1000, Method: paymentagg.PaymentMethodCash})
	require.NoError(t, err)
	uc := NewCreateRazorpayOrderUseCase(uow, &fakeOrderCreatorApp{}, "key_id")
	_, err = uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: string(invID)})
	require.ErrorIs(t, err, ErrInvoiceAlreadySettled)
}

func TestCreateRazorpayOrder_Success_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	creator := &fakeOrderCreatorApp{order: &razorpay.Order{ID: "order_123", Amount: 100000, Currency: "INR", Status: "created"}}
	uc := NewCreateRazorpayOrderUseCase(uow, creator, "key_test_123")
	res, err := uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: string(invID)})
	require.NoError(t, err)
	assert.Equal(t, "order_123", res.OrderID)
	assert.Equal(t, "key_test_123", res.RazorpayKeyID)
	assert.Equal(t, string(invID), res.InvoiceID)
	assert.Equal(t, string(invID), creator.gotInvoiceID)
}

func TestCreateRazorpayOrder_OrderCreatorError_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	creator := &fakeOrderCreatorApp{err: errors.New("razorpay down")}
	uc := NewCreateRazorpayOrderUseCase(uow, creator, "key_id")
	_, err := uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: string(invID)})
	require.Error(t, err)
}

func TestCreateRazorpayOrder_NotConfiguredFromCreator_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	creator := &fakeOrderCreatorApp{err: razorpay.ErrNotConfigured}
	uc := NewCreateRazorpayOrderUseCase(uow, creator, "key_id")
	_, err := uc.Execute(context.Background(), CreateRazorpayOrderCommand{TenantID: "1", InvoiceID: string(invID)})
	require.ErrorIs(t, err, ErrRazorpayNotConfigured)
}

type fakeOrderCreatorApp struct {
	order        *razorpay.Order
	err          error
	gotInvoiceID string
	gotAmount    float64
}

func (f *fakeOrderCreatorApp) CreateOrder(invoiceID string, amount float64, currency string) (*razorpay.Order, error) {
	f.gotInvoiceID = invoiceID
	f.gotAmount = amount
	if f.err != nil {
		return nil, f.err
	}
	if f.order != nil {
		return f.order, nil
	}
	return &razorpay.Order{ID: "order_fake", Amount: int64(amount * 100), Currency: currency}, nil
}

// ---- Verify ----

func TestVerifyRazorpayPayment_NotConfigured_App(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	uow := newFakeUnitOfWork()
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "", clock)
	_, err := uc.Execute(context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1")), VerifyRazorpayPaymentCommand{TenantID: "1", InvoiceID: "inv-1", OrderID: "o1", PaymentID: "p1", Signature: "sig"})
	require.ErrorIs(t, err, ErrRazorpayNotConfigured)
}

func TestVerifyRazorpayPayment_AlreadyExists_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	ref := "pay_existing_123"
	_, err := recordUC.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: string(invID), PaymentDate: clock.Now(), Amount: 500, Method: paymentagg.PaymentMethodRazorpay, Reference: &ref, RazorpayOrderID: "order_1", RazorpayPaymentID: "pay_existing_123", RazorpaySignature: "sig"})
	require.NoError(t, err)
	verifyUC := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock)
	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	id, err := verifyUC.Execute(ctx, VerifyRazorpayPaymentCommand{TenantID: "1", InvoiceID: string(invID), OrderID: "order_1", PaymentID: "pay_existing_123", Signature: "sig"})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	payments, err := uow.repos.payments.GetPaymentsByInvoice(ctx, string(invID), "1")
	require.NoError(t, err)
	assert.Len(t, payments, 1)
}

func TestVerifyRazorpayPayment_InvoiceNotFound_App(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	uow := newFakeUnitOfWork()
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock)
	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	_, err := uc.Execute(ctx, VerifyRazorpayPaymentCommand{TenantID: "1", InvoiceID: "missing", OrderID: "o1", PaymentID: "p1", Signature: "sig"})
	require.ErrorIs(t, err, ErrInvoiceNotFound)
}

func TestVerifyRazorpayPayment_AlreadySettled_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 500)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	_, err := recordUC.Execute(context.Background(), RecordPaymentCommand{TenantID: "1", InvoiceID: string(invID), PaymentDate: clock.Now(), Amount: 500, Method: paymentagg.PaymentMethodCash})
	require.NoError(t, err)
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock)
	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	_, err = uc.Execute(ctx, VerifyRazorpayPaymentCommand{TenantID: "1", InvoiceID: string(invID), OrderID: "o1", PaymentID: "pay_new", Signature: "sig"})
	require.ErrorIs(t, err, ErrInvoiceAlreadySettled)
}

func TestVerifyRazorpayPayment_Success_App(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	uow := newFakeUnitOfWork()
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	recordUC := NewRecordPaymentUseCase(uow, &fakeIDGen{}, clock)
	uc := NewVerifyRazorpayPaymentUseCase(uow, recordUC, &fakeVerifier{ok: true}, "secret", clock)
	ctx := context.WithValue(context.Background(), shared.TenantIDKey, shared.TenantID("1"))
	id, err := uc.Execute(ctx, VerifyRazorpayPaymentCommand{TenantID: "1", InvoiceID: string(invID), OrderID: "order_999", PaymentID: "pay_999", Signature: "sig_999"})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// ---- Webhook extra ----

func TestRazorpayWebhook_Execute_App(t *testing.T) {
	ctx, uow, _, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 1000)
	body := capturedWebhookBody("pay_exec_1", string(invID), 100000)
	sig := signWebhook(t, body, testWebhookSecret)
	id, err := webhookUC.Execute(ctx, body, sig)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestRazorpayWebhook_ExecuteEvent_OrderPaid_App(t *testing.T) {
	ctx, uow, _, _, webhookUC, clock, _ := setupWebhookUnitTest(t)
	invID := seedInvoice(t, uow.repos.invoices, clock, 2000)
	payload := map[string]interface{}{
		"event": "order.paid",
		"payload": map[string]interface{}{
			"order": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":          "order_paid_1",
					"amount_paid": int64(200000),
					"currency":    "INR",
					"status":      "paid",
					"notes": map[string]interface{}{
						"invoice_id": string(invID),
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	sig := signWebhook(t, body, testWebhookSecret)
	id, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_order_paid_1", RazorpayWebhookEvent{})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	payments, err := uow.repos.payments.GetPaymentsByInvoice(ctx, string(invID), "1")
	require.NoError(t, err)
	require.Len(t, payments, 1)
	assert.Equal(t, 2000.0, payments[0].Amount)
	id2, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_order_paid_1", RazorpayWebhookEvent{})
	require.NoError(t, err)
	assert.Equal(t, id, id2)
}

func TestRazorpayWebhook_ExecuteEvent_OrderPaid_MissingInvoice_App(t *testing.T) {
	ctx, _, _, _, webhookUC, _, _ := setupWebhookUnitTest(t)
	payload := map[string]interface{}{
		"event": "order.paid",
		"payload": map[string]interface{}{
			"order": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":          "order_missing_inv",
					"amount_paid": int64(100000),
					"currency":    "INR",
					"status":      "paid",
					"notes": map[string]interface{}{
						"invoice_id": "",
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	sig := signWebhook(t, body, testWebhookSecret)
	id, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_order_missing", RazorpayWebhookEvent{})
	require.NoError(t, err)
	assert.Empty(t, id)
}

func TestRazorpayWebhook_ExecuteEvent_UnknownEvent_App(t *testing.T) {
	ctx, _, _, _, webhookUC, _, _ := setupWebhookUnitTest(t)
	body := []byte(`{"event":"payment.authorized","payload":{}}`)
	sig := signWebhook(t, body, testWebhookSecret)
	id, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_unknown_1", RazorpayWebhookEvent{})
	require.NoError(t, err)
	assert.Empty(t, id)
	status := webhookUC.Status()
	assert.Equal(t, int64(1), status.Counts["payment.authorized"])
}

func TestRazorpayWebhook_ExecuteEvent_InvalidSignature_App(t *testing.T) {
	ctx, _, _, _, webhookUC, _, _ := setupWebhookUnitTest(t)
	body := capturedWebhookBody("pay_bad_sig", "inv-1", 100000)
	_, err := webhookUC.ExecuteEvent(ctx, body, "invalidsig", "evt_bad", RazorpayWebhookEvent{})
	require.ErrorIs(t, err, ErrWebhookInvalidSignature)
}

func TestRazorpayWebhook_ExecuteEvent_RefundWithoutReverseUC_App(t *testing.T) {
	ctx, uow, _, _, _, clock, _ := setupWebhookUnitTest(t)
	webhookUC := NewRazorpayWebhookUseCase(nil, uow, testWebhookSecret, clock)
	body := refundWebhookBody("rfnd_no_uc", "pay_orig", 100000)
	sig := signWebhook(t, body, testWebhookSecret)
	_, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_refund_no_uc", RazorpayWebhookEvent{})
	require.Error(t, err)
}

func TestRazorpayWebhook_ExecuteEvent_RefundMissingOriginal_App(t *testing.T) {
	ctx, _, _, _, webhookUC, _, _ := setupWebhookUnitTest(t)
	body := refundWebhookBody("rfnd_missing", "pay_nonexistent", 100000)
	sig := signWebhook(t, body, testWebhookSecret)
	_, err := webhookUC.ExecuteEvent(ctx, body, sig, "evt_refund_missing", RazorpayWebhookEvent{})
	require.ErrorIs(t, err, ErrWebhookOriginalPaymentNotFound)
}

func TestRazorpayWebhook_VerifySignature_InvalidHex_App(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	uc := NewRazorpayWebhookUseCase(nil, nil, testWebhookSecret, clock)
	err := uc.VerifySignature([]byte("body"), "not-hex-zzzz")
	require.ErrorIs(t, err, ErrWebhookInvalidSignature)
}

func TestRazorpayWebhook_Status_App(t *testing.T) {
	_, _, _, _, webhookUC, _, _ := setupWebhookUnitTest(t)
	status := webhookUC.Status()
	assert.NotNil(t, status.Counts)
}
