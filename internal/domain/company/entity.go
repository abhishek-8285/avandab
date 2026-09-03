package company

import "time"

// CompanySettings holds global company configuration.
type CompanySettings struct {
	ID            int64
	CompanyName   string
	LogoPath      *string
	Currency      string
	Timezone      string
	GSTEnabled    bool
	GSTRate       float64
	BookingPrefix string
	TripPrefix    string
	InvoicePrefix string
	Address       *string
	Phone         *string
	Email         *string
	GSTNumber     *string
	PanNumber     *string
	StateCode     string
	FinancialYear *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TaxTier returns the 3-Tier operator classification:
// Tier 1: GST Registered Enterprise (GSTIN present)
// Tier 2: Non-GST with PAN (PAN present, no GSTIN)
// Tier 3: Micro / Unorganized Transporter (neither GSTIN nor PAN)
func (c CompanySettings) TaxTier() int {
	if c.GSTNumber != nil && *c.GSTNumber != "" {
		return 1
	}
	if c.PanNumber != nil && *c.PanNumber != "" {
		return 2
	}
	return 3
}

// LegalInvoiceTitle returns the legal title based on operator tier:
// Tier 1: "TAX INVOICE"
// Tier 2: "BILL OF SUPPLY / FREIGHT BILL"
// Tier 3: "CONSIGNMENT FREIGHT BILL"
func (c CompanySettings) LegalInvoiceTitle() string {
	switch c.TaxTier() {
	case 1:
		return "TAX INVOICE"
	case 2:
		return "BILL OF SUPPLY / FREIGHT BILL"
	default:
		return "CONSIGNMENT FREIGHT BILL"
	}
}
