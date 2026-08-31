package handlers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestPhase12TemplatesRender verifies the four new Phase 12 templates render
// without error using representative data (Spec 16 §8).
func TestPhase12TemplatesRender(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	tmpl, err := parseTemplates(&mockAuthSvc{})
	require.NoError(t, err)

	user := &auth.SessionData{UserID: "admin-1", Role: "admin"}
	thr := 100000.0
	now := time.Now()

	samples := map[string]interface{}{
		"UnacknowledgedSignals": 2,
		"ActiveExperiments":     1,
		"OpenOpsAlerts":         3,
		"RecentSignals": []service.FounderSignal{
			{ID: "fs1", TenantID: string(shared.DefaultTenant), SignalType: service.SignalRevenueMilestone, SignalValue: 150000, ThresholdValue: &thr, Direction: service.DirectionAbove},
			{ID: "fs2", TenantID: string(shared.DefaultTenant), SignalType: service.SignalCashFlowAlert, SignalValue: -500, Direction: service.DirectionBelow},
		},
		"LatestPNL": service.PNLSnapshot{
			SnapshotDate: "2026-08-20", Revenue: 1000, Expenses: 800,
			FuelCosts: 100, DriverPayouts: 600, NetProfit: 200, TripCount: 5,
		},
		"Alerts": []service.OpsAlert{
			{ID: "a1", TenantID: string(shared.DefaultTenant), AlertType: service.OpsAlertSettlementDispute, Severity: service.OpsAlertSeverityHigh,
				Title: "Dispute", Status: service.OpsAlertStatusOpen, CreatedAt: now},
		},
		"Snapshots": []service.PNLSnapshot{
			{SnapshotDate: "2026-08-20", Revenue: 1000, Expenses: 800, FuelCosts: 100, DriverPayouts: 600, NetProfit: 200, TripCount: 5},
		},
		"From": "2026-07-20",
		"To":   "2026-08-20",
		"Experiments": []service.Experiment{
			{ID: "e1", TenantID: string(shared.DefaultTenant), Name: "exp1", Status: service.ExperimentStatusRunning, TrafficSplit: 50, MetricName: "conv", CreatedAt: now},
		},
		"User": user,
	}

	cases := []string{
		"founder_dashboard.html",
		"ops_alerts_list.html",
		"pnl_dashboard.html",
		"experiments_list.html",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			tpl := tmpl.Lookup(name)
			require.NotNil(t, tpl, "template %s not found", name)
			data := map[string]interface{}{"User": user}
			for k, v := range samples {
				data[k] = v
			}
			var buf bytes.Buffer
			err := tpl.Execute(&buf, data)
			require.NoError(t, err, "template %s failed to render", name)
			assert.NotEmpty(t, buf.String())
		})
	}
}
