package auth

// Focused test for the extracted phone-verified stamp (handlers-end-state
// ADR, Phase 2).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSweepMarkPhoneVerified_FirstStampWins(t *testing.T) {
	name := fmt.Sprintf("sweep_phone_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, phone TEXT, phone_verified_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, phone) VALUES ('u-1', '+919876543210')`)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, MarkPhoneVerified(ctx, db, "+919876543210"))
	var first sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT phone_verified_at FROM users WHERE id = 'u-1'`).Scan(&first))
	assert.True(t, first.Valid, "possession proof stamps phone_verified_at")

	require.NoError(t, MarkPhoneVerified(ctx, db, "+919876543210"))
	var second sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT phone_verified_at FROM users WHERE id = 'u-1'`).Scan(&second))
	assert.Equal(t, first.Time.UTC().Format(time.RFC3339), second.Time.UTC().Format(time.RFC3339), "already-verified rows are untouched")

	assert.NoError(t, MarkPhoneVerified(ctx, db, "+910000000000"), "unknown phone is a no-op")
	assert.NoError(t, MarkPhoneVerified(ctx, nil, "+919876543210"))
}
