package handlers

import (
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// TestMigration00159_00160_DownUp proves facility links + run linkage apply
// and roll back (Prove-It protocol: migrations must do both).
func TestMigration00159_00160_DownUp(t *testing.T) {
	db := newCustomersSelectedDB(t)

	colPresent := func(table, col string) int {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n))
		return n
	}
	permCount := func(name string) int {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM permissions WHERE name = ?`, name).Scan(&n))
		return n
	}

	require.Equal(t, 1, colPresent("bookings", "pickup_facility_id"), "00159 must add bookings.pickup_facility_id on up")
	require.Equal(t, 1, colPresent("bookings", "drop_facility_id"), "00159 must add bookings.drop_facility_id on up")
	require.Equal(t, 1, colPresent("planned_stops", "run_id"), "00160 must add planned_stops.run_id on up")
	for _, p := range []string{"dispatch:read", "dispatch:create", "dispatch:update"} {
		require.Equal(t, 1, permCount(p), "00159 must seed %s on up", p)
	}

	goose.SetLogger(goose.NopLogger())
	// Explicit floor (not bare Down): later migrations must not change what
	// this test rolls back.
	require.NoError(t, goose.DownTo(db, "../../db/migrations", 158))
	require.Equal(t, 0, colPresent("bookings", "pickup_facility_id"), "00159 down must drop bookings.pickup_facility_id")
	require.Equal(t, 0, colPresent("bookings", "drop_facility_id"), "00159 down must drop bookings.drop_facility_id")
	require.Equal(t, 0, colPresent("planned_stops", "run_id"), "00160 down must drop planned_stops.run_id")
	for _, p := range []string{"dispatch:read", "dispatch:create", "dispatch:update"} {
		require.Equal(t, 0, permCount(p), "00159 down must remove %s", p)
	}

	require.NoError(t, goose.Up(db, "../../db/migrations"))
	require.Equal(t, 1, colPresent("bookings", "pickup_facility_id"), "00159 must re-apply cleanly after down")
	require.Equal(t, 1, colPresent("planned_stops", "run_id"), "00160 must re-apply cleanly after down")
	for _, p := range []string{"dispatch:read", "dispatch:create", "dispatch:update"} {
		require.Equal(t, 1, permCount(p), "%s must re-apply cleanly after down", p)
	}
}
