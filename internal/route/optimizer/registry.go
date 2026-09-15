package optimizer

import (
	"os"
	"strings"
)

// Get returns the optimizer for provider name.
// Supported: "mock", "osrm", "osrm-public", "osrm-selfhost"
// Empty or unknown falls back to mock.
func Get(provider string) Optimizer {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "mock", "":
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
		return &MockOptimizer{}
	}
}

// Available lists provider ids.
func Available() []string {
	return []string{"mock", "vrp", "osrm-public", "osrm-selfhost"}
}
