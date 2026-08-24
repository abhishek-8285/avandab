package service

import (
	"context"
	"fmt"
	"strings"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// RouteService handles route management.
type RouteService struct {
	baseService
	geocoder Geocoder
}

// WithGeocoder attaches a best-effort forward geocoder used to standardize
// route endpoints on create/update. Nil (default) keeps free-text-only.
func (s *RouteService) WithGeocoder(g Geocoder) *RouteService {
	if g != nil {
		s.geocoder = g
	}
	return s
}

func tenantIDFromContext(ctx context.Context) string {
	return string(shared.TenantIDFromContext(ctx))
}

func normalizePlace(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// CreateRoute creates a new route (backward-compatible signature).
func (s *RouteService) CreateRoute(ctx context.Context, source, destination string, distance, estHours, standardFare float64, remarks string) (domain.Route, error) {
	return s.CreateRouteFull(ctx, domain.CreateRouteRequest{
		Source:         source,
		Destination:    destination,
		Distance:       distance,
		EstimatedHours: estHours,
		StandardFare:   standardFare,
		Remarks:        remarks,
		Direction:      "oneway",
	})
}

// CreateRouteFull creates a new route with all fields including reverse fare.
func (s *RouteService) CreateRouteFull(ctx context.Context, req domain.CreateRouteRequest) (domain.Route, error) {
	if req.Source == "" || req.Destination == "" {
		return domain.Route{}, fmt.Errorf("source and destination are required")
	}
	if req.Source == req.Destination {
		return domain.Route{}, fmt.Errorf("source and destination must be different")
	}
	if req.Distance <= 0 {
		return domain.Route{}, fmt.Errorf("distance must be greater than zero")
	}
	if req.EstimatedHours <= 0 {
		return domain.Route{}, fmt.Errorf("estimated hours must be greater than zero")
	}
	if req.StandardFare <= 0 {
		return domain.Route{}, fmt.Errorf("base fare must be greater than zero")
	}

	direction := req.Direction
	if direction == "" {
		direction = "oneway"
	}

	tenantID := tenantIDFromContext(ctx)

	// Check uniqueness (normalized, tenant-scoped)
	if _, err := s.store.GetRouteBySourceAndDestination(ctx, req.Source, req.Destination); err == nil {
		return domain.Route{}, fmt.Errorf("route from %s to %s already exists", req.Source, req.Destination)
	}

	route := domain.Route{
		ID:                  domain.RouteID(generateID()),
		TenantID:            tenantID,
		Source:              req.Source,
		Destination:         req.Destination,
		SourceNormalized:    normalizePlace(req.Source),
		DestNormalized:      normalizePlace(req.Destination),
		Distance:            req.Distance,
		EstimatedHours:      req.EstimatedHours,
		StandardFare:        req.StandardFare,
		ReverseDistance:     req.ReverseDistance,
		ReverseStandardFare: req.ReverseStandardFare,
		Direction:           direction,
		IsActive:            true,
		Remarks:             strPtr(req.Remarks),
	}

	created, err := s.store.CreateRoute(ctx, route)
	if err != nil {
		return domain.Route{}, err
	}

	s.geocodeEndpoints(ctx, string(created.ID), req.Source, req.Destination)

	s.log.Info("route created", "route_id", created.ID)
	return created, nil
}

// GetRoute retrieves a route by ID.
func (s *RouteService) GetRoute(ctx context.Context, id domain.RouteID) (domain.Route, error) {
	return s.store.GetRouteByID(ctx, id)
}

// ListRoutes retrieves routes with search and pagination.
func (s *RouteService) ListRoutes(ctx context.Context, query string, limit, offset int) ([]domain.Route, int64, error) {
	routes, err := s.store.SearchRoutes(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountRoutes(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	return routes, total, nil
}

// UpdateRoute updates an existing route (backward-compatible signature).
func (s *RouteService) UpdateRoute(ctx context.Context, id domain.RouteID, source, destination string, distance, estHours, standardFare float64, remarks string) (domain.Route, error) {
	return s.UpdateRouteFull(ctx, id, domain.UpdateRouteRequest{
		Source:         source,
		Destination:    destination,
		Distance:       distance,
		EstimatedHours: estHours,
		StandardFare:   standardFare,
		Remarks:        remarks,
		Direction:      "oneway",
		IsActive:       true,
	})
}

// UpdateRouteFull updates a route with all fields including reverse fare.
func (s *RouteService) UpdateRouteFull(ctx context.Context, id domain.RouteID, req domain.UpdateRouteRequest) (domain.Route, error) {
	route, err := s.store.GetRouteByID(ctx, id)
	if err != nil {
		return domain.Route{}, domain.ErrRouteNotFound
	}

	if req.Source == "" || req.Destination == "" {
		return domain.Route{}, fmt.Errorf("source and destination are required")
	}
	if req.Source == req.Destination {
		return domain.Route{}, fmt.Errorf("source and destination must be different")
	}
	if req.Distance <= 0 {
		return domain.Route{}, fmt.Errorf("distance must be greater than zero")
	}
	if req.EstimatedHours <= 0 {
		return domain.Route{}, fmt.Errorf("estimated hours must be greater than zero")
	}
	if req.StandardFare <= 0 {
		return domain.Route{}, fmt.Errorf("base fare must be greater than zero")
	}

	direction := req.Direction
	if direction == "" {
		direction = "oneway"
	}

	// Check uniqueness for other routes (normalized)
	if existing, err := s.store.GetRouteBySourceAndDestination(ctx, req.Source, req.Destination); err == nil && existing.ID != id {
		return domain.Route{}, fmt.Errorf("route from %s to %s already exists", req.Source, req.Destination)
	}

	route.Source = req.Source
	route.Destination = req.Destination
	route.SourceNormalized = normalizePlace(req.Source)
	route.DestNormalized = normalizePlace(req.Destination)
	route.Distance = req.Distance
	route.EstimatedHours = req.EstimatedHours
	route.StandardFare = req.StandardFare
	route.ReverseDistance = req.ReverseDistance
	route.ReverseStandardFare = req.ReverseStandardFare
	route.Direction = direction
	route.IsActive = req.IsActive
	route.Remarks = strPtr(req.Remarks)

	updated, err := s.store.UpdateRoute(ctx, route)
	if err != nil {
		return domain.Route{}, err
	}

	s.geocodeEndpoints(ctx, string(id), req.Source, req.Destination)

	s.log.Info("route updated", "route_id", id)
	return updated, nil
}

// DeleteRoute deletes a route.
func (s *RouteService) DeleteRoute(ctx context.Context, id domain.RouteID) error {
	if err := s.store.DeleteRoute(ctx, id); err != nil {
		return err
	}
	s.log.Info("route deleted", "route_id", id)
	return nil
}
