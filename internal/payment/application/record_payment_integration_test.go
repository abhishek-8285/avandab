package application

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	invoiceagg "transport-app/internal/invoice/domain/aggregate"
	invoicesql "transport-app/internal/invoice/infrastructure/persistence/sql"
	paymentagg "transport-app/internal/payment/domain/aggregate"
	paymentsql "transport-app/internal/payment/infrastructure/persistence/sql"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/shared/uow"
)

func setupRecordPaymentSQLTest(t *testing.T) (*sql.DB, context.Context, *RecordPaymentUseCase, RecordPaymentCommand) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, "../../../db/migrations"))
	_, err = db.Exec(`PRAGMA foreign_keys = ON`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "payment-replay-test")
	tenantID := shared.TenantIDFromContext(ctx)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Payment Replay Test', 'payment-replay-test')`, tenantID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone)
		VALUES ('replay-customer', ?, 'Replay Buyer', 'replay@example.com', '9999999999')`, tenantID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('replay-route', ?, 'Delhi', 'Jaipur', 280, 5, 1000)`, tenantID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status)
		VALUES ('replay-booking', ?, 'BK-REPLAY', 'replay-customer', ?, 'replay-route', 'truck', 1000, 'confirmed')`,
		tenantID, time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	clk := &fakeClock{now: time.Date(2026, 9, 17, 12, 34, 56, 123456789, time.UTC)}
	unitOfWork := uow.NewSQLUnitOfWork(db)
	inv := invoiceagg.NewInvoiceAggregate("replay-invoice", tenantID, "INV-REPLAY", "replay-booking",
		"replay-customer", nil, 1000, 0, 0, 1000, invoiceagg.PaymentStatusPending, clk.Now())
	require.NoError(t, unitOfWork.Execute(ctx, func(txCtx ports.TxContext) error {
		return invoicesql.NewInvoiceRepository(db).Save(txCtx, inv)
	}))
	return db, ctx, NewRecordPaymentUseCase(unitOfWork, &fakeIDGen{}, clk), RecordPaymentCommand{
		TenantID: shared.TenantIDFromContext(ctx), InvoiceID: string(inv.ID), PaymentDate: clk.Now(),
		Amount: 500, Method: paymentagg.PaymentMethodCash,
	}
}

type recordPaymentSQLState struct {
	paidAmount    float64
	paymentStatus string
	status        string
	version       int64
	payments      int
	outbox        int
}

func readRecordPaymentSQLState(t *testing.T, db *sql.DB, cmd RecordPaymentCommand) recordPaymentSQLState {
	t.Helper()
	var state recordPaymentSQLState
	require.NoError(t, db.QueryRow(`SELECT paid_amount, payment_status, status, version,
		(SELECT COUNT(*) FROM payments WHERE invoice_id = invoices.id AND tenant_id = invoices.tenant_id),
		(SELECT COUNT(*) FROM outbox_events)
		FROM invoices WHERE id = ? AND tenant_id = ?`, cmd.InvoiceID, cmd.TenantID).
		Scan(&state.paidAmount, &state.paymentStatus, &state.status, &state.version, &state.payments, &state.outbox))
	return state
}

func TestRecordPayment_SQLReplayLeavesInvoiceUnchanged(t *testing.T) {
	reference := "ordinary-payment-reference"
	blankReference := "   "
	for _, tc := range []struct {
		name      string
		reference *string
	}{
		{name: "reference", reference: &reference},
		{name: "no_reference"},
		{name: "blank_reference", reference: &blankReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, ctx, uc, cmd := setupRecordPaymentSQLTest(t)
			cmd.Reference = tc.reference
			before := readRecordPaymentSQLState(t, db, cmd)
			require.Equal(t, recordPaymentSQLState{paymentStatus: "pending", status: "outstanding", version: 1, outbox: 1}, before)

			firstID, err := uc.Execute(ctx, cmd)
			require.NoError(t, err)
			require.NotEmpty(t, firstID)
			first := readRecordPaymentSQLState(t, db, cmd)
			require.Equal(t, recordPaymentSQLState{paidAmount: 500, paymentStatus: "partially_paid", status: "outstanding", version: 2, payments: 1, outbox: 3}, first)
			stored, err := paymentsql.NewPaymentRepository(db).Find(ctx, firstID, cmd.TenantID)
			require.NoError(t, err)
			require.True(t, cmd.PaymentDate.Equal(stored.PaymentDate), "payment date must round-trip through real write path")

			replayID, err := uc.Execute(ctx, cmd)
			require.NoError(t, err)
			assert.Equal(t, firstID, replayID, "replay must return existing payment ID")
			replay := readRecordPaymentSQLState(t, db, cmd)
			assert.Equal(t, 1, replay.payments, "replay must leave one payment row")
			assert.Equal(t, first.paidAmount, replay.paidAmount, "replay must not change invoice paid_amount")
			assert.Equal(t, first.paymentStatus, replay.paymentStatus, "replay must not change invoice payment_status")
			assert.Equal(t, first.status, replay.status, "replay must not change invoice status")
			assert.Equal(t, first.version, replay.version, "replay must not change invoice version")
			assert.Equal(t, first.outbox, replay.outbox, "replay must not append outbox events")
		})
	}
}

func TestRecordPayment_SQLInvoiceFailureRollsBackClaim(t *testing.T) {
	db, ctx, uc, cmd := setupRecordPaymentSQLTest(t)
	before := readRecordPaymentSQLState(t, db, cmd)
	_, err := db.Exec(`CREATE TRIGGER fail_invoice_update BEFORE UPDATE ON invoices
		BEGIN
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM payments WHERE invoice_id = OLD.id AND tenant_id = OLD.tenant_id
			) THEN RAISE(ABORT, 'test payment claim missing before invoice save') END;
			SELECT RAISE(ABORT, 'test invoice save failure after payment claim');
		END`)
	require.NoError(t, err)

	paymentID, err := uc.Execute(ctx, cmd)
	require.ErrorContains(t, err, "test invoice save failure after payment claim")
	assert.Empty(t, paymentID)
	assert.Equal(t, before, readRecordPaymentSQLState(t, db, cmd), "invoice failure must roll back payment claim and outbox writes")

	_, err = db.Exec(`DROP TRIGGER fail_invoice_update`)
	require.NoError(t, err)
	paymentID, err = uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, paymentID)
	assert.Equal(t, recordPaymentSQLState{paidAmount: 500, paymentStatus: "partially_paid", status: "outstanding", version: 2, payments: 1, outbox: 3}, readRecordPaymentSQLState(t, db, cmd))
	stored, err := paymentsql.NewPaymentRepository(db).Find(ctx, paymentID, cmd.TenantID)
	require.NoError(t, err)
	assert.True(t, cmd.PaymentDate.Equal(stored.PaymentDate))
}

func TestRecordPayment_SQLSameReferenceIsTenantScoped(t *testing.T) {
	db, ctx, uc, cmd := setupRecordPaymentSQLTest(t)
	otherCtx := shared.ContextWithTenantID(context.Background(), "payment-replay-other")
	otherTenant := shared.TenantIDFromContext(otherCtx)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Other Buyer Tenant', 'payment-replay-other')`, otherTenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, email, phone)
		VALUES ('other-customer', ?, 'Other Buyer', 'other@example.com', '8888888888')`, otherTenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('other-route', ?, 'Delhi', 'Jaipur', 280, 5, 1000)`, otherTenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status)
		VALUES ('other-booking', ?, 'BK-OTHER', 'other-customer', ?, 'other-route', 'truck', 1000, 'confirmed')`, otherTenant, cmd.PaymentDate)
	require.NoError(t, err)
	inv := invoiceagg.NewInvoiceAggregate("other-invoice", otherTenant, "INV-OTHER", "other-booking",
		"other-customer", nil, 1000, 0, 0, 1000, invoiceagg.PaymentStatusPending, cmd.PaymentDate)
	require.NoError(t, uow.NewSQLUnitOfWork(db).Execute(otherCtx, func(txCtx ports.TxContext) error {
		return invoicesql.NewInvoiceRepository(db).Save(txCtx, inv)
	}))

	reference := "same-reference-across-tenants"
	cmd.Reference = &reference
	otherCmd := cmd
	otherCmd.TenantID = shared.TenantIDFromContext(otherCtx)
	otherCmd.InvoiceID = string(inv.ID)
	firstID, err := uc.Execute(ctx, cmd)
	require.NoError(t, err)
	otherID, err := uc.Execute(otherCtx, otherCmd)
	require.NoError(t, err)
	require.NotEmpty(t, firstID)
	require.NotEmpty(t, otherID)
	assert.NotEqual(t, firstID, otherID, "same reference must create independent payments across tenants")
	first := readRecordPaymentSQLState(t, db, cmd)
	other := readRecordPaymentSQLState(t, db, otherCmd)
	expected := recordPaymentSQLState{paidAmount: 500, paymentStatus: "partially_paid", status: "outstanding", version: 2, payments: 1, outbox: 6}
	require.Equal(t, expected, first)
	require.Equal(t, expected, other)
	for _, tc := range []struct {
		ctx context.Context
		cmd RecordPaymentCommand
		id  paymentagg.PaymentID
	}{
		{ctx: ctx, cmd: cmd, id: firstID},
		{ctx: otherCtx, cmd: otherCmd, id: otherID},
	} {
		replayID, err := uc.Execute(tc.ctx, tc.cmd)
		require.NoError(t, err)
		assert.Equal(t, tc.id, replayID)
		assert.Equal(t, first, readRecordPaymentSQLState(t, db, cmd), "replay must leave first tenant unchanged")
		assert.Equal(t, other, readRecordPaymentSQLState(t, db, otherCmd), "replay must leave other tenant unchanged")
	}
}

func TestRecordPayment_SQLPaymentFailureRollsBackInvoice(t *testing.T) {
	db, ctx, uc, cmd := setupRecordPaymentSQLTest(t)
	before := readRecordPaymentSQLState(t, db, cmd)
	_, err := db.Exec(`CREATE TRIGGER fail_payment_insert BEFORE INSERT ON payments
		BEGIN SELECT RAISE(ABORT, 'test payment insert failure'); END`)
	require.NoError(t, err)

	paymentID, err := uc.Execute(ctx, cmd)
	require.ErrorContains(t, err, "test payment insert failure")
	assert.Empty(t, paymentID, "failed payment must not return an ID")
	assert.Equal(t, before, readRecordPaymentSQLState(t, db, cmd), "payment failure must roll back invoice, payment and outbox writes")

	_, err = db.Exec(`DROP TRIGGER fail_payment_insert`)
	require.NoError(t, err)
	paymentID, err = uc.Execute(ctx, cmd)
	require.NoError(t, err)
	assert.NotEmpty(t, paymentID)
	assert.Equal(t, recordPaymentSQLState{paidAmount: 500, paymentStatus: "partially_paid", status: "outstanding", version: 2, payments: 1, outbox: 3}, readRecordPaymentSQLState(t, db, cmd))
}
