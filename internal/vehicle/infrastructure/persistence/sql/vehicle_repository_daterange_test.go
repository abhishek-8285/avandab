package sql

import (
	"context"
	"testing"

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
	mk := func(id, reg string, createdAt string, status string) {
		_, err := dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, 'truck', 1000, 'diesel', datetime('now','+1 year'), datetime('now','+1 year'), datetime('now','+1 year'), ?, '1', ?, ?)`,
			id, reg, "VN-"+id, status, createdAt, createdAt)
		require.NoError(t, err)
	}
	// RFC3339 strings exactly as the Go driver writes them.
	mk("veh-1", "REG-AUG01", "2026-08-01T09:00:00Z", "available")
	mk("veh-2", "REG-AUG10", "2026-08-10T09:00:00Z", "running")
	mk("veh-3", "REG-AUG20", "2026-08-20T09:00:00Z", "maintenance")
	mk("veh-4", "REG-SEP05", "2026-09-05T09:00:00Z", "available")
	// IST-day-boundary row: 2026-08-09T18:45:00Z = 00:15 IST Aug 10.
	mk("veh-5", "REG-AUG10-IST", "2026-08-09T18:45:00Z", "available")

	rows, total, err := repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-01", "2026-08-31", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.Len(t, rows, 4)
	for _, r := range rows {
		assert.Contains(t, []string{"REG-AUG01", "REG-AUG10", "REG-AUG20", "REG-AUG10-IST"}, r.RegistrationNumber)
	}

	// Single-day window (from == to) — the IST-boundary row belongs to Aug 10,
	// so this window now returns both Aug 10 rows.
	rows, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-10", "2026-08-10", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	regs := []string{rows[0].RegistrationNumber, rows[1].RegistrationNumber}
	assert.Contains(t, regs, "REG-AUG10")
	assert.Contains(t, regs, "REG-AUG10-IST")

	// From-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "2026-08-11", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)

	// To-only bound
	_, total, err = repo.SearchReadModelsDateRange(ctx, "1", "", "", "", "2026-08-09", 10, 0)
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
	assert.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	regs = []string{rows[0].RegistrationNumber, rows[1].RegistrationNumber}
	assert.Contains(t, regs, "REG-AUG10")
	assert.Contains(t, regs, "REG-AUG10-IST")
}
