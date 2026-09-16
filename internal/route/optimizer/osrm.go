package optimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OSRMClient calls OSRM route/table for distance matrix, then greedy assigns.
// Pluggable URL: public demo http://router.project-osrm.org or self-host http://osrm.internal:5000
type OSRMClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (o *OSRMClient) Name() string {
	if strings.Contains(o.BaseURL, "project-osrm.org") {
		return "osrm-public"
	}
	if o.BaseURL != "" {
		return "osrm-selfhost"
	}
	return "osrm"
}

func (o *OSRMClient) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Solve uses OSRM table for distance matrix; falls back to haversine on error.
func (o *OSRMClient) Solve(ctx context.Context, in OptimizationInput) (OptimizationOutput, error) {
	if err := in.Validate(); err != nil {
		return OptimizationOutput{}, err
	}
	start := time.Now()

	// If no URL configured, delegate to mock.
	if o.BaseURL == "" {
		m := &MockOptimizer{}
		out, err := m.Solve(ctx, in)
		if err == nil {
			out.ProviderName = o.Name()
		}
		return out, err
	}

	// Build coordinate list: vehicles starts + shipments
	coords := []string{}
	for _, v := range in.Vehicles {
		coords = append(coords, fmt.Sprintf("%f,%f", v.StartLng, v.StartLat))
	}
	for _, s := range in.Shipments {
		coords = append(coords, fmt.Sprintf("%f,%f", s.Longitude, s.Latitude))
	}
	coordStr := strings.Join(coords, ";")
	nVeh := len(in.Vehicles)

	// OSRM table: sources = vehicles, destinations = shipments
	// GET /table/v1/driving/{coords}?sources=0..nVeh-1&destinations=nVeh..end&annotations=distance,duration
	u, _ := url.Parse(strings.TrimRight(o.BaseURL, "/") + "/table/v1/driving/" + coordStr)
	q := u.Query()
	srcIdx := make([]string, nVeh)
	for i := 0; i < nVeh; i++ {
		srcIdx[i] = fmt.Sprintf("%d", i)
	}
	dstIdx := make([]string, len(in.Shipments))
	for i := range in.Shipments {
		dstIdx[i] = fmt.Sprintf("%d", nVeh+i)
	}
	q.Set("sources", strings.Join(srcIdx, ";"))
	q.Set("destinations", strings.Join(dstIdx, ";"))
	q.Set("annotations", "distance,duration")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := o.client().Do(req)
	if err != nil {
		// Fallback to mock on network error
		m := &MockOptimizer{}
		out, _ := m.Solve(ctx, in)
		out.ProviderName = o.Name() + "-fallback"
		return out, nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug("close OSRM response body", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = body
		m := &MockOptimizer{}
		out, _ := m.Solve(ctx, in)
		out.ProviderName = o.Name() + "-fallback"
		return out, nil
	}
	var tbl struct {
		Code      string      `json:"code"`
		Distances [][]float64 `json:"distances"`
		Durations [][]float64 `json:"durations"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &tbl); err != nil || tbl.Code != "Ok" {
		m := &MockOptimizer{}
		out, _ := m.Solve(ctx, in)
		out.ProviderName = o.Name() + "-fallback"
		return out, nil
	}

	// Greedy assignment using OSRM distances
	routes := make([]OptimizedRoute, nVeh)
	for i, v := range in.Vehicles {
		routes[i] = OptimizedRoute{VehicleID: v.ID}
	}
	assigned := make([]bool, len(in.Shipments))
	totalKM := 0.0
	for k := 0; k < len(in.Shipments); k++ {
		bestS, bestV := -1, -1
		bestD := 1e12
		for si := range in.Shipments {
			if assigned[si] {
				continue
			}
			for vi := 0; vi < nVeh; vi++ {
				if vi >= len(tbl.Distances) || si >= len(tbl.Distances[vi]) {
					continue
				}
				dM := tbl.Distances[vi][si] // meters
				if dM <= 0 {
					continue
				}
				dKM := dM / 1000.0
				if dKM < bestD {
					bestD = dKM
					bestS = si
					bestV = vi
				}
			}
		}
		if bestS == -1 {
			break
		}
		assigned[bestS] = true
		durMin := 0.0
		if bestV < len(tbl.Durations) && bestS < len(tbl.Durations[bestV]) {
			durMin = tbl.Durations[bestV][bestS] / 60.0
		}
		if durMin <= 0 {
			durMin = bestD / 40.0 * 60.0
		}
		leg := RouteLeg{
			ShipmentID:  in.Shipments[bestS].ID,
			DistanceKM:  bestD,
			DurationMin: durMin,
			Sequence:    len(routes[bestV].Legs) + 1,
		}
		routes[bestV].Legs = append(routes[bestV].Legs, leg)
		routes[bestV].TotalKM += bestD
		routes[bestV].TotalMin += durMin
		totalKM += bestD
	}

	// Enrich each route with real road geometry + per-leg ETA/distance from
	// OSRM /route. On any error this degrades gracefully to the table-derived
	// values computed above — never fails the whole solve.
	o.enrichRoutes(ctx, in, routes)

	// Recompute totals from the (possibly enriched) routes. enrichRoutes
	// overwrites each route's TotalKM/TotalMin with road-based values, so we
	// must recompute here rather than add to the greedy-loop sum above, which
	// would double-count.
	totalKM = 0
	for i := range routes {
		totalKM += routes[i].TotalKM
	}

	out := OptimizationOutput{
		Routes:       routes,
		TotalKM:      totalKM,
		TotalCost:    totalKM,
		SolveTimeMS:  time.Since(start).Milliseconds(),
		ProviderName: o.Name(),
		CreatedAt:    time.Now().UTC(),
	}
	return out, nil
}

// enrichRoutes fetches /route for each vehicle's ordered stop sequence
// (vehicle start followed by its assigned shipment legs) and overwrites the
// table-derived leg DistanceKM/DurationMin with real road values plus the
// full encoded-polyline Geometry. Errors / non-Ok responses leave the
// existing (fallback) values untouched so a solve never fails on routing.
type osrmRouteResp struct {
	Code   string `json:"code"`
	Routes []struct {
		Geometry string `json:"geometry"`
		Legs     []struct {
			Distance float64 `json:"distance"` // meters
			Duration float64 `json:"duration"` // seconds
		} `json:"legs"`
	} `json:"routes"`
}

func (o *OSRMClient) enrichRoutes(ctx context.Context, in OptimizationInput, routes []OptimizedRoute) {
	if o.BaseURL == "" {
		return
	}
	// shipment ID -> coordinates, to build ordered waypoints per route.
	shipCoord := make(map[string][2]float64, len(in.Shipments))
	for _, s := range in.Shipments {
		shipCoord[s.ID] = [2]float64{s.Longitude, s.Latitude}
	}
	vehStart := make(map[string][2]float64, len(in.Vehicles))
	for _, v := range in.Vehicles {
		vehStart[v.ID] = [2]float64{v.StartLng, v.StartLat}
	}

	for i := range routes {
		r := &routes[i]
		if len(r.Legs) == 0 {
			continue
		}
		start, ok := vehStart[r.VehicleID]
		if !ok {
			continue
		}
		coords := []string{fmt.Sprintf("%f,%f", start[0], start[1])}
		for _, leg := range r.Legs {
			c, ok := shipCoord[leg.ShipmentID]
			if !ok {
				return
			}
			coords = append(coords, fmt.Sprintf("%f,%f", c[0], c[1]))
		}
		resp, ok := o.fetchRoute(ctx, coords)
		if !ok {
			continue
		}
		if len(resp.Routes) == 0 || resp.Routes[0].Geometry == "" {
			continue
		}
		rt := resp.Routes[0]
		r.Geometry = rt.Geometry
		totalKM, totalMin := 0.0, 0.0
		for li := 0; li < len(rt.Legs) && li < len(r.Legs); li++ {
			distKM := rt.Legs[li].Distance / 1000.0
			durMin := rt.Legs[li].Duration / 60.0
			r.Legs[li].DistanceKM = distKM
			r.Legs[li].DurationMin = durMin
			totalKM += distKM
			totalMin += durMin
		}
		r.TotalKM = totalKM
		r.TotalMin = totalMin
	}
}

// fetchRoute calls GET /route/v1/driving/{coords}?overview=full&geometries=polyline
// and returns the parsed body plus an ok flag. Non-200 or non-Ok responses
// return ok=false so the caller keeps its fallback values.
func (o *OSRMClient) fetchRoute(ctx context.Context, coords []string) (osrmRouteResp, bool) {
	u, _ := url.Parse(strings.TrimRight(o.BaseURL, "/") + "/route/v1/driving/" + strings.Join(coords, ";"))
	q := u.Query()
	q.Set("overview", "full")
	q.Set("geometries", "polyline")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := o.client().Do(req)
	if err != nil {
		return osrmRouteResp{}, false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug("close OSRM route response body", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return osrmRouteResp{}, false
	}
	var out osrmRouteResp
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &out); err != nil || out.Code != "Ok" {
		return osrmRouteResp{}, false
	}
	return out, true
}
