package application

// Focused test for the extracted portal-access link (handlers-end-state ADR,
// Phase 2): idempotent customer_users insert.

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

func TestSweepLinkPortalUser_Idempotent(t *testing.T) {
	name := fmt.Sprintf("sweep_portal_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE customer_users (id TEXT PRIMARY KEY, customer_id TEXT, user_id TEXT, UNIQUE(customer_id, user_id))`)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, LinkPortalUser(ctx, db, "cust-1", "user-1"))
	require.NoError(t, LinkPortalUser(ctx, db, "cust-1", "user-1"), "re-link is a no-op")
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM customer_users`).Scan(&n))
	assert.Equal(t, 1, n)

	require.NoError(t, LinkPortalUser(ctx, db, "cust-2", "user-1"))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM customer_users`).Scan(&n))
	assert.Equal(t, 2, n, "a second customer link is a distinct row")
	assert.NoError(t, LinkPortalUser(ctx, nil, "cust-1", "user-1"))
}
