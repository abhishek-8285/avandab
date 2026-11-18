package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"transport-app/internal/agent/rl"
)

type denyPermissionChecker struct{}

func (denyPermissionChecker) Can(userID, resource, action string) bool { return false }

func TestApproveRechecksRBACBeforeExecution(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	executed := false
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		executed = true
		return "booking created", nil
	})

	// The gated tool carries the same RBAC tag as its REST counterpart.
	gated := approval.GatedTool(&RegisteredTool{
		Name:     "create_booking",
		Resource: "bookings",
		Action:   "create",
	})
	if _, err := gated.Handler(context.Background(), json.RawMessage(`{"price":100}`)); err != nil {
		t.Fatal(err)
	}

	pending, err := svc.ListPendingActions()
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected 1 pending action, got %d (err=%v)", len(pending), err)
	}

	// Admin whose Casbin policy denies bookings:create must not execute.
	ctx := WithPermissionChecker(context.Background(), denyPermissionChecker{})
	action, err := approval.Approve(ctx, pending[0].ID, "admin-denied", "Denied Admin")
	if err == nil {
		t.Fatalf("expected permission denied, got action %+v", action)
	}
	if executed {
		t.Fatal("mutation executed despite denied RBAC")
	}
}
