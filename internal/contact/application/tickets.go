package application

// Public support/grievance ticket store, extracted from
// internal/handlers/contact.go (handlers-end-state ADR, Phase 2). The handler
// keeps HTTP form/JSON parsing, required-field validation, the status
// whitelist, SLA derivation and redirects; this file keeps the
// contact_submissions SQL.
//
// LEGACY SCOPE PRESERVED EXACTLY: contact_submissions is a company-global
// support inbox with no tenant_id, so every query here is intentionally
// global like the old handler (the admin triage routes are gated by
// users:manage; see the handler note about multi-tenant use).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

// ErrTicketNotFound reports an unknown ticket number/email pair.
var ErrTicketNotFound = errors.New("contact: ticket not found")

// Ticket is one contact_submissions row rendered on the status page.
type Ticket struct {
	ID             string
	TicketNumber   string
	Name           string
	Email          string
	Phone          string
	CompanyName    string
	Subject        string
	Category       string
	Message        string
	Status         string
	AcknowledgedAt string
	CreatedAt      string
	UpdatedAt      string
}

// ListRow is one ticket in the admin triage list (legacy JSON field names).
type ListRow struct {
	TicketNumber   string `json:"ticket"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Subject        string `json:"subject"`
	Category       string `json:"category"`
	Status         string `json:"status"`
	AcknowledgedAt string `json:"acknowledged_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// SubmitInput carries the validated contact-form fields.
type SubmitInput struct {
	Name        string
	Email       string
	Phone       string
	CompanyName string
	Subject     string
	Category    string
	Message     string
}

// Service runs contact-ticket queries.
type Service struct {
	db *sql.DB
}

// NewService builds a contact-ticket service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// generateTicketNumber mints the public AVN-XXXXXXXX reference.
func generateTicketNumber() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		return fmt.Sprintf("AVN-%X", n.Int64())
	}
	return fmt.Sprintf("AVN-%X", b)
}

// Submit inserts a pending ticket and returns its public ticket number.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (string, error) {
	if s == nil || s.db == nil {
		return "", sql.ErrConnDone
	}
	id := uuid.New().String()
	ticketNo := generateTicketNumber()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO contact_submissions (id, ticket_number, name, email, phone, company_name, subject, category, message, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
	`, id, ticketNo, in.Name, in.Email, in.Phone, in.CompanyName, in.Subject, in.Category, in.Message)
	if err != nil {
		return "", err
	}
	return ticketNo, nil
}

// FindForRequester returns the ticket matching both the public number and the
// submitter email. Any lookup failure (including no rows) maps to
// ErrTicketNotFound - the legacy handler showed the same message either way.
func (s *Service) FindForRequester(ctx context.Context, ticketNo, email string) (*Ticket, error) {
	if s == nil || s.db == nil {
		return nil, ErrTicketNotFound
	}
	var t Ticket
	err := s.db.QueryRowContext(ctx, `
		SELECT id, ticket_number, name, email, COALESCE(phone, ''), COALESCE(company_name, ''), subject, category, message, status, COALESCE(CAST(acknowledged_at AS TEXT), ''), created_at, updated_at
		FROM contact_submissions
		WHERE ticket_number = $1 AND email = $2
		ORDER BY created_at DESC LIMIT 1
	`, ticketNo, email).Scan(
		&t.ID, &t.TicketNumber, &t.Name, &t.Email, &t.Phone, &t.CompanyName, &t.Subject, &t.Category, &t.Message, &t.Status, &t.AcknowledgedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, ErrTicketNotFound
	}
	return &t, nil
}

// UpdateStatus transitions a ticket; the caller validates the status
// whitelist. The first touch stamps acknowledged_at exactly once
// (COALESCE keeps the first acknowledgement timestamp). Returns found=false
// when no ticket number matched (handler 404).
func (s *Service) UpdateStatus(ctx context.Context, ticketNo, status string) (bool, error) {
	if s == nil || s.db == nil {
		return false, sql.ErrConnDone
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE contact_submissions
		SET status = $1, updated_at = CURRENT_TIMESTAMP,
		    acknowledged_at = COALESCE(acknowledged_at, CURRENT_TIMESTAMP)
		WHERE ticket_number = $2
	`, status, ticketNo)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// List returns tickets newest-first (cap 100), optionally filtered by the
// validated status. Scan/rows errors are surfaced (legacy 500 path).
func (s *Service) List(ctx context.Context, status string) ([]ListRow, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrConnDone
	}
	query := `SELECT ticket_number, name, email, subject, category, status,
		COALESCE(CAST(acknowledged_at AS TEXT), ''), created_at, updated_at
		FROM contact_submissions`
	var args []interface{}
	if status != "" {
		query += ` WHERE status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ListRow, 0)
	for rows.Next() {
		var t ListRow
		if err := rows.Scan(&t.TicketNumber, &t.Name, &t.Email, &t.Subject,
			&t.Category, &t.Status, &t.AcknowledgedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
