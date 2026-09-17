package optimizer

import (
	"context"
	"testing"
)

// TestVRPMinCost_ConsolidatesWhenCapacityNotBinding is the red-proven ratchet
// for the greedy construction cost: a fleet with slack capacity must produce
// a minimal-vehicle plan (all stops on the cheapest feasible route), not
// round-robin stops across idle vehicles. Failed pre-fix with 2 routes for
// 3 stops / 2 vehicles.
func TestVRPMinCost_ConsolidatesWhenCapacityNotBinding(t *testing.T) {
	v := &VRPMinCost{}
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 18.40, Longitude: 73.50},
			{ID: "s2", Latitude: 18.55, Longitude: 73.80},
			{ID: "s3", Latitude: 18.70, Longitude: 74.10},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 18.5204, StartLng: 73.8567, Capacity: 50},
			{ID: "v2", StartLat: 18.5204, StartLng: 73.8567, Capacity: 51},
		},
	}
	out, err := v.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve error: %v", err)
	}
	used := 0
	assigned := 0
	for _, r := range out.Routes {
		if len(r.Legs) > 0 {
			used++
			assigned += len(r.Legs)
		}
	}
	if assigned != 3 {
		t.Fatalf("all 3 shipments must be assigned, got %d", assigned)
	}
	if used != 1 {
		t.Fatalf("capacity-not-binding case must consolidate to 1 route, got %d", used)
	}
}
