package mqttservice

// Broker credential persistence, extracted from
// internal/handlers/mqtt_credentials.go (handlers-end-state ADR, Phase 2).
// The handler keeps HTTP auth/tenant gating, secret validation, response
// shapes and status codes; this store keeps the exact tenant-scoped identity
// lookup and the credential row read/upsert.
//
// Secret hygiene: only the mosquitto password-file hash is ever written to
// driver_mqtt_credentials. The plaintext secret is minted by NewBrokerSecret
// and returned to the device exactly once by the handler - it never enters
// this store, any log line or any response other than the mint/rotate one.

import (
	"context"
	"database/sql"
)

// CredentialStore persists broker credential hashes.
type CredentialStore struct {
	db *sql.DB
}

// NewCredentialStore wires the store against the database.
func NewCredentialStore(db *sql.DB) *CredentialStore {
	return &CredentialStore{db: db}
}

// BrokerUsername resolves the identity the mobile publishes in its MQTT topic
// and the broker ACL binds with %u: the driver code once a driver row exists
// for the caller's org/email, otherwise the auth user id (the production case
// - the drivers table is often empty). sql.ErrNoRows (no users row) surfaces
// like the legacy handler's 500 path.
func (s *CredentialStore) BrokerUsername(ctx context.Context, tenantID, userID string) (string, error) {
	if s.db == nil {
		return "", sql.ErrNoRows
	}
	var username sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT d.driver_id FROM users u
		LEFT JOIN drivers d
		  ON d.tenant_id = u.tenant_id
		 AND d.email IS NOT NULL AND d.email = u.email
		WHERE u.id = $1 AND u.tenant_id = $2`, userID, tenantID).Scan(&username)
	if err != nil {
		return "", err
	}
	if username.Valid && username.String != "" {
		return username.String, nil
	}
	return userID, nil
}

// HashByUsername returns the stored mosquitto password hash for a username.
// sql.ErrNoRows means the device has not been provisioned yet (legacy signal
// for minting a secret); other errors surface as the 500 path.
func (s *CredentialStore) HashByUsername(ctx context.Context, username string) (string, error) {
	if s.db == nil {
		return "", sql.ErrNoRows
	}
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM driver_mqtt_credentials WHERE username = $1`, username).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}

// Upsert records a freshly minted/rotated hash. The legacy single statement
// handles both paths: a rotate against a driver with no stored row (row
// deleted, provisioning write lost, phone reinstalled against a fresh
// database) must still record the secret because the broker password file is
// built from this table.
func (s *CredentialStore) Upsert(ctx context.Context, tenantID, username, hash string) error {
	if s.db == nil {
		return sql.ErrConnDone
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO driver_mqtt_credentials (driver_key, tenant_id, username, password_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (username) DO UPDATE SET password_hash = excluded.password_hash,
		                                        rotated_at = CURRENT_TIMESTAMP
		`, username, tenantID, username, hash)
	return err
}
