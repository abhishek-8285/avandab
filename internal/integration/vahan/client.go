// Package vahan provides RC/DL registry lookups against the Indian Vahan
// /Sarathi data (Spec 22 §11 open item). Provider selection is
// config-driven; the mock ships as default so no licence or network is
// required to build, test, or demo. Radar enrichment works without it.
package vahan

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Registration is the subset of the RC the radar and UI consume.
type Registration struct {
	VehicleNumber string
	OwnerName     string
	Model         string
	Class         string // LMV, HGV…
	FitnessUpto   time.Time
	InsuranceUpto time.Time
	PucUpto       time.Time
	Fuel          string
}

// Client fetches registration data by vehicle number.
type Client interface {
	FetchRC(ctx context.Context, vehicleNumber string) (*Registration, error)
}

// NewClient returns the provider for cfg ("mock" default). A real
// aggregator client lands once licensing is decided — never call
// external services unless explicitly configured.
func NewClient(provider string) Client {
	_ = provider
	// Only the mock exists today — a real aggregator client lands once
	// licensing is decided (§11). Unknown providers degrade to mock.
	return NewMock()
}

// MockClient returns deterministic sample data so flows can be built
// end-to-end before a paid provider is chosen.
type MockClient struct{}

func NewMock() *MockClient { return &MockClient{} }

func (m *MockClient) FetchRC(ctx context.Context, vehicleNumber string) (*Registration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := strings.ToUpper(strings.TrimSpace(vehicleNumber))
	if n == "" {
		return nil, fmt.Errorf("vehicle number required")
	}
	return &Registration{
		VehicleNumber: n,
		OwnerName:     "Mock Owner",
		Model:         "Tata Signa 4225",
		Class:         "HGV",
		FitnessUpto:   time.Now().AddDate(1, 0, 0),
		InsuranceUpto: time.Now().AddDate(0, 6, 0),
		PucUpto:       time.Now().AddDate(0, 5, 0),
		Fuel:          "Diesel",
	}, nil
}
