package pipeline

import (
	"context"
	"testing"

	sqliterepo "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// ingestOne runs one alert through the pipeline and returns the stored row.
func ingestOne(t *testing.T, e *Engine, dedupKey string, payload map[string]any) *storedAlertRow {
	t.Helper()
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	if err := e.ProcessEvent(ctx, events.Event{Type: "AlertEvent", Payload: payload}); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	alert, err := e.repo.FindOpenByDedupKey(ctx, dedupKey)
	if err != nil {
		t.Fatalf("FindOpenByDedupKey: %v", err)
	}
	if alert == nil {
		t.Fatalf("alert %q not stored", dedupKey)
	}
	return &storedAlertRow{alert.SeverityRank, alert.MoneyAtRisk, alert.TenantID}
}

type storedAlertRow struct {
	SeverityRank int
	MoneyAtRisk  float64
	TenantID     string
}

// TestSeverityRankAssignment — Spec 22 §7 S1: rank assignment is
// table-driven over severity defaults and explicit payload overrides.
func TestSeverityRankAssignment(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	cases := []struct {
		name     string
		severity string
		wantRank int
	}{
		{"blocker maps critical", "blocker", 1},
		{"critical maps urgent", "critical", 2},
		{"warning maps waste", "warning", 4},
		{"info maps info", "info", 5},
		{"unknown severity falls back to info", "weird", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := ingestOne(t, engine, "telemetry:speeding:veh-rank-"+tc.severity, map[string]any{
				"source":     "telemetry",
				"alert_type": "speeding",
				"severity":   tc.severity,
				"vehicle_id": "veh-rank-" + tc.severity,
			})
			if row.SeverityRank != tc.wantRank {
				t.Fatalf("severity %q: rank = %d, want %d", tc.severity, row.SeverityRank, tc.wantRank)
			}
		})
	}

	override := ingestOne(t, engine, "telemetry:ewaybill_expiring:veh-rank-override", map[string]any{
		"source":        "telemetry",
		"alert_type":    "ewaybill_expiring",
		"severity":      "info",
		"severity_rank": float64(1),
		"money_at_risk": float64(4500),
		"vehicle_id":    "veh-rank-override",
	})
	if override.SeverityRank != 1 {
		t.Fatalf("explicit rank = %d, want 1", override.SeverityRank)
	}
	if override.MoneyAtRisk != 4500 {
		t.Fatalf("money_at_risk = %f, want 4500", override.MoneyAtRisk)
	}

	bad := ingestOne(t, engine, "telemetry:night_driving:veh-rank-bad", map[string]any{
		"source":        "telemetry",
		"alert_type":    "night_driving",
		"severity":      "warning",
		"severity_rank": float64(9),
		"vehicle_id":    "veh-rank-bad",
	})
	if bad.SeverityRank != 4 {
		t.Fatalf("out-of-range rank fell back wrong: %d, want 4", bad.SeverityRank)
	}
}

// TestIngestTenantFromContext — tenant comes from publisher context when
// present; unattributable events are skipped instead of filed under the
// bootstrap tenant (fail-closed: alerts:read spans every org).
func TestIngestTenantFromContext(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	payload := map[string]any{
		"source":     "telemetry",
		"alert_type": "speeding",
		"severity":   "warning",
		"vehicle_id": "veh-tenant-ctx",
	}
	if err := engine.ProcessEvent(ctx, events.Event{Type: "AlertEvent", Payload: payload}); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	stored, err := repo.FindOpenByDedupKey(context.Background(), "telemetry:speeding:veh-tenant-ctx")
	if err != nil || stored == nil {
		t.Fatalf("alert not found: %v", err)
	}
	if stored.TenantID != "tenant-42" {
		t.Fatalf("tenant from ctx = %q, want tenant-42", stored.TenantID)
	}

	// No tenant in ctx → skipped, never filed under the bootstrap tenant.
	if err := engine.ProcessEvent(context.Background(), events.Event{Type: "AlertEvent", Payload: map[string]any{
		"source":     "telemetry",
		"alert_type": "speeding",
		"severity":   "warning",
		"vehicle_id": "veh-tenant-default",
	}}); err != nil {
		t.Fatalf("ProcessEvent default: %v", err)
	}
	def, err := repo.FindOpenByDedupKey(context.Background(), "telemetry:speeding:veh-tenant-default")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if def != nil {
		t.Fatalf("unattributable alert stored under tenant %q, want skipped", def.TenantID)
	}
}
