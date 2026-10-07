package mqttservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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
//
// username/password authenticate the backend superuser. A hardened broker
// (docs/13 §5.2: allow_anonymous false + acl_file) refuses anonymous clients,
// so an empty username only works against a dev broker.
func NewMQTTBroker(brokerURL, username, password string, handler TelemetryHandler) *MQTTBroker {
	client := mqtt.NewClient(brokerOptions(brokerURL, username, password, BackendClientID()))
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("[MQTT WARNING] Could not connect to MQTT Broker (%s): %v (running fallback mode)", brokerURL, token.Error())
	} else {
		log.Printf("[MQTT] Connected to broker at %s", brokerURL)
	}

	b := &MQTTBroker{client: client, handler: handler}
	b.subscribeTelemetry()
	return b
}

// BackendClientID returns an MQTT client id unique per running process.
//
// Mosquitto disconnects the older session whenever two clients share an id, so
// a hardcoded id made production (:8080) and staging (:8081) evict each other in
// a loop — the broker log filled with "already connected, closing old
// connection" and every GPS frame arriving during the flap was dropped, with no
// error anywhere. MQTT_CLIENT_ID overrides; PORT keeps the default unique.
func BackendClientID() string {
	if id := os.Getenv("MQTT_CLIENT_ID"); id != "" {
		return id
	}
	if port := os.Getenv("PORT"); port != "" {
		return "avandab_backend_" + port
	}
	return "avandab_backend"
}

// brokerOptions builds the Paho options for the backend subscriber. Credentials
// are set only when a username exists: mosquitto treats an empty username as a
// login attempt and rejects it, so an anonymous dev broker must stay unset.
func brokerOptions(brokerURL, username, password, clientID string) *mqtt.ClientOptions {
	opts := mqtt.NewClientOptions().AddBroker(brokerURL)
	opts.SetClientID(clientID)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	if username != "" {
		opts.SetUsername(username)
		opts.SetPassword(password)
	}
	return opts
}

// subscription is one broker subscription: a topic filter and its callback.
type subscription struct {
	topic   string
	handler mqtt.MessageHandler
}

// subscriptions returns every GPS topic the backend listens on.
//
// The mobile app publishes to avandab/telemetry/drivers/{driverId}/gps; that
// topic used to be bound to logOnlyHandler unconditionally, which discarded
// 100% of phone-published fixes (audit 2026-09-27, claim 1). Both GPS topics
// carry the same payload shape and the ingest handler resolves the identity
// from the topic, so they now share one sink.
func subscriptions(h TelemetryHandler) []subscription {
	sink := logOnlyHandler
	if h != nil {
		sink = asPahoHandler(h)
	}
	return []subscription{
		{topic: "avandab/telemetry/devices/+/gps", handler: sink},
		{topic: "avandab/telemetry/drivers/+/gps", handler: sink},
	}
}

// subscribeTelemetry subscribes the GPS topics and routes them to the
// ingestion pipeline (log-only when no handler is wired).
func (b *MQTTBroker) subscribeTelemetry() {
	if !b.client.IsConnected() {
		return
	}
	for _, sub := range subscriptions(b.handler) {
		b.client.Subscribe(sub.topic, 1, sub.handler)
	}
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
