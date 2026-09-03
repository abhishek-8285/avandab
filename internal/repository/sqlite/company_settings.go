package sqlite

import (
	"context"

	"transport-app/internal/domain"

	db "transport-app/db/generated/sqlite"
)

// CompanySettingsRepository implementation

func (r *SQLRepository) GetCompanySettings(ctx context.Context) (domain.CompanySettings, error) {
	cs, err := r.Q(ctx).GetCompanySettings(ctx)
	if err != nil {
		return domain.CompanySettings{}, err
	}
	return toDomainCompanySetting(cs), nil
}

func (r *SQLRepository) UpdateCompanySettings(ctx context.Context, settings domain.CompanySettings) (domain.CompanySettings, error) {
	updated, err := r.Q(ctx).UpdateCompanySettings(ctx, db.UpdateCompanySettingsParams{
		CompanyName:   settings.CompanyName,
		LogoPath:      nullString(settings.LogoPath),
		Currency:      settings.Currency,
		Timezone:      settings.Timezone,
		GstEnabled:    settings.GSTEnabled,
		GstRate:       settings.GSTRate,
		BookingPrefix: settings.BookingPrefix,
		TripPrefix:    settings.TripPrefix,
		InvoicePrefix: settings.InvoicePrefix,
		FinancialYear: nullString(settings.FinancialYear),
		Address:       nullString(settings.Address),
		Phone:         nullString(settings.Phone),
		Email:         nullString(settings.Email),
		GstNumber:     nullString(settings.GSTNumber),
		PanNumber:     nullString(settings.PanNumber),
	})
	if err != nil {
		return domain.CompanySettings{}, err
	}
	return toDomainCompanySetting(updated), nil
}
