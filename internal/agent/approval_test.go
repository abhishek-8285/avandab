package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"transport-app/internal/agent/rl"
)

func TestGatedToolSubmitsAction(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	toolEnv := &ToolEnv{UserName: "Dispatcher"}
	approval := NewApprovalService(svc, toolEnv)
	executed := false
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		executed = true
		return "booking created", nil
	})

	gated := approval.GatedTool(&RegisteredTool{Name: "create_booking", Description: "x"})
	result, err := gated.Handler(context.Background(), json.RawMessage(`{"price":100}`))
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Error("gated tool must not execute immediately")
	}
	if !strings.Contains(result, "submitted for admin approval") {
		t.Errorf("expected submission message, got %q", result)
	}

	pending, _ := svc.ListPendingActions()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending action, got %d", len(pending))
	}
	if pending[0].ToolName != "create_booking" {
		t.Errorf("wrong tool: %s", pending[0].ToolName)
	}
	if pending[0].RequestedBy != "Dispatcher" {
		t.Errorf("wrong requester: %s", pending[0].RequestedBy)
	}
}

func TestApproveExecutesWithAdminIdentity(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	toolEnv := &ToolEnv{UserName: "Dispatcher", UserID: "usr-dispatcher"}
	approval := NewApprovalService(svc, toolEnv)

	var actedUser string
	approval.Gate("approve_kharcha", func(ctx context.Context, args json.RawMessage) (string, error) {
		actedUser = userIDFrom(ctx)
		return "approved", nil
	})

	// submit via the gated wrapper
	gated := approval.GatedTool(&RegisteredTool{Name: "approve_kharcha"})
	if _, err := gated.Handler(context.Background(), json.RawMessage(`{"expense_id":"exp-1"}`)); err != nil {
		t.Fatal(err)
	}
	pending, _ := svc.ListPendingActions()
	if len(pending) != 1 {
		t.Fatal("expected pending action")
	}

	action, err := approval.Approve(context.Background(), pending[0].ID, "usr-admin", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != rl.ActionExecuted {
		t.Errorf("expected executed, got %s", action.Status)
	}
	if actedUser != "usr-admin" {
		t.Errorf("expected admin identity during execution, got %q", actedUser)
	}
	if action.DecidedBy != "Admin" {
		t.Errorf("expected decided_by Admin, got %q", action.DecidedBy)
	}
}

func TestApproveRejectsNonPending(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})

	gated := approval.GatedTool(&RegisteredTool{Name: "create_booking"})
	if _, err := gated.Handler(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	// reject first
	action, err := approval.Reject(context.Background(), mustPending(t, svc), "Admin", "not needed")
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != rl.ActionRejected {
		t.Errorf("expected rejected, got %s", action.Status)
	}

	// second decision must fail
	if _, err := approval.Approve(context.Background(), action.ID, "Admin", "Admin"); err == nil {
		t.Error("expected error approving an already-decided action")
	}
}

func TestApproveExecutionFailure(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	approval.Gate("record_payment", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", json.Unmarshal(nil, (*struct{})(nil)) // force error
	})

	gated := approval.GatedTool(&RegisteredTool{Name: "record_payment"})
	if _, err := gated.Handler(context.Background(), json.RawMessage(`{"amount":50}`)); err != nil {
		t.Fatal(err)
	}

	action, err := approval.Approve(context.Background(), mustPending(t, svc), "usr-admin", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != rl.ActionFailed {
		t.Errorf("expected failed status, got %s", action.Status)
	}
	if action.Error == "" {
		t.Error("expected error recorded on action")
	}
}

func TestSummaryOf(t *testing.T) {
	s := summaryOf("create_booking", json.RawMessage(`{"customer_id":"cust_1","route_id":"rout_2","price":4500,"notes":"urgent"}`))
	if !strings.Contains(s, "customer_id=cust_1") || !strings.Contains(s, "price=4500") {
		t.Errorf("unexpected summary: %q", s)
	}
	if strings.Contains(s, "urgent") {
		t.Errorf("summary should skip non-key fields: %q", s)
	}
}

func mustPending(t *testing.T, svc *rl.Service) string {
	t.Helper()
	pending, err := svc.ListPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) == 0 {
		t.Fatal("no pending actions")
	}
	return pending[0].ID
}

func TestApproveNormalizesDoubleEncodedArgs(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	approval := NewApprovalService(svc, &ToolEnv{})
	var gotArgs json.RawMessage
	approval.Gate("create_booking", func(ctx context.Context, args json.RawMessage) (string, error) {
		gotArgs = args
		return "ok", nil
	})

	// OpenAI-style double encoding, as stored when submitted via chat loop.
	double := json.RawMessage(`"{\"price\":100}"`)
	gated := approval.GatedTool(&RegisteredTool{Name: "create_booking"})
	if _, err := gated.Handler(context.Background(), double); err != nil {
		t.Fatal(err)
	}
	action, err := approval.Approve(context.Background(), mustPending(t, svc), "usr-admin", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != rl.ActionExecuted {
		t.Fatalf("expected executed, got %s", action.Status)
	}
	if string(gotArgs) != `{"price":100}` {
		t.Errorf("approve path must normalize args like live path, got %s", string(gotArgs))
	}
}
