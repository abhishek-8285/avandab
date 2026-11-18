package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// get_quote must apply the same per-tenant billing overlay as invoice creation:
// tenant billing.gst_enabled=false means zero tax, and a custom gst_rate wins
// over the global company_settings default.
func TestGetQuote_HonorsTenantGSTOverlay(t *testing.T) {
	db := newAgentTestDB(t)

	_, err := db.Exec(`INSERT INTO company_settings (id, company_name, gst_enabled, gst_rate) VALUES (1, 'Test Co', 1, 18)
		ON CONFLICT(id) DO UPDATE SET gst_enabled = excluded.gst_enabled, gst_rate = excluded.gst_rate`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, status) VALUES
		('quote-no-gst', 'No GST', 'active'), ('quote-custom-rate', 'Custom Rate', 'active')
		ON CONFLICT(id) DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, source_normalized, dest_normalized, distance, estimated_hours, standard_fare) VALUES
		('rout-no-gst', 'quote-no-gst', 'Mumbai', 'Pune', 'mumbai', 'pune', 150, 3, 1000),
		('rout-custom', 'quote-custom-rate', 'Mumbai', 'Pune', 'mumbai', 'pune', 150, 3, 1000)`)
	require.NoError(t, err)
	// Non-default tenants read billing defaults from tenant_company_profiles,
	// not the global company_settings singleton.
	_, err = db.Exec(`INSERT INTO tenant_company_profiles (tenant_id, company_name, gst_enabled, gst_rate) VALUES
		('quote-no-gst', 'No GST', 1, 18), ('quote-custom-rate', 'Custom Rate', 1, 18)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES
		('quote-no-gst', ?, 'false'),
		('quote-custom-rate', ?, 'true'),
		('quote-custom-rate', ?, '5')`,
		service.ConfigKeyGSTEnabled, service.ConfigKeyGSTEnabled, service.ConfigKeyGSTRate)
	require.NoError(t, err)

	services := service.NewServices(sqlite.NewRepository(db), &config.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	var quoteTool *RegisteredTool
	for _, tool := range RegisterTools(&ToolEnv{Services: services}) {
		if tool.Name == "get_quote" {
			quoteTool = tool
			break
		}
	}
	require.NotNil(t, quoteTool)

	call := func(tenant string) map[string]any {
		t.Helper()
		ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID(tenant))
		res, err := quoteTool.Handler(ctx, json.RawMessage(`{"from":"Mumbai","to":"Pune"}`))
		require.NoError(t, err)
		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(res), &out))
		return out
	}

	noGST := call("quote-no-gst")
	assert.Equal(t, float64(0), noGST["gst_amount"], "GST disabled for tenant must yield zero tax")
	assert.Equal(t, float64(1000), noGST["total_with_gst"])

	custom := call("quote-custom-rate")
	assert.Equal(t, float64(50), custom["gst_amount"], "tenant gst_rate override must win over company_settings")
	assert.Equal(t, float64(1050), custom["total_with_gst"])
}
