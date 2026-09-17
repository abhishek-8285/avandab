package mqttservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type GPSTelemetryPayload struct {
	DriverID  string    `json:"driver_id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Timestamp time.Time `json:"timestamp"`
}

// TelemetryHandler is invoked for each canonical GPS message received on
// "avandab/telemetry/devices/{imei}/gps". Paho does not expose the publishing
// client's username in the message callback (Spec 01 gotcha D8 #1): the topic
// IMEI is extracted from the topic string, and the handler validates it against
// the payload IMEI. Broker ACL (Mosquitto acl_file) provides connection-level
// spoof protection.
type TelemetryHandler func(ctx context.Context, topic string, payload []byte)

// MQTTBroker wraps a Paho MQTT client.
type MQTTBroker struct {
	client  mqtt.Client
	handler TelemetryHandler
}

// NewMQTTBroker creates and connects a broker that subscribes to canonical
// GPS telemetry topics. When handler is nil, messages are logged only.
func NewMQTTBroker(brokerURL string, handler TelemetryHandler) *MQTTBroker {
	opts := mqtt.NewClientOptions().AddBroker(brokerURL)
	opts.SetClientID("avandab_backend_server")
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("[MQTT WARNING] Could not connect to MQTT Broker (%s): %v (running fallback mode)", brokerURL, token.Error())
	} else {
		log.Printf("[MQTT] Connected to broker at %s", brokerURL)
	}

	b := &MQTTBroker{client: client, handler: handler}
	b.subscribeTelemetry()
	return b
}

// subscribeTelemetry subscribes to the canonical device topic (routing to the
// handler) and keeps the legacy driver topic as a log-only bridge for the
// mobile app until it is retrofitted (Spec 01 Phase 3).
func (b *MQTTBroker) subscribeTelemetry() {
	if !b.client.IsConnected() {
		return
	}

	// Canonical topic: own GPS hardware devices.
	canonicalTopic := "avandab/telemetry/devices/+/gps"
	if b.handler != nil {
		b.client.Subscribe(canonicalTopic, 1, asPahoHandler(b.handler))
	} else {
		b.client.Subscribe(canonicalTopic, 1, logOnlyHandler)
	}

	// Legacy bridge: mobile app still publishes here.
	// Log-only until the mobile app is retrofitted (Spec 01 Phase 3).
	b.client.Subscribe("avandab/telemetry/drivers/+/gps", 1, logOnlyHandler)
}

// logOnlyHandler acknowledges a message without logging its payload.
// It previously logged the raw topic+payload, which on the legacy driver
// GPS topic emitted every driver's live lat/lng to stdout on each publish
// — a DPDP exposure with no operational value (audit 2026-09-17).
func logOnlyHandler(_ mqtt.Client, m mqtt.Message) {
	_ = m
}

// asPahoHandler adapts a TelemetryHandler to Paho's message callback signature.
func asPahoHandler(h TelemetryHandler) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		h(context.Background(), m.Topic(), m.Payload())
	}
}

func (b *MQTTBroker) PublishTripUpdate(driverID string, tripID string, status string) {
	if !b.client.IsConnected() {
		return
	}
	topic := fmt.Sprintf("avandab/trips/drivers/%s/updates", driverID)
	data, _ := json.Marshal(map[string]string{
		"trip_id": tripID,
		"status":  status,
		"time":    time.Now().Format(time.RFC3339),
	})
	b.client.Publish(topic, 1, false, data)
}
