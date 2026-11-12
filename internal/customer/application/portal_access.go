package application

// Shipper-portal access link, extracted from internal/handlers/customers.go
// (handlers-end-state ADR, Phase 2). The handler keeps user creation/role
// assignment/password handling; this file keeps the customer_users link
// insert (migration 00073).

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// LinkPortalUser links a user to a customer so they can sign in to the
// shipper portal. Idempotent: ON CONFLICT DO NOTHING keeps an existing link
// (legacy behavior); a nil DB is a no-op because the legacy handler only
// wrote when the DB was present.
func LinkPortalUser(ctx context.Context, db *sql.DB, customerID, userID string) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO customer_users (id, customer_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		uuid.NewString(), customerID, userID)
	return err
}
