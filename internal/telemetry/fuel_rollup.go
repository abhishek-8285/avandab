package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	appdb "transport-app/internal/database"
	"transport-app/internal/shared"
)

// ErrTripNotFound is returned when the trip is unknown or outside the
// caller's tenant.
var ErrTripNotFound = errors.New("trip not found")

// FuelRollup is one trip's distance/speed/fuel/KMPL rollup: playback
// telemetry over fuel in (refills + claims). Drains/thefts count as
// anomalies, never as fuel in. No new tables — fuel_events and
// fuel_claim_audits already index trip_id.
type FuelRollup struct {
	TripID          string     `json:"trip_id"`
	DistanceKM      float64    `json:"distance_km"`
	DurationSeconds int64      `json:"duration_seconds"`
	DurationText    string     `json:"duration_text"`
	AvgSpeedKMH     float64    `json:"avg_speed_kmh"`
	MaxSpeedKMH     float64    `json:"max_speed_kmh"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	FuelLitres      float64    `json:"fuel_litres"`
	RefillLitres    float64    `json:"refill_litres"`
	ClaimedLitres   float64    `json:"claimed_litres"`
	KMPL            *float64   `json:"kmpl,omitempty"`
	Points          int        `json:"points"`
	RefillCount     int        `json:"refill_count"`
	ClaimCount      int        `json:"claim_count"`
	AnomalyCount    int        `json:"anomaly_count"`
}

// LoadFuelRollup assembles one trip's rollup. Tenant is taken from ctx and
// enforced on the trip row; fuel tables (no tenant column) are scoped by
// joining back to that same trip row.
func LoadFuelRollup(ctx context.Context, db *sql.DB, tripID string) (*FuelRollup, error) {
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" || tripID == "" {
		return nil, ErrTripNotFound
	}

	var started, completed sql.NullTime
	err := db.QueryRowContext(ctx,
		`SELECT started_at, completed_at FROM trips WHERE id = ? AND tenant_id = ?`,
		tripID, tenantID).Scan(&started, &completed)
	if err != nil {
		return nil, ErrTripNotFound
	}

	roll := &FuelRollup{TripID: tripID}
	if started.Valid {
		t := started.Time.UTC()
		roll.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time.UTC()
		roll.CompletedAt = &t
	}

	pts, _, _, _, err := fetchHistoryPointsWithCursor(ctx, db, tenantID, "", tripID, nil, nil, nil, "", 5000)
	if err != nil {
		return nil, err
	}
	summary, _ := computeSummary(pts)
	roll.DistanceKM = summary.DistanceKM
	roll.DurationSeconds = summary.DurationSeconds
	roll.AvgSpeedKMH = summary.AvgSpeedKMH
	roll.MaxSpeedKMH = summary.MaxSpeedKMH
	roll.Points = summary.ReturnedPoints

	anomalyQ := `SELECT COUNT(*)
		FROM fuel_events
		WHERE trip_id = ?
		  AND event_type IN ('drain_theft_suspected', 'abnormal_drain', 'siphon_confirmed', 'odometer_rollback')
		  AND EXISTS (SELECT 1 FROM trips t WHERE t.id = fuel_events.trip_id AND t.tenant_id = ?)`
	claimQ := `SELECT COALESCE(SUM(litres_claimed), 0), COUNT(*)
		FROM fuel_claim_audits
		WHERE trip_id = ?
		  AND EXISTS (SELECT 1 FROM trips t WHERE t.id = fuel_claim_audits.trip_id AND t.tenant_id = ?)`
	for _, q := range []struct {
		query string
		dest  []any
	}{
		{`SELECT COALESCE(SUM(estimated_litres), 0), COUNT(*) FROM fuel_events WHERE trip_id = ? AND event_type = 'refill_detected' AND EXISTS (SELECT 1 FROM trips t WHERE t.id = fuel_events.trip_id AND t.tenant_id = ?)`, []any{&roll.RefillLitres, &roll.RefillCount}},
		{claimQ, []any{&roll.ClaimedLitres, &roll.ClaimCount}},
		{anomalyQ, []any{&roll.AnomalyCount}},
	} {
		rebound, rerr := appdb.Rebind(q.query)
		if rerr != nil {
			return nil, rerr
		}
		if err := db.QueryRowContext(ctx, rebound, tripID, tenantID).Scan(q.dest...); err != nil {
			return nil, err
		}
	}

	roll.FuelLitres = math.Round((roll.RefillLitres+roll.ClaimedLitres)*100) / 100
	roll.DurationText = formatDuration(roll.DurationSeconds)
	if roll.FuelLitres > 0 && roll.DistanceKM > 0 {
		kmpl := math.Round(roll.DistanceKM/roll.FuelLitres*10) / 10
		roll.KMPL = &kmpl
	}
	return roll, nil
}

// formatDuration renders seconds as "2h 05m" / "45m" / "0m".
func formatDuration(sec int64) string {
	if sec < 60 {
		return "0m"
	}
	if h, m := sec/3600, (sec%3600)/60; h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", sec/60)
}
