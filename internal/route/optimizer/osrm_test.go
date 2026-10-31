package optimizer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubOSRM spins up an httptest server that answers /table (distance matrix)
// and /route (geometry + per-leg ETA) exactly like OSRM's HTTP API. It returns
// the server plus the OSRMClient bound to it.
func stubOSRM(t *testing.T) (*httptest.Server, *OSRMClient) {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/table/", func(w http.ResponseWriter, r *http.Request) {
		// 1 vehicle -> source 0; 2 shipments -> destinations 1,2.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "Ok",
			"distances": [][]float64{
				{1000.0, 2000.0}, // v1 -> s1, s2 (meters)
			},
			"durations": [][]float64{
				{60.0, 120.0}, // seconds
			},
		})
	})

	mux.HandleFunc("/route/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// legs correspond to start->s1, s1->s2 road segments.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "Ok",
			"routes": []map[string]any{
				{
					"geometry": "encoded_polyline_here",
					"distance": 3500.0, // meters
					"duration": 180.0,  // seconds
					"legs": []map[string]any{
						{"distance": 1500.0, "duration": 80.0},
						{"distance": 2000.0, "duration": 100.0},
					},
				},
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &OSRMClient{BaseURL: srv.URL}
}

func TestOSRMClient_Solve_GeometryAndPerLegETA(t *testing.T) {
	_, o := stubOSRM(t)
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777},
			{ID: "s2", Latitude: 19.2183, Longitude: 72.9781},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
		},
	}
	out, err := o.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve failed: %v", err)
	}
	route := out.Routes[0]
	if route.Geometry != "encoded_polyline_here" {
		t.Fatalf("expected enriched geometry, got %q", route.Geometry)
	}
	if len(route.Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(route.Legs))
	}
	// Leg 1 from /route: 1500m -> 1.5km, 80s -> 1.333min
	if nearlyEqual(route.Legs[0].DistanceKM, 1.5) == false {
		t.Fatalf("leg0 distance expected 1.5km, got %v", route.Legs[0].DistanceKM)
	}
	if nearlyEqual(route.Legs[0].DurationMin, 80.0/60.0) == false {
		t.Fatalf("leg0 duration expected %.4f, got %v", 80.0/60.0, route.Legs[0].DurationMin)
	}
	// Total must reflect enriched road values (1.5 + 2.0 km), not double-count.
	if nearlyEqual(out.TotalKM, 3.5) == false {
		t.Fatalf("expected TotalKM 3.5 (road), got %v", out.TotalKM)
	}
	if nearlyEqual(route.TotalKM, 3.5) == false {
		t.Fatalf("expected route TotalKM 3.5, got %v", route.TotalKM)
	}
}

func TestOSRMClient_Solve_GracefulFallbackOnRouteError(t *testing.T) {
	mux := http.NewServeMux()
	// Only /table is served; /route returns 500 -> enrichment must degrade
	// gracefully to table-derived values without failing the solve.
	mux.HandleFunc("/table/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":      "Ok",
			"distances": [][]float64{{1000.0, 2000.0}},
			"durations": [][]float64{{60.0, 120.0}},
		})
	})
	mux.HandleFunc("/route/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	o := &OSRMClient{BaseURL: srv.URL}

	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777},
			{ID: "s2", Latitude: 19.2183, Longitude: 72.9781},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
		},
	}
	out, err := o.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve should not fail on /route error: %v", err)
	}
	// Geometry left empty, table-derived km (1.0 + 2.0).
	if out.Routes[0].Geometry != "" {
		t.Fatalf("expected empty geometry on fallback, got %q", out.Routes[0].Geometry)
	}
	if nearlyEqual(out.TotalKM, 3.0) == false {
		t.Fatalf("expected fallback TotalKM 3.0, got %v", out.TotalKM)
	}
}

func TestOSRMClient_Name(t *testing.T) {
	cases := map[string]string{
		"http://router.project-osrm.org": "osrm-public",
		"http://127.0.0.1:5000":          "osrm-selfhost",
		"":                               "osrm",
	}
	for base, want := range cases {
		o := &OSRMClient{BaseURL: base}
		if got := o.Name(); got != want {
			t.Fatalf("Name(%q) = %q, want %q", base, got, want)
		}
	}
}

func testInput() OptimizationInput {
	return OptimizationInput{
		Shipments: []Shipment{{ID: "s1", Latitude: 19.0760, Longitude: 72.8777}},
		Vehicles:  []Vehicle{{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 10}},
	}
}

// P0: an OSRM 500 used to be swallowed and answered with fabricated mock
// distances. It must surface as an error instead.
func TestOSRMClient_Solve_Table500FailsHonest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	o := &OSRMClient{BaseURL: srv.URL}
	out, err := o.Solve(context.Background(), testInput())
	if err == nil {
		t.Fatalf("expected an error on OSRM 500, got fabricated success: %+v", out)
	}
}

// P0: unreachable OSRM host must fail, not fall back to mock routes.
func TestOSRMClient_Solve_UnreachableHostFailsHonest(t *testing.T) {
	o := &OSRMClient{
		BaseURL:    "http://127.0.0.1:1",
		HTTPClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
	out, err := o.Solve(context.Background(), testInput())
	if err == nil {
		t.Fatalf("expected an error on unreachable OSRM, got fabricated success: %+v", out)
	}
}

// P0: an unparseable / non-Ok table response must fail too.
func TestOSRMClient_Solve_ParseFailureFailsHonest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"InvalidQuery"}`))
	}))
	t.Cleanup(srv.Close)

	o := &OSRMClient{BaseURL: srv.URL}
	out, err := o.Solve(context.Background(), testInput())
	if err == nil {
		t.Fatalf("expected an error on non-Ok OSRM response, got fabricated success: %+v", out)
	}
}

// A client with no base URL is a misconfiguration, not a licence to fake.
func TestOSRMClient_Solve_EmptyBaseURLFailsHonest(t *testing.T) {
	o := &OSRMClient{}
	if _, err := o.Solve(context.Background(), testInput()); err == nil {
		t.Fatal("expected an error when OSRM BaseURL is empty")
	}
}

// The mock solver stays available, but only when explicitly selected.
func TestGet_ExplicitMockStillWorks(t *testing.T) {
	out, err := Get("mock").Solve(context.Background(), testInput())
	if err != nil {
		t.Fatalf("explicit mock solve failed: %v", err)
	}
	if out.ProviderName != "mock" {
		t.Fatalf("expected provider mock, got %q", out.ProviderName)
	}
}

// Unknown provider ids must not silently degrade to fabricated mock routes.
func TestGet_UnknownProviderFailsHonest(t *testing.T) {
	if _, err := Get("typo-osrm").Solve(context.Background(), testInput()); err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
}

func nearlyEqual(a, b float64) bool {
	const eps = 1e-6
	return (a-b) < eps && (b-a) < eps
}

// Ensure our stub routes are actually reached (guards against path regressions).
func TestOSRMClient_RoutePathContainsCoords(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/table/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":      "Ok",
			"distances": [][]float64{{1000.0}},
			"durations": [][]float64{{60.0}},
		})
	})
	mux.HandleFunc("/route/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "Ok",
			"routes": []map[string]any{{
				"geometry": "x",
				"distance": 1000.0,
				"duration": 60.0,
				"legs":     []map[string]any{{"distance": 1000.0, "duration": 60.0}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	o := &OSRMClient{BaseURL: srv.URL}
	in := OptimizationInput{
		Shipments: []Shipment{{ID: "s1", Latitude: 19.0, Longitude: 72.0}},
		Vehicles:  []Vehicle{{ID: "v1", StartLat: 19.0, StartLng: 72.0, Capacity: 10}},
	}
	if _, err := o.Solve(context.Background(), in); err != nil {
		t.Fatalf("Solve failed: %v", err)
	}
	if !strings.Contains(gotPath, "/route/v1/driving/") {
		t.Fatalf("expected /route path, got %q", gotPath)
	}
}
