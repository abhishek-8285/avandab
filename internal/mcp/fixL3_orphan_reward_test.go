package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"transport-app/internal/agent"
	"transport-app/internal/agent/rl"
	"transport-app/internal/shared"
)

// MCP calls have no episode row. A synthetic "mcp-*" episode ID makes the
// approval decision write a reward against a nonexistent episode (orphan row).
func TestMCPActionDecisionLeavesNoOrphanReward(t *testing.T) {
	rlDBPath := filepath.Join(t.TempDir(), "rl.db")
	svc, err := rl.New(rlDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := agent.NewApprovalService(svc, &agent.ToolEnv{})
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "booking created", nil
	})
	mutating := &agent.RegisteredTool{
		Name:        "create_booking",
		Description: "Create a booking",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "booking created", nil
		},
	}
	s := NewServer(ServerConfig{
		Tools:           []*agent.RegisteredTool{mutating},
		ApprovalService: approval,
		RequireApproval: true,
	})

	reqBody := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create_booking","arguments":{"price":500}}}`
	req := sessionCtx(httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody)), "usr-1", "admin", "Admin", "tenant-a")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	pending, err := svc.ListPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending actions = %d, want 1", len(pending))
	}
	if pending[0].EpisodeID != "" {
		t.Fatalf("MCP action carries synthetic episode id %q with no episode row", pending[0].EpisodeID)
	}

	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-a"))
	if _, err := approval.Approve(ctx, pending[0].ID, "admin-1", "Admin"); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", rlDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_rewards`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan reward rows after MCP action decision = %d, want 0", count)
	}
}
