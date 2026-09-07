package accounting

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

type mockClient struct {
	cfg Config
}

// mockWarn marks demo-mode fabrications at Warn with tenant context so mock
// external IDs are never mistaken for real provider data in logs. Every mock
// method must call it on the mock path. NOTE: this default adapter
// intentionally serves explicit demo mode (UseMock) regardless of Enabled —
// the Enabled gate lives in the tally/zoho/quickbooks adapters, which return
// ErrDisabled/ErrNotImplemented honestly. IDs containing MOCK- plus the
// "(mock)" message mark results as synthetic.
func mockWarn(ctx context.Context, msg string, args ...any) {
	args = append(args, "mock", true, "tenant", string(shared.TenantIDFromContext(ctx)))
	slog.Default().Warn(msg, args...)
}

func (c *mockClient) ExportInvoice(ctx context.Context, invoice ExportedInvoice) (ExportResult, error) {
	if !c.cfg.UseMock {
		return ExportResult{}, ErrNotImplemented
	}

	slog.Default().Info("[accounting:mock] ExportInvoice called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "invoice", invoice.InvoiceNumber)
	mockWarn(ctx, "[accounting:mock] mock ExportInvoice returning demo data", "invoice", invoice.InvoiceNumber)
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
	mockWarn(ctx, "[accounting:mock] mock SyncContacts returning demo data", "count", len(contacts))
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
	mockWarn(ctx, "[accounting:mock] mock PushJournalEntry returning demo data", "reference", entry.Reference)
	return JournalEntryResult{
		EntryID: "JE-MOCK-" + uuid.New().String()[:8],
		Status:  "SUCCESS",
		Message: "Journal entry pushed successfully (mock)",
	}, nil
}
