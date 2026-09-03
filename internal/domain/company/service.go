package company

import "context"

// CompanyService defines the interface for company settings operations.
type CompanyService interface {
	GetSettings(ctx context.Context) (CompanySettings, error)
	UpdateSettings(ctx context.Context, companyName, currency, timezone string, gstEnabled bool, gstRate float64, bookingPrefix, tripPrefix, invoicePrefix, financialYear string, address, phone, email, gstNumber, panNumber string, logoPath *string) (CompanySettings, error)
}
