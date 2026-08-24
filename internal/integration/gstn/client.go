package gstn

import (
	"context"
	"fmt"
	"log/slog"
)

// Config holds connection settings for the GSTN/GSP API.
type Config struct {
	Endpoint     string
	APIKey       string
	Enabled      bool
	UseMock      bool
	Username     string
	Password     string
	ClientID     string
	ClientSecret string
}

// GSTINDetails represents the response of a GSTIN validation lookup.
type GSTINDetails struct {
	GSTIN              string `json:"gstin"`
	LegalName          string `json:"legal_name"`
	TradeName          string `json:"trade_name"`
	Status             string `json:"status"`
	RegistrationType   string `json:"registration_type"`
	Address            string `json:"address"`
	StateCode          string `json:"state_code"`
	CenterJurisdiction string `json:"center_jurisdiction"`
}

// GSTR1Summary represents a monthly GSTR-1 summary.
type GSTR1Summary struct {
	GSTIN             string  `json:"gstin"`
	Period            string  `json:"period"`
	FilingStatus      string  `json:"filing_status"`
	FiledDate         string  `json:"filed_date"`
	TotalTaxableValue float64 `json:"total_taxable_value"`
	TotalIGST         float64 `json:"total_igst"`
	TotalCGST         float64 `json:"total_cgst"`
	TotalSGST         float64 `json:"total_sgst"`
	TotalInvoiceCount int     `json:"total_invoice_count"`
}

// GSTR3BSummary represents a monthly GSTR-3B summary.
type GSTR3BSummary struct {
	GSTIN        string  `json:"gstin"`
	Period       string  `json:"period"`
	FilingStatus string  `json:"filing_status"`
	FiledDate    string  `json:"filed_date"`
	TaxPayable   float64 `json:"tax_payable"`
	TaxPaid      float64 `json:"tax_paid"`
	ITCClaimed   float64 `json:"itc_claimed"`
}

// Client defines operations supported by the GSTN/GSP API.
type Client interface {
	ValidateGSTIN(ctx context.Context, gstin string) (GSTINDetails, error)
	FetchGSTR1Summary(ctx context.Context, gstin, period string) (GSTR1Summary, error)
	FetchGSTR3BSummary(ctx context.Context, gstin, period string) (GSTR3BSummary, error)
	GenerateIRN(ctx context.Context, inv InvoiceView) (*IRNResponse, error)
	PushEInvoice(ctx context.Context, invoiceID, irn string) (*PushResponse, error)
}

type stubClient struct {
	cfg      Config
	einvoice *MockEInvoiceClient
}

func (c *stubClient) ValidateGSTIN(ctx context.Context, gstin string) (GSTINDetails, error) {
	slog.Default().Info("[gstn] ValidateGSTIN called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "gstin", gstin)
	if !c.cfg.Enabled {
		return GSTINDetails{}, fmt.Errorf("gstn integration disabled")
	}
	if !c.cfg.UseMock {
		return GSTINDetails{}, fmt.Errorf("gstn: GSP credentials not configured; set INTEGRATION_GSTN_API_KEY or INTEGRATION_GSTN_USE_MOCK=true for demo mode")
	}
	return GSTINDetails{
		GSTIN:              gstin,
		LegalName:          "Stub Legal Entity Pvt Ltd",
		TradeName:          "Stub Traders",
		Status:             "Active",
		RegistrationType:   "Regular",
		Address:            "123 Stub Road, New Delhi - 110001",
		StateCode:          "07",
		CenterJurisdiction: "Delhi North",
	}, nil
}

func (c *stubClient) FetchGSTR1Summary(ctx context.Context, gstin, period string) (GSTR1Summary, error) {
	slog.Default().Info("[gstn] FetchGSTR1Summary called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "gstin", gstin, "period", period)
	if !c.cfg.Enabled {
		return GSTR1Summary{}, fmt.Errorf("gstn integration disabled")
	}
	if !c.cfg.UseMock {
		return GSTR1Summary{}, fmt.Errorf("gstn: GSP credentials not configured; set INTEGRATION_GSTN_API_KEY or INTEGRATION_GSTN_USE_MOCK=true for demo mode")
	}
	return GSTR1Summary{
		GSTIN:             gstin,
		Period:            period,
		FilingStatus:      "FILED",
		FiledDate:         "2026-07-11",
		TotalTaxableValue: 125000.00,
		TotalIGST:         22500.00,
		TotalCGST:         0.00,
		TotalSGST:         0.00,
		TotalInvoiceCount: 42,
	}, nil
}

func (c *stubClient) FetchGSTR3BSummary(ctx context.Context, gstin, period string) (GSTR3BSummary, error) {
	slog.Default().Info("[gstn] FetchGSTR3BSummary called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "gstin", gstin, "period", period)
	if !c.cfg.Enabled {
		return GSTR3BSummary{}, fmt.Errorf("gstn integration disabled")
	}
	if !c.cfg.UseMock {
		return GSTR3BSummary{}, fmt.Errorf("gstn: GSP credentials not configured; set INTEGRATION_GSTN_API_KEY or INTEGRATION_GSTN_USE_MOCK=true for demo mode")
	}
	return GSTR3BSummary{
		GSTIN:        gstin,
		Period:       period,
		FilingStatus: "FILED",
		FiledDate:    "2026-07-20",
		TaxPayable:   45000.00,
		TaxPaid:      45000.00,
		ITCClaimed:   12000.00,
	}, nil
}

func (c *stubClient) GenerateIRN(ctx context.Context, inv InvoiceView) (*IRNResponse, error) {
	return c.einvoice.GenerateIRN(ctx, inv)
}

func (c *stubClient) PushEInvoice(ctx context.Context, invoiceID, irn string) (*PushResponse, error) {
	return c.einvoice.PushEInvoice(ctx, invoiceID, irn)
}
