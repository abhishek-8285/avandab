package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	sqliterepo "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

type mockProvider struct {
	name     string
	mu       sync.Mutex
	messages []channels.Message
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{name: name}
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Send(ctx context.Context, msg channels.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockProvider) Messages() []channels.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]channels.Message, len(m.messages))
	copy(copied, m.messages)
	return copied
}

func TestEscalation_MultiStepSchedule(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)

	inAppMock := newMockProvider("in_app")
	telegramMock := newMockProvider("telegram")
	providerMap := map[string]channels.Provider{
		"in_app":   inAppMock,
		"telegram": telegramMock,
	}

	engine := NewEngine(repo, providerMap, nil)
	escalator := NewEscalator(repo, providerMap, nil)

	startTime := time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)
	clk := &mockClock{current: startTime}
	engine.SetClock(clk)
	escalator.SetClock(clk)

	// Update rule_temp_breach with 2-step escalation schedule
	_, err := db.Exec(`
		UPDATE alert_rules
		SET escalation_schedule = '[{"after_seconds":60,"target_role":"dispatcher","channel":"in_app"},{"after_seconds":120,"target_role":"admin","channel":"telegram"}]'
		WHERE id = 'rule_temp_breach'
	`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "temp_breach",
			"severity":   "critical",
			"vehicle_id": "v-reefer-1",
			"title":      "Temp Breach +32C",
			"details":    "Reefer temperature spiked to +32C",
		},
	}

	// 1. Ingest initial alert
	err = engine.ProcessEvent(ctx, ev)
	require.NoError(t, err)

	// Initial message sent on creation
	require.Len(t, inAppMock.Messages(), 1)
	require.Len(t, telegramMock.Messages(), 1)

	alert, err := repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-1")
	require.NoError(t, err)
	require.NotNil(t, alert)
	require.NotNil(t, alert.NextEscalationAt)
	assert.Equal(t, startTime.Add(60*time.Second), *alert.NextEscalationAt)
	assert.Equal(t, 0, alert.EscalationStep)

	// 2. Advance time to 30s (no escalation yet)
	clk.Advance(30 * time.Second)
	err = escalator.Tick(ctx)
	require.NoError(t, err)
	assert.Len(t, inAppMock.Messages(), 1, "no escalation before after_seconds")

	// 3. Advance time past 60s (step 1 fires)
	clk.Advance(35 * time.Second) // total 65s
	err = escalator.Tick(ctx)
	require.NoError(t, err)

	// Step 1 sends to in_app
	require.Len(t, inAppMock.Messages(), 2)
	assert.Contains(t, inAppMock.Messages()[1].Title, "[Escalation Step 1]")

	// Verify alert updated for step 2
	alert, err = repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-1")
	require.NoError(t, err)
	assert.Equal(t, 1, alert.EscalationStep)
	assert.Equal(t, domain.StatusEscalated, alert.Status)
	require.NotNil(t, alert.NextEscalationAt)
	assert.Equal(t, clk.Now().Add(120*time.Second), *alert.NextEscalationAt)

	// 4. Advance time past next 120s (step 2 fires)
	clk.Advance(125 * time.Second)
	err = escalator.Tick(ctx)
	require.NoError(t, err)

	// Step 2 sends to telegram
	require.Len(t, telegramMock.Messages(), 2)
	assert.Contains(t, telegramMock.Messages()[1].Title, "[Escalation Step 2]")

	// Verify alert has no further escalation
	alert, err = repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-1")
	require.NoError(t, err)
	assert.Equal(t, 2, alert.EscalationStep)
	assert.Nil(t, alert.NextEscalationAt, "no further steps remaining")
}

func TestFlusher_StormBatchConsolidation(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)

	mockCh := newMockProvider("in_app")
	providerMap := map[string]channels.Provider{"in_app": mockCh}

	engine := NewEngine(repo, providerMap, nil)
	flusher := NewFlusher(repo, providerMap, nil)

	startTime := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	clk := &mockClock{current: startTime}
	engine.SetClock(clk)
	flusher.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "speeding",
			"severity":   "warning",
			"vehicle_id": "v-storm-flusher",
			"title":      "Speeding 92 km/h",
		},
	}

	// 5 rapid events in storm window
	for i := 0; i < 5; i++ {
		err := engine.ProcessEvent(ctx, ev)
		require.NoError(t, err)
		clk.Advance(5 * time.Second)
	}

	// Initial message sent on creation
	assert.Len(t, mockCh.Messages(), 1)

	// Advance clock past storm window (60s)
	clk.Advance(60 * time.Second)

	// Run flusher
	err := flusher.Flush(ctx)
	require.NoError(t, err)

	// Flusher emitted consolidated batch notification
	messages := mockCh.Messages()
	require.Len(t, messages, 2)
	assert.Contains(t, messages[1].Title, "⚠️ Storm:")
	assert.Contains(t, messages[1].Body, "occurred 5 times")
}

type failingProvider struct{ name string }

func (f *failingProvider) Name() string { return f.name }
func (f *failingProvider) Send(ctx context.Context, msg channels.Message) error {
	return errors.New("channel down")
}

// P0: a channel that cannot deliver must surface, not be swallowed as if the
// storm batch was delivered.
func TestFlusher_PropagatesDeliveryFailure(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)

	mockCh := newMockProvider("in_app")
	providerMap := map[string]channels.Provider{"in_app": mockCh, "whatsapp": &failingProvider{name: "whatsapp"}}

	engine := NewEngine(repo, map[string]channels.Provider{"in_app": mockCh}, nil)
	flusher := NewFlusher(repo, providerMap, nil)

	startTime := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	clk := &mockClock{current: startTime}
	engine.SetClock(clk)
	flusher.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "speeding",
			"severity":   "warning",
			"vehicle_id": "v-storm-failing",
			"title":      "Speeding 92 km/h",
		},
	}
	for i := 0; i < 5; i++ {
		require.NoError(t, engine.ProcessEvent(ctx, ev))
		clk.Advance(5 * time.Second)
	}
	clk.Advance(60 * time.Second)

	require.Error(t, flusher.Flush(ctx), "failed channel delivery must be reported")
}

// TestEscalation_ShippedDefaultScheduleFires proves migration 00181 actually
// drives the escalator: a shipped critical rule escalates through its three
// default steps without any manual UPDATE of alert_rules.
func TestEscalation_ShippedDefaultScheduleFires(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)

	inAppMock := newMockProvider("in_app")
	telegramMock := newMockProvider("telegram")
	providerMap := map[string]channels.Provider{
		"in_app":   inAppMock,
		"telegram": telegramMock,
	}

	engine := NewEngine(repo, providerMap, nil)
	escalator := NewEscalator(repo, providerMap, nil)

	startTime := time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)
	clk := &mockClock{current: startTime}
	engine.SetClock(clk)
	escalator.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "temp_breach",
			"severity":   "critical",
			"vehicle_id": "v-reefer-default",
			"title":      "Temp Breach +32C",
			"details":    "Reefer temperature spiked to +32C",
		},
	}
	require.NoError(t, engine.ProcessEvent(ctx, ev))

	alert, err := repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-default")
	require.NoError(t, err)
	require.NotNil(t, alert)
	require.NotNil(t, alert.NextEscalationAt, "shipped critical rule must schedule an escalation")
	assert.Equal(t, startTime.Add(300*time.Second), *alert.NextEscalationAt)
	assert.Equal(t, 0, alert.EscalationStep)

	// Step 1 @300s -> dispatcher via in_app
	clk.Advance(301 * time.Second)
	require.NoError(t, escalator.Tick(ctx))
	alert, err = repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-default")
	require.NoError(t, err)
	assert.Equal(t, 1, alert.EscalationStep)
	require.NotNil(t, alert.NextEscalationAt)
	assert.Equal(t, clk.Now().Add(900*time.Second), *alert.NextEscalationAt)
	assert.True(t, hasEscalationStep(inAppMock.Messages(), 1))

	// Step 2 @900s -> admin via in_app
	clk.Advance(901 * time.Second)
	require.NoError(t, escalator.Tick(ctx))
	alert, err = repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-default")
	require.NoError(t, err)
	assert.Equal(t, 2, alert.EscalationStep)
	require.NotNil(t, alert.NextEscalationAt)
	assert.True(t, hasEscalationStep(inAppMock.Messages(), 2))

	// Step 3 @1800s -> admin via telegram, then no further escalation
	clk.Advance(1801 * time.Second)
	require.NoError(t, escalator.Tick(ctx))
	alert, err = repo.FindOpenByDedupKey(ctx, "telemetry:temp_breach:v-reefer-default")
	require.NoError(t, err)
	assert.Equal(t, 3, alert.EscalationStep)
	assert.Nil(t, alert.NextEscalationAt)
	assert.True(t, hasEscalationStep(telegramMock.Messages(), 3))
}

func hasEscalationStep(messages []channels.Message, step int) bool {
	needle := fmt.Sprintf("[Escalation Step %d]", step)
	for _, m := range messages {
		if strings.Contains(m.Title, needle) {
			return true
		}
	}
	return false
}

// TestShippedAlertRules_HaveValidEscalationSchedules guards migration 00181:
// no shipped rule may regress to a NULL or invalid schedule.
func TestShippedAlertRules_HaveValidEscalationSchedules(t *testing.T) {
	db := newAlertsTestDB(t)
	rows, err := db.Query(`SELECT id, severity, escalation_schedule FROM alert_rules ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	allowed := map[string]bool{"in_app": true, "telegram": true, "email": true, "sms": true, "whatsapp": true}
	n := 0
	for rows.Next() {
		var id, severity string
		var raw sql.NullString
		require.NoError(t, rows.Scan(&id, &severity, &raw))
		n++
		require.True(t, raw.Valid && raw.String != "", "%s (%s) shipped without escalation_schedule", id, severity)
		var steps []EscalationStep
		require.NoError(t, json.Unmarshal([]byte(raw.String), &steps), "%s has invalid schedule JSON", id)
		require.NotEmpty(t, steps, "%s has an empty schedule", id)
		for i, step := range steps {
			assert.Greater(t, step.AfterSeconds, 0, "%s step %d must have after_seconds", id, i)
			assert.NotEmpty(t, step.TargetRole, "%s step %d must target a role", id, i)
			assert.True(t, allowed[step.Channel], "%s step %d uses unregistered channel %q", id, i, step.Channel)
		}
	}
	require.NoError(t, rows.Err())
	assert.GreaterOrEqual(t, n, 16, "expected the 16 shipped alert rules")
}

// TestEngine_AvailabilitySLABreachRaisesPerTenantAlert proves the SLA monitor's
// availability.sla_breach event becomes a real alert through rule_sla_breach
// (migration 00182), with per-tenant dedup and a scheduled escalation.
func TestEngine_AvailabilitySLABreachRaisesPerTenantAlert(t *testing.T) {
	db := newAlertsTestDB(t)
	// Alerts have a tenants(id) FK; seed a second org to prove per-tenant alerts.
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-7','Tenant 7','tenant-7')`)
	require.NoError(t, err)
	repo := sqliterepo.NewAlertRepository(db)

	inAppMock := newMockProvider("in_app")
	engine := NewEngine(repo, map[string]channels.Provider{"in_app": inAppMock}, nil)

	startTime := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clk := &mockClock{current: startTime}
	engine.SetClock(clk)

	mkEvent := func(tenant string) events.Event {
		return events.Event{Type: "availability.sla_breach", Payload: map[string]interface{}{
			"tenant_id":      tenant,
			"source":         "availability",
			"alert_type":     "sla_breach",
			"title":          "30-day uptime 98.50% is below the 99.9% SLA target",
			"message":        "Measured platform uptime is below target.",
			"uptime_percent": 98.5,
		}}
	}

	ctx := context.Background()
	require.NoError(t, engine.ProcessEvent(ctx, mkEvent("tenant-42")))
	require.NoError(t, engine.ProcessEvent(ctx, mkEvent("tenant-7")))

	for _, tenant := range []string{"tenant-42", "tenant-7"} {
		alert, err := repo.FindOpenByDedupKey(ctx, "availability:sla_breach:"+tenant)
		require.NoError(t, err)
		require.NotNil(t, alert, "tenant %s must get its own alert", tenant)
		assert.Equal(t, domain.SourceAvailability, alert.Source)
		assert.Equal(t, domain.AlertTypeSLABreach, alert.AlertType)
		assert.Equal(t, domain.SeverityCritical, alert.Severity)
		require.NotNil(t, alert.RuleID)
		assert.Equal(t, "rule_sla_breach", *alert.RuleID)
		require.NotNil(t, alert.NextEscalationAt, "SLA breach must schedule escalation")
		assert.Equal(t, startTime.Add(300*time.Second), *alert.NextEscalationAt)
		assert.Equal(t, tenant, alert.TenantID)
	}
}

func TestEscalation_ExplicitRecipientsEmail(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	emailMock := newMockProvider("email")
	providers := map[string]channels.Provider{"email": emailMock}
	engine := NewEngine(repo, providers, nil)
	escalator := NewEscalator(repo, providers, nil)

	clk := &mockClock{current: time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)
	escalator.SetClock(clk)

	_, err := db.Exec(`
		UPDATE alert_rules
		SET escalation_schedule = '[{"after_seconds":60,"target_role":"dispatcher","channel":"email","recipients":["ops@tenant42.test","cfo@tenant42.test"]}]',
		    channel_routing = '{"in_app":true}'
		WHERE id = 'rule_temp_breach'`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{Type: "AlertEvent", Payload: map[string]interface{}{
		"source": "telemetry", "alert_type": "temp_breach", "severity": "critical",
		"vehicle_id": "v-recip-1", "title": "Temp", "details": "hot",
	}}
	require.NoError(t, engine.ProcessEvent(ctx, ev))

	clk.Advance(65 * time.Second)
	require.NoError(t, escalator.Tick(ctx))

	msgs := emailMock.Messages()
	require.Len(t, msgs, 2, "one email per explicit recipient")
	assert.Equal(t, "ops@tenant42.test", msgs[0].Email)
	assert.Equal(t, "cfo@tenant42.test", msgs[1].Email)
	assert.Contains(t, msgs[0].Title, "[Escalation Step 1]")
}

func TestEscalation_TargetRoleResolvesTenantUsers(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	emailMock := newMockProvider("email")
	providers := map[string]channels.Provider{"email": emailMock}
	engine := NewEngine(repo, providers, nil)
	escalator := NewEscalator(repo, providers, nil)

	clk := &mockClock{current: time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)
	escalator.SetClock(clk)

	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-43','Tenant 43','tenant-43')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO users (id, email, password_hash, name, phone, role_id, status, tenant_id)
		SELECT 'u-disp-1', 'disp1@t42.test', 'x', 'Disp One', '+919000000001', id, 'active', 'tenant-42' FROM roles WHERE name = 'dispatcher';
		INSERT INTO users (id, email, password_hash, name, phone, role_id, status, tenant_id)
		SELECT 'u-disp-2', 'disp2@t42.test', 'x', 'Disp Two', '+919000000002', id, 'active', 'tenant-42' FROM roles WHERE name = 'dispatcher';
		INSERT INTO users (id, email, password_hash, name, phone, role_id, status, tenant_id)
		SELECT 'u-disp-other', 'other@t43.test', 'x', 'Other Tenant', '+919000000003', id, 'active', 'tenant-43' FROM roles WHERE name = 'dispatcher'`)
	require.NoError(t, err)

	_, err = db.Exec(`
		UPDATE alert_rules
		SET escalation_schedule = '[{"after_seconds":60,"target_role":"dispatcher","channel":"email"}]',
		    channel_routing = '{"in_app":true}'
		WHERE id = 'rule_temp_breach'`)
	require.NoError(t, err)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")
	ev := events.Event{Type: "AlertEvent", Payload: map[string]interface{}{
		"source": "telemetry", "alert_type": "temp_breach", "severity": "critical",
		"vehicle_id": "v-role-1", "title": "Temp", "details": "hot",
	}}
	require.NoError(t, engine.ProcessEvent(ctx, ev))

	clk.Advance(65 * time.Second)
	require.NoError(t, escalator.Tick(ctx))

	msgs := emailMock.Messages()
	require.Len(t, msgs, 2, "one email per active dispatcher in the alert tenant")
	assert.ElementsMatch(t, []string{"disp1@t42.test", "disp2@t42.test"},
		[]string{msgs[0].Email, msgs[1].Email})
	assert.NotContains(t, []string{msgs[0].Email, msgs[1].Email}, "other@t43.test",
		"cross-tenant user must never receive the escalation")
}
