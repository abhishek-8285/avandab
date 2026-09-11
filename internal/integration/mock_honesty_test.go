package integration

// A1 mock-honesty carpet: every provider mock that synthesizes an external ID
// must mark it MOCK- (EWB-MOCK-, MOCK-, JE-MOCK-, EXT-MOCK-) so demo data is
// never mistaken for real provider data. GSTN e-invoices are the documented
// exception: the IRN is format-locked (64-hex NIC hash persisted on invoices),
// so honesty there is mockWarn logs + mock_qr_ payloads instead of a prefix.
import (
	"context"
	"strings"
	"testing"

	"transport-app/internal/integration/accounting"
	"transport-app/internal/integration/ewaybill"
	"transport-app/internal/integration/fastag"
	"transport-app/internal/integration/gstn"
)

func TestMockHonesty_SyntheticIDsCarryMockPrefix(t *testing.T) {
	ctx := context.Background()

	ewb, err := ewaybill.NewClient(ewaybill.Config{Enabled: true, UseMock: true}).Generate(ctx, ewaybill.GenerateRequest{DocumentNumber: "INV-A1"})
	if err != nil {
		t.Fatalf("ewaybill mock generate: %v", err)
	}
	if !strings.HasPrefix(ewb.EwbNumber, "EWB-MOCK-") {
		t.Errorf("ewaybill mock EwbNumber %q missing EWB-MOCK- prefix", ewb.EwbNumber)
	}

	txn, err := fastag.NewClient(fastag.Config{Enabled: true, UseMock: true}).DeductToll(ctx, fastag.DeductTollRequest{VehicleNumber: "MH01A1", TagID: "TAG-A1", Amount: 100})
	if err != nil {
		t.Fatalf("fastag mock deduct: %v", err)
	}
	if !strings.HasPrefix(txn.TransactionID, "MOCK-") {
		t.Errorf("fastag mock TransactionID %q missing MOCK- prefix", txn.TransactionID)
	}

	acct := accounting.NewClient(accounting.Config{UseMock: true})
	exp, err := acct.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-A1"})
	if err != nil {
		t.Fatalf("accounting mock export: %v", err)
	}
	if !strings.HasPrefix(exp.ExternalID, "EXT-MOCK-") {
		t.Errorf("accounting mock ExternalID %q missing EXT-MOCK- prefix", exp.ExternalID)
	}
	je, err := acct.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "JE-A1"})
	if err != nil {
		t.Fatalf("accounting mock journal: %v", err)
	}
	if !strings.HasPrefix(je.EntryID, "JE-MOCK-") {
		t.Errorf("accounting mock EntryID %q missing JE-MOCK- prefix", je.EntryID)
	}

	for provider, want := range map[string][2]string{
		"tally":      {"TALLY-MOCK-INV-", "TALLY-MOCK-JE-"},
		"zoho":       {"ZOHO-MOCK-INV-", "ZOHO-MOCK-JE-"},
		"quickbooks": {"QB-MOCK-INV-", "QB-MOCK-JE-"},
	} {
		cli := accounting.NewClient(accounting.Config{Provider: provider, Enabled: true, UseMock: true})
		exp, err := cli.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-A1"})
		if err != nil {
			t.Fatalf("%s mock export: %v", provider, err)
		}
		if !strings.HasPrefix(exp.ExternalID, want[0]) {
			t.Errorf("%s mock ExternalID %q missing %s prefix", provider, exp.ExternalID, want[0])
		}
		jr, err := cli.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "JE-A1"})
		if err != nil {
			t.Fatalf("%s mock journal: %v", provider, err)
		}
		if !strings.HasPrefix(jr.EntryID, want[1]) {
			t.Errorf("%s mock EntryID %q missing %s prefix", provider, jr.EntryID, want[1])
		}
	}

	irn, err := gstn.NewMockEInvoiceClient(gstn.Config{Enabled: true, UseMock: true}).GenerateIRN(ctx, gstn.InvoiceView{
		InvoiceID: "inv-a1", InvoiceNumber: "INV-A1", InvoiceDate: "2026-09-11",
		SupplierGSTIN: "27AAAAA0000A1Z5", RecipientGSTIN: "27BBBBB0000B1Z5", TotalValue: 1180,
	})
	if err != nil {
		t.Fatalf("gstn mock IRN: %v", err)
	}
	if irn.IRN != gstn.ComputeIRN(gstn.InvoiceView{
		InvoiceID: "inv-a1", InvoiceNumber: "INV-A1", InvoiceDate: "2026-09-11",
		SupplierGSTIN: "27AAAAA0000A1Z5", RecipientGSTIN: "27BBBBB0000B1Z5", TotalValue: 1180,
	}) {
		t.Error("gstn mock IRN must be the deterministic demo hash, not provider data")
	}
	if !strings.Contains(irn.SignedQR, "mock_qr_") {
		t.Errorf("gstn mock QR must be marked mock_qr_, got %q", irn.SignedQR)
	}
}
