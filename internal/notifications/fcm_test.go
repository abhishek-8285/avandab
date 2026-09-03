package notifications_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/notifications"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

const testSchema = `
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    email TEXT,
    license_number TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'available',
    tenant_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS routes (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    destination TEXT NOT NULL,
    distance REAL NOT NULL,
    estimated_hours REAL NOT NULL,
    standard_fare REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT NOT NULL UNIQUE,
    booking_id TEXT,
    driver_id TEXT,
    vehicle_id TEXT,
    route_id TEXT NOT NULL,
    departure_time DATETIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    tenant_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS driver_push_tokens (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    driver_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    push_token TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT 'android',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(testSchema)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestFCMService_MockMode(t *testing.T) {
	db := setupTestDB(t)

	// Unset config triggers mock mode
	cfg := notifications.FCMConfig{}
	svc := notifications.NewFCMService(db, cfg, nil)
	assert.True(t, svc.IsMockMode())

	ctx := context.Background()

	// 1. Direct SendToToken in mock mode
	err := svc.SendToToken(ctx, "ExponentPushToken[mock-token-123]", "Test Title", "Test Body", map[string]string{
		"custom_key": "custom_val",
	})
	require.NoError(t, err)

	sent := svc.GetSentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, "ExponentPushToken[mock-token-123]", sent[0].Token)
	assert.Equal(t, "Test Title", sent[0].Title)
	assert.Equal(t, "Test Body", sent[0].Body)
	assert.Equal(t, "custom_val", sent[0].Data["custom_key"])
	assert.True(t, sent[0].MockMode)

	// 2. Empty token validation
	errEmpty := svc.SendToToken(ctx, "", "Title", "Body", nil)
	assert.ErrorIs(t, errEmpty, notifications.ErrEmptyToken)

	// 3. Clear messages
	svc.ClearSentMessages()
	assert.Empty(t, svc.GetSentMessages())
}

func TestFCMService_MultiDeviceDriverDispatch(t *testing.T) {
	db := setupTestDB(t)
	tenantID := "tenant-alpha"
	driverID := "drv-101"

	// Seed tenant, driver and 2 active push tokens (e.g. phone + tablet)
	_, err := db.Exec("INSERT INTO tenants (id, name, slug) VALUES (?, ?, ?)", tenantID, "Alpha Fleet", "alpha-fleet")
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO driver_push_tokens (id, tenant_id, driver_id, user_id, device_id, push_token, platform, is_active)
		VALUES
			('tok-1', ?, ?, ?, 'phone-dev-1', 'ExponentPushToken[device-phone-1]', 'android', 1),
			('tok-2', ?, ?, ?, 'tablet-dev-2', 'ExponentPushToken[device-tablet-2]', 'android', 1),
			('tok-3', ?, ?, ?, 'old-dev-3', 'ExponentPushToken[device-old-3]', 'android', 0)
	`, tenantID, driverID, driverID, tenantID, driverID, driverID, tenantID, driverID, driverID)
	require.NoError(t, err)

	cfg := notifications.FCMConfig{MockMode: true}
	svc := notifications.NewFCMService(db, cfg, nil)
	ctx := context.Background()

	// Get active tokens
	tokens, err := svc.GetActiveTokens(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Len(t, tokens, 2)
	assert.Contains(t, tokens, "ExponentPushToken[device-phone-1]")
	assert.Contains(t, tokens, "ExponentPushToken[device-tablet-2]")

	// Send to driver -> dispatches to both devices
	count, err := svc.SendToDriver(ctx, tenantID, driverID, "Assignment Update", "You have a new route", map[string]string{
		"route": "DELHI-JAIPUR",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	sent := svc.GetSentMessages()
	require.Len(t, sent, 2)
	assert.Equal(t, "ExponentPushToken[device-phone-1]", sent[0].Token)
	assert.Equal(t, "ExponentPushToken[device-tablet-2]", sent[1].Token)
}

func TestFCMService_LegacyHTTPDispatch(t *testing.T) {
	var receivedAuthHeader string
	var receivedPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/fcm/send", r.URL.Path)
		receivedAuthHeader = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		_ = json.Unmarshal(body, &receivedPayload)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"multicast_id":123,"success":1,"failure":0,"canonical_ids":0,"results":[{"message_id":"msg-1"}]}`))
	}))
	defer server.Close()

	cfg := notifications.FCMConfig{
		ServerKey: "fcm-secret-server-key-xyz",
		Endpoint:  server.URL,
	}

	svc := notifications.NewFCMService(nil, cfg, nil)
	assert.False(t, svc.IsMockMode())

	ctx := context.Background()
	err := svc.SendToToken(ctx, "target-fcm-token-456", "Dispatch Alert", "Pickup at Warehouse A", map[string]string{
		"order_id": "ORD-999",
	})
	require.NoError(t, err)

	assert.Equal(t, "key=fcm-secret-server-key-xyz", receivedAuthHeader)
	assert.Equal(t, "target-fcm-token-456", receivedPayload["to"])
	notif, ok := receivedPayload["notification"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Dispatch Alert", notif["title"])
	assert.Equal(t, "Pickup at Warehouse A", notif["body"])
	data, ok := receivedPayload["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ORD-999", data["order_id"])
}

func TestFCMService_HTTPv1Dispatch(t *testing.T) {
	var receivedAuthHeader string
	var receivedPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/projects/my-firebase-proj/messages:send", r.URL.Path)
		receivedAuthHeader = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		_ = json.Unmarshal(body, &receivedPayload)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/my-firebase-proj/messages/msg-v1-001"}`))
	}))
	defer server.Close()

	cfg := notifications.FCMConfig{
		ProjectID: "my-firebase-proj",
		ServerKey: "bearer-token-abc",
		Endpoint:  server.URL,
	}

	svc := notifications.NewFCMService(nil, cfg, nil)
	ctx := context.Background()

	err := svc.SendToToken(ctx, "target-v1-token-789", "Trip Assigned", "Proceed to Hub", map[string]string{
		"trip_id": "trip-001",
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer bearer-token-abc", receivedAuthHeader)
	msg, ok := receivedPayload["message"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "target-v1-token-789", msg["token"])
	notif, ok := msg["notification"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Trip Assigned", notif["title"])
	assert.Equal(t, "Proceed to Hub", notif["body"])
}

func TestFCMService_InvalidTokenDeactivation(t *testing.T) {
	db := setupTestDB(t)
	tenantID := "tenant-test"
	driverID := "drv-inv-1"
	invalidToken := "ExponentPushToken[invalid-device-tok]"

	_, err := db.Exec(`
		INSERT INTO driver_push_tokens (id, tenant_id, driver_id, user_id, device_id, push_token, platform, is_active)
		VALUES ('tok-inv', ?, ?, ?, 'dev-inv', ?, 'android', 1)
	`, tenantID, driverID, driverID, invalidToken)
	require.NoError(t, err)

	// Mock server returning FCM Unregistered error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"multicast_id":123,"success":0,"failure":1,"canonical_ids":0,"results":[{"error":"NotRegistered"}]}`))
	}))
	defer server.Close()

	cfg := notifications.FCMConfig{
		ServerKey: "fcm-key",
		Endpoint:  server.URL,
	}

	svc := notifications.NewFCMService(db, cfg, nil)
	ctx := context.Background()

	// Dispatch to driver with invalid token -> should fail and deactivate token in DB
	_, err = svc.SendToDriver(ctx, tenantID, driverID, "Hello", "Test", nil)
	assert.Error(t, err)

	// Verify token was deactivated
	var isActive int
	err = db.QueryRow("SELECT is_active FROM driver_push_tokens WHERE push_token = ?", invalidToken).Scan(&isActive)
	require.NoError(t, err)
	assert.Equal(t, 0, isActive)
}

func TestFCMService_EventBusSubscriptions(t *testing.T) {
	db := setupTestDB(t)
	bus := events.NewInMemoryBus()
	ctx := context.Background()

	tenantID := "tenant-fleet"
	driverID := "drv-assigned-1"
	routeID := "route-mum-pune"
	tripID := "trip-777"
	tripNum := "TRP-2026-001"

	// Seed route and trip
	_, err := db.Exec(`
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES (?, 'Mumbai JNPT', 'Pune Bhosari MIDC', 150.0, 4.0, 12000.0)
	`, routeID)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO trips (id, trip_number, route_id, driver_id, vehicle_id, departure_time, status, tenant_id)
		VALUES (?, ?, ?, ?, 'veh-101', CURRENT_TIMESTAMP, 'assigned', ?)
	`, tripID, tripNum, routeID, driverID, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO driver_push_tokens (id, tenant_id, driver_id, user_id, device_id, push_token, platform, is_active)
		VALUES ('tok-active-1', ?, ?, ?, 'driver-phone', 'ExponentPushToken[driver-phone-token]', 'android', 1)
	`, tenantID, driverID, driverID)
	require.NoError(t, err)

	cfg := notifications.FCMConfig{MockMode: true}
	svc := notifications.NewFCMService(db, cfg, nil)
	svc.SubscribeEvents(bus)

	// 1. Publish trip.assigned event
	bus.Publish(ctx, events.Event{
		Type: events.TripAssigned,
		Payload: map[string]interface{}{
			"trip_id":   tripID,
			"driver_id": driverID,
			"tenant_id": tenantID,
		},
	})

	sent := svc.GetSentMessages()
	require.NotEmpty(t, sent)
	lastSent := sent[len(sent)-1]
	assert.Equal(t, "ExponentPushToken[driver-phone-token]", lastSent.Token)
	assert.Contains(t, lastSent.Title, "TRP-2026-001")
	assert.Contains(t, lastSent.Body, "Pune Bhosari MIDC")
	assert.Equal(t, "trip_assigned", lastSent.Data["type"])
	assert.Equal(t, tripID, lastSent.Data["trip_id"])

	// 2. Publish trip.cancelled event
	bus.Publish(ctx, events.Event{
		Type: events.TripCancelled,
		Payload: map[string]interface{}{
			"trip_id":   tripID,
			"driver_id": driverID,
			"tenant_id": tenantID,
		},
	})

	sent = svc.GetSentMessages()
	require.True(t, len(sent) >= 2)
	lastSent = sent[len(sent)-1]
	assert.Equal(t, "Trip Cancelled", lastSent.Title)
	assert.Contains(t, lastSent.Body, "TRP-2026-001")
	assert.Equal(t, "trip_cancelled", lastSent.Data["type"])

	// 3. Publish alert.sos / SOSEvent
	bus.Publish(ctx, events.Event{
		Type: "alert.sos",
		Payload: map[string]interface{}{
			"driver_id":  driverID,
			"tenant_id":  tenantID,
			"vehicle_id": "veh-101",
			"sos_id":     "sos-999",
			"reason":     "Highway Breakdown Panic Alert",
		},
	})

	sent = svc.GetSentMessages()
	require.True(t, len(sent) >= 3)
	lastSent = sent[len(sent)-1]
	assert.Equal(t, "EMERGENCY SOS ALERT", lastSent.Title)
	assert.Contains(t, lastSent.Body, "veh-101")
	assert.Contains(t, lastSent.Body, "Highway Breakdown")
	assert.Equal(t, "high", lastSent.Data["priority"])
}

func TestFCMService_SendPushNotificationPort(t *testing.T) {
	db := setupTestDB(t)
	tenantID := "tenant-port"
	driverID := "drv-port-1"

	_, err := db.Exec(`
		INSERT INTO driver_push_tokens (id, tenant_id, driver_id, user_id, device_id, push_token, platform, is_active)
		VALUES ('tok-p1', ?, ?, ?, 'phone', 'ExponentPushToken[port-test-tok]', 'android', 1)
	`, tenantID, driverID, driverID)
	require.NoError(t, err)

	cfg := notifications.FCMConfig{MockMode: true}
	svc := notifications.NewFCMService(db, cfg, nil)

	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID(tenantID))

	// Send through ports.NotificationMessage
	err = svc.SendPush(ctx, ports.NotificationMessage{
		TenantID:  tenantID,
		UserID:    driverID,
		Recipient: driverID,
		Subject:   "Document Expiry Warning",
		Body:      "Your driving license expires in 7 days",
		Type:      ports.NotificationTypePush,
		Metadata: map[string]interface{}{
			"doc_type": "driving_license",
		},
	})
	require.NoError(t, err)

	sent := svc.GetSentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, "ExponentPushToken[port-test-tok]", sent[0].Token)
	assert.Equal(t, "Document Expiry Warning", sent[0].Title)
	assert.Equal(t, "Your driving license expires in 7 days", sent[0].Body)
	assert.Equal(t, "driving_license", sent[0].Data["doc_type"])
}
