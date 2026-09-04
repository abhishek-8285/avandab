package pipeline

import (
	"context"
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
