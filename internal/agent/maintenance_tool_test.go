package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newAgentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_agent_maint_%d", time.Now().UnixNano())
	// NOTE: file-based, NOT in-memory. The get_open_alerts tool reaches the
	// alerts table through env.Services.DB(), which is a *different* logical
	// connection than the one migrations + seeding run on. SQLite's
	// in-memory + shared-cache mode gives each connection its own private
	// view of uncommitted/committed rows under WAL, so a tool query seeded
	// through the test handle silently saw zero rows. A file DSN shares one
	// on-disk database across both connections.
	db, err := sql.Open("sqlite", "file:"+name+".db?mode=rwc&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(name + ".db") })

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAgentTools_ListAvailableVehicles_ExcludesMaintenanceDue(t *testing.T) {
	db := newAgentTestDB(t)
	store := sqlite.NewRepository(db)
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewInMemoryBus()
	services := service.NewServices(store, cfg, logger, bus)

	// Seed 2 vehicles: veh-avail (clean) and veh-maint (maintenance_due set)
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES
		('veh-avail', 'REG-AVAIL', 'MH-01-AVAIL', 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), NULL, '1'),
		('veh-maint', 'REG-MAINT', 'MH-01-MAINT', 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '2026-08-19', '1')`)
	require.NoError(t, err)

	env := &ToolEnv{Services: services}
	tools := RegisterTools(env)

	var listTool *RegisteredTool
	for _, tool := range tools {
		if tool.Name == "list_available_vehicles" {
			listTool = tool
			break
		}
	}
	require.NotNil(t, listTool)

	ctx := shared.ContextWithTenantID(context.Background(), "1")
	res, err := listTool.Handler(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)

	// Result should contain veh-avail and NOT veh-maint
	assert.Contains(t, res, "veh-avail")
	assert.NotContains(t, res, "veh-maint")
}
