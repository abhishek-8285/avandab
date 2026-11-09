package availability

import (
	"context"
	"sync"
	"testing"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

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

func (b *captureBus) Subscribe(string, events.Handler) (unsubscribe func()) {
	return func() {}
}

func (b *captureBus) snapshot() ([]events.Event, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]events.Event(nil), b.events...), append([]string(nil), b.tenants...)
}

func TestSLAMonitor_NoSamplesNoAlert(t *testing.T) {
	db := testDB(t)
	bus := &captureBus{}
	NewSLAMonitor(db, bus, 30*24*time.Hour, time.Minute, time.Minute, nil).check(context.Background())
	evs, _ := bus.snapshot()
	if len(evs) != 0 {
		t.Fatalf("not-measured uptime must not alert, got %d events", len(evs))
	}
}

func TestSLAMonitor_HealthyUptimeNoAlert(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE tenants (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'active')`); err != nil {
		t.Fatalf("tenants table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tenants (id, status) VALUES ('tenant-a','active')`); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		if err := recordAt(context.Background(), db, ComponentOverall, true, time.Millisecond, now.Add(-3*time.Minute+time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	bus := &captureBus{}
	NewSLAMonitor(db, bus, 30*24*time.Hour, time.Minute, time.Minute, nil).check(context.Background())
	evs, _ := bus.snapshot()
	if len(evs) != 0 {
		t.Fatalf("100%% uptime must not alert, got %d events", len(evs))
	}
}

func TestSLAMonitor_BreachAlertsActiveTenantsOnly(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE tenants (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'active')`); err != nil {
		t.Fatalf("tenants table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tenants (id, status) VALUES ('tenant-a','active'), ('tenant-b','suspended'), ('tenant-c','active')`); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	// 2 healthy samples, expected 4 -> 50% measured uptime.
	now := time.Now().UTC()
	for _, offset := range []int{-3, 0} {
		if err := recordAt(context.Background(), db, ComponentOverall, true, time.Millisecond, now.Add(time.Duration(offset)*time.Minute)); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	bus := &captureBus{}
	NewSLAMonitor(db, bus, 30*24*time.Hour, time.Minute, time.Minute, nil).check(context.Background())

	evs, tenants := bus.snapshot()
	if len(evs) != 2 {
		t.Fatalf("expected one alert per active tenant (2), got %d", len(evs))
	}
	if evs[0].Type != "availability.sla_breach" {
		t.Fatalf("unexpected event type %q", evs[0].Type)
	}
	seen := map[string]bool{}
	for i, ev := range evs {
		payload, ok := ev.Payload.(map[string]interface{})
		if !ok {
			t.Fatalf("payload type %T", ev.Payload)
		}
		if payload["source"] != "availability" || payload["alert_type"] != "sla_breach" {
			t.Fatalf("bad payload: %+v", payload)
		}
		tid, _ := payload["tenant_id"].(string)
		seen[tid] = true
		if tenants[i] != tid {
			t.Fatalf("event %d tenant context %q != payload %q", i, tenants[i], tid)
		}
		if pct, _ := payload["uptime_percent"].(float64); pct >= SLATargetPercent {
			t.Fatalf("breach payload uptime %.2f should be below target", pct)
		}
	}
	if !seen["tenant-a"] || !seen["tenant-c"] || seen["tenant-b"] {
		t.Fatalf("active tenants only, got %v", seen)
	}
}

func TestSLAMonitor_RunStopsOnCancel(t *testing.T) {
	db := testDB(t)
	bus := &captureBus{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewSLAMonitor(db, bus, 30*24*time.Hour, time.Minute, 10*time.Millisecond, nil).Run(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SLA monitor did not stop on cancellation")
	}
}
