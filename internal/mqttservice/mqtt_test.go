package mqttservice

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

func TestFakeMessageImplementsMessage(t *testing.T) {
	_ = mqtt.Message((*fakeMessage)(nil))
}

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

// codePtr returns a function's code pointer so two callbacks can be compared.
func codePtr(f any) uintptr { return reflect.ValueOf(f).Pointer() }

// Audit claim 1: avandab/telemetry/drivers/{driverId}/gps carried the mobile
// app's live fixes but was bound to logOnlyHandler unconditionally, so 100% of
// phone-published positions were discarded. With a handler wired, BOTH GPS
// topics must reach it.
func TestSubscriptions_DriverTopicRoutesToHandler(t *testing.T) {
	// No handler wired: nothing to route to, log-only everywhere.
	for _, sub := range subscriptions(nil) {
		if got, want := codePtr(sub.handler), codePtr(logOnlyHandler); got != want {
			t.Errorf("%s: handler = %#x, want logOnlyHandler %#x", sub.topic, got, want)
		}
	}

	subs := subscriptions(func(context.Context, string, []byte) {})
	if len(subs) != 2 {
		t.Fatalf("subscriptions = %d, want 2 (device + driver)", len(subs))
	}
	for _, sub := range subs {
		if codePtr(sub.handler) == codePtr(logOnlyHandler) {
			t.Errorf("%s is log-only: frames published there are discarded", sub.topic)
		}
	}
}

// Claim 2c: a hardened broker (docs/13 §5.2) runs allow_anonymous false, so
// the backend must present its superuser credentials — without them the
// broker refuses the connection and every GPS frame stops arriving.
func TestBrokerOptions_CarriesSuperuserCredentials(t *testing.T) {
	opts := brokerOptions("tcp://localhost:1883", "avandab_backend", "s3cret", "avandab_backend_8080")

	if opts.Username != "avandab_backend" {
		t.Fatalf("username = %q, want avandab_backend", opts.Username)
	}
	if opts.Password != "s3cret" {
		t.Fatalf("password = %q, want s3cret", opts.Password)
	}
	if opts.ClientID != "avandab_backend_8080" {
		t.Fatalf("client id = %q, want the per-instance id passed in", opts.ClientID)
	}
}

// Dev brokers are anonymous; the broker must not send an empty username,
// which mosquitto treats as a failed login rather than "no auth".
func TestBrokerOptions_AnonymousWhenNoCredentialsConfigured(t *testing.T) {
	opts := brokerOptions("tcp://localhost:1883", "", "", "avandab_backend_8080")

	if opts.Username != "" || opts.Password != "" {
		t.Fatalf("username/password = %q/%q, want empty for an anonymous dev broker",
			opts.Username, opts.Password)
	}
}

// Claim 2c: a hardened broker (allow_anonymous false + acl_file) needs a real
// password file. Mosquitto 2.x stores PBKDF2-SHA512 lines
// `<user>:$7$1000$<b64 salt>$<b64 hash>`; a line in any other shape is silently
// unusable, so the format is pinned here.
func TestNewBrokerSecret_ProducesMosquittoPasswordFileLine(t *testing.T) {
	secret, hash, err := NewBrokerSecret()
	if err != nil {
		t.Fatalf("NewBrokerSecret: %v", err)
	}
	if len(secret) < 32 {
		t.Fatalf("secret %q is too short to brute-force", secret)
	}
	if strings.Contains(hash, secret) {
		t.Fatal("hash must not contain the plaintext secret")
	}

	// "$7$1000$<salt>$<hash>" splits as ["", "7", "1000", salt, hash].
	parts := strings.Split(hash, "$")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "7" {
		t.Fatalf("hash %q is not a mosquitto $7$ entry: %#v", hash, parts)
	}
	if parts[2] != "1000" {
		t.Fatalf("iterations = %q, want 1000 (mosquitto 2.x)", parts[2])
	}
	salt, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != mosquittoSaltBytes {
		t.Fatalf("salt decode err=%v len=%d, want %d", err, len(salt), mosquittoSaltBytes)
	}
	derived, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil || len(derived) != mosquittoDerivedSize {
		t.Fatalf("hash decode err=%v len=%d, want %d", err, len(derived), mosquittoDerivedSize)
	}

	// The line must verify: same secret, same salt, same derived bytes.
	want, err := pbkdf2.Key(sha512.New, secret, salt, mosquittoIterations, mosquittoDerivedSize)
	if err != nil {
		t.Fatalf("pbkdf2: %v", err)
	}
	if !bytes.Equal(want, derived) {
		t.Fatal("stored hash does not verify against its own secret and salt")
	}

	line := BrokerPasswordFileLine("drv-42", hash)
	if !strings.HasPrefix(line, "drv-42:$7$") {
		t.Fatalf("password file line = %q, want drv-42:$7$...", line)
	}
}

func TestValidateBrokerUsername(t *testing.T) {
	for _, ok := range []string{"drv-42", "94a28d84-0618-47de-99fc-73a35e6de1a7", "avandab_backend"} {
		if err := ValidateBrokerUsername(ok); err != nil {
			t.Fatalf("ValidateBrokerUsername(%q) = %v, want nil", ok, err)
		}
	}
	// `+` and `#` are wildcards: a username with either would make
	// `pattern write avandab/telemetry/drivers/%u/gps` match other topics.
	for _, bad := range []string{"", "drv+1", "drv#1", "drv 1", "drv\t1"} {
		if err := ValidateBrokerUsername(bad); !errors.Is(err, ErrInvalidBrokerUsername) {
			t.Fatalf("ValidateBrokerUsername(%q) = %v, want ErrInvalidBrokerUsername", bad, err)
		}
	}
}

// Mosquitto drops the older session when two clients share an id, so a
// hardcoded id made production (:8080) and staging (:8081) evict each other in
// a loop and every GPS frame in the gap was dropped — silently.
func TestBackendClientID_IsUniquePerInstance(t *testing.T) {
	t.Setenv("MQTT_CLIENT_ID", "")
	t.Setenv("PORT", "8080")
	prod := BackendClientID()

	t.Setenv("PORT", "8081")
	staging := BackendClientID()

	if prod == staging {
		t.Fatalf("prod and staging share the MQTT client id %q — the broker will evict one of them", prod)
	}
	if !strings.Contains(prod, "8080") || !strings.Contains(staging, "8081") {
		t.Fatalf("client ids should carry the instance port, got %q and %q", prod, staging)
	}
}

func TestBackendClientID_ExplicitEnvWins(t *testing.T) {
	t.Setenv("MQTT_CLIENT_ID", "avandab_backend_fixed")
	if got := BackendClientID(); got != "avandab_backend_fixed" {
		t.Fatalf("BackendClientID() = %q, want the MQTT_CLIENT_ID override", got)
	}
}

func TestBrokerOptions_CarriesTheGivenClientID(t *testing.T) {
	opts := brokerOptions("tcp://localhost:1883", "", "", "avandab_backend_8081")
	if opts.ClientID != "avandab_backend_8081" {
		t.Fatalf("client id = %q, want avandab_backend_8081", opts.ClientID)
	}
}
