package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	vehicledomain "transport-app/internal/vehicle/domain"
)

// TestVehicleRepository_SearchReadModelsDateRange proves the from/to window
// filters on created_at used by the vehicles list page calendar.
func TestVehicleRepository_SearchReadModelsDateRange(t *testing.T) {
	dbConn := setupVehicleTestDB(t)
	repo, ok := NewVehicleRepository(dbConn).(interface {
		SearchReadModelsDateRange(ctx context.Context, tenantID shared.TenantID, query string, status string, from string, to string, limit int, offset int) ([]vehicledomain.VehicleReadModel, int64, error)
	})
	require.True(t, ok, "vehicle repo must implement date-range search")

	ctx := context.Background()
	mk := func(id, reg string, day int, status string) {
		createdAt := time.Date(2026, 8, day, 9, 0, 0, 0, time.UTC)
		_, err := dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, 'truck', 1000, 'diesel', datetime('now','+1 year'), datetime('now','+1 year'), datetime('now','+1 year'), ?, '1', ?, ?)`,
			id, reg, "VN-"+id, status, createdAt, createdAt)
		require.NoError(t, err)
	}
	mk("veh-1", "REG-AUG01", 1, "available")
	mk("veh-2", "REG-AUG10", 10, "running")
	mk("veh-3", "REG-AUG20", 20, "maintenance")
	mk("veh-4", "REG-SEP05", 5, "available")

	_, err := dbConn.Exec(`UPDATE vehicles SET created_at = ? WHERE id = 'veh-4'`, time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, rows, 3)
	for _, r := range rows {
		assert.Contains(t, []string{"REG-AUG01", "REG-AUG10", "REG-AUG20"}, r.RegistrationNumber)
	}

	// Single-day window (from == to)
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "REG-AUG10", rows[0].RegistrationNumber)

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-09-01", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-05", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + date combined
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "maintenance", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "REG-AUG20", rows[0].RegistrationNumber)

	// Search + date combined
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "AUG1", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "REG-AUG10", rows[0].RegistrationNumber)
}
