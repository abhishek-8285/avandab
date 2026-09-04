package comm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
)

// Unknown trip + no tenant anywhere → skip, never enqueue into tenant "1".
func TestHandlePODEvent_UnknownTenantSkipped(t *testing.T) {
	db := newOutboxTestDB(t)
	s := NewEventSubscriber(db, nil)

	err := s.HandlePODEvent(context.Background(), events.Event{
		Type:    "trip.delivered",
		Payload: map[string]any{"trip_id": "trip-ghost", "customer_email": "a@x.in"},
	})
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM comm_outbox`).Scan(&count))
	assert.Equal(t, 0, count, "unroutable POD event must not enqueue anywhere")
}

// Trip row carries tenant X → outbox row lands in X, not "1".
func TestHandlePODEvent_TenantFromTripRow(t *testing.T) {
	db := newOutboxTestDB(t)
	_, err := db.Exec(`CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, trip_number TEXT, booking_id TEXT, pod_url TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE bookings (id TEXT PRIMARY KEY, customer_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE customers (id TEXT PRIMARY KEY, email TEXT, phone TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number) VALUES ('trip-1', 'tenant-X', 'TR-X-1')`)
	require.NoError(t, err)

	s := NewEventSubscriber(db, nil)
	err = s.HandlePODEvent(context.Background(), events.Event{
		Type:    "trip.delivered",
		Payload: map[string]any{"trip_id": "trip-1", "customer_email": "buyer@x.in", "pod_url": "https://pod/x.jpg"},
	})
	require.NoError(t, err)

	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM comm_outbox LIMIT 1`).Scan(&tenant))
	assert.Equal(t, "tenant-X", tenant)
}

// Auth event: tenant resolved from the account's own users row.
func TestHandleAuthEvent_TenantFromUserRow(t *testing.T) {
	db := newOutboxTestDB(t)
	_, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, tenant_id) VALUES ('u-1', 'driver@y.in', 'tenant-Y')`)
	require.NoError(t, err)

	s := NewEventSubscriber(db, nil)
	err = s.HandleAuthEvent(context.Background(), events.Event{
		Type:    "auth.password_reset",
		Payload: map[string]any{"email": "driver@y.in", "reset_link": "https://app/reset?t=abc"},
	})
	require.NoError(t, err)

	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM comm_outbox LIMIT 1`).Scan(&tenant))
	assert.Equal(t, "tenant-Y", tenant)
}

// Auth event for an unknown email → skip, never file under tenant "1".
func TestHandleAuthEvent_UnknownEmailSkipped(t *testing.T) {
	db := newOutboxTestDB(t)
	_, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, tenant_id TEXT)`)
	require.NoError(t, err)

	s := NewEventSubscriber(db, nil)
	err = s.HandleAuthEvent(context.Background(), events.Event{
		Type:    "auth.password_reset",
		Payload: map[string]any{"email": "ghost@z.in", "reset_link": "https://app/reset?t=abc"},
	})
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM comm_outbox`).Scan(&count))
	assert.Equal(t, 0, count)
}
