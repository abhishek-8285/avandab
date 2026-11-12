package application

// Focused tests for the extracted global search (handlers-end-state ADR,
// Phase 2): permission gating, tenant scoping, LIKE escaping.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const sweepSearchSchema = `
CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT, registration_number TEXT, vehicle_number TEXT);
CREATE TABLE bookings (id TEXT PRIMARY KEY, tenant_id TEXT, booking_number TEXT, customer_id TEXT, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE customers (id TEXT PRIMARY KEY, name TEXT, company TEXT, gst TEXT, phone TEXT);
`

func sweepSearchDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("sweep_search_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(sweepSearchSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSweepSearch_TenantScopedAndPermissionGated(t *testing.T) {
	db := sweepSearchDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number) VALUES
		('v-a', 'tenant-a', 'MH12AA0001', 'VAN-A'), ('v-b', 'tenant-b', 'KA01BB0002', 'VAN-B')`)
	require.NoError(t, err)

	svc := NewService(db)
	// vehicles permitted, bookings denied.
	allowed := map[string]bool{"vehicles": true}
	sections := svc.Search(ctx, "tenant-a", "MH12", func(resource string) bool { return allowed[resource] })
	require.Len(t, sections, 1, "only the permitted, matching section")
	assert.Equal(t, "vehicles", sections[0].Key)
	require.Len(t, sections[0].Rows, 1)
	assert.Equal(t, "v-a", sections[0].Rows[0].ID, "other tenant's vehicle must not match")
	assert.Equal(t, "/vehicles/v-a", sections[0].Rows[0].Href)

	// Nothing permitted -> no sections.
	assert.Empty(t, svc.Search(ctx, "tenant-a", "MH12", func(string) bool { return false }))
	// Blank query -> no sections.
	assert.Empty(t, svc.Search(ctx, "tenant-a", "  ", func(string) bool { return true }))
	// nil DB -> no sections.
	assert.Empty(t, NewService(nil).Search(ctx, "tenant-a", "x", func(string) bool { return true }))
}

func TestSweepSearch_LikeEscape(t *testing.T) {
	assert.Equal(t, `50\%\_x\\y`, LikeEscape(`50%_x\y`))
	assert.True(t, strings.HasPrefix(LikeEscape("a"), "a"))
}
