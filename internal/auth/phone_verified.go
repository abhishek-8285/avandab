package auth

// Phone-verification persistence, extracted from internal/handlers/otp.go
// (handlers-end-state ADR, Phase 2). The handler keeps the OTP store logic,
// rate limiting, dev-code envelope and logging; this file keeps the
// phone_verified_at stamp.

import (
	"context"
	"database/sql"
)

// MarkPhoneVerified stamps users.phone_verified_at for every user holding the
// phone number (typically one). Already-verified users are left untouched.
func MarkPhoneVerified(ctx context.Context, db *sql.DB, phone string) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`UPDATE users SET phone_verified_at = CURRENT_TIMESTAMP WHERE phone = $1 AND phone_verified_at IS NULL`,
		phone)
	return err
}
