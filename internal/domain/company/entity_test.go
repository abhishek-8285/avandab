package company_test

import (
	"testing"
	"time"

	"transport-app/internal/domain/company"
)

func TestCompanySettings_Struct(t *testing.T) {
	now := time.Now()
	gst := "27ABCDE1234F1Z5"
	pan := "ABCDE1234F"
	fy := "2026-2027"

	cs := company.CompanySettings{
		ID:            1,
		CompanyName:   "Avandab Freight Systems",
		Currency:      "INR",
		Timezone:      "Asia/Kolkata",
		GSTEnabled:    true,
		GSTRate:       18.0,
		BookingPrefix: "BK-",
		TripPrefix:    "TRP-",
		InvoicePrefix: "INV-",
		GSTNumber:     &gst,
		PanNumber:     &pan,
		FinancialYear: &fy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if cs.CompanyName != "Avandab Freight Systems" || cs.GSTRate != 18.0 || *cs.GSTNumber != gst || *cs.PanNumber != pan {
		t.Fatalf("company settings struct mismatch")
	}

	// Tier 1: GST Registered Enterprise
	if cs.TaxTier() != 1 || cs.LegalInvoiceTitle() != "TAX INVOICE" {
		t.Fatalf("expected Tier 1 and TAX INVOICE, got tier %d and title %s", cs.TaxTier(), cs.LegalInvoiceTitle())
	}

	// Tier 2: Non-GST with PAN
	csNonGST := cs
	csNonGST.GSTNumber = nil
	if csNonGST.TaxTier() != 2 || csNonGST.LegalInvoiceTitle() != "BILL OF SUPPLY / FREIGHT BILL" {
		t.Fatalf("expected Tier 2 and BILL OF SUPPLY / FREIGHT BILL, got tier %d and title %s", csNonGST.TaxTier(), csNonGST.LegalInvoiceTitle())
	}

	// Tier 3: Micro Transporter without GST and without PAN
	csMicro := cs
	csMicro.GSTNumber = nil
	csMicro.PanNumber = nil
	if csMicro.TaxTier() != 3 || csMicro.LegalInvoiceTitle() != "CONSIGNMENT FREIGHT BILL" {
		t.Fatalf("expected Tier 3 and CONSIGNMENT FREIGHT BILL, got tier %d and title %s", csMicro.TaxTier(), csMicro.LegalInvoiceTitle())
	}
}
