package application

// Focused tests for the AV command application service extracted from
// internal/handlers/vehicles.go (handlers-end-state ADR, Phase 2): the
// actuation status transitions, tenant binding of both the command row and the
// status update, the acknowledged audit row, and command-type validation.

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertCommandTestVehicle(t *testing.T, db *sql.DB, id, tenantID, status string, blockedReason *string) {
	t.Helper()
	var reason any
	if blockedReason != nil {
		reason = *blockedReason
	}
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, status, blocked_reason) VALUES (?, ?, ?, ?, ?)`,
		id, tenantID, "REG-"+id, status, reason)
	require.NoError(t, err)
}

func commandTestVehicleState(t *testing.T, db *sql.DB, id string) (string, string) {
	t.Helper()
	var status, reason string
	require.NoError(t, db.QueryRow(`SELECT status, COALESCE(blocked_reason, '') FROM vehicles WHERE id = ?`, id).Scan(&status, &reason))
	return status, reason
}

func TestVehicleCommandService_StatusTransitions(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	svc := NewVehicleCommandService(db)
	ctx := context.Background()
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name        string
		command     string
		startStatus string
		startReason *string
		wantStatus  string
		wantReason  string
	}{
		{"E_STOP blocks with the operator reason", "E_STOP", "available", nil, "blocked", "AV E-STOP Triggered"},
		{"RESUME_MISSION runs and clears the block reason", "RESUME_MISSION", "blocked", strPtr("AV E-STOP Triggered"), "running", ""},
		{"HOLD_POSITION parks available and keeps the reason column", "HOLD_POSITION", "running", strPtr("keep-me"), "available", "keep-me"},
		{"SET_SPEED_LIMIT leaves the status untouched", "SET_SPEED_LIMIT", "running", strPtr("keep-me"), "running", "keep-me"},
		{"RETURN_TO_BASE leaves the status untouched", "RETURN_TO_BASE", "available", nil, "available", ""},
		{"REROUTE leaves the status untouched", "REROUTE", "maintenance", nil, "maintenance", ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vehicleID := "veh-cmd-" + strconv.Itoa(i)
			insertCommandTestVehicle(t, db, vehicleID, "tenant-cmd", tc.startStatus, tc.startReason)

			require.NoError(t, svc.Send(ctx, SendVehicleCommand{
				TenantID:    "tenant-cmd",
				VehicleID:   vehicleID,
				CommandType: tc.command,
				IssuedBy:    "operator-1",
			}))

			status, reason := commandTestVehicleState(t, db, vehicleID)
			assert.Equal(t, tc.wantStatus, status)
			assert.Equal(t, tc.wantReason, reason)

			var storedStatus string
			require.NoError(t, db.QueryRow(`SELECT status FROM vehicle_commands WHERE vehicle_id = ?`, vehicleID).Scan(&storedStatus))
			assert.Equal(t, "acknowledged", storedStatus)
		})
	}
}

func TestVehicleCommandService_TenantIsolation(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	svc := NewVehicleCommandService(db)
	ctx := context.Background()

	insertCommandTestVehicle(t, db, "v-shared", "tenant-a", "available", nil)
	insertCommandTestVehicle(t, db, "v-other", "tenant-b", "available", nil)

	// Legacy behavior preserved: the INSERT is keyed by the ACTING tenant and
	// does not verify vehicle ownership, while the status UPDATE is scoped to
	// vehicle id AND tenant. A cross-tenant command is logged but never touches
	// the other tenant vehicle.
	require.NoError(t, svc.Send(ctx, SendVehicleCommand{
		TenantID: "tenant-b", VehicleID: "v-shared", CommandType: "E_STOP", IssuedBy: "operator-b",
	}))
	status, reason := commandTestVehicleState(t, db, "v-shared")
	assert.Equal(t, "available", status, "cross-tenant E_STOP must not block the other tenant vehicle")
	assert.Empty(t, reason)
	var loggedTenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM vehicle_commands WHERE vehicle_id = ?`, "v-shared").Scan(&loggedTenant))
	assert.Equal(t, "tenant-b", loggedTenant)

	// The owning tenant command does actuate its own vehicle and nothing else.
	require.NoError(t, svc.Send(ctx, SendVehicleCommand{
		TenantID: "tenant-a", VehicleID: "v-shared", CommandType: "E_STOP", IssuedBy: "operator-a",
	}))
	status, reason = commandTestVehicleState(t, db, "v-shared")
	assert.Equal(t, "blocked", status)
	assert.Equal(t, "AV E-STOP Triggered", reason)
	otherStatus, _ := commandTestVehicleState(t, db, "v-other")
	assert.Equal(t, "available", otherStatus)
}

func TestVehicleCommandService_PersistsAcknowledgedCommand(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	svc := NewVehicleCommandService(db)
	ctx := context.Background()
	insertCommandTestVehicle(t, db, "v-persist", "tenant-a", "available", nil)

	require.NoError(t, svc.Send(ctx, SendVehicleCommand{
		TenantID: "tenant-a", VehicleID: "v-persist", CommandType: "HOLD_POSITION", IssuedBy: "Asha Rao",
	}))

	var (
		id, tenantID, vehicleID, commandType, status, issuedBy string
		createdAt                                              sql.NullTime
	)
	require.NoError(t, db.QueryRow(`SELECT id, tenant_id, vehicle_id, command_type, status, issued_by, created_at FROM vehicle_commands WHERE vehicle_id = ?`, "v-persist").
		Scan(&id, &tenantID, &vehicleID, &commandType, &status, &issuedBy, &createdAt))
	assert.True(t, strings.HasPrefix(id, "cmd-"), "legacy command id prefix")
	assert.Len(t, id, len("cmd-")+36, "legacy UUID suffix")
	assert.Equal(t, "tenant-a", tenantID)
	assert.Equal(t, "v-persist", vehicleID)
	assert.Equal(t, "HOLD_POSITION", commandType)
	assert.Equal(t, "acknowledged", status)
	assert.Equal(t, "Asha Rao", issuedBy)
	assert.True(t, createdAt.Valid)
}

func TestIsValidVehicleCommand(t *testing.T) {
	for _, command := range []string{"E_STOP", "HOLD_POSITION", "RESUME_MISSION", "SET_SPEED_LIMIT", "REROUTE", "RETURN_TO_BASE"} {
		assert.True(t, IsValidVehicleCommand(command), command)
	}
	for _, command := range []string{"", "invalid", "E-STOP", "e_stop", "RESTART"} {
		assert.False(t, IsValidVehicleCommand(command), command)
	}
}

func TestVehicleCommandService_RejectsInvalidCommand(t *testing.T) {
	db := openVehicleDetailTestDB(t)
	svc := NewVehicleCommandService(db)
	insertCommandTestVehicle(t, db, "v-invalid", "tenant-a", "available", nil)

	err := svc.Send(context.Background(), SendVehicleCommand{
		TenantID: "tenant-a", VehicleID: "v-invalid", CommandType: "invalid", IssuedBy: "op",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidVehicleCommand))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vehicle_commands`).Scan(&count))
	assert.Zero(t, count, "an invalid command must never be persisted")
	status, _ := commandTestVehicleState(t, db, "v-invalid")
	assert.Equal(t, "available", status)
}

func TestVehicleCommandService_StoreErrorsSurface(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		db := openVehicleDetailTestDB(t)
		svc := NewVehicleCommandService(db)
		insertCommandTestVehicle(t, db, "v-closed", "tenant-a", "available", nil)
		require.NoError(t, db.Close())
		err := svc.Send(context.Background(), SendVehicleCommand{
			TenantID: "tenant-a", VehicleID: "v-closed", CommandType: "HOLD_POSITION", IssuedBy: "op",
		})
		require.Error(t, err)
	})

	t.Run("nil database", func(t *testing.T) {
		svc := NewVehicleCommandService(nil)
		err := svc.Send(context.Background(), SendVehicleCommand{
			TenantID: "tenant-a", VehicleID: "v", CommandType: "HOLD_POSITION", IssuedBy: "op",
		})
		require.Error(t, err)
	})
}
