package availability

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// SLATargetPercent is the published uptime target. Measured 30-day uptime
// below this value raises availability.sla_breach alerts.
const SLATargetPercent = 99.9

// SLAMonitor publishes an operational alert for every active tenant while the
// measured 30-day uptime is below the SLA target. Alerts are deduplicated by
// the alert engine per tenant (dedup key includes the tenant id).
type SLAMonitor struct {
	db             *sql.DB
	bus            events.EventBus
	logger         *slog.Logger
	window         time.Duration
	sampleInterval time.Duration
	checkInterval  time.Duration
}

// NewSLAMonitor builds an SLA monitor. Zero intervals fall back to sane
// defaults (15m check, 60s sample, 30-day window).
func NewSLAMonitor(db *sql.DB, bus events.EventBus, window, sampleInterval, checkInterval time.Duration, logger *slog.Logger) *SLAMonitor {
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	if sampleInterval <= 0 {
		sampleInterval = time.Minute
	}
	if checkInterval <= 0 {
		checkInterval = 15 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SLAMonitor{
		db:             db,
		bus:            bus,
		logger:         logger,
		window:         window,
		sampleInterval: sampleInterval,
		checkInterval:  checkInterval,
	}
}

// Run blocks until ctx is cancelled: one evaluation immediately, then every
// check interval.
func (m *SLAMonitor) Run(ctx context.Context) {
	if m == nil || m.db == nil || m.bus == nil {
		return
	}
	m.check(ctx)
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// check evaluates the measured window and publishes one event per active
// tenant when the target is missed. No samples yet => no alert (not measured).
func (m *SLAMonitor) check(ctx context.Context) {
	snap := LoadSnapshot(ctx, m.db, time.Now().UTC(), m.window, m.sampleInterval)
	if !snap.Measured {
		m.logger.Debug("sla monitor: uptime not measured yet, no alert")
		return
	}
	if snap.UptimePct >= SLATargetPercent {
		return
	}
	tenants, err := m.activeTenants(ctx)
	if err != nil {
		m.logger.Warn("sla monitor: tenant lookup failed", "error", err)
		return
	}
	if len(tenants) == 0 {
		m.logger.Warn("sla monitor: SLA missed but no active tenants to notify", "uptime_percent", snap.UptimePct)
		return
	}
	for _, tenantID := range tenants {
		m.publish(ctx, tenantID, snap)
	}
}

func (m *SLAMonitor) activeTenants(ctx context.Context) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id FROM tenants WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (m *SLAMonitor) publish(ctx context.Context, tenantID string, snap Snapshot) {
	title := fmt.Sprintf("30-day uptime %.2f%% is below the %.1f%% SLA target", snap.UptimePct, SLATargetPercent)
	message := fmt.Sprintf(
		"Measured platform uptime over the last %d days is %.2f%% (%d healthy of %d expected samples); target is %.1f%%. See /status for live checks.",
		int(m.window.Hours()/24), snap.UptimePct, snap.Healthy, snap.Expected, SLATargetPercent,
	)
	payload := map[string]interface{}{
		"tenant_id":        tenantID,
		"source":           "availability",
		"alert_type":       "sla_breach",
		"title":            title,
		"message":          message,
		"uptime_percent":   snap.UptimePct,
		"target_percent":   SLATargetPercent,
		"samples":          snap.Samples,
		"expected_samples": snap.Expected,
		"window_days":      int(m.window.Hours() / 24),
	}
	tctx := shared.ContextWithTenantID(ctx, shared.TenantID(tenantID))
	if err := m.bus.Publish(tctx, events.Event{Type: "availability.sla_breach", Payload: payload}); err != nil {
		m.logger.Warn("sla monitor: failed to publish SLA breach alert", "tenant", tenantID, "error", err)
	}
}
