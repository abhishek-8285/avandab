package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// get_open_alerts used to turn a DB failure into a user-facing string with a
// nil error, hiding outages from Agent.Run's error tracing.
func TestGetOpenAlertsReturnsDBError(t *testing.T) {
	db := newAgentTestDB(t)
	services := service.NewServices(sqlite.NewRepository(db), &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())

	var alertTool *RegisteredTool
	for _, tool := range RegisterTools(&ToolEnv{Services: services}) {
		if tool.Name == "get_open_alerts" {
			alertTool = tool
			break
		}
	}
	if alertTool == nil {
		t.Fatal("get_open_alerts tool must be registered")
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-a"))
	res, err := alertTool.Handler(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("DB failure was swallowed, tool returned %q with nil error", res)
	}
}
