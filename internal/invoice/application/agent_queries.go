package application

// Agent-facing invoice read model for the money tools, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The tools keep
// schemas, argument validation, RBAC/approval gates and model strings; this
// file owns the tenant-scoped reads and the outstanding-balance math.

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/shared"
)

// AgentInvoiceView is one invoice plus its joined customer and outstanding
// balance (legacy tool JSON source).
type AgentInvoiceView struct {
	ID          string     `json:"id"`
	Number      string     `json:"number"`
	Customer    string     `json:"customer"`
	Total       float64    `json:"total"`
	Paid        float64    `json:"paid"`
	Outstanding float64    `json:"outstanding"`
	Status      string     `json:"status"`
	DueDate     *time.Time `json:"due_date"`
}

// AgentInvoiceRow is the unpaid-list JSON row (legacy tool shape).
type AgentInvoiceRow struct {
	ID          string  `json:"id"`
	Number      string  `json:"number"`
	Customer    string  `json:"customer"`
	Total       float64 `json:"total"`
	Paid        float64 `json:"paid"`
	Outstanding float64 `json:"outstanding"`
}

// AgentInvoiceQueries is the read model facade. Stateless.
type AgentInvoiceQueries struct {
	db *sql.DB
}

// NewAgentInvoiceQueries wires the read model against the database.
func NewAgentInvoiceQueries(db *sql.DB) *AgentInvoiceQueries {
	return &AgentInvoiceQueries{db: db}
}

const agentInvoiceSelect = `
	SELECT i.id, i.invoice_number, c.name, i.total, i.paid_amount, i.status, i.due_date,
	       i.payment_status,
	       CAST(COALESCE((SELECT SUM(p.amount) FROM payments p
	                      WHERE p.invoice_id = i.id AND p.tenant_id = i.tenant_id), 0) AS REAL) AS paid_sum
	FROM invoices i
	JOIN customers c ON c.id = i.customer_id`

// GetByNumber returns the tenant's invoice by invoice number. Missing or
// foreign rows surface sql.ErrNoRows exactly like the legacy store lookup
// (the tool wraps it as "invoice not found: <err>").
func (q *AgentInvoiceQueries) GetByNumber(ctx context.Context, tenantID shared.TenantID, number string) (*AgentInvoiceView, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	row := q.db.QueryRowContext(ctx, agentInvoiceSelect+` WHERE i.tenant_id = ? AND i.invoice_number = ?`, string(tenantID), number)
	v, _, err := scanAgentInvoice(row)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// GetByID returns the tenant's invoice by id (payment pre-flight balance read).
// Missing or foreign rows surface sql.ErrNoRows like the legacy store lookup.
func (q *AgentInvoiceQueries) GetByID(ctx context.Context, tenantID shared.TenantID, id string) (*AgentInvoiceView, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	row := q.db.QueryRowContext(ctx, agentInvoiceSelect+` WHERE i.tenant_id = ? AND i.id = ?`, string(tenantID), id)
	v, _, err := scanAgentInvoice(row)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ListUnpaid returns the tenant's 50 newest invoices then drops fully-paid
// ones (legacy tool semantics: SearchInvoices limit 50, filter paid). Balance
// math mirrors GetBalance: total - sum(payments), clamped at 0.
func (q *AgentInvoiceQueries) ListUnpaid(ctx context.Context, tenantID shared.TenantID, limit int) ([]AgentInvoiceRow, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.db.QueryContext(ctx, agentInvoiceSelect+` WHERE i.tenant_id = ? ORDER BY i.created_at DESC LIMIT ?`, string(tenantID), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AgentInvoiceRow
	for rows.Next() {
		v, paidStatus, err := scanAgentInvoice(rows)
		if err != nil {
			return nil, err
		}
		if paidStatus == "paid" {
			continue
		}
		out = append(out, AgentInvoiceRow{v.ID, v.Number, v.Customer, v.Total, v.Paid, v.Outstanding})
	}
	return out, rows.Err()
}

// scanAgentInvoice scans the shared select; the second return value is the
// raw payment_status used to filter fully-paid rows.
func scanAgentInvoice(row interface{ Scan(...any) error }) (*AgentInvoiceView, string, error) {
	var v AgentInvoiceView
	var due sql.NullTime
	var paymentStatus string
	var paidSum float64
	if err := row.Scan(&v.ID, &v.Number, &v.Customer, &v.Total, &v.Paid, &v.Status, &due, &paymentStatus, &paidSum); err != nil {
		return nil, "", err
	}
	if due.Valid {
		t := due.Time
		v.DueDate = &t
	}
	v.Outstanding = v.Total - paidSum
	if v.Outstanding < 0 {
		v.Outstanding = 0
	}
	return &v, paymentStatus, nil
}
