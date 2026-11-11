package application

// Agent-facing driver-expense (kharcha) facade, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The tools keep
// schemas, argument validation, RBAC/approval gates and model strings; this
// file owns the tenant-scoped pending list, the approve state machine
// (fuel-audit gate + settlement deduction) and the reject state machine,
// copied from the legacy service behavior.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// PendingExpense is one expense awaiting approval (tool JSON source).
type PendingExpense struct {
	ID          string  `json:"id"`
	TripNumber  string  `json:"trip_number"`
	DriverName  string  `json:"driver"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
}

// AgentExpenseStore is the kharcha facade. Stateless.
type AgentExpenseStore struct {
	db *sql.DB
}

// NewAgentExpenseStore wires the facade against the database.
func NewAgentExpenseStore(db *sql.DB) *AgentExpenseStore {
	return &AgentExpenseStore{db: db}
}

// ListPending returns the tenant's pending expenses, oldest first (legacy
// ListPendingExpenses ordering).
func (s *AgentExpenseStore) ListPending(ctx context.Context, tenantID shared.TenantID) ([]PendingExpense, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT de.id,
		       COALESCE(t.trip_number, '') AS trip_number,
		       COALESCE(d.first_name||' '||d.last_name, '') AS driver_name,
		       COALESCE(de.category, de.expense_type, 'other') AS category,
		       de.amount,
		       COALESCE(de.description, '') AS description
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		WHERE COALESCE(de.status, 'pending') = 'pending'
		  AND de.tenant_id = $1
		ORDER BY de.created_at ASC`, string(tenantID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PendingExpense
	for rows.Next() {
		var e PendingExpense
		if err := rows.Scan(&e.ID, &e.TripNumber, &e.DriverName, &e.Category, &e.Amount, &e.Description); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Approve approves a pending expense: fuel-audit enforce gate first (a
// needs_review claim must never flip to approved), then the pending ->
// approved update and the driver-settlement deduction/line, all in one
// transaction. Error strings mirror the legacy service.
func (s *AgentExpenseStore) Approve(ctx context.Context, tenantID shared.TenantID, expenseID, actor string) error {
	if s == nil || s.db == nil {
		return sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tenant := string(tenantID)
	now := time.Now()

	if s.fuelAuditEnforce(ctx, tenant) {
		var auditStatus string
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(audit_status, 'pending') FROM driver_expenses WHERE id = $1 AND tenant_id = $2`,
			expenseID, tenant).Scan(&auditStatus); err != nil {
			return fmt.Errorf("expense not found")
		}
		if auditStatus == "needs_review" {
			return fmt.Errorf("claim flagged by fuel audit (needs review); review at /fuel/audit")
		}
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE driver_expenses
		 SET status = 'approved', approved_by = $1, approved_at = $2
		 WHERE id = $3 AND tenant_id = $4 AND COALESCE(status, 'pending') = 'pending'`,
		actor, now, expenseID, tenant)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("expense already processed or not found")
	}

	var tripID, driverID, category string
	var amount float64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(trip_id,''), COALESCE(driver_id,''), amount, COALESCE(category, 'kharcha') FROM driver_expenses WHERE id = $1 AND tenant_id = $2`,
		expenseID, tenant).Scan(&tripID, &driverID, &amount, &category); err != nil {
		return err
	}
	if tripID != "" && driverID != "" {
		var settlementID string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM driver_settlements WHERE trip_id = $1 AND driver_id = $2`,
			tripID, driverID).Scan(&settlementID); err == nil && settlementID != "" {
			_, _ = tx.ExecContext(ctx,
				`UPDATE driver_settlements
				 SET advances_kharcha = advances_kharcha + $1,
				     net_payout = CASE WHEN net_payout - $2 > 0 THEN net_payout - $2 ELSE 0.0 END
				 WHERE id = $3`,
				amount, amount, settlementID)
			lineID := "stl-ln-" + uuid.New().String()
			label := fmt.Sprintf("Approved expense (%s) #%s", category, expenseID)
			_, _ = tx.ExecContext(ctx,
				`INSERT INTO settlement_lines (id, settlement_id, trip_id, line_type, label, amount, ref_id, created_at)
				 VALUES ($1, $2, $3, 'deduction', $4, $5, $6, CURRENT_TIMESTAMP)`,
				lineID, settlementID, tripID, label, -amount, expenseID)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.writeAudit(ctx, tenant, "approve_kharcha", expenseID, nil)
	return nil
}

// Reject rejects a pending expense; reason is mandatory. Error strings mirror
// the legacy service.
func (s *AgentExpenseStore) Reject(ctx context.Context, tenantID shared.TenantID, expenseID, actor, reason string) error {
	if reason == "" {
		return fmt.Errorf("rejection reason is required")
	}
	if s == nil || s.db == nil {
		return sql.ErrNoRows
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE driver_expenses
		 SET status = 'rejected', approved_by = $1, rejected_reason = $2
		 WHERE id = $3 AND tenant_id = $4 AND COALESCE(status, 'pending') = 'pending'`,
		actor, reason, expenseID, string(tenantID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("expense already processed or not found")
	}
	s.writeAudit(ctx, string(tenantID), "reject_kharcha", expenseID, &reason)
	return nil
}

// fuelAuditEnforce reports whether fuel.audit_enforce is 'true' for the
// tenant (enforce mode gates approval of needs_review claims).
func (s *AgentExpenseStore) fuelAuditEnforce(ctx context.Context, tenantID string) bool {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM company_config WHERE tenant_id = ? AND key = 'fuel.audit_enforce'`,
		tenantID).Scan(&v)
	return err == nil && v == "true"
}

// writeAudit appends the legacy audit_logs row; best-effort like the legacy
// logAudit (never fails the already-committed state change).
func (s *AgentExpenseStore) writeAudit(ctx context.Context, tenantID, action, expenseID string, reason *string) {
	var newValues *string
	if reason != nil {
		newValues = reason
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, user_id, action, table_name, record_id, old_values, new_values, ip_address, location, tenant_id)
		 VALUES (?, NULL, ?, 'driver_expenses', ?, NULL, ?, NULL, NULL, ?)`,
		uuid.NewString(), action, expenseID, newValues, tenantID)
}
