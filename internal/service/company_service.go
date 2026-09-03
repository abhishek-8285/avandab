package service

import (
	"context"
	"strings"

	"transport-app/internal/domain"
)

// CompanySettingsService handles company configuration.
type CompanySettingsService struct {
	baseService
}

// GetSettings returns the company settings.
func (s *CompanySettingsService) GetSettings(ctx context.Context) (domain.CompanySettings, error) {
	return s.store.GetCompanySettings(ctx)
}

// UpdateSettings updates the company settings.
func (s *CompanySettingsService) UpdateSettings(ctx context.Context, companyName, currency, timezone string, gstEnabled bool, gstRate float64, bookingPrefix, tripPrefix, invoicePrefix, financialYear string, address, phone, email, gstNumber, panNumber string, logoPath *string) (domain.CompanySettings, error) {
	gstNumber = strings.TrimSpace(strings.ToUpper(gstNumber))
	panNumber = strings.TrimSpace(strings.ToUpper(panNumber))

	// If gst_number is provided, automatically extract PAN (gst_number[2:12]) if pan_number is empty
	if gstNumber != "" && len(gstNumber) == 15 && panNumber == "" {
		panNumber = gstNumber[2:12]
	}

	settings := domain.CompanySettings{
		CompanyName:   companyName,
		Currency:      currency,
		Timezone:      timezone,
		GSTEnabled:    gstEnabled,
		GSTRate:       gstRate,
		BookingPrefix: bookingPrefix,
		TripPrefix:    tripPrefix,
		InvoicePrefix: invoicePrefix,
		FinancialYear: strPtr(financialYear),
		Address:       &address,
		Phone:         &phone,
		Email:         &email,
		GSTNumber:     &gstNumber,
		PanNumber:     &panNumber,
		LogoPath:      logoPath,
	}

	updated, err := s.store.UpdateCompanySettings(ctx, settings)
	if err != nil {
		return domain.CompanySettings{}, err
	}

	s.log.Info("company settings updated")
	return updated, nil
}
