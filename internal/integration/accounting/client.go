package accounting

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrDisabled indicates that the external accounting integration is disabled.
var ErrDisabled = errors.New("accounting integration disabled")

// Config holds connection settings for the external accounting API.
type Config struct {
	Endpoint string
	APIKey   string
	Enabled  bool
	Provider string
	UseMock  bool
}

// ErrNotImplemented is returned by provider adapters that have no real
// integration behind them yet. It must never be masked as success.
var ErrNotImplemented = errors.New("accounting provider adapter not implemented")

// LineItem represents a single invoice line item.
type LineItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Amount      float64 `json:"amount"`
}

// ExportedInvoice represents an invoice to be pushed to the accounting system.
type ExportedInvoice struct {
	ExternalID    string     `json:"external_id"`
	InvoiceNumber string     `json:"invoice_number"`
	CustomerName  string     `json:"customer_name"`
	CustomerGSTIN string     `json:"customer_gstin"`
	Amount        float64    `json:"amount"`
	TaxAmount     float64    `json:"tax_amount"`
	TotalAmount   float64    `json:"total_amount"`
	InvoiceDate   time.Time  `json:"invoice_date"`
	DueDate       time.Time  `json:"due_date"`
	LineItems     []LineItem `json:"line_items"`
}

// ExportResult represents the response from exporting an invoice.
type ExportResult struct {
	SyncID     string `json:"sync_id"`
	Status     string `json:"status"`
	ExternalID string `json:"external_id"`
	Message    string `json:"message"`
}

// Contact represents a customer or vendor contact.
type Contact struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	GSTIN       string `json:"gstin"`
	Address     string `json:"address"`
	ContactType string `json:"contact_type"`
}

// SyncResult represents the response from syncing contacts.
type SyncResult struct {
	Synced  int      `json:"synced"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors"`
	Message string   `json:"message"`
}

// JournalLine represents a debit/credit line in a journal entry.
type JournalLine struct {
	Account string  `json:"account"`
	Debit   float64 `json:"debit"`
	Credit  float64 `json:"credit"`
}

// JournalEntry represents a journal entry to push to the accounting system.
type JournalEntry struct {
	EntryDate time.Time     `json:"entry_date"`
	Reference string        `json:"reference"`
	Narration string        `json:"narration"`
	Lines     []JournalLine `json:"lines"`
}

// JournalEntryResult represents the response from pushing a journal entry.
type JournalEntryResult struct {
	EntryID string `json:"entry_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Client defines operations supported by the external accounting API.
type Client interface {
	ExportInvoice(ctx context.Context, invoice ExportedInvoice) (ExportResult, error)
	SyncContacts(ctx context.Context, contacts []Contact) (SyncResult, error)
	PushJournalEntry(ctx context.Context, entry JournalEntry) (JournalEntryResult, error)
}

// NewClient returns an accounting client for the configured provider.
func NewClient(cfg Config) Client {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.accounting.example.com"
	}
	switch strings.ToLower(cfg.Provider) {
	case "tally":
		return &tallyClient{cfg: cfg}
	case "zoho":
		return &zohoClient{cfg: cfg}
	case "quickbooks":
		return &quickbooksClient{cfg: cfg}
	default:
		return &mockClient{cfg: cfg}
	}
}
