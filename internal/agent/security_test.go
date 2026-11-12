package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"transport-app/internal/agent/rl"
	"transport-app/internal/auth"
)

// sessionCtx wraps a request with a session identity.
func sessionCtx(r *http.Request, userID, role, name string) *http.Request {
	ctx := context.WithValue(r.Context(), auth.ContextUser, &auth.SessionData{UserID: userID, Role: role, Name: name})
	return r.WithContext(ctx)
}

func TestApprovalRoutesRequireAdmin(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	approval := NewApprovalService(svc, &ToolEnv{})
	h := NewApprovalHandler(approval)

	for _, role := range []string{"viewer", "dispatcher", "accountant"} {
		req := httptest.NewRequest(http.MethodGet, "/api/agent/actions", nil)
		req = sessionCtx(req, "usr-x", role, "X")
		rr := httptest.NewRecorder()
		h.list(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("role %s: expected 403, got %d", role, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/actions", nil)
	req = sessionCtx(req, "usr-admin", "admin", "Admin")
	rr := httptest.NewRecorder()
	h.list(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("admin: expected 200, got %d", rr.Code)
	}
}

func TestApprovalRoutesRejectUnauthenticated(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	h := NewApprovalHandler(NewApprovalService(svc, &ToolEnv{}))

	req := httptest.NewRequest(http.MethodGet, "/api/agent/actions", nil)
	rr := httptest.NewRecorder()
	h.list(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session, got %d", rr.Code)
	}
}

func TestHandleChatBlocksViewer(t *testing.T) {
	h := NewHandler(nil, &ToolEnv{})
	req := httptest.NewRequest(http.MethodPost, "/assistant/chat", nil)
	req = sessionCtx(req, "usr-viewer", "viewer", "Viewer")
	rr := httptest.NewRecorder()
	h.handleChat(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("viewer: expected 403, got %d", rr.Code)
	}
}

func TestHandleChatRequiresAuth(t *testing.T) {
	h := NewHandler(nil, &ToolEnv{})
	req := httptest.NewRequest(http.MethodPost, "/assistant/chat", nil)
	rr := httptest.NewRecorder()
	h.handleChat(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session, got %d", rr.Code)
	}
}

func TestBuildAgentSetFailsClosedWithoutApprovalService(t *testing.T) {
	env := &ToolEnv{DB: nil}
	toolsByName := make(map[string]*RegisteredTool)
	for _, tl := range RegisterTools(env) {
		toolsByName[tl.Name] = tl
	}

	agents := BuildAgentSet(toolsByName, nil, AgentSetOptions{RequireApproval: true})
	byName := map[string][]string{}
	for _, sub := range agents {
		for _, tl := range sub.Tools {
			byName[sub.Name] = append(byName[sub.Name], tl.Name)
		}
	}
	// gated tools must be OMITTED entirely — never present ungated
	for _, sub := range agents {
		for _, name := range MutatingTools() {
			for _, tlName := range byName[sub.Name] {
				if tlName == name {
					t.Errorf("mutating tool %s present on agent %s without approval service (fail-open!)", name, sub.Name)
				}
			}
		}
	}
	// read-only tools must still work
	if len(byName["booking"]) == 0 {
		t.Error("booking agent should still have read-only tools")
	}
}

func TestGatedToolRecordsRequesterFromContext(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	approval := NewApprovalService(svc, &ToolEnv{UserName: "Fallback"})
	gated := approval.GatedTool(&RegisteredTool{Name: "record_payment"})

	ctx := context.WithValue(context.Background(), userNameCtxKey, "Dispatcher A")
	if _, err := gated.Handler(ctx, json.RawMessage(`{"amount":100}`)); err != nil {
		t.Fatal(err)
	}
	pending, err := svc.ListPendingActions()
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected 1 pending action, got %d (err=%v)", len(pending), err)
	}
	if pending[0].RequestedBy != "Dispatcher A" {
		t.Errorf("expected requester from context, got %q", pending[0].RequestedBy)
	}
}
