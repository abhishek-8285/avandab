package optimizer

// HaversineKM returns great-circle distance in kilometres between two
// coordinates. Exported so application services (dispatch tuner what-if KPI)
// compute distances identically to the solvers.
func HaversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	return haversine(lat1, lon1, lat2, lon2)
}

// DefaultSpeedKMH is the assumed average speed (km/h) for duration estimates
// when no routing provider supplies per-edge durations. Matches the constant
// used by MockOptimizer and VRPMinCost.
const DefaultSpeedKMH = 40.0
