package mqttservice

// Characterization + extraction tests for the broker credential store moved
// out of internal/handlers/mqtt_credentials.go (handlers-end-state ADR,
// Phase 2): tenant-scoped broker-username resolution, credential read/upsert
// rotation semantics and secret hygiene (hash-only persistence).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const credentialStoreTestSchema = `
CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT);
CREATE TABLE drivers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT, driver_id TEXT);
CREATE TABLE driver_mqtt_credentials (
    driver_key TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rotated_at DATETIME
);
`

func newCredentialStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("mqtt_creds_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(credentialStoreTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecCreds(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestCredentialStore_BrokerUsernameTenantScoped(t *testing.T) {
	db := newCredentialStoreTestDB(t)
	ctx := context.Background()
	mustExecCreds(t, db, `INSERT INTO users (id, tenant_id, email) VALUES
		('u-1', 'tenant-1', 'one@x.in'),
		('u-2', 'tenant-1', 'two@x.in'),
		('u-3', 'tenant-1', 'three@x.in')`)
	mustExecCreds(t, db, `INSERT INTO drivers (id, tenant_id, email, driver_id) VALUES
		('d-1', 'tenant-1', 'one@x.in', 'DRV-1'),
		('d-2', 'tenant-2', 'two@x.in', 'DRV-2'),
		('d-3', 'tenant-1', 'three@x.in', NULL)`)

	store := NewCredentialStore(db)

	// Driver code wins when a same-tenant driver row matches the user email.
	username, err := store.BrokerUsername(ctx, "tenant-1", "u-1")
	require.NoError(t, err)
	assert.Equal(t, "DRV-1", username)

	// A matching driver in ANOTHER tenant must not be used: fall back to the
	// auth user id (the broker ACL still binds only this user's topic).
	username, err = store.BrokerUsername(ctx, "tenant-1", "u-2")
	require.NoError(t, err)
	assert.Equal(t, "u-2", username)

	// A matching driver row with a NULL driver_id falls back to the user id.
	username, err = store.BrokerUsername(ctx, "tenant-1", "u-3")
	require.NoError(t, err)
	assert.Equal(t, "u-3", username)

	// No users row is the legacy 500 signal (sql.ErrNoRows surfaces).
	_, err = store.BrokerUsername(ctx, "tenant-1", "u-missing")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	_, err = NewCredentialStore(nil).BrokerUsername(ctx, "tenant-1", "u-1")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestCredentialStore_HashReadAndRotationUpsert(t *testing.T) {
	db := newCredentialStoreTestDB(t)
	ctx := context.Background()
	store := NewCredentialStore(db)

	// Not provisioned yet: sql.ErrNoRows is the legacy mint signal.
	_, err := store.HashByUsername(ctx, "DRV-1")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// Mint a real secret + hash; only the hash is persisted.
	secret, hash, err := NewBrokerSecret()
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.NotEmpty(t, hash)
	assert.NotEqual(t, secret, hash, "the plaintext secret must never equal the stored hash")
	assert.True(t, strings.HasPrefix(hash, "$7$"), "mosquitto PBKDF2-SHA512 scheme")

	require.NoError(t, store.Upsert(ctx, "tenant-1", "DRV-1", hash))
	stored, err := store.HashByUsername(ctx, "DRV-1")
	require.NoError(t, err)
	assert.Equal(t, hash, stored)
	assert.NotEqual(t, secret, stored, "the plaintext secret is never stored")

	var driverKey, tenantID, username string
	require.NoError(t, db.QueryRow(`SELECT driver_key, tenant_id, username FROM driver_mqtt_credentials WHERE username = 'DRV-1'`).
		Scan(&driverKey, &tenantID, &username))
	assert.Equal(t, "DRV-1", driverKey)
	assert.Equal(t, "tenant-1", tenantID)
	assert.Equal(t, "DRV-1", username)

	// Rotate: same row, new hash, rotated_at stamped (legacy upsert).
	_, newHash, err := NewBrokerSecret()
	require.NoError(t, err)
	require.NoError(t, store.Upsert(ctx, "tenant-1", "DRV-1", newHash))
	stored, err = store.HashByUsername(ctx, "DRV-1")
	require.NoError(t, err)
	assert.Equal(t, newHash, stored)
	var rotatedAt sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT rotated_at FROM driver_mqtt_credentials WHERE username = 'DRV-1'`).Scan(&rotatedAt))
	assert.True(t, rotatedAt.Valid, "rotate must stamp rotated_at")
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_mqtt_credentials`).Scan(&count))
	assert.Equal(t, 1, count, "one row per username, rotation is an upsert")

	// Rotate against a fresh username provisions the row (legacy INSERT path).
	require.NoError(t, store.Upsert(ctx, "tenant-2", "u-fresh", newHash))
	stored, err = store.HashByUsername(ctx, "u-fresh")
	require.NoError(t, err)
	assert.Equal(t, newHash, stored)

	// nil DB degrades to the not-provisioned signal / write error.
	_, err = NewCredentialStore(nil).HashByUsername(ctx, "DRV-1")
	assert.True(t, errors.Is(err, sql.ErrNoRows))
	assert.Error(t, NewCredentialStore(nil).Upsert(ctx, "tenant-1", "DRV-1", hash))
}
