package optimizer

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// unknownOptimizer fails loud when an unknown provider is invoked.
type unknownOptimizer struct {
	name string
}

func (u *unknownOptimizer) Name() string { return u.name }

func (u *unknownOptimizer) Solve(ctx context.Context, in OptimizationInput) (OptimizationOutput, error) {
	return OptimizationOutput{}, fmt.Errorf("route optimizer provider %q is unknown or unconfigured", u.name)
}

// Get returns the optimizer for provider name.
// Supported: "mock", "vrp", "osrm", "osrm-public", "osrm-selfhost", or an explicit http(s) URL.
// Unknown or empty provider returns an unknownOptimizer that fails loud on Solve.
func Get(provider string) Optimizer {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "mock":
		return &MockOptimizer{}
	case "vrp", "vrp-min-cost":
		return &VRPMinCost{}
	case "osrm", "osrm-public":
		return &OSRMClient{BaseURL: "http://router.project-osrm.org"}
	case "osrm-selfhost":
		url := os.Getenv("OSRM_URL")
		if url == "" {
			url = os.Getenv("ROUTING_OSRM_URL")
		}
		if url == "" {
			url = "http://osrm.internal:5000"
		}
		return &OSRMClient{BaseURL: url}
	default:
		// If provider looks like a URL, treat as custom OSRM
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			return &OSRMClient{BaseURL: provider}
		}
		return &unknownOptimizer{name: provider}
	}
}

// Available lists provider ids.
func Available() []string {
	return []string{"mock", "vrp", "osrm-public", "osrm-selfhost"}
}
