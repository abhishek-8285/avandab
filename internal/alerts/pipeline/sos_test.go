package pipeline

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/events"
)

type mockChannel struct {
	name string
	sent []channels.Message
}

func (m *mockChannel) Name() string { return m.name }
func (m *mockChannel) Send(ctx context.Context, msg channels.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func setupSOSTestDB(t *testing.T) (*sql.DB, *Engine, *mockChannel, *mockChannel, *fakeClock) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE alert_rules (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			alert_type TEXT NOT NULL,
			name TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'warning',
			threshold REAL,
			threshold_unit TEXT,
			dedup_key_expr TEXT NOT NULL,
			cooldown_seconds INTEGER NOT NULL DEFAULT 300,
			storm_window_seconds INTEGER NOT NULL DEFAULT 120,
			storm_batch_min INTEGER NOT NULL DEFAULT 5,
			channel_routing TEXT NOT NULL DEFAULT '{}',
			escalation_schedule TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE alert_rule_overrides (
			id TEXT PRIMARY KEY,
			rule_id TEXT NOT NULL,
			entity_id TEXT,
			severity TEXT,
			threshold REAL,
			cooldown_seconds INTEGER,
			channels TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE alerts (
			id TEXT PRIMARY KEY,
			rule_id TEXT,
			source TEXT NOT NULL,
			alert_type TEXT NOT NULL,
			severity TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			dedup_key TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT '1',
			ack_status TEXT NOT NULL DEFAULT 'open',
			severity_rank INTEGER NOT NULL DEFAULT 5,
			money_at_risk REAL NOT NULL DEFAULT 0,
			snoozed_until DATETIME,
			entity_type TEXT,
			entity_id TEXT,
			user_id TEXT,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			occurrences INTEGER NOT NULL DEFAULT 1,
			first_seen_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL,
			next_escalation_at DATETIME,
			escalation_step INTEGER NOT NULL DEFAULT 0,
			latitude REAL,
			longitude REAL,
			metadata TEXT NOT NULL DEFAULT '{}',
			acked_by TEXT,
			acked_at DATETIME,
			resolved_by TEXT,
			resolved_at DATETIME,
			resolution_note TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE notifications (
			id TEXT PRIMARY KEY,
			alert_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			recipient TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			retry_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			sent_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	repo := alertsqlite.NewAlertRepository(db)
	inApp := &mockChannel{name: "in_app"}
	telegram := &mockChannel{name: "telegram"}
	chMap := map[string]channels.Provider{
		"in_app":   inApp,
		"telegram": telegram,
	}

	engine := NewEngine(repo, chMap, nil)
	clk := &fakeClock{now: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)

	return db, engine, inApp, telegram, clk
}

func TestSOS_TriggerFanOutAndDedup(t *testing.T) {
	db, engine, inApp, telegram, clk := setupSOSTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 1. Send SOSEvent
	sosEv := events.Event{
		Type: "SOSEvent",
		Payload: map[string]interface{}{
			"VehicleID":  "veh-sos-99",
			"DriverID":   "drv-sos-11",
			"Latitude":   19.0760,
			"Longitude":  72.8777,
			"OccurredAt": clk.Now().Format(time.RFC3339),
		},
	}

	err := engine.ProcessEvent(ctx, sosEv)
	require.NoError(t, err)

	// Verify alert in canonical alerts table
	var id, source, alertType, severity, status, title, msg string
	var occurrences int
	var nextEsc time.Time
	err = db.QueryRow(`
		SELECT id, source, alert_type, severity, status, title, message, occurrences, next_escalation_at
		FROM alerts WHERE dedup_key = 'sos:veh-sos-99'`).Scan(
		&id, &source, &alertType, &severity, &status, &title, &msg, &occurrences, &nextEsc)
	require.NoError(t, err)

	assert.Equal(t, "sos", source)
	assert.Equal(t, "sos", alertType)
	assert.Equal(t, domain.SeverityBlocker, severity)
	assert.Equal(t, "open", status)
	assert.Contains(t, title, "SOS")
	assert.Contains(t, msg, "drv-sos-11")
	assert.Equal(t, 1, occurrences)
	assert.Equal(t, clk.Now().Add(10*time.Minute), nextEsc)

	// Verify fan-out
	assert.Len(t, inApp.sent, 1)
	assert.Len(t, telegram.sent, 1)
	assert.Equal(t, domain.SeverityBlocker, inApp.sent[0].Severity)
	assert.Equal(t, domain.SeverityBlocker, telegram.sent[0].Severity)

	// 2. Dedup cooldown test: repeat SOS within 60s
	clk.now = clk.now.Add(30 * time.Second)
	err = engine.ProcessEvent(ctx, sosEv)
	require.NoError(t, err)

	err = db.QueryRow(`SELECT occurrences FROM alerts WHERE id = ?`, id).Scan(&occurrences)
	require.NoError(t, err)
	assert.Equal(t, 2, occurrences)

	// No extra channel sends during cooldown
	assert.Len(t, inApp.sent, 1)
	assert.Len(t, telegram.sent, 1)
}
