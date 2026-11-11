package application

// Agent-facing money facade for the payment tools, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). Reads own the
// tenant-scoped revenue SQL (same definitions as db/query/payments.sql); the
// write delegates to the clean RecordPaymentUseCase that the HTTP payment
// handlers already use, so agent and web share one money rule set.

import (
	"context"
	"database/sql"
	"time"

	paymentagg "transport-app/internal/payment/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// MonthlyRevenuePoint is one month bucket of the revenue summary.
type MonthlyRevenuePoint struct {
	Month string  `json:"month"`
	Total float64 `json:"total"`
}

// AgentMoneyService is the money facade. Stateless.
type AgentMoneyService struct {
	db *sql.DB
}

// NewAgentMoneyService wires the facade against the database.
func NewAgentMoneyService(db *sql.DB) *AgentMoneyService {
	return &AgentMoneyService{db: db}
}

// Revenue returns total payments and the per-month breakdown for the tenant.
func (s *AgentMoneyService) Revenue(ctx context.Context, tenantID shared.TenantID) (float64, []MonthlyRevenuePoint, error) {
	if s == nil || s.db == nil {
		return 0, nil, sql.ErrNoRows
	}
	var total float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT CAST(COALESCE(SUM(amount), 0) AS REAL) FROM payments WHERE tenant_id = ?`,
		string(tenantID)).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(CAST(payment_date AS TEXT), 1, 7) AS month, CAST(COALESCE(SUM(amount), 0) AS REAL) AS total
		FROM payments WHERE tenant_id = ?
		GROUP BY substr(CAST(payment_date AS TEXT), 1, 7)
		ORDER BY month ASC`, string(tenantID))
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = rows.Close() }()
	var monthly []MonthlyRevenuePoint
	for rows.Next() {
		var m MonthlyRevenuePoint
		if err := rows.Scan(&m.Month, &m.Total); err != nil {
			return 0, nil, err
		}
		monthly = append(monthly, m)
	}
	return total, monthly, rows.Err()
}

// RecordPayment records a payment through the clean payment use case
// (validation, idempotent claim, invoice paid_amount/payment_status update).
// The returned id is the payment id; the tool keeps its pre-flight
// amount-vs-balance guard and JSON shape.
func (s *AgentMoneyService) RecordPayment(ctx context.Context, tenantID shared.TenantID, invoiceID string, amount float64, method, reference, remarks string) (string, error) {
	if s == nil || s.db == nil {
		return "", sql.ErrNoRows
	}
	uc := NewRecordPaymentUseCase(uow.NewSQLUnitOfWork(s.db), id.NewUUIDGenerator(), clock.NewRealClock())
	var ref, rem *string
	if reference != "" {
		ref = &reference
	}
	if remarks != "" {
		rem = &remarks
	}
	pid, err := uc.Execute(ctx, RecordPaymentCommand{
		TenantID: tenantID, InvoiceID: invoiceID, PaymentDate: time.Now().UTC(),
		Amount: amount, Method: paymentagg.PaymentMethod(method), Reference: ref, Remarks: rem,
	})
	if err != nil {
		return "", err
	}
	return string(pid), nil
}
