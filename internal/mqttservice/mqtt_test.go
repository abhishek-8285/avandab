package mqttservice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// fakeMessage is an in-memory mqtt.Message — no broker needed.
type fakeMessage struct {
	topic   string
	payload []byte
}

func (m *fakeMessage) Duplicate() bool   { return false }
func (m *fakeMessage) Qos() byte         { return 1 }
func (m *fakeMessage) Retained() bool    { return false }
func (m *fakeMessage) Topic() string     { return m.topic }
func (m *fakeMessage) MessageID() uint16 { return 1 }
func (m *fakeMessage) Payload() []byte   { return m.payload }
func (m *fakeMessage) Ack()              {}

var _ mqtt.Message = (*fakeMessage)(nil)

// Telemetry payload must survive a JSON round-trip (wire format contract).
func TestGPSTelemetryPayload_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	in := GPSTelemetryPayload{DriverID: "drv-42", Latitude: 12.9716, Longitude: 77.5946, Timestamp: ts}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var out GPSTelemetryPayload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v, want %+v", out, in)
	}
}

// asPahoHandler must forward topic + payload bytes verbatim to the handler.
func TestAsPahoHandler_ForwardsTopicAndPayload(t *testing.T) {
	var gotTopic string
	var gotPayload []byte
	h := asPahoHandler(func(_ context.Context, topic string, payload []byte) {
		gotTopic, gotPayload = topic, payload
	})

	wantTopic := "avandab/telemetry/devices/IMEI123/gps"
	wantPayload := []byte(`{"driver_id":"drv-42"}`)
	h(nil, &fakeMessage{topic: wantTopic, payload: wantPayload})

	if gotTopic != wantTopic {
		t.Errorf("topic = %q, want %q", gotTopic, wantTopic)
	}
	if string(gotPayload) != string(wantPayload) {
		t.Errorf("payload = %s, want %s", gotPayload, wantPayload)
	}
}
