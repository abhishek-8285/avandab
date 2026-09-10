package realtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// TestStampTenant_MapPayloadStamps proves publish-time tenant stamping: a
// map payload published under a tenant context gains tenant_id on a copied
// map (input never mutated), per Spec 04 SSE tenant isolation.
func TestStampTenant_MapPayloadStamps(t *testing.T) {
	orig := map[string]interface{}{
		"vehicle_id": "v-1",
		"lat":        19.0760,
	}
	e := events.Event{Type: "telemetry.snapshot", Payload: orig}

	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-A"))
	out := StampTenant(ctx, e)

	m, ok := out.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map payload, got %T", out.Payload)
	}
	if got, want := m["tenant_id"], "tenant-A"; got != want {
		t.Fatalf("tenant_id = %v, want %v", got, want)
	}
	if m["vehicle_id"] != "v-1" {
		t.Fatalf("vehicle_id lost in copy: %v", m["vehicle_id"])
	}
	// Pure function: the input map must not be mutated.
	if _, leaked := orig["tenant_id"]; leaked {
		t.Fatal("input map was mutated by StampTenant")
	}
}

// TestStampTenant_EmptyContextPassthrough: an empty publish-context tenant
// leaves the event unstamped (legacy passthrough) — never a hardcoded
// tenant literal.
func TestStampTenant_EmptyContextPassthrough(t *testing.T) {
	payload := map[string]interface{}{"vehicle_id": "v-2"}
	e := events.Event{Type: "telemetry.snapshot", Payload: payload}

	out := StampTenant(context.Background(), e)

	m, ok := out.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map payload, got %T", out.Payload)
	}
	if _, stamped := m["tenant_id"]; stamped {
		t.Fatalf("unstamped publish must stay unstamped, got %v", m)
	}
	if m["vehicle_id"] != "v-2" {
		t.Fatalf("payload must pass through untouched, got %v", m)
	}
}

// TestStampTenant_AlreadyStamped: an event that already carries tenant_id
// passes through untouched (publisher's stamp wins — never overwritten).
func TestStampTenant_AlreadyStamped(t *testing.T) {
	payload := map[string]interface{}{"tenant_id": "tenant-B", "vehicle_id": "v-3"}
	e := events.Event{Type: "SOSEvent", Payload: payload}

	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-A"))
	out := StampTenant(ctx, e)

	m := out.Payload.(map[string]interface{})
	if m["tenant_id"] != "tenant-B" {
		t.Fatalf("existing stamp overwritten: %v", m["tenant_id"])
	}
	if &out.Payload == &e.Payload {
		t.Fatal("already-stamped event must not be copied")
	}
}

// TestStampTenant_StructPayloadPassthrough: struct payloads carry TenantID
// from service publishers — the stamp seam must not touch them (StampTenant
// cannot marshal structs cheaply, and eventTenant already reads them).
func TestStampTenant_StructPayloadPassthrough(t *testing.T) {
	type dummy struct {
		TenantID string `json:"tenant_id"`
		Msg      string
	}
	e := events.Event{Type: "trip.completed", Payload: dummy{TenantID: "tenant-C", Msg: "hi"}}

	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-A"))
	out := StampTenant(ctx, e)

	if out != e {
		t.Fatal("struct payload must pass through untouched")
	}
}

// TestStampTenant_NilPayload: nil payload passes through without panic.
func TestStampTenant_NilPayload(t *testing.T) {
	e := events.Event{Type: "maintenance.due", Payload: nil}
	out := StampTenant(context.Background(), e)
	if out.Payload != nil {
		t.Fatalf("expected nil payload, got %v", out.Payload)
	}
}

// TestAttachToBus_StampingTenant: end-to-end — events published with a
// tenant context reach SSE frames with tenant_id; events published without
// one do not carry a fabricated stamp.
func TestAttachToBus_StampingTenant(t *testing.T) {
	bus := events.NewInMemoryBus()
	hub := NewHub(15, nil)
	AttachToBus(bus, hub)

	ch, unsub := hub.Subscribe(context.Background(), nil)
	defer unsub()

	tenantCtx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-X"))
	bus.Publish(tenantCtx, events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"vehicle_id": "v-stamp-1",
			"lat":        19.0760,
		},
	})
	select {
	case frame := <-ch:
		if !strings.Contains(string(frame), `"tenant_id":"tenant-X"`) {
			t.Fatalf("expected stamped frame, got %s", string(frame))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for stamped telemetry.snapshot")
	}

	bus.Publish(context.Background(), events.Event{
		Type: "telemetry.snapshot",
		Payload: map[string]interface{}{
			"vehicle_id": "v-stamp-2",
		},
	})
	select {
	case frame := <-ch:
		if strings.Contains(string(frame), `"tenant_id"`) {
			t.Fatalf("unstamped publish must stay unstamped, got %s", string(frame))
		}
		if !strings.Contains(string(frame), "v-stamp-2") {
			t.Fatalf("expected frame to contain v-stamp-2, got %s", string(frame))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for unstamped telemetry.snapshot")
	}
}

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
