package accounting

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

type mockClient struct {
	cfg Config
}

func (c *mockClient) ExportInvoice(ctx context.Context, invoice ExportedInvoice) (ExportResult, error) {
	if !c.cfg.UseMock {
		return ExportResult{}, ErrNotImplemented
	}

	slog.Default().Info("[accounting:mock] ExportInvoice called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "invoice", invoice.InvoiceNumber)
	extID := "EXT-" + invoice.InvoiceNumber
	return ExportResult{
		SyncID:     uuid.New().String(),
		Status:     "SUCCESS",
		ExternalID: extID,
		Message:    "Invoice exported successfully (mock)",
	}, nil
}

func (c *mockClient) SyncContacts(ctx context.Context, contacts []Contact) (SyncResult, error) {
	if !c.cfg.UseMock {
		return SyncResult{}, ErrNotImplemented
	}

	slog.Default().Info("[accounting:mock] SyncContacts called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "count", len(contacts))
	return SyncResult{
		Synced:  len(contacts),
		Failed:  0,
		Errors:  nil,
		Message: fmt.Sprintf("Synced %d contacts (mock)", len(contacts)),
	}, nil
}

func (c *mockClient) PushJournalEntry(ctx context.Context, entry JournalEntry) (JournalEntryResult, error) {
	if !c.cfg.UseMock {
		return JournalEntryResult{}, ErrNotImplemented
	}

	slog.Default().Info("[accounting:mock] PushJournalEntry called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "reference", entry.Reference)
	return JournalEntryResult{
		EntryID: "JE-MOCK-" + uuid.New().String()[:8],
		Status:  "SUCCESS",
		Message: "Journal entry pushed successfully (mock)",
	}, nil
}
