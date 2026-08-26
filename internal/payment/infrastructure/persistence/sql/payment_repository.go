package sql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/payment/domain"
	"transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/payment/infrastructure/persistence/sql/converters"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/outbox"
)

type paymentRepository struct {
	dbConn *sql.DB
	q      *db.Queries
	outbox *outbox.OutboxWriter
}

// NewPaymentRepository creates a SQLite-backed implementation of PaymentRepository.
func NewPaymentRepository(dbConn *sql.DB) domain.PaymentRepository {
	return &paymentRepository{
		dbConn: dbConn,
		q:      db.New(dbConn),
		outbox: outbox.NewOutboxWriter(dbConn),
	}
}

func (r *paymentRepository) Q(ctx context.Context) *db.Queries {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return r.q.WithTx(tx)
	}
	return r.q
}

func (r *paymentRepository) Save(ctx context.Context, p *aggregate.PaymentAggregate) error {
	key := idempotencyKey(p)

	if key != "" {
		existingID, err := r.findIDByIdempotencyKey(ctx, p.TenantID, key)
		if err == nil && existingID != "" {
			p.ID = aggregate.PaymentID(existingID)
			return nil
		}
	}

	var reference, remarks sql.NullString
	if p.Reference != nil {
		reference = sql.NullString{String: *p.Reference, Valid: true}
	}
	if p.Remarks != nil {
		remarks = sql.NullString{String: *p.Remarks, Valid: true}
	}

	_, err := r.Q(ctx).GetPaymentByID(ctx, db.GetPaymentByIDParams{
		ID:       string(p.ID),
		TenantID: string(p.TenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if key != "" {
				err = r.insertPayment(ctx, p, key)
				if err != nil && isIdempotencyConflict(err) {
					if existingID, e := r.findIDByIdempotencyKey(ctx, p.TenantID, key); e == nil && existingID != "" {
						p.ID = aggregate.PaymentID(existingID)
						return nil
					}
				}
				if err != nil {
					return err
				}
			} else {
				_, err = r.Q(ctx).CreatePayment(ctx, db.CreatePaymentParams{
					ID:          string(p.ID),
					InvoiceID:   p.InvoiceID,
					PaymentDate: p.PaymentDate,
					Amount:      p.Amount,
					Method:      string(p.Method),
					Reference:   reference,
					Remarks:     remarks,
					TenantID:    string(p.TenantID),
				})
				if err != nil {
					return err
				}
			}
		} else {
			return err
		}
	} else {
		return errors.New("updating payments is not allowed (immutable transaction records)")
	}

	err = r.outbox.SaveEvents(ctx, string(p.ID), "Payment", p.Events())
	if err != nil {
		return err
	}
	p.ClearEvents()
	return nil
}

const insertPaymentSQL = `
INSERT INTO payments (id, invoice_id, payment_date, amount, method, reference, remarks, tenant_id, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`

const findPaymentIDByKeySQL = `
SELECT id FROM payments WHERE tenant_id = ? AND idempotency_key = ? LIMIT 1
`

func (r *paymentRepository) insertPayment(ctx context.Context, p *aggregate.PaymentAggregate, key string) error {
	var reference, remarks any = nil, nil
	if p.Reference != nil {
		reference = *p.Reference
	}
	if p.Remarks != nil {
		remarks = *p.Remarks
	}
	_, err := r.exec(ctx).ExecContext(ctx, insertPaymentSQL,
		string(p.ID),
		p.InvoiceID,
		p.PaymentDate,
		p.Amount,
		string(p.Method),
		reference,
		remarks,
		string(p.TenantID),
		key,
	)
	return err
}

func (r *paymentRepository) findIDByIdempotencyKey(ctx context.Context, tenantID shared.TenantID, key string) (string, error) {
	var id string
	err := r.exec(ctx).QueryRowContext(ctx, findPaymentIDByKeySQL, string(tenantID), key).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (r *paymentRepository) FindByReference(ctx context.Context, reference string, tenantID shared.TenantID) (aggregate.PaymentID, error) {
	if strings.TrimSpace(reference) == "" {
		return "", sql.ErrNoRows
	}
	id, err := r.findIDByIdempotencyKey(ctx, tenantID, "ref:"+reference)
	if err != nil {
		return "", err
	}
	return aggregate.PaymentID(id), nil
}

func (r *paymentRepository) exec(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.dbConn
}

// idempotencyKey derives a stable key for duplicate detection: a client-supplied
// reference when present, otherwise a date-aware key built from invoice,
// amount and payment day. Including the day lets legitimate repeat
// installments of the same amount on later days record normally, while
// same-day double-submits stay deduplicated.
func idempotencyKey(p *aggregate.PaymentAggregate) string {
	if p.Reference != nil {
		if ref := strings.TrimSpace(*p.Reference); ref != "" {
			return "ref:" + ref
		}
	}
	amount := shared.FloatToMoney(p.Amount, "INR").MoneyToFloat()
	return "inv:" + p.InvoiceID + ":amt:" + strconv.FormatFloat(amount, 'f', 2, 64) +
		":d:" + p.PaymentDate.Format("2006-01-02")
}

func isIdempotencyConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") && strings.Contains(msg, "idempotency_key")
}

func (r *paymentRepository) Find(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID) (*aggregate.PaymentAggregate, error) {
	row, err := r.Q(ctx).GetPaymentByID(ctx, db.GetPaymentByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return nil, err
	}
	p := db.Payment{
		ID:          row.ID,
		InvoiceID:   row.InvoiceID,
		PaymentDate: row.PaymentDate,
		Amount:      row.Amount,
		Method:      row.Method,
		Reference:   row.Reference,
		Remarks:     row.Remarks,
		TenantID:    row.TenantID,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	return converters.ToDomain(p), nil
}

func (r *paymentRepository) GetReadModel(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID) (domain.PaymentReadModel, error) {
	row, err := r.Q(ctx).GetPaymentByID(ctx, db.GetPaymentByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return domain.PaymentReadModel{}, err
	}
	var ref, rem *string
	if row.Reference.Valid {
		ref = &row.Reference.String
	}
	if row.Remarks.Valid {
		rem = &row.Remarks.String
	}
	return domain.PaymentReadModel{
		ID:            row.ID,
		InvoiceID:     row.InvoiceID,
		InvoiceNumber: row.InvoiceNumber,
		PaymentDate:   row.PaymentDate,
		Amount:        row.Amount,
		Method:        row.Method,
		Reference:     ref,
		Remarks:       rem,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func (r *paymentRepository) GetPaymentsByInvoice(ctx context.Context, invoiceID string, tenantID shared.TenantID) ([]domain.PaymentReadModel, error) {
	rows, err := r.Q(ctx).GetPaymentsByInvoice(ctx, db.GetPaymentsByInvoiceParams{
		InvoiceID: invoiceID,
		TenantID:  string(tenantID),
	})
	if err != nil {
		return nil, err
	}

	readModels := make([]domain.PaymentReadModel, len(rows))
	for i, row := range rows {
		var ref, rem *string
		if row.Reference.Valid {
			ref = &row.Reference.String
		}
		if row.Remarks.Valid {
			rem = &row.Remarks.String
		}
		readModels[i] = domain.PaymentReadModel{
			ID:            row.ID,
			InvoiceID:     row.InvoiceID,
			InvoiceNumber: row.InvoiceNumber,
			PaymentDate:   row.PaymentDate,
			Amount:        row.Amount,
			Method:        row.Method,
			Reference:     ref,
			Remarks:       rem,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
	}

	return readModels, nil
}

func (r *paymentRepository) SearchReadModels(ctx context.Context, tenantID shared.TenantID, method string, limit int, offset int) ([]domain.PaymentReadModel, int64, error) {
	rows, err := r.Q(ctx).SearchPayments(ctx, db.SearchPaymentsParams{
		TenantID: string(tenantID),
		Column2:  method,
		Method:   method,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, 0, err
	}

	total, err := r.Q(ctx).CountPayments(ctx, db.CountPaymentsParams{
		TenantID: string(tenantID),
		Column2:  method,
		Method:   method,
	})
	if err != nil {
		return nil, 0, err
	}

	readModels := make([]domain.PaymentReadModel, len(rows))
	for i, row := range rows {
		var ref, rem *string
		if row.Reference.Valid {
			ref = &row.Reference.String
		}
		if row.Remarks.Valid {
			rem = &row.Remarks.String
		}
		readModels[i] = domain.PaymentReadModel{
			ID:            row.ID,
			InvoiceID:     row.InvoiceID,
			InvoiceNumber: row.InvoiceNumber,
			PaymentDate:   row.PaymentDate,
			Amount:        row.Amount,
			Method:        row.Method,
			Reference:     ref,
			Remarks:       rem,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
	}

	return readModels, total, nil
}

const setRazorpayFieldsSQL = `
UPDATE payments
SET razorpay_order_id = ?, razorpay_payment_id = ?, razorpay_signature = ?, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ?
`

// SetRazorpayFields stores the Razorpay order/payment/signature identifiers on
// the payment row (Spec 11 §5.1).
func (r *paymentRepository) SetRazorpayFields(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID, orderID, paymentID, signature string) error {
	_, err := r.exec(ctx).ExecContext(ctx, setRazorpayFieldsSQL, orderID, paymentID, signature, string(id), string(tenantID))
	return err
}

const findRazorpayPaymentSQL = `
SELECT id FROM payments WHERE tenant_id = ? AND razorpay_payment_id = ? LIMIT 1
`

// ExistsRazorpayPayment returns the payment ID recorded against a Razorpay
// payment ID, or sql.ErrNoRows when none exists.
func (r *paymentRepository) ExistsRazorpayPayment(ctx context.Context, tenantID shared.TenantID, paymentID string) (aggregate.PaymentID, error) {
	var id string
	err := r.exec(ctx).QueryRowContext(ctx, findRazorpayPaymentSQL, string(tenantID), paymentID).Scan(&id)
	if err != nil {
		return "", err
	}
	return aggregate.PaymentID(id), nil
}

const findWebhookEventSQL = `
SELECT id FROM payments WHERE tenant_id = ? AND webhook_event_id = ? LIMIT 1
`

// ExistsWebhookEvent returns the payment ID processed for a Razorpay webhook
// event ID, or sql.ErrNoRows when none exists.
func (r *paymentRepository) ExistsWebhookEvent(ctx context.Context, tenantID shared.TenantID, eventID string) (aggregate.PaymentID, error) {
	var id string
	err := r.exec(ctx).QueryRowContext(ctx, findWebhookEventSQL, string(tenantID), eventID).Scan(&id)
	if err != nil {
		return "", err
	}
	return aggregate.PaymentID(id), nil
}

const setWebhookEventIDSQL = `
UPDATE payments SET webhook_event_id = ?, updated_at = datetime('now')
WHERE id = ? AND tenant_id = ?
`

// SetWebhookEventID persists the Razorpay webhook event ID on the payment row
// for restart-safe deduplication (Spec 11 §5.1).
func (r *paymentRepository) SetWebhookEventID(ctx context.Context, id aggregate.PaymentID, tenantID shared.TenantID, eventID string) error {
	_, err := r.exec(ctx).ExecContext(ctx, setWebhookEventIDSQL, eventID, string(id), string(tenantID))
	return err
}

// FindReferenceTenant discovers the owning tenant of a payment by its gateway
// reference (Razorpay payment id). Exists for unauthenticated refund webhooks
// that must resolve tenancy without a request context (Spec 24 §Business logic).
func (r *paymentRepository) FindReferenceTenant(ctx context.Context, reference string) (shared.TenantID, error) {
	var tenantID string
	err := r.exec(ctx).QueryRowContext(ctx,
		`SELECT p.tenant_id FROM payments p WHERE p.reference = ?
		 ORDER BY p.created_at DESC LIMIT 1`, reference).Scan(&tenantID)
	if err != nil {
		return "", err
	}
	return shared.TenantID(tenantID), nil
}
