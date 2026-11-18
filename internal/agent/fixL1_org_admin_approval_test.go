package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/agent/rl"
	"transport-app/internal/shared"
)

// org_admin is the role self-serve tenants receive. They can submit mutations
// through chat, so they must also be able to decide their own approval queue.
func TestOrgAdminCanListAndApproveOwnTenantActions(t *testing.T) {
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
	if _, err := gated.Handler(ctxA, json.RawMessage(`{"price":7}`)); err != nil {
		t.Fatal(err)
	}

	h := NewApprovalHandler(approval)

	listReq := testTenantReq(http.MethodGet, "/api/agent/actions", "tenant-a", "oa-1", "org_admin", "Org Admin", "")
	listRR := httptest.NewRecorder()
	h.list(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("org_admin list status = %d, want 200: %s", listRR.Code, listRR.Body.String())
	}
	var body struct {
		Actions []rl.Action `json:"actions"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Actions) != 1 {
		t.Fatalf("org_admin sees %d actions, want 1", len(body.Actions))
	}
	id := body.Actions[0].ID

	approveReq := testTenantReq(http.MethodPost, "/api/agent/actions/"+id+"/approve", "tenant-a", "oa-1", "org_admin", "Org Admin", "")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	approveReq = approveReq.WithContext(context.WithValue(approveReq.Context(), chi.RouteCtxKey, rctx))
	approveRR := httptest.NewRecorder()
	h.approve(approveRR, approveReq)
	if approveRR.Code != http.StatusOK {
		t.Fatalf("org_admin approve status = %d, want 200: %s", approveRR.Code, approveRR.Body.String())
	}

	got, err := svc.GetAction(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != rl.ActionExecuted {
		t.Fatalf("action status = %s, want executed", got.Status)
	}
}
