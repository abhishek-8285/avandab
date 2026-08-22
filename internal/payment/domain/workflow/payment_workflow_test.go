package workflow

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	invoiceDomain "transport-app/internal/invoice/domain"
	invoiceAgg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/payment/application"
	"transport-app/internal/payment/domain"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

func TestNewPaymentWorkflow(t *testing.T) {
	w := NewPaymentWorkflow(nil, nil, nil)
	require.NotNil(t, w)
	clock := &fakeClock{now: time.Now()}
	idGen := &fakeIDGen{}
	uow := newFakeUoW()
	recordUC := application.NewRecordPaymentUseCase(uow, idGen, clock)
	getUC := application.NewGetPaymentUseCase(uow)
	listUC := application.NewListPaymentsUseCase(uow)
	w2 := NewPaymentWorkflow(recordUC, getUC, listUC)
	require.NotNil(t, w2)
	assert.NotNil(t, w2.recordUC)
	assert.NotNil(t, w2.getUC)
	assert.NotNil(t, w2.listUC)
}

func TestPaymentWorkflow_CanProcessPayment(t *testing.T) {
	w := NewPaymentWorkflow(nil, nil, nil)
	assert.True(t, w.CanProcessPayment("pending"))
	assert.True(t, w.CanProcessPayment("partially_paid"))
	assert.False(t, w.CanProcessPayment("paid"))
	assert.False(t, w.CanProcessPayment("cancelled"))
	assert.False(t, w.CanProcessPayment(""))
	assert.False(t, w.CanProcessPayment("draft"))
	assert.False(t, w.CanProcessPayment("Pending"))
}

func TestPaymentWorkflow_RecordPayment_Delegates(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	idGen := &fakeIDGen{}
	uow := newFakeUoW()
	invID := seedInvoiceWorkflow(t, uow.repos.invoices, clock, 1000)
	recordUC := application.NewRecordPaymentUseCase(uow, idGen, clock)
	getUC := application.NewGetPaymentUseCase(uow)
	listUC := application.NewListPaymentsUseCase(uow)
	w := NewPaymentWorkflow(recordUC, getUC, listUC)

	ctx := context.Background()
	cmd := application.RecordPaymentCommand{
		TenantID:    shared.TenantID("1"),
		InvoiceID:   string(invID),
		PaymentDate: clock.Now(),
		Amount:      500,
		Method:      paymentagg.PaymentMethodCash,
	}
	id, err := w.RecordPayment(ctx, cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	payments, err := uow.repos.payments.GetPaymentsByInvoice(ctx, string(invID), "1")
	require.NoError(t, err)
	require.Len(t, payments, 1)
	assert.Equal(t, 500.0, payments[0].Amount)
}

// ---- helpers ----

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDGen struct {
	next int
}

func (g *fakeIDGen) GenerateUUID() string {
	g.next++
	return "uuid-wf-" + string(rune('0'+g.next))
}

func (g *fakeIDGen) GenerateDisplayID(prefix string) string {
	g.next++
	return prefix + "-123"
}

func newFakeUoW() *fakeUnitOfWork {
	return &fakeUnitOfWork{
		repos: &fakeRepoProvider{
			payments: &fakePaymentRepo{
				byID:           make(map[paymentagg.PaymentID]*paymentagg.PaymentAggregate),
				byRef:          make(map[string]paymentagg.PaymentID),
				byRazorpayPay:  make(map[string]paymentagg.PaymentID),
				byWebhookEvent: make(map[string]paymentagg.PaymentID),
			},
			invoices: &fakeInvoiceRepo{
				byID: make(map[invoiceAgg.InvoiceID]*invoiceAgg.InvoiceAggregate),
			},
		},
	}
}

func seedInvoiceWorkflow(t *testing.T, repo *fakeInvoiceRepo, clock *fakeClock, total float64) invoiceAgg.InvoiceID {
	t.Helper()
	id := invoiceAgg.InvoiceID("inv-wf-" + time.Now().Format("150405.000000000"))
	inv := invoiceAgg.NewInvoiceAggregate(
		id,
		"1",
		"INV-WF-001",
		"bk-1",
		"cust-1",
		nil,
		total-180,
		180,
		0,
		total,
		invoiceAgg.PaymentStatusPending,
		clock.Now(),
	)
	require.NoError(t, repo.Save(context.Background(), inv))
	return id
}

type fakeRepoProvider struct {
	payments *fakePaymentRepo
	invoices *fakeInvoiceRepo
}

func (f *fakeRepoProvider) Bookings() any    { return nil }
func (f *fakeRepoProvider) Trips() any       { return nil }
func (f *fakeRepoProvider) Drivers() any     { return nil }
func (f *fakeRepoProvider) Vehicles() any    { return nil }
func (f *fakeRepoProvider) Invoices() any    { return f.invoices }
func (f *fakeRepoProvider) Payments() any    { return f.payments }
func (f *fakeRepoProvider) AuditLogs() any   { return nil }
func (f *fakeRepoProvider) Maintenance() any { return nil }

type fakeTxContext struct {
	context.Context
	repos *fakeRepoProvider
}

func (tx *fakeTxContext) Repositories() ports.RepositoryProvider { return tx.repos }

type fakeUnitOfWork struct {
	repos *fakeRepoProvider
}

func (u *fakeUnitOfWork) Execute(ctx context.Context, fn func(ports.TxContext) error) error {
	return fn(&fakeTxContext{Context: ctx, repos: u.repos})
}

type fakePaymentRepo struct {
	mu             sync.Mutex
	byID           map[paymentagg.PaymentID]*paymentagg.PaymentAggregate
	byRef          map[string]paymentagg.PaymentID
	byRazorpayPay  map[string]paymentagg.PaymentID
	byWebhookEvent map[string]paymentagg.PaymentID
}

func (r *fakePaymentRepo) Save(_ context.Context, p *paymentagg.PaymentAggregate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID] = p
	if p.Reference != nil && *p.Reference != "" {
		r.byRef[*p.Reference] = p.ID
	}
	return nil
}

func (r *fakePaymentRepo) Find(_ context.Context, id paymentagg.PaymentID, _ shared.TenantID) (*paymentagg.PaymentAggregate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.byID[id]; ok {
		return p, nil
	}
	return nil, sql.ErrNoRows
}

func (r *fakePaymentRepo) FindByReference(_ context.Context, reference string, _ shared.TenantID) (paymentagg.PaymentID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byRef[reference]; ok {
		return id, nil
	}
	return "", sql.ErrNoRows
}

func (r *fakePaymentRepo) GetReadModel(_ context.Context, _ paymentagg.PaymentID, _ shared.TenantID) (domain.PaymentReadModel, error) {
	return domain.PaymentReadModel{}, nil
}

func (r *fakePaymentRepo) GetPaymentsByInvoice(_ context.Context, invoiceID string, _ shared.TenantID) ([]domain.PaymentReadModel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.PaymentReadModel
	for _, p := range r.byID {
		if p.InvoiceID == invoiceID {
			ref := ""
			if p.Reference != nil {
				ref = *p.Reference
			}
			out = append(out, domain.PaymentReadModel{
				ID:        string(p.ID),
				InvoiceID: p.InvoiceID,
				Amount:    p.Amount,
				Method:    string(p.Method),
				Reference: &ref,
			})
		}
	}
	return out, nil
}

func (r *fakePaymentRepo) SearchReadModels(_ context.Context, _ shared.TenantID, _ string, _, _ int) ([]domain.PaymentReadModel, int64, error) {
	return nil, 0, nil
}

func (r *fakePaymentRepo) SetRazorpayFields(_ context.Context, id paymentagg.PaymentID, _ shared.TenantID, _ string, paymentID string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return sql.ErrNoRows
	}
	if paymentID != "" {
		r.byRazorpayPay[paymentID] = id
	}
	return nil
}

func (r *fakePaymentRepo) ExistsRazorpayPayment(_ context.Context, _ shared.TenantID, paymentID string) (paymentagg.PaymentID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byRazorpayPay[paymentID]; ok {
		return id, nil
	}
	return "", sql.ErrNoRows
}

func (r *fakePaymentRepo) ExistsWebhookEvent(_ context.Context, _ shared.TenantID, eventID string) (paymentagg.PaymentID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byWebhookEvent[eventID]; ok {
		return id, nil
	}
	return "", sql.ErrNoRows
}

func (r *fakePaymentRepo) SetWebhookEventID(_ context.Context, id paymentagg.PaymentID, _ shared.TenantID, eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byWebhookEvent[eventID] = id
	return nil
}

type fakeInvoiceRepo struct {
	mu   sync.Mutex
	byID map[invoiceAgg.InvoiceID]*invoiceAgg.InvoiceAggregate
}

func (r *fakeInvoiceRepo) Save(_ context.Context, inv *invoiceAgg.InvoiceAggregate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[inv.ID] = inv
	return nil
}

func (r *fakeInvoiceRepo) Find(_ context.Context, id invoiceAgg.InvoiceID, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inv, ok := r.byID[id]; ok {
		return inv, nil
	}
	return nil, sql.ErrNoRows
}

func (r *fakeInvoiceRepo) FindByBookingID(_ context.Context, _ string, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	return nil, sql.ErrNoRows
}

func (r *fakeInvoiceRepo) FindByTripID(_ context.Context, _ string, _ shared.TenantID) (*invoiceAgg.InvoiceAggregate, error) {
	return nil, sql.ErrNoRows
}

func (r *fakeInvoiceRepo) GetReadModel(_ context.Context, _ invoiceAgg.InvoiceID, _ shared.TenantID) (invoiceDomain.InvoiceReadModel, error) {
	return invoiceDomain.InvoiceReadModel{}, nil
}

func (r *fakeInvoiceRepo) SearchReadModels(_ context.Context, _ shared.TenantID, _, _ string, _, _ int) ([]invoiceDomain.InvoiceReadModel, int64, error) {
	return nil, 0, nil
}
