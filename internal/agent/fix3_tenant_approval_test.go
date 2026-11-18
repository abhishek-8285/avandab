package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"transport-app/internal/agent/rl"
	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func testTenantReq(method, path, tenant, userID, role, name string, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: userID, Role: role, Name: name})
	return req.WithContext(ctx)
}

func TestApprovalQueueTenantIsolation(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "executed", nil
	})
	gated := approval.GatedTool(&RegisteredTool{Name: "create_booking", Resource: "bookings", Action: "create"})

	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")
	ctxB := shared.ContextWithTenantID(context.Background(), "tenant-b")
	if _, err := gated.Handler(ctxA, json.RawMessage(`{"price":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := gated.Handler(ctxB, json.RawMessage(`{"price":2}`)); err != nil {
		t.Fatal(err)
	}

	// Tenant A's admin lists the queue: must see only A's action.
	h := NewApprovalHandler(approval)
	req := testTenantReq(http.MethodGet, "/api/agent/actions", "tenant-a", "admin-a", "admin", "A", "")
	rr := httptest.NewRecorder()
	h.list(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rr.Code)
	}
	var body struct {
		Actions []rl.Action `json:"actions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Actions) != 1 {
		t.Fatalf("tenant A sees %d actions, want 1 (cross-tenant leak)", len(body.Actions))
	}
	if !strings.Contains(body.Actions[0].Summary, "price=1") {
		t.Fatalf("tenant A saw wrong action: %q", body.Actions[0].Summary)
	}
}

func TestApproveRejectsCrossTenantClaim(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	executed := false
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		executed = true
		return "executed", nil
	})
	gated := approval.GatedTool(&RegisteredTool{Name: "create_booking", Resource: "bookings", Action: "create"})

	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")
	ctxB := shared.ContextWithTenantID(context.Background(), "tenant-b")
	if _, err := gated.Handler(ctxA, json.RawMessage(`{"price":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := gated.Handler(ctxB, json.RawMessage(`{"price":2}`)); err != nil {
		t.Fatal(err)
	}

	pending, err := svc.ListPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	var idA string
	for _, a := range pending {
		if strings.Contains(a.Summary, "price=1") {
			idA = a.ID
		}
	}
	if idA == "" {
		t.Fatal("tenant A action not found")
	}

	if _, err := approval.Approve(ctxB, idA, "admin-b", "B"); err == nil {
		t.Fatal("tenant B admin claimed tenant A's action (cross-tenant approval)")
	}
	if executed {
		t.Fatal("tenant B executed tenant A's mutation")
	}
}
