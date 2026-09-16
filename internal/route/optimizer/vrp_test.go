package optimizer

import (
	"context"
	"testing"
)

// TestVRPMinCost_HonorsCapacity proves the solver never overloads a vehicle
// (ratchet law / CTO doc 03 §8: prove the solver honours a binding constraint).
func TestVRPMinCost_HonorsCapacity(t *testing.T) {
	v := &VRPMinCost{}
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777, Demand: 60},
			{ID: "s2", Latitude: 19.2183, Longitude: 72.9781, Demand: 60},
			{ID: "s3", Latitude: 19.0600, Longitude: 72.8300, Demand: 60},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
		},
	}
	out, err := v.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve error: %v", err)
	}

	// Capacity 100 < 3 x 60 => at most 2 shipments can be routed on the single
	// vehicle. The third must be left unassigned, never overloaded.
	if len(out.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(out.Routes))
	}
	if len(out.Routes[0].Legs) > 2 {
		t.Fatalf("capacity violated: %d shipments on a 100-cap vehicle, each demand 60", len(out.Routes[0].Legs))
	}
}

// TestVRPMinCost_HonorsTimeWindow proves a shipment whose delivery window closes
// too early for the travel time is not placed on that vehicle (or is left
// unassigned rather than violating its window).
func TestVRPMinCost_HonorsTimeWindow(t *testing.T) {
	v := &VRPMinCost{}
	// Far shipment with an immediate end window: unreachable within its window
	// from the depot -> must not be assigned late (stays unassigned).
	earlyEnd := "2026-09-16T00:05:00Z"
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777, TimeWindowEnd: &earlyEnd},
			{ID: "s2", Latitude: 28.6139, Longitude: 77.2090, TimeWindowEnd: &earlyEnd}, // ~1400 km away
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
		},
	}
	out, err := v.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve error: %v", err)
	}
	assigned := map[string]bool{}
	for _, l := range out.Routes[0].Legs {
		assigned[l.ShipmentID] = true
	}
	// s2 (Delhi) is ~1400 km away; at 40 km/h that's ~2100 minutes, far past the
	// 5-minute window -> it must NOT be assigned.
	if assigned["s2"] {
		t.Fatalf("time window violated: far shipment assigned despite 5-min closing window")
	}
}

// TestVRPMinCost_Deterministic proves identical input yields identical output
// (interface contract), including stable ordering after 2-opt.
func TestVRPMinCost_Deterministic(t *testing.T) {
	v := &VRPMinCost{}
	build := func() OptimizationInput {
		return OptimizationInput{
			Shipments: []Shipment{
				{ID: "s1", Latitude: 19.0760, Longitude: 72.8777, Demand: 10},
				{ID: "s2", Latitude: 19.2183, Longitude: 72.9781, Demand: 10},
				{ID: "s3", Latitude: 19.0600, Longitude: 72.8300, Demand: 10},
				{ID: "s4", Latitude: 19.1800, Longitude: 72.9000, Demand: 10},
			},
			Vehicles: []Vehicle{
				{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
			},
		}
	}
	out1, err1 := v.Solve(context.Background(), build())
	out2, err2 := v.Solve(context.Background(), build())
	if err1 != nil || err2 != nil {
		t.Fatalf("solve errors: %v / %v", err1, err2)
	}
	if len(out1.Routes) != len(out2.Routes) {
		t.Fatalf("nondeterministic route count")
	}
	for i := range out1.Routes {
		if len(out1.Routes[i].Legs) != len(out2.Routes[i].Legs) {
			t.Fatalf("nondeterministic leg count on route %d", i)
		}
		for j := range out1.Routes[i].Legs {
			if out1.Routes[i].Legs[j].ShipmentID != out2.Routes[i].Legs[j].ShipmentID {
				t.Fatalf("nondeterministic order: route %d leg %d %s vs %s",
					i, j, out1.Routes[i].Legs[j].ShipmentID, out2.Routes[i].Legs[j].ShipmentID)
			}
		}
	}
}

// TestVRPMinCost_MaxRouteDuration proves the driver-hours (max route duration)
// constraint is enforced.
func TestVRPMinCost_MaxRouteDuration(t *testing.T) {
	v := &VRPMinCost{}
	// Three far-apart shipments and a driver-hours cap of 120 minutes force
	// unassigned shipments rather than exceeding the cap.
	maxDur := 120
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777},
			{ID: "s2", Latitude: 28.6139, Longitude: 77.2090}, // far
			{ID: "s3", Latitude: 26.9124, Longitude: 75.7873}, // far
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 1000},
		},
		Constraints: Constraints{MaxRouteDurationMin: &maxDur},
	}
	out, err := v.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve error: %v", err)
	}
	// Total route duration must not exceed the cap.
	if out.Routes[0].TotalMin > float64(maxDur)+0.001 {
		t.Fatalf("max route duration violated: TotalMin=%v cap=%d", out.Routes[0].TotalMin, maxDur)
	}
}
