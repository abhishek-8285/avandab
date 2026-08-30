package ewaybill

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
)

// Monitor periodically scans active E-Way Bills approaching expiration.
type Monitor struct {
	svc      *EWayBillService
	interval time.Duration
	leadSec  int
	logger   *slog.Logger
	bus      events.EventBus
}

// NewMonitor creates a new E-Way Bill expiry monitor.
func NewMonitor(svc *EWayBillService, cfg Config) *Monitor {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	leadSec := cfg.ExtensionLeadSeconds
	if leadSec <= 0 {
		leadSec = 28800 // 8 hours per GST rule
	}
	return &Monitor{
		svc:      svc,
		interval: interval,
		leadSec:  leadSec,
		logger:   svc.logger,
		bus:      svc.bus,
	}
}

// Run starts the monitor loop until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Tick(ctx)
		}
	}
}

// Tick performs a single scan pass.
func (m *Monitor) Tick(ctx context.Context) {
	// 1. Process already expired EWBs (valid_until <= now)
	m.processExpiredEWBs(ctx)

	// 2. Process expiring soon EWBs (valid_until <= now + leadSec)
	m.processExpiringSoonEWBs(ctx)
}

func (m *Monitor) processExpiredEWBs(ctx context.Context) {
	query := `
		SELECT e.ewb_number, e.trip_id, COALESCE(t.tenant_id, '1')
		FROM eway_bills e
		LEFT JOIN trips t ON e.trip_id = t.id
		WHERE e.status IN ('active', 'part_a')
		  AND e.valid_until <= datetime('now')
		LIMIT 50
	`
	rows, err := m.svc.db.QueryContext(ctx, query)
	if err != nil {
		m.logger.Warn("ewaybill expired scan failed", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	type expiredItem struct {
		ewbNumber string
		tripID    string
		tenantID  string
	}
	var expired []expiredItem
	for rows.Next() {
		var it expiredItem
		var tripID sql.NullString
		if err := rows.Scan(&it.ewbNumber, &tripID, &it.tenantID); err == nil {
			it.tripID = tripID.String
			expired = append(expired, it)
		}
	}

	for _, it := range expired {
		_, err := m.svc.db.ExecContext(ctx, `
			UPDATE eway_bills
			SET status = 'expired', updated_at = datetime('now')
			WHERE ewb_number = ? AND status IN ('active', 'part_a')
		`, it.ewbNumber)
		if err == nil {
			eventID := uuid.NewString()
			_, _ = m.svc.db.ExecContext(ctx, `
				INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by, created_at)
				VALUES (?, ?, ?, 'EXPIRED', '{"reason":"validity_expired"}', 'system', datetime('now'))
			`, eventID, it.ewbNumber, it.tripID)

			if m.bus != nil {
				m.bus.Publish(ctx, events.Event{
					Type: "AlertEvent",
					Payload: map[string]interface{}{
						"source":      "ewaybill",
						"alert_type":  "ewb_expired",
						"severity":    "critical",
						"title":       "E-Way Bill Expired",
						"details":     fmt.Sprintf("E-Way Bill #%s for trip %s has expired.", it.ewbNumber, it.tripID),
						"trip_id":     it.tripID,
						"tenant_id":   it.tenantID,
						"occurred_at": time.Now().UTC(),
					},
				})
			}
		}
	}
}

func (m *Monitor) processExpiringSoonEWBs(ctx context.Context) {
	query := fmt.Sprintf(`
		SELECT e.ewb_number, e.trip_id, t.status, COALESCE(t.tenant_id, '1')
		FROM eway_bills e
		LEFT JOIN trips t ON e.trip_id = t.id
		WHERE e.status IN ('active', 'part_a')
		  AND e.valid_until > datetime('now')
		  AND e.valid_until <= datetime('now', '+%d seconds')
		LIMIT 50
	`, m.leadSec)

	rows, err := m.svc.db.QueryContext(ctx, query)
	if err != nil {
		m.logger.Warn("ewaybill monitor query failed", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	type expiringItem struct {
		ewbNumber  string
		tripID     string
		tripStatus string
		tenantID   string
	}
	var items []expiringItem
	for rows.Next() {
		var it expiringItem
		var tripID, tripStatus sql.NullString
		if err := rows.Scan(&it.ewbNumber, &tripID, &tripStatus, &it.tenantID); err == nil {
			it.tripID = tripID.String
			it.tripStatus = tripStatus.String
			items = append(items, it)
		}
	}

	for _, it := range items {
		// Alert central alert engine if expiring soon
		if m.bus != nil {
			m.bus.Publish(ctx, events.Event{
				Type: "AlertEvent",
				Payload: map[string]interface{}{
					"source":      "ewaybill",
					"alert_type":  "ewb_expiring_soon",
					"severity":    "warning",
					"title":       "E-Way Bill Expiring Soon",
					"details":     fmt.Sprintf("E-Way Bill #%s is nearing expiration within 8 hours.", it.ewbNumber),
					"trip_id":     it.tripID,
					"tenant_id":   it.tenantID,
					"occurred_at": time.Now().UTC(),
				},
			})
		}

		switch it.tripStatus {
		case "in_transit", "started", "reached_pickup":
			// Try auto-extension if supported by geofence evidence
			_, err := m.svc.Extend(ctx, it.ewbNumber, ExtendRequest{
				EwbNumber: it.ewbNumber,
				Reason:    "auto_expiry_monitor_extension",
			})
			if err != nil {
				m.logger.Debug("auto extend skipped/denied", "ewb", it.ewbNumber, "reason", err)
			}
		case "completed", "cancelled", "delivered":
			_, _ = m.svc.Cancel(ctx, it.ewbNumber, "trip_completed_or_cancelled")
		}
	}
}
