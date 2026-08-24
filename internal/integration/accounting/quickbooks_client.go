package accounting

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

type quickbooksClient struct {
	cfg Config
}

func (c *quickbooksClient) ExportInvoice(ctx context.Context, invoice ExportedInvoice) (ExportResult, error) {
	slog.Default().Info("[accounting:quickbooks] ExportInvoice called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "invoice", invoice.InvoiceNumber)
	if !c.cfg.Enabled {
		return ExportResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		return ExportResult{}, ErrNotImplemented
	}
	return ExportResult{
		SyncID:     uuid.New().String(),
		Status:     "SUCCESS",
		ExternalID: "QB-INV-" + invoice.InvoiceNumber,
		Message:    "Invoice exported to QuickBooks successfully",
	}, nil
}

func (c *quickbooksClient) SyncContacts(ctx context.Context, contacts []Contact) (SyncResult, error) {
	slog.Default().Info("[accounting:quickbooks] SyncContacts called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "count", len(contacts))
	if !c.cfg.Enabled {
		return SyncResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		return SyncResult{}, ErrNotImplemented
	}
	return SyncResult{
		Synced:  len(contacts),
		Failed:  0,
		Errors:  nil,
		Message: fmt.Sprintf("Synced %d contacts to QuickBooks", len(contacts)),
	}, nil
}

func (c *quickbooksClient) PushJournalEntry(ctx context.Context, entry JournalEntry) (JournalEntryResult, error) {
	slog.Default().Info("[accounting:quickbooks] PushJournalEntry called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "reference", entry.Reference)
	if !c.cfg.Enabled {
		return JournalEntryResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		return JournalEntryResult{}, ErrNotImplemented
	}
	return JournalEntryResult{
		EntryID: "QB-JE-" + uuid.New().String()[:8],
		Status:  "SUCCESS",
		Message: "Journal entry pushed to QuickBooks successfully",
	}, nil
}
