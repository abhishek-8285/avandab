package realtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"transport-app/internal/events"
)

func TestAttachToBus(t *testing.T) {
	bus := events.NewInMemoryBus()
	hub := NewHub(15, nil)
	AttachToBus(bus, hub)

	ctx := context.Background()
	ch, unsub := hub.Subscribe(ctx, nil)
	defer unsub()

	// 1. Test telemetry.snapshot
	bus.Publish(ctx, events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"vehicle_id": "v-bus-1",
			"lat":        19.0760,
			"lng":        72.8777,
		},
	})

	select {
	case frame := <-ch:
		if !strings.Contains(string(frame), "v-bus-1") {
			t.Fatalf("expected frame to contain v-bus-1, got %s", string(frame))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for telemetry.snapshot event")
	}

	// 2. Test maintenance.due
	bus.Publish(ctx, events.Event{
		Type: "maintenance.due",
		Payload: map[string]interface{}{
			"vehicle_id": "v-bus-2",
			"reason":     "oil_change",
		},
	})

	select {
	case frame := <-ch:
		if !strings.Contains(string(frame), "v-bus-2") {
			t.Fatalf("expected frame to contain v-bus-2, got %s", string(frame))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for maintenance.due event")
	}
}

func TestAttachToBus_ForwardsAlertsAndSOS(t *testing.T) {
	bus := events.NewInMemoryBus()
	hub := NewHub(15, nil)
	AttachToBus(bus, hub)

	ctx := context.Background()
	ch, unsub := hub.Subscribe(ctx, nil)
	defer unsub()

	for _, tc := range []struct {
		typ     string
		payload map[string]interface{}
		want    string
	}{
		{"telemetry.alert", map[string]interface{}{"vehicle_id": "v-alert-1", "alert_type": "low_battery"}, "v-alert-1"},
		{"SOSEvent", map[string]interface{}{"vehicle_id": "v-sos-1"}, "v-sos-1"},
		{"driver.sos_triggered", map[string]interface{}{"vehicle_id": "v-sos-2"}, "v-sos-2"},
		{"AlertEvent", map[string]interface{}{"vehicle_id": "v-rule-1"}, "v-rule-1"},
		{"trip.completed", map[string]interface{}{"trip_id": "t-bus-1"}, "t-bus-1"},
		{"trip.started", map[string]interface{}{"trip_id": "t-bus-2"}, "t-bus-2"},
	} {
		bus.Publish(ctx, events.Event{Type: tc.typ, Payload: tc.payload})
		select {
		case frame := <-ch:
			if !strings.Contains(string(frame), tc.want) {
				t.Fatalf("%s: expected frame to contain %s, got %s", tc.typ, tc.want, string(frame))
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for %s event", tc.typ)
		}
	}
}
