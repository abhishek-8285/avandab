package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeviceStore_ListByTenantFiltered proves the free-text, status and
// created_at window filters used by the telemetry devices list page. Rows are
// seeded with RFC3339 created_at values on purpose: the window must use
// date(substr(col,1,10)), not date(col).
func TestDeviceStore_ListByTenantFiltered(t *testing.T) {
	db := newTestIngestorDB(t)
	store := NewDeviceStore(db)
	ctx := tenantCtx()

	insert := func(id, imei, status string, created string) {
		_, err := db.Exec(`INSERT INTO telemetry_devices
			(id, tenant_id, imei, device_type, status, created_at, updated_at)
			VALUES (?, '1', ?, 'hardware', ?, ?, ?)`,
			id, imei, status, created, created)
		require.NoError(t, err)
	}
	insert("d1", "100000000000001", "inventory", "2026-08-01T08:00:00Z")
	insert("d2", "100000000000002", "active", "2026-08-10T08:00:00Z")
	insert("d3", "100000000000003", "retired", "2026-08-20T08:00:00Z")

	// Full-month window
	rows, total, err := listFiltered(store, ctx, "", "", "2026-08-01", "2026-08-31")
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	assert.EqualValues(t, 3, total)

	// Single-day window (from == to)
	rows, total, err = listFiltered(store, ctx, "", "", "2026-08-10", "2026-08-10")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "100000000000002", rows[0].IMEI)

	// From-only bound
	_, total, err = listFiltered(store, ctx, "", "", "2026-08-11", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// To-only bound
	_, total, err = listFiltered(store, ctx, "", "", "", "2026-08-05")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// Status + query + date combined
	rows, total, err = listFiltered(store, ctx, "0002", "active", "2026-08-01", "2026-08-31")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "d2", rows[0].ID)
}

func listFiltered(s *DeviceStore, ctx context.Context, query, status, from, to string) ([]Device, int64, error) {
	rows, err := s.ListByTenantFiltered(ctx, "1", query, status, from, to, 50, 0)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.CountByTenantFiltered(ctx, "1", query, status, from, to)
	return rows, total, err
}
