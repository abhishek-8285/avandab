package application

// Focused tests for the shared tenant vehicle dropdown (handlers-end-state
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

func TestSweepVehicleOptions_TenantScopedOrdering(t *testing.T) {
	name := fmt.Sprintf("sweep_vehopt_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT, vehicle_number TEXT, registration_number TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles VALUES ('v-b','tenant-a','VN-B','REG-B'), ('v-a','tenant-a','VN-A','REG-A'), ('v-x','tenant-b','VN-X','REG-X')`)
	require.NoError(t, err)

	opts := VehicleOptions(context.Background(), db, "tenant-a")
	require.Len(t, opts, 2)
	assert.Equal(t, "v-a", opts[0].ID, "ordered by vehicle_number ASC")
	assert.Equal(t, "REG-A", opts[0].RegistrationNumber)
	assert.Equal(t, "v-b", opts[1].ID)
	assert.Nil(t, VehicleOptions(context.Background(), db, "tenant-missing"))
	assert.Nil(t, VehicleOptions(context.Background(), nil, "tenant-a"))
}
