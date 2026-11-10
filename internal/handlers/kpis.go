package handlers

import (
	"context"

	"transport-app/internal/kpi"
	"transport-app/internal/shared"
)

// KPI is kept as an alias so templates and existing tests keep their type while
// the read model lives in internal/kpi (handlers-end-state ADR Phase 1).
type KPI = kpi.KPI

func (a *App) kpiService() *kpi.Service {
	return kpi.New(a.DB)
}

func (a *App) bookingKPIs(ctx context.Context) []KPI {
	return a.kpiService().Bookings(ctx, string(shared.TenantIDFromContext(ctx)))
}

func (a *App) tripKPIs(ctx context.Context) []KPI {
	return a.kpiService().Trips(ctx, string(shared.TenantIDFromContext(ctx)))
}

func (a *App) vehicleKPIs(ctx context.Context) []KPI {
	return a.kpiService().Vehicles(ctx, string(shared.TenantIDFromContext(ctx)))
}

func (a *App) driverKPIs(ctx context.Context) []KPI {
	return a.kpiService().Drivers(ctx, string(shared.TenantIDFromContext(ctx)))
}

func (a *App) paymentKPIs(ctx context.Context) []KPI {
	return a.kpiService().Payments(ctx, string(shared.TenantIDFromContext(ctx)))
}

func (a *App) invoiceKPIs(ctx context.Context) []KPI {
	return a.kpiService().Invoices(ctx, string(shared.TenantIDFromContext(ctx)))
}
