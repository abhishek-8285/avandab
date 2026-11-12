package application

// Agent-facing route read model + fare quote, extracted from
// internal/agent/tools.go (handlers-end-state ADR, Phase 3). The tools keep
// schemas/validation/RBAC/JSON wrapping; this file owns the tenant-scoped
// route query, the legacy bidirectional matching (domain route rule) and the
// GST rate read.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	routeentity "transport-app/internal/domain/route"
	"transport-app/internal/domain/types"
	"transport-app/internal/shared"
)

// AgentRouteRow is the search_routes JSON row.
type AgentRouteRow struct {
	ID             string  `json:"id"`
	Source         string  `json:"source"`
	Destination    string  `json:"destination"`
	DistanceKM     float64 `json:"distance_km"`
	EstimatedHours float64 `json:"estimated_hours"`
	StandardFare   float64 `json:"standard_fare"`
}

// AgentQuote is the get_quote JSON shape.
type AgentQuote struct {
	RouteID        string  `json:"route_id"`
	From           string  `json:"from"`
	To             string  `json:"to"`
	DistanceKM     float64 `json:"distance_km"`
	EstimatedHours float64 `json:"estimated_hours"`
	BaseFare       float64 `json:"base_fare"`
	GSTRatePct     float64 `json:"gst_rate_pct"`
	GSTAmount      float64 `json:"gst_amount"`
	TotalWithGST   float64 `json:"total_with_gst"`
}

// AgentRouteQueries is the route facade. Stateless.
type AgentRouteQueries struct{ db *sql.DB }

// NewAgentRouteQueries wires the facade against the database.
func NewAgentRouteQueries(db *sql.DB) *AgentRouteQueries { return &AgentRouteQueries{db: db} }

const agentRouteColumns = `id, source, destination, distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare`

// Search returns the tenant's routes matching query in source or destination,
// newest first (legacy SearchRoutes semantics).
func (q *AgentRouteQueries) Search(ctx context.Context, tenantID shared.TenantID, query string, limit int) ([]AgentRouteRow, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := q.db.QueryContext(ctx, `SELECT `+agentRouteColumns+` FROM routes WHERE tenant_id = ? AND (lower(source) LIKE ? OR lower(destination) LIKE ?) ORDER BY created_at DESC LIMIT ?`,
		string(tenantID), "%"+strings.ToLower(query)+"%", "%"+strings.ToLower(query)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AgentRouteRow
	for rows.Next() {
		r, err := scanAgentRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, AgentRouteRow{r.ID.String(), r.Source, r.Destination, r.Distance, r.EstimatedHours, r.StandardFare})
	}
	return out, rows.Err()
}

// Quote matches a route between from/to using the domain bidirectional rule
// (GET/pickup direction reverse fare honored) and computes GST like the legacy
// tool. A missing route returns (nil, nil); the tool renders the hint string.
func (q *AgentRouteQueries) Quote(ctx context.Context, tenantID shared.TenantID, from, to string) (*AgentQuote, error) {
	if q == nil || q.db == nil {
		return nil, sql.ErrNoRows
	}
	rows, err := q.db.QueryContext(ctx, `SELECT `+agentRouteColumns+` FROM routes WHERE tenant_id = ? AND (lower(source) LIKE ? OR lower(destination) LIKE ?) ORDER BY created_at DESC LIMIT 50`,
		string(tenantID), "%"+strings.ToLower(from)+"%", "%"+strings.ToLower(from)+"%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var match *routeentity.Route
	for rows.Next() {
		r, err := scanAgentRoute(rows)
		if err != nil {
			return nil, err
		}
		d, f, rev := r.GetDistanceAndFare(from, to)
		if d > 0 && f >= 0 {
			match = &r
			match.Distance = d
			match.StandardFare = f
			_ = rev
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}
	rate, err := q.gstRate(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("settings unavailable: %w", err)
	}
	tax := match.StandardFare * rate / 100
	return &AgentQuote{
		RouteID: match.ID.String(), From: match.Source, To: match.Destination,
		DistanceKM: match.Distance, EstimatedHours: match.EstimatedHours,
		BaseFare: match.StandardFare, GSTRatePct: rate, GSTAmount: tax,
		TotalWithGST: match.StandardFare + tax,
	}, nil
}

// gstRate mirrors legacy GetSettings: per-tenant profile rate for org tenants,
// company_settings id=1 for the bootstrap/global tenant; absent row = 0%.
func (q *AgentRouteQueries) gstRate(ctx context.Context, tenantID shared.TenantID) (float64, error) {
	if tid := string(tenantID); tid != "" && tid != string(shared.DefaultTenant) { //nolint:tenant-default // bootstrap tenant reads the global company_settings GST rate; identity branching only
		var rate float64
		err := q.db.QueryRowContext(ctx, `SELECT gst_rate FROM tenant_company_profiles WHERE tenant_id = ?`, tid).Scan(&rate)
		if err == nil {
			return rate, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	var rate float64
	err := q.db.QueryRowContext(ctx, `SELECT gst_rate FROM company_settings WHERE id = 1`).Scan(&rate)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rate, err
}

// scanAgentRoute loads one route into the domain entity so GetDistanceAndFare
// stays the single source of the bidirectional matching rule.
func scanAgentRoute(row interface{ Scan(...any) error }) (routeentity.Route, error) {
	var r routeentity.Route
	var id string
	var revDist, revFare sql.NullFloat64
	if err := row.Scan(&id, &r.Source, &r.Destination, &r.Distance, &r.EstimatedHours, &r.StandardFare, &revDist, &revFare); err != nil {
		return r, err
	}
	r.ID = types.RouteID(id)
	r.SourceNormalized = strings.ToLower(strings.TrimSpace(r.Source))
	r.DestNormalized = strings.ToLower(strings.TrimSpace(r.Destination))
	if revDist.Valid {
		r.ReverseDistance = &revDist.Float64
	}
	if revFare.Valid {
		r.ReverseStandardFare = &revFare.Float64
	}
	return r, nil
}
