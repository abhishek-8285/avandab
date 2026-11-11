package application

// Focused tests for the agent route facade (handlers-end-state ADR, Phase 3):
// tenant-scoped search, legacy bidirectional matching and GST rate lookup.

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

	"transport-app/internal/shared"
)

const agentRouteTestSchema = `
CREATE TABLE routes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, source TEXT, destination TEXT,
 distance REAL, estimated_hours REAL, standard_fare REAL, reverse_distance REAL, reverse_standard_fare REAL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE tenant_company_profiles (tenant_id TEXT PRIMARY KEY, gst_rate REAL NOT NULL DEFAULT 0);
CREATE TABLE company_settings (id INTEGER PRIMARY KEY, gst_rate REAL NOT NULL DEFAULT 0);
`

func newAgentRouteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("agent_route_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(agentRouteTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAgentRouteQueries_SearchAndQuote(t *testing.T) {
	db := newAgentRouteTestDB(t)
	_, err := db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare, reverse_distance, reverse_standard_fare) VALUES
		('r-a','tenant-a','Mumbai','Pune',150,3,1000,160,1100),
		('r-b','tenant-b','Delhi','Jaipur',280,5,2000,NULL,NULL),
		('r-1','1','Mumbai','Pune',150,3,1000,NULL,NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_company_profiles (tenant_id, gst_rate) VALUES ('tenant-a', 18), ('tenant-b', 12)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_settings (id, gst_rate) VALUES (1, 5)`)
	require.NoError(t, err)
	ctx := context.Background()
	q := NewAgentRouteQueries(db)

	rows, err := q.Search(ctx, shared.TenantID("tenant-a"), "mum", 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Mumbai", rows[0].Source)
	assert.Equal(t, 150.0, rows[0].DistanceKM)
	assert.Empty(t, qSearchOther(ctx, t, q))

	// Forward quote with the tenant profile GST rate.
	quote, err := q.Quote(ctx, shared.TenantID("tenant-a"), "Mumbai", "Pune")
	require.NoError(t, err)
	require.NotNil(t, quote)
	assert.Equal(t, 1000.0, quote.BaseFare)
	assert.Equal(t, 18.0, quote.GSTRatePct)
	assert.Equal(t, 180.0, quote.GSTAmount)
	assert.Equal(t, 1180.0, quote.TotalWithGST)

	// Reverse direction uses the reverse distance/fare (domain rule).
	rev, err := q.Quote(ctx, shared.TenantID("tenant-a"), "Pune", "Mumbai")
	require.NoError(t, err)
	require.NotNil(t, rev)
	assert.Equal(t, 160.0, rev.DistanceKM)
	assert.Equal(t, 1100.0, rev.BaseFare)

	// Bootstrap tenant reads company_settings id=1.
	boot, err := q.Quote(ctx, shared.TenantID("1"), "Mumbai", "Pune")
	require.NoError(t, err)
	require.NotNil(t, boot)
	assert.Equal(t, 5.0, boot.GSTRatePct)

	// No match -> nil, nil (tool renders the hint string).
	missing, err := q.Quote(ctx, shared.TenantID("tenant-a"), "Goa", "Kochi")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func qSearchOther(ctx context.Context, t *testing.T, q *AgentRouteQueries) []AgentRouteRow {
	t.Helper()
	rows, err := q.Search(ctx, shared.TenantID("tenant-b"), "mum", 20)
	require.NoError(t, err)
	return rows
}
