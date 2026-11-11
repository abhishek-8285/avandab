package application

// Focused read-model test for the agent booking facade (handlers-end-state
// ADR, Phase 3); creation runs through the clean CreateBookingUseCase and is
// covered by the agent-tool integration test.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

func TestAgentBookingService_GetByNumberTenantScoped(t *testing.T) {
	name := fmt.Sprintf("agent_bk_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE customers (id TEXT PRIMARY KEY, name TEXT);
		CREATE TABLE routes (id TEXT PRIMARY KEY, source TEXT, destination TEXT);
		CREATE TABLE bookings (id TEXT PRIMARY KEY, booking_number TEXT, customer_id TEXT, route_id TEXT,
		 pickup_date DATETIME, vehicle_type TEXT, passengers INTEGER, price REAL, status TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers VALUES ('c1','Acme'); INSERT INTO routes VALUES ('r1','Mumbai','Pune');
		INSERT INTO bookings VALUES ('b1','BK-1','c1','r1','2026-10-01 09:00:00','truck',2,1500,'confirmed','tenant-a')`)
	require.NoError(t, err)
	ctx := context.Background()
	svc := NewAgentBookingService(db)

	v, err := svc.GetByNumber(ctx, shared.TenantID("tenant-a"), "BK-1")
	require.NoError(t, err)
	assert.Equal(t, "BK-1", v.Number)
	assert.Equal(t, "Acme", v.Customer)
	assert.Equal(t, "Mumbai -> Pune", v.Route)
	assert.Equal(t, int64(2), v.Passengers)
	assert.Equal(t, 1500.0, v.Price)

	_, err = svc.GetByNumber(ctx, shared.TenantID("tenant-b"), "BK-1")
	assert.ErrorIs(t, err, sql.ErrNoRows, "foreign booking invisible (tool maps to booking not found)")
	_, err = svc.GetByNumber(ctx, shared.TenantID("tenant-a"), "BK-NOPE")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
