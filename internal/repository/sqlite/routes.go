package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"

	db "transport-app/db/generated/sqlite"
)

// RouteRepository implementation

func (r *SQLRepository) CreateRoute(ctx context.Context, route domain.Route) (domain.Route, error) {
	tid := route.TenantID
	if tid == "" {
		tid = tenantIDFromCtx(ctx)
	}
	created, err := r.Q(ctx).CreateRoute(ctx, db.CreateRouteParams{
		ID:                  string(route.ID),
		TenantID:            tid,
		Source:              route.Source,
		Destination:         route.Destination,
		SourceNormalized:    route.SourceNormalized,
		DestNormalized:      route.DestNormalized,
		Distance:            route.Distance,
		EstimatedHours:      route.EstimatedHours,
		StandardFare:        route.StandardFare,
		ReverseDistance:     nullFloat(route.ReverseDistance),
		ReverseStandardFare: nullFloat(route.ReverseStandardFare),
		Direction:           route.Direction,
		IsActive:            boolToInt64(route.IsActive),
		Remarks:             nullString(route.Remarks),
	})
	if err != nil {
		return domain.Route{}, err
	}
	return routeRowToDomain(created), nil
}

func (r *SQLRepository) GetRouteByID(ctx context.Context, id domain.RouteID) (domain.Route, error) {
	route, err := r.Q(ctx).GetRouteByID(ctx, db.GetRouteByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Route{}, err
	}
	return routeRowToDomain(route), nil
}

func (r *SQLRepository) GetRouteBySourceAndDestination(ctx context.Context, source, destination string) (domain.Route, error) {
	route, err := r.Q(ctx).GetRouteBySourceAndDestination(ctx, db.GetRouteBySourceAndDestinationParams{
		Source:      source,
		Destination: destination,
		TenantID:    tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Route{}, err
	}
	return routeRowToDomain(route), nil
}

func (r *SQLRepository) UpdateRoute(ctx context.Context, route domain.Route) (domain.Route, error) {
	updated, err := r.Q(ctx).UpdateRoute(ctx, db.UpdateRouteParams{
		Source:              route.Source,
		Destination:         route.Destination,
		SourceNormalized:    route.SourceNormalized,
		DestNormalized:      route.DestNormalized,
		Distance:            route.Distance,
		EstimatedHours:      route.EstimatedHours,
		StandardFare:        route.StandardFare,
		ReverseDistance:     nullFloat(route.ReverseDistance),
		ReverseStandardFare: nullFloat(route.ReverseStandardFare),
		Direction:           route.Direction,
		IsActive:            boolToInt64(route.IsActive),
		Remarks:             nullString(route.Remarks),
		ID:                  string(route.ID),
		TenantID:            tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Route{}, err
	}
	return routeRowToDomain(updated), nil
}

func (r *SQLRepository) DeleteRoute(ctx context.Context, id domain.RouteID) error {
	return r.Q(ctx).DeleteRoute(ctx, db.DeleteRouteParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchRoutes(ctx context.Context, query string, limit, offset int) ([]domain.Route, error) {
	rows, err := r.Q(ctx).SearchRoutes(ctx, db.SearchRoutesParams{
		Column1:  sql.NullString{String: query, Valid: true},
		Column2:  sql.NullString{String: query, Valid: true},
		TenantID: tenantIDFromCtx(ctx),
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Route, len(rows))
	for i, row := range rows {
		result[i] = routeRowToDomain(row)
	}
	return result, nil
}

func (r *SQLRepository) CountRoutes(ctx context.Context, query string) (int64, error) {
	return r.Q(ctx).CountRoutes(ctx, db.CountRoutesParams{
		Column1:  sql.NullString{String: query, Valid: query != ""},
		Column2:  sql.NullString{String: query, Valid: query != ""},
		TenantID: tenantIDFromCtx(ctx),
	})
}
