package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"transport-app/internal/domain"
	reporepo "transport-app/internal/repository"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// GST credit/debit notes (migration 00098). Issued invoices are immutable —
// once an IRN exists or payments are recorded the guards in
// invoice_immutability.go lock them — so every post-issuance correction MUST
// flow through a note:
//
//   - CREDIT note: reduces invoice value (rate corrections, post-supply
//     discounts, cancellations). Capped at invoice total minus prior credits
//     against the same invoice.
//   - DEBIT note: increases invoice value (additional charges discovered after
//     issuance). No upper bound.
//
// Note numbering "{CN|DN}/{FY}/{seq:04d}" comes from the note_sequences table,
// whose PK includes note_type so CN and DN counters advance independently and
// never punch gaps into the shared invoice_sequences counter.
type CreditNoteService struct {
	baseService
}

// Sentinel errors mapped to HTTP status codes by the handlers
// (noteGuardError mirrors invoiceGuardError).
var (
	ErrNoteReasonRequired = errors.New("reason is required for a credit/debit note")
	ErrNoteInvalidAmount  = errors.New("note total must be greater than zero")
	// Business-rule conflict, not malformed input: the credit would wipe out
	// more value than remains on the invoice.
	ErrNoteExceedsInvoiceTotal = errors.New("credit note total exceeds invoice total minus prior credit notes")
)

// Note request payload for CreateCreditNote / CreateDebitNote.
type NoteRequest struct {
	InvoiceID     string
	Reason        string
	PlaceOfSupply string
	TaxableValue  float64
	IGST          float64
	CGST          float64
	SGST          float64
	CreatedBy     string
}

// CreditNoteRecord is one issued note as returned by create/get.
type CreditNoteRecord struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	NoteNumber    string    `json:"note_number"`
	NoteType      string    `json:"note_type"`
	InvoiceID     string    `json:"invoice_id"`
	Reason        string    `json:"reason"`
	PlaceOfSupply string    `json:"place_of_supply,omitempty"`
	TaxableValue  float64   `json:"taxable_value"`
	IGST          float64   `json:"igst"`
	CGST          float64   `json:"cgst"`
	SGST          float64   `json:"sgst"`
	Total         float64   `json:"total"`
	IRN           string    `json:"irn,omitempty"`
	CreatedBy     string    `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// nextNoteNumberSQL atomically allocates the next note sequence for a
// (financial_year, tenant_id, note_type) triple — the note_sequences table
// from migration 00098. Same race-safe upsert + RETURNING shape as
// nextInvoiceNumberSQL: SQLite serialises writers, so concurrent callers can
// never observe the same last_number.
const nextNoteNumberSQL = `
INSERT INTO note_sequences (financial_year, tenant_id, note_type, last_number, prefix)
VALUES (?, ?, ?, 1, ?)
ON CONFLICT(financial_year, tenant_id, note_type)
DO UPDATE SET last_number = note_sequences.last_number + 1
RETURNING last_number
`

// CreateCreditNote issues a credit note reducing an invoice's value. The
// sum of all credit notes against an invoice can never exceed its total.
func (s *CreditNoteService) CreateCreditNote(ctx context.Context, req NoteRequest) (*CreditNoteRecord, error) {
	return s.create(ctx, "credit", req)
}

// CreateDebitNote issues a debit note increasing an invoice's value.
// Debit notes carry no upper bound.
func (s *CreditNoteService) CreateDebitNote(ctx context.Context, req NoteRequest) (*CreditNoteRecord, error) {
	return s.create(ctx, "debit", req)
}

// GetNotesForInvoice lists every note issued against an invoice, newest first.
// Tenant-scoped: notes from other tenants' identical invoice ids never leak.
func (s *CreditNoteService) GetNotesForInvoice(ctx context.Context, invoiceID string) ([]*CreditNoteRecord, error) {
	tenant := shared.TenantIDFromContext(ctx)
	if tenant == "" {
		return nil, fmt.Errorf("credit/debit note lookup requires a tenant in context")
	}
	db, err := s.rawDB()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, tenant_id, note_number, note_type, invoice_id, reason,
		       COALESCE(place_of_supply, ''), taxable_value, igst, cgst, sgst, total,
		       COALESCE(irn, ''), COALESCE(created_by, ''), created_at
		FROM credit_debit_notes
		WHERE tenant_id = ? AND invoice_id = ?
		ORDER BY created_at DESC, id DESC
	`, string(tenant), invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list notes for invoice %s: %w", invoiceID, err)
	}
	defer func() { _ = rows.Close() }()

	var notes []*CreditNoteRecord
	for rows.Next() {
		n := &CreditNoteRecord{}
		if err := rows.Scan(&n.ID, &n.TenantID, &n.NoteNumber, &n.NoteType, &n.InvoiceID,
			&n.Reason, &n.PlaceOfSupply, &n.TaxableValue, &n.IGST, &n.CGST, &n.SGST,
			&n.Total, &n.IRN, &n.CreatedBy, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan credit/debit note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// create validates, allocates the number and inserts the note inside one
// transaction; the money-ledger entry is appended AFTER commit because the
// ledger is audit infrastructure — a failure there must never fail the note
// (same contract as the payment hook).
func (s *CreditNoteService) create(ctx context.Context, noteType string, req NoteRequest) (*CreditNoteRecord, error) {
	tenant := shared.TenantIDFromContext(ctx)
	if tenant == "" {
		return nil, fmt.Errorf("credit/debit note requires a tenant in context")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrNoteReasonRequired
	}
	for _, v := range []float64{req.TaxableValue, req.IGST, req.CGST, req.SGST} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, ErrNoteInvalidAmount
		}
	}
	total := roundPaise(req.TaxableValue + req.IGST + req.CGST + req.SGST)
	if total <= 0 {
		return nil, ErrNoteInvalidAmount
	}

	db, err := s.rawDB()
	if err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin note transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // no-op after commit

	// The invoice must exist IN THIS TENANT — cross-tenant ids resolve to
	// not-found exactly like a bogus id.
	var invoiceTotal float64
	err = tx.QueryRowContext(ctx,
		`SELECT total FROM invoices WHERE id = ? AND tenant_id = ?`,
		req.InvoiceID, string(tenant)).Scan(&invoiceTotal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvoiceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup invoice for note: %w", err)
	}

	// Credit cap: prior credits + this note ≤ invoice total (paise math with
	// 1-paisa rounding slack, mirroring the payment overpay guard).
	if noteType == "credit" {
		var priorCredits float64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(total), 0) FROM credit_debit_notes
			WHERE tenant_id = ? AND invoice_id = ? AND note_type = 'credit'
		`, string(tenant), req.InvoiceID).Scan(&priorCredits); err != nil {
			return nil, fmt.Errorf("sum prior credit notes: %w", err)
		}
		if ToMinor(priorCredits)+ToMinor(total) > ToMinor(invoiceTotal)+1 {
			return nil, ErrNoteExceedsInvoiceTotal
		}
	}

	prefix := "CN"
	if noteType == "debit" {
		prefix = "DN"
	}
	fy := sqliterepo.FinancialYear(time.Now())
	var seq int64
	if err := tx.QueryRowContext(ctx, nextNoteNumberSQL, fy, string(tenant), noteType, prefix).Scan(&seq); err != nil {
		return nil, fmt.Errorf("allocate %s note sequence (fy=%s tenant=%s): %w", noteType, fy, tenant, err)
	}
	noteNumber := fmt.Sprintf("%s/%s/%04d", prefix, fy, seq)

	id := generateID()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO credit_debit_notes (
			id, tenant_id, note_number, note_type, invoice_id, reason,
			place_of_supply, taxable_value, igst, cgst, sgst, total, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, string(tenant), noteNumber, noteType, req.InvoiceID,
		strings.TrimSpace(req.Reason), strings.TrimSpace(req.PlaceOfSupply),
		req.TaxableValue, req.IGST, req.CGST, req.SGST, total, req.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("insert %s note: %w", noteType, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit %s note: %w", noteType, err)
	}

	s.log.Info("credit/debit note created",
		"note_id", id, "note_number", noteNumber, "note_type", noteType,
		"invoice_id", req.InvoiceID, "total", total, "tenant_id", tenant)
	s.logAudit(ctx, nil, "create", "credit_debit_notes", id, nil, nil)

	// Money ledger (migration 00097): a credit note reduces what the customer
	// owes us → adjustment DEBIT; a debit note increases it → adjustment
	// CREDIT. Audit-only: failure logged, note stands.
	direction := "debit"
	if noteType == "debit" {
		direction = "credit"
	}
	if err := NewMoneyLedgerService(s.store, s.log).AppendEntry(ctx, LedgerEntry{
		TxnType:     "adjustment",
		RefTable:    "credit_notes",
		RefID:       id,
		Direction:   direction,
		AmountMinor: ToMinor(total),
		Memo:        fmt.Sprintf("%s note %s against invoice %s: %s", noteType, noteNumber, req.InvoiceID, req.Reason),
	}); err != nil {
		s.log.Warn("money ledger append failed; note stands",
			"note_id", id, "error", err)
	}

	// IRN stays NULL: NIC supports issuing IRNs for CN/DN, but e-invoicing of
	// notes is a future integration (out of scope this wave).
	return &CreditNoteRecord{
		ID:            id,
		TenantID:      string(tenant),
		NoteNumber:    noteNumber,
		NoteType:      noteType,
		InvoiceID:     req.InvoiceID,
		Reason:        strings.TrimSpace(req.Reason),
		PlaceOfSupply: strings.TrimSpace(req.PlaceOfSupply),
		TaxableValue:  req.TaxableValue,
		IGST:          req.IGST,
		CGST:          req.CGST,
		SGST:          req.SGST,
		Total:         total,
		CreatedBy:     req.CreatedBy,
	}, nil
}

// rawDB resolves the raw *sql.DB via the optional DBGetter capability,
// same as the money ledger service.
func (s *CreditNoteService) rawDB() (*sql.DB, error) {
	g, ok := s.store.(reporepo.DBGetter)
	if !ok || g == nil || g.DB() == nil {
		return nil, errors.New("credit/debit notes: store exposes no *sql.DB")
	}
	return g.DB(), nil
}

// roundPaise snaps a rupee amount to 2 decimals so float drift cannot tip a
// boundary check or store a third decimal.
func roundPaise(v float64) float64 {
	return math.Round(v*100) / 100
}
