package application

// Agent-facing customer search, extracted from internal/agent/tools.go
// (handlers-end-state ADR, Phase 3). Tenant-scoped LIKE search over the same
// columns as the legacy SearchCustomers query.

import (
	"context"
	"database/sql"
	"strings"

	"transport-app/internal/shared"
)

// AgentCustomerRow is the search_customers JSON row.
type AgentCustomerRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Company      string `json:"company"`
	Phone        string `json:"phone"`
	CustomerCode string `json:"customer_code"`
}

// AgentCustomerQueries is the customer facade. Stateless.
type AgentCustomerQueries struct{ db *sql.DB }

// NewAgentCustomerQueries wires the facade against the database.
func NewAgentCustomerQueries(db *sql.DB) *AgentCustomerQueries { return &AgentCustomerQueries{db: db} }

// Search returns the tenant's 10 newest customers matching query across code,
// name, company, phone, email, contact person and internal id.
func (q *AgentCustomerQueries) Search(ctx context.Context, tenantID shared.TenantID, query string, limit int) ([]AgentCustomerRow, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	if limit <= 0 {
		limit = 10
	}
	like := "%" + strings.ToLower(query) + "%"
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(company,''), phone, customer_code
		FROM customers WHERE tenant_id = ?
		  AND (lower(customer_code) LIKE ? OR lower(name) LIKE ? OR lower(company) LIKE ?
		       OR lower(phone) LIKE ? OR lower(email) LIKE ? OR lower(contact_person) LIKE ? OR lower(internal_id) LIKE ?)
		ORDER BY created_at DESC LIMIT ?`,
		string(tenantID), like, like, like, like, like, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AgentCustomerRow
	for rows.Next() {
		var c AgentCustomerRow
		if err := rows.Scan(&c.ID, &c.Name, &c.Company, &c.Phone, &c.CustomerCode); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
