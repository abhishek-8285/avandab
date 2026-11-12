package optimizer

import (
	"context"
	"testing"
)

func TestMockOptimizer_Solve(t *testing.T) {
	m := &MockOptimizer{}
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "s1", Latitude: 19.0760, Longitude: 72.8777},
			{ID: "s2", Latitude: 19.2183, Longitude: 72.9781},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 100},
		},
	}
	out, err := m.Solve(context.Background(), in)
	if err != nil {
		t.Fatalf("Solve failed: %v", err)
	}
	if len(out.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(out.Routes))
	}
	if len(out.Routes[0].Legs) != 2 {
		t.Fatalf("expected 2 legs, got %d", len(out.Routes[0].Legs))
	}
	if out.ProviderName != "mock" {
		t.Fatalf("expected mock provider, got %s", out.ProviderName)
	}
	if out.TotalKM <= 0 {
		t.Fatalf("expected positive totalKM")
	}
}

func TestMockOptimizer_Validate(t *testing.T) {
	m := &MockOptimizer{}
	_, err := m.Solve(context.Background(), OptimizationInput{})
	if err != ErrNoShipments {
		t.Fatalf("expected ErrNoShipments, got %v", err)
	}
	in := OptimizationInput{
		Shipments: make([]Shipment, 51),
		Vehicles:  []Vehicle{{ID: "v1", StartLat: 0, StartLng: 0}},
	}
	for i := range in.Shipments {
		in.Shipments[i] = Shipment{ID: "s", Latitude: 1, Longitude: 1}
	}
	_, err = m.Solve(context.Background(), in)
	if err != ErrTooManyShipments {
		t.Fatalf("expected ErrTooManyShipments, got %v", err)
	}
}

func TestRegistry_Get(t *testing.T) {
	if Get("mock").Name() != "mock" {
		t.Fatalf("mock failed")
	}
	// Unknown provider fails loud on solve
	in := OptimizationInput{
		Shipments: []Shipment{{ID: "s1", Latitude: 19.0760, Longitude: 72.8777}},
		Vehicles:  []Vehicle{{ID: "v1", StartLat: 19.0760, StartLng: 72.8777, Capacity: 10}},
	}
	if _, err := Get("unknown-xyz").Solve(context.Background(), in); err == nil {
		t.Fatalf("unknown should fail on Solve")
	}
	// OSRM variants return osrm-* but still implement Optimizer
	if Get("osrm-public").Name() == "" {
		t.Fatalf("osrm-public name empty")
	}
}

func TestMockOptimizer_Deterministic(t *testing.T) {
	m := &MockOptimizer{}
	in := OptimizationInput{
		Shipments: []Shipment{
			{ID: "a", Latitude: 19.0, Longitude: 72.0},
			{ID: "b", Latitude: 19.1, Longitude: 72.1},
		},
		Vehicles: []Vehicle{
			{ID: "v1", StartLat: 19.0, StartLng: 72.0},
			{ID: "v2", StartLat: 19.2, StartLng: 72.2},
		},
	}
	out1, _ := m.Solve(context.Background(), in)
	out2, _ := m.Solve(context.Background(), in)
	if out1.TotalKM != out2.TotalKM {
		t.Fatalf("not deterministic")
	}
}
