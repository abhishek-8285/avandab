package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/settlement/domain"
	"transport-app/internal/shared"
)

type fakeReader struct {
	rows   []domain.PayoutInstruction
	err    error
	cutoff time.Time
	limit  int
}

func (f *fakeReader) ListPayoutsAwaitingCompletion(_ context.Context, olderThan time.Time, limit int) ([]domain.PayoutInstruction, error) {
	f.cutoff = olderThan
	f.limit = limit
	return f.rows, f.err
}

type captureBus struct {
	mu      sync.Mutex
	events  []events.Event
	tenants []string
}

func (b *captureBus) Publish(ctx context.Context, e events.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	b.tenants = append(b.tenants, string(shared.TenantIDFromContext(ctx)))
	return nil
}

func (b *captureBus) Subscribe(string, events.Handler) (unsubscribe func()) { return func() {} }

func (b *captureBus) snapshot() ([]events.Event, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]events.Event(nil), b.events...), append([]string(nil), b.tenants...)
}

func TestSweepOnce_PublishesAlertPerStalePayout(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	reader := &fakeReader{rows: []domain.PayoutInstruction{
		{ID: "pout-1", TenantID: "tenant-1", DriverID: "drv-1", Amount: 500, Currency: "INR",
			Status: domain.PayoutProcessing, InitiatedAt: now.Add(-25 * time.Hour)},
		{ID: "pout-2", TenantID: "tenant-2", DriverID: "drv-2", Amount: 1250.5, Currency: "INR",
			Status: domain.PayoutInitiated, InitiatedAt: now.Add(-30 * time.Hour)},
	}}
	bus := &captureBus{}
	sweeper := NewSweeper(reader, bus, 24*time.Hour, time.Hour, nil)
	sweeper.now = func() time.Time { return now }

	count, err := sweeper.SweepOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, now.Add(-24*time.Hour), reader.cutoff)
	assert.Equal(t, 100, reader.limit)

	evs, tenants := bus.snapshot()
	require.Len(t, evs, 2)
	assert.Equal(t, "AlertEvent", evs[0].Type)
	payload, ok := evs[0].Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "settlement", payload["source"])
	assert.Equal(t, "payout_stuck", payload["alert_type"])
	assert.Equal(t, "critical", payload["severity"])
	assert.Equal(t, "pout-1", payload["payout_id"])
	assert.Equal(t, "drv-1", payload["driver_id"])
	assert.Equal(t, 500.0, payload["amount"])
	assert.Contains(t, payload["title"], "stuck")
	assert.Contains(t, payload["details"], "pout-1")
	assert.Equal(t, []string{"tenant-1", "tenant-2"}, tenants, "each event carries the payout's tenant")
}

func TestSweepOnce_NoStalePayouts(t *testing.T) {
	bus := &captureBus{}
	sweeper := NewSweeper(&fakeReader{}, bus, 24*time.Hour, time.Hour, nil)
	count, err := sweeper.SweepOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count)
	evs, _ := bus.snapshot()
	assert.Empty(t, evs)
}

func TestSweepOnce_ReadErrorSurfaces(t *testing.T) {
	sweeper := NewSweeper(&fakeReader{err: errors.New("db down")}, &captureBus{}, 24*time.Hour, time.Hour, nil)
	_, err := sweeper.SweepOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestSweepOnce_NoBusDoesNotPanic(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{rows: []domain.PayoutInstruction{{
		ID: "pout-1", TenantID: "tenant-1", DriverID: "drv-1", Amount: 100, Currency: "INR",
		Status: domain.PayoutProcessing, InitiatedAt: now.Add(-48 * time.Hour),
	}}}
	sweeper := NewSweeper(reader, nil, 24*time.Hour, time.Hour, nil)
	count, err := sweeper.SweepOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, count, "without an event bus nothing is published")
}

func TestNewSweeper_Defaults(t *testing.T) {
	sweeper := NewSweeper(&fakeReader{}, nil, 0, 0, nil)
	assert.Equal(t, 24*time.Hour, sweeper.after)
	assert.Equal(t, time.Hour, sweeper.interval)
	assert.Equal(t, 100, sweeper.limit)
}
