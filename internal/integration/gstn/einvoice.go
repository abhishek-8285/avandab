package gstn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LineItemView represents one line item for GST e-invoicing.
type LineItemView struct {
	HSNSACCode   string  `json:"hsn_sac_code"`
	Description  string  `json:"description"`
	Unit         string  `json:"unit"`
	Quantity     float64 `json:"quantity"`
	Rate         float64 `json:"rate"`
	TaxableValue float64 `json:"taxable_value"`
	CGSTRate     float64 `json:"cgst_rate"`
	SGSTRate     float64 `json:"sgst_rate"`
	IGSTRate     float64 `json:"igst_rate"`
	CGSTAmount   float64 `json:"cgst_amount"`
	SGSTAmount   float64 `json:"sgst_amount"`
	IGSTAmount   float64 `json:"igst_amount"`
	Total        float64 `json:"total"`
}

// InvoiceView represents canonical invoice data needed for IRN generation.
type InvoiceView struct {
	InvoiceID      string         `json:"invoice_id"`
	InvoiceNumber  string         `json:"invoice_number"`
	InvoiceDate    string         `json:"invoice_date"` // YYYY-MM-DD
	SupplierGSTIN  string         `json:"supplier_gstin"`
	RecipientGSTIN string         `json:"recipient_gstin"`
	TotalValue     float64        `json:"total_value"`
	CGST           float64        `json:"cgst"`
	SGST           float64        `json:"sgst"`
	IGST           float64        `json:"igst"`
	LineItems      []LineItemView `json:"line_items"`
}

// IRNResponse is the result of IRN generation (Spec 07 §2.1).
type IRNResponse struct {
	InvoiceID string `json:"invoice_id"`
	IRN       string `json:"irn"`
	AckNo     string `json:"ack_no"`
	AckDate   string `json:"ack_date"`
	SignedQR  string `json:"signed_qr"`
	Status    string `json:"status"`
}

// PushResponse is the result of pushing e-invoice to GSTN (Spec 07 §2.2).
type PushResponse struct {
	InvoiceID string `json:"invoice_id"`
	IRN       string `json:"irn"`
	AckNo     string `json:"ack_no"`
	AckDate   string `json:"ack_date"`
	SignedQR  string `json:"signed_qr"`
	Pushed    bool   `json:"pushed"`
}

// EInvoiceClient defines operations for GST E-Invoicing / IRN generation.
type EInvoiceClient interface {
	GenerateIRN(ctx context.Context, inv InvoiceView) (*IRNResponse, error)
	PushEInvoice(ctx context.Context, invoiceID, irn string) (*PushResponse, error)
}

// ComputeIRN calculates a deterministic 64-char hex SHA-256 hash
// of the canonical invoice data (Spec 07 §5.3).
func ComputeIRN(inv InvoiceView) string {
	// Canonicalize line items
	sortedItems := make([]LineItemView, len(inv.LineItems))
	copy(sortedItems, inv.LineItems)
	sort.Slice(sortedItems, func(i, j int) bool {
		if sortedItems[i].HSNSACCode != sortedItems[j].HSNSACCode {
			return sortedItems[i].HSNSACCode < sortedItems[j].HSNSACCode
		}
		if sortedItems[i].Description != sortedItems[j].Description {
			return sortedItems[i].Description < sortedItems[j].Description
		}
		return sortedItems[i].Rate < sortedItems[j].Rate
	})

	itemBytes, _ := json.Marshal(sortedItems)
	itemSum := sha256.Sum256(itemBytes)
	itemHash := hex.EncodeToString(itemSum[:])

	canonical := struct {
		SupplierGSTIN  string  `json:"supplier_gstin"`
		RecipientGSTIN string  `json:"recipient_gstin"`
		InvoiceNo      string  `json:"invoice_no"`
		InvoiceDate    string  `json:"invoice_date"`
		TotalValue     float64 `json:"total_value"`
		ItemHash       string  `json:"item_hash"`
	}{
		SupplierGSTIN:  strings.ToUpper(strings.TrimSpace(inv.SupplierGSTIN)),
		RecipientGSTIN: strings.ToUpper(strings.TrimSpace(inv.RecipientGSTIN)),
		InvoiceNo:      strings.TrimSpace(inv.InvoiceNumber),
		InvoiceDate:    strings.TrimSpace(inv.InvoiceDate),
		TotalValue:     inv.TotalValue,
		ItemHash:       itemHash,
	}

	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// MockEInvoiceClient provides deterministic mock IRN generation without network calls.
type MockEInvoiceClient struct {
	cfg Config
}

func NewMockEInvoiceClient(cfg Config) *MockEInvoiceClient {
	return &MockEInvoiceClient{cfg: cfg}
}

func (m *MockEInvoiceClient) GenerateIRN(ctx context.Context, inv InvoiceView) (*IRNResponse, error) {
	if !m.cfg.Enabled {
		return nil, fmt.Errorf("gstn integration disabled")
	}
	if !m.cfg.UseMock {
		return nil, fmt.Errorf("gstn: e-invoice GSP credentials not configured; set INTEGRATION_GSTN_API_KEY or INTEGRATION_GSTN_USE_MOCK=true for demo mode")
	}
	irn := ComputeIRN(inv)
	ackNo := fmt.Sprintf("ACK%012d", time.Now().UnixNano()%1000000000000)
	ackDate := time.Now().Format("2006-01-02 15:04:05")
	signedQR := fmt.Sprintf("data:image/png;base64,mock_qr_%s", irn[:16])

	return &IRNResponse{
		InvoiceID: inv.InvoiceID,
		IRN:       irn,
		AckNo:     ackNo,
		AckDate:   ackDate,
		SignedQR:  signedQR,
		Status:    "ACTIVE",
	}, nil
}

func (m *MockEInvoiceClient) PushEInvoice(ctx context.Context, invoiceID, irn string) (*PushResponse, error) {
	if !m.cfg.Enabled {
		return nil, fmt.Errorf("gstn integration disabled")
	}
	if !m.cfg.UseMock {
		return nil, fmt.Errorf("gstn: e-invoice GSP credentials not configured; set INTEGRATION_GSTN_API_KEY or INTEGRATION_GSTN_USE_MOCK=true for demo mode")
	}
	if irn == "" {
		return nil, fmt.Errorf("irn is required to push e-invoice")
	}
	ackNo := fmt.Sprintf("ACK%012d", time.Now().UnixNano()%1000000000000)
	ackDate := time.Now().Format("2006-01-02 15:04:05")
	signedQR := fmt.Sprintf("data:image/png;base64,mock_qr_%s", irn[:min(16, len(irn))])

	return &PushResponse{
		InvoiceID: invoiceID,
		IRN:       irn,
		AckNo:     ackNo,
		AckDate:   ackDate,
		SignedQR:  signedQR,
		Pushed:    true,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
