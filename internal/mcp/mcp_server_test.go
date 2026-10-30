package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transport-app/internal/agent"
	"transport-app/internal/agent/rl"
	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

type mockPermChecker struct {
	allowed map[string]bool
}

func (m *mockPermChecker) Can(userID, resource, action string) bool {
	key := fmt.Sprintf("%s:%s:%s", userID, resource, action)
	return m.allowed[key]
}

func sessionCtx(r *http.Request, userID, role, name, tenant string) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
		UserID: userID,
		Role:   role,
		Name:   name,
	})
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenant))
	return r.WithContext(ctx)
}

func TestServer_Initialize(t *testing.T) {
	s := NewServer(ServerConfig{
		Tools: []*agent.RegisteredTool{
			{Name: "sample_tool", Description: "A sample tool", Parameters: map[string]any{"type": "object"}},
		},
	})

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if resMap["protocolVersion"] != ProtocolVersion {
		t.Errorf("expected protocolVersion %s, got %v", ProtocolVersion, resMap["protocolVersion"])
	}
	serverInfo, ok := resMap["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "avandab-fleet-mcp" {
		t.Errorf("expected server name avandab-fleet-mcp, got %v", serverInfo)
	}
}

func TestServer_NotificationsInitialized(t *testing.T) {
	s := NewServer(ServerConfig{})

	reqBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for notification, got %d", w.Code)
	}
}

func TestServer_Ping(t *testing.T) {
	s := NewServer(ServerConfig{})

	reqBody := `{"jsonrpc":"2.0","id":"ping-1","method":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp.ID != "ping-1" {
		t.Errorf("expected id ping-1, got %v", resp.ID)
	}
}

func TestServer_ToolsList(t *testing.T) {
	tool1 := &agent.RegisteredTool{
		Name:        "get_quote",
		Description: "Calculate quote",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"origin": map[string]any{"type": "string"},
			},
		},
	}
	s := NewServer(ServerConfig{
		Tools: []*agent.RegisteredTool{tool1},
	})

	reqBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	resMap := resp.Result.(map[string]any)
	toolsList, ok := resMap["tools"].([]any)
	if !ok || len(toolsList) != 1 {
		t.Fatalf("expected 1 tool in list, got %v", resMap["tools"])
	}
	t0 := toolsList[0].(map[string]any)
	if t0["name"] != "get_quote" {
		t.Errorf("expected tool get_quote, got %v", t0["name"])
	}
}

func TestServer_ToolCall_ReadTool(t *testing.T) {
	called := false
	tool := &agent.RegisteredTool{
		Name:        "search_routes",
		Description: "Search routes",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			called = true
			return `[{"id":"rt-1","source":"Delhi","destination":"Jaipur"}]`, nil
		},
	}
	s := NewServer(ServerConfig{
		Tools: []*agent.RegisteredTool{tool},
	})

	reqBody := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_routes","arguments":{"query":"Delhi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "dispatcher", "Dispatcher", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected tool handler to be called")
	}

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	resMap := resp.Result.(map[string]any)
	if resMap["isError"] == true {
		t.Fatalf("expected isError=false, got true")
	}
	content := resMap["content"].([]any)
	c0 := content[0].(map[string]any)
	if !strings.Contains(c0["text"].(string), "Delhi") {
		t.Errorf("unexpected output text: %v", c0["text"])
	}
}

func TestServer_ToolCall_MutatingGated(t *testing.T) {
	rlDBPath := filepath.Join(t.TempDir(), "rl.db")
	rlSvc, err := rl.New(rlDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rlSvc.Close()

	approvalSvc := agent.NewApprovalService(rlSvc, &agent.ToolEnv{UserName: "Fallback"})

	executedImmediately := false
	mutatingTool := &agent.RegisteredTool{
		Name:        "create_booking",
		Description: "Create a booking",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			executedImmediately = true
			return "booking created", nil
		},
	}
	approvalSvc.Gate("create_booking", mutatingTool.Handler)

	s := NewServer(ServerConfig{
		Tools:           []*agent.RegisteredTool{mutatingTool},
		ApprovalService: approvalSvc,
		RequireApproval: true,
	})

	reqBody := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_booking","arguments":{"customer_id":"cust-101","price":"5000"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin User", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if executedImmediately {
		t.Fatal("mutating tool was executed immediately! It must be approval-gated")
	}

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	resMap := resp.Result.(map[string]any)
	content := resMap["content"].([]any)
	c0 := content[0].(map[string]any)
	text := c0["text"].(string)

	if !strings.Contains(text, "submitted for admin approval") {
		t.Fatalf("expected approval confirmation, got: %s", text)
	}

	// Verify action exists in pending approval queue
	pending, err := approvalSvc.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending action, got %d", len(pending))
	}
	if pending[0].ToolName != "create_booking" {
		t.Errorf("expected pending tool create_booking, got %s", pending[0].ToolName)
	}
}

func TestServer_ToolCall_MutatingFailClosed(t *testing.T) {
	mutatingTool := &agent.RegisteredTool{
		Name:        "record_payment",
		Description: "Record payment",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "payment recorded", nil
		},
	}

	// Approval is required, but ApprovalService is nil -> must fail closed (omit mutating tools)
	s := NewServer(ServerConfig{
		Tools:           []*agent.RegisteredTool{mutatingTool},
		ApprovalService: nil,
		RequireApproval: true,
	})

	// 1. Tool list must be empty
	if len(s.Tools()) != 0 {
		t.Fatalf("expected 0 tools registered, got %d", len(s.Tools()))
	}

	// 2. Direct call must fail with tool not found
	reqBody := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"record_payment","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	resMap := resp.Result.(map[string]any)
	if resMap["isError"] != true {
		t.Fatalf("expected isError=true for unlisted mutating tool")
	}
}

func TestServer_ToolCall_RBACDenied(t *testing.T) {
	checker := &mockPermChecker{
		allowed: map[string]bool{
			"usr-1:routes:read": false, // denied
		},
	}

	tool := &agent.RegisteredTool{
		Name:        "search_routes",
		Description: "Search routes",
		Resource:    "routes",
		Action:      "read",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "routes ok", nil
		},
	}

	s := NewServer(ServerConfig{
		Tools:       []*agent.RegisteredTool{tool},
		PermChecker: checker,
	})

	reqBody := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"search_routes","arguments":{"query":"Delhi"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "dispatcher", "Dispatcher", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	resMap := resp.Result.(map[string]any)
	if resMap["isError"] != true {
		t.Fatalf("expected isError=true when Casbin permission is denied")
	}
	content := resMap["content"].([]any)
	c0 := content[0].(map[string]any)
	if !strings.Contains(c0["text"].(string), "permission denied") {
		t.Errorf("expected permission denied message, got: %s", c0["text"])
	}
}

func TestServer_MethodNotFound(t *testing.T) {
	s := NewServer(ServerConfig{})

	reqBody := `{"jsonrpc":"2.0","id":7,"method":"unsupported_method"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected error code %d, got %v", CodeMethodNotFound, resp.Error)
	}
}

func TestServer_BatchRequest(t *testing.T) {
	s := NewServer(ServerConfig{})

	reqBody := `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	var resps []Response
	if err := json.Unmarshal(w.Body.Bytes(), &resps); err != nil {
		t.Fatalf("unmarshal batch: %v", err)
	}
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
}

func TestServer_RoleEnforcement(t *testing.T) {
	s := NewServer(ServerConfig{})

	for _, role := range []string{"driver", "viewer", "guest"} {
		reqBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
		req = sessionCtx(req, "usr-x", role, "X", "org-tenant-1")
		w := httptest.NewRecorder()

		s.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("role %s: expected 403, got %d", role, w.Code)
		}
	}
}

func TestServer_MultiTenantIsolation(t *testing.T) {
	capturedTenant := ""
	tool := &agent.RegisteredTool{
		Name:        "get_tenant_fleet",
		Description: "Returns current tenant fleet",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			capturedTenant = string(shared.TenantIDFromContext(ctx))
			return "tenant:" + capturedTenant, nil
		},
	}

	s := NewServer(ServerConfig{
		Tools: []*agent.RegisteredTool{tool},
	})

	reqBody := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_tenant_fleet","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", bytes.NewBufferString(reqBody))
	req = sessionCtx(req, "usr-t99", "admin", "Tenant Admin", "tenant-alpha-99")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if capturedTenant != "tenant-alpha-99" {
		t.Fatalf("expected captured tenant 'tenant-alpha-99', got '%s'", capturedTenant)
	}
}

func TestServer_SSE_Transport(t *testing.T) {
	s := NewServer(ServerConfig{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = sessionCtx(r, "usr-1", "admin", "Admin", "org-tenant-1")
		s.ServeHTTP(w, r)
	}))
	defer ts.Close()

	// 1. Initiate GET with SSE header
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/mcp/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SSE connect, got %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)

	// Read initial endpoint event
	var endpointLine string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed reading endpoint event: %v", err)
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			endpointLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	if !strings.Contains(endpointLine, "sessionId=") {
		t.Fatalf("expected sessionId in endpoint, got: %s", endpointLine)
	}

	// 2. Send POST to the received endpoint with sessionId
	postURL := ts.URL + endpointLine
	postBody := `{"jsonrpc":"2.0","id":"sse-ping-1","method":"ping"}`
	postReq, err := http.NewRequest(http.MethodPost, postURL, bytes.NewBufferString(postBody))
	if err != nil {
		t.Fatal(err)
	}
	postReq.Header.Set("Content-Type", "application/json")

	postResp, err := client.Do(postReq)
	if err != nil {
		t.Fatal(err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted on POST to SSE session, got %d", postResp.StatusCode)
	}

	// 3. Read message event on the SSE stream
	done := make(chan string, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				done <- strings.TrimPrefix(line, "data: ")
				return
			}
		}
	}()

	select {
	case msg := <-done:
		if !strings.Contains(msg, "sse-ping-1") {
			t.Fatalf("expected response for sse-ping-1 in SSE event, got: %s", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE message event")
	}
}
