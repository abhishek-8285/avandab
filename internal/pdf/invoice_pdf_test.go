package pdf

import (
	"bytes"
	"strings"
	"testing"
)

func validPDF(t *testing.T, b []byte, minLen int) {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF: %q", b[:min(8, len(b))])
	}
	if len(b) < minLen {
		t.Fatalf("PDF suspiciously small: %d bytes", len(b))
	}
}

func gstFixture(intra bool) InvoicePDFData {
	d := InvoicePDFData{
		Company:       PDFParty{Name: "Sharma Roadlines", Address: "12 Transport Nagar, Nagpur", GSTIN: "27ABCDE1234F1Z5", StateCode: "27", Phone: "9876543210"},
		Customer:      PDFParty{Name: "Acme Manufacturing Pvt Ltd", Address: "Plot 7, MIDC, Pune", GSTIN: "27AACCA1234B1Z9", StateCode: "27"},
		InvoiceNumber: "INV/2026/0042",
		PaymentStatus: "pending",
		InvoiceDate:   "10 Aug 2026",
		DueDate:       "25 Aug 2026",
		PlaceOfSupply: "27",
		Subtotal:      100000,
		Tax:           18000,
		Total:         118000,
		Paid:          50000,
		Balance:       68000,
		GSTBreakdown:  true,
		IntraState:    intra,
		TaxableTotal:  100000,
		Items: []PDFLineItem{
			{Description: "Full truck load, Pune to Nagpur", HSNSAC: "996511", Quantity: "1 TRIP", Rate: 100000, TaxableValue: 100000,
				CGST:  map[bool]float64{true: 9000, false: 0}[intra],
				SGST:  map[bool]float64{true: 9000, false: 0}[intra],
				IGST:  map[bool]float64{true: 0, false: 18000}[intra],
				Total: 118000},
		},
	}
	return d
}

func TestGenerateInvoicePDF_IntraStateGST(t *testing.T) {
	b, err := GenerateInvoicePDF(gstFixture(true))
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 2000)
}

func TestGenerateInvoicePDF_InterStateIGST(t *testing.T) {
	d := gstFixture(false)
	d.Customer.GSTIN = "29AABCU9603R1ZM" // Karnataka
	d.Customer.StateCode = "29"
	d.PlaceOfSupply = "29"
	b, err := GenerateInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 2000)
}

func TestGenerateInvoicePDF_SimpleFallback_NoGSTData(t *testing.T) {
	b, err := GenerateInvoicePDF(InvoicePDFData{
		Company:       PDFParty{Name: "Solo Trucking"},
		InvoiceNumber: "INV-7",
		InvoiceDate:   "01 Aug 2026",
		Subtotal:      5000,
		Tax:           0,
		Total:         5000,
		Balance:       5000,
		Items: []PDFLineItem{
			{Description: "Local haulage", Quantity: "2 NOS", Rate: 2500, Total: 5000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 1200)
}

func TestGenerateInvoicePDF_EmptyDataStillValid(t *testing.T) {
	b, err := GenerateInvoicePDF(InvoicePDFData{})
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 800)
}

func TestGenerateInvoicePDF_MultiPageManyItems(t *testing.T) {
	d := gstFixture(true)
	for i := 0; i < 80; i++ {
		d.Items = append(d.Items, PDFLineItem{
			Description:  strings.Repeat("Trip leg ", 1) + string(rune('a'+i%26)) + " — local shuttle run",
			HSNSAC:       "996511",
			Quantity:     "1 TRIP",
			Rate:         1000,
			TaxableValue: 1000,
			CGST:         90,
			SGST:         90,
			Total:        1180,
		})
	}
	d.Total += float64(len(d.Items)-1) * 1180
	b, err := GenerateInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 15000)
	if n := bytes.Count(b, []byte("/Type /Page")); n < 2 {
		t.Errorf("expected multiple pages, found %d markers", n)
	}
}

func TestGenerateInvoicePDF_IRNAndQRRendered(t *testing.T) {
	d := gstFixture(true)
	d.IRN = "64-char-irn-payload-example-0000000000000000000000000000000000"
	d.IRNAckNo = "112010036408862"
	d.IRNAckDate = "2026-08-24 10:00:00"
	d.EWBNumber = "271002349876"
	d.SignedQR = tinyPNG()
	b, err := GenerateInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 3000)
	// Embedded image must be present in the PDF object stream.
	if !bytes.Contains(b, []byte("/Image")) && !bytes.Contains(b, []byte("/XObject")) {
		t.Error("QR image not embedded in PDF")
	}
}

func TestGenerateInvoicePDF_URLQRIsNotFetched(t *testing.T) {
	d := gstFixture(true)
	d.SignedQR = []byte("https://example.com/qr.png") // must be ignored
	b, err := GenerateInvoicePDF(d)
	if err != nil {
		t.Fatal(err)
	}
	validPDF(t, b, 2000)
	if bytes.Contains(b, []byte("https://example.com")) {
		t.Error("URL leaked into document")
	}
}
