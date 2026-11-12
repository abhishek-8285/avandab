package application

// Focused test for the agent driver availability facade (Phase 3).

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

func TestAgentDriverQueries_AvailableTenantScoped(t *testing.T) {
	name := fmt.Sprintf("agent_drv_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE drivers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, first_name TEXT, last_name TEXT, phone TEXT, license_number TEXT, status TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, license_number, status) VALUES
		('d-a','tenant-a','Asha','Rao','999','DL-1','available'),
		('d-a-off','tenant-a','Busy','Driver','888','DL-2','on_trip'),
		('d-b','tenant-b','Beta','Driver','777','DL-3','available')`)
	require.NoError(t, err)

	rows, err := NewAgentDriverQueries(db).Available(context.Background(), shared.TenantID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "d-a", rows[0].ID)
	assert.Equal(t, "Asha Rao", rows[0].Name)
	assert.Equal(t, "DL-1", rows[0].License)

	other, err := NewAgentDriverQueries(db).Available(context.Background(), shared.TenantID("tenant-b"))
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, "d-b", other[0].ID)
}
