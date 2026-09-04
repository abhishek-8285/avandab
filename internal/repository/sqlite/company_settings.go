package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"transport-app/internal/domain"
	"transport-app/internal/shared"

	db "transport-app/db/generated/sqlite"
)

// CompanySettingsRepository implementation
//
// Reads and writes are tenant-scoped through tenant_company_profiles
// (migration 00125). The company_settings singleton (id=1) survives only as
// the platform-global default owned by the bootstrap tenant and tenant-less
// contexts: tenant row wins, rowless non-default tenants get a blank identity
// over table defaults (forces /company/onboard), and the bootstrap tenant
// keeps the legacy global behavior for background jobs and tests.

func (r *SQLRepository) GetCompanySettings(ctx context.Context) (domain.CompanySettings, error) {
	if tid := shared.TenantIDFromContext(ctx); tid != "" && tid != shared.DefaultTenant {
		if p, err := r.Q(ctx).GetTenantCompanyProfile(ctx, string(tid)); err == nil {
			return toDomainTenantCompanyProfile(p), nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return domain.CompanySettings{}, err
		}
		// No tenant row yet: blank identity (forces /company/onboard) over
		// the table defaults so prefix/currency consumers keep working.
		return domain.CompanySettings{
			Currency: "INR", Timezone: "Asia/Kolkata",
			BookingPrefix: "BK", TripPrefix: "TR", InvoicePrefix: "INV",
			StateCode: "27",
		}, nil
	}
	cs, err := r.Q(ctx).GetCompanySettings(ctx)
	if err != nil {
		return domain.CompanySettings{}, err
	}
	return toDomainCompanySetting(cs), nil
}

func (r *SQLRepository) UpdateCompanySettings(ctx context.Context, settings domain.CompanySettings) (domain.CompanySettings, error) {
	// Bootstrap tenant and tenant-less contexts own the global singleton;
	// every other tenant writes its isolated profile row.
	if tid := shared.TenantIDFromContext(ctx); tid != "" && tid != shared.DefaultTenant {
		stateCode := settings.StateCode
		if stateCode == "" {
			stateCode = "27"
		}
		p, err := r.Q(ctx).UpsertTenantCompanyProfile(ctx, db.UpsertTenantCompanyProfileParams{
			TenantID:      string(tid),
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
			StateCode:     stateCode,
		})
		if err != nil {
			return domain.CompanySettings{}, err
		}
		return toDomainTenantCompanyProfile(p), nil
	}
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
