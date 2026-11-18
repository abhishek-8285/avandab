package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"transport-app/internal/agent"
)

func TestServer_ToolErrorIsSanitized(t *testing.T) {
	tool := &agent.RegisteredTool{
		Name: "boom",
		Handler: func(context.Context, json.RawMessage) (string, error) {
			return "", errors.New("SELECT * FROM drivers WHERE tenant_id='1': SQLITE_BUSY\nconstraint users_email_key failed")
		},
	}
	s := NewServer(ServerConfig{Tools: []*agent.RegisteredTool{tool}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp",
		bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	res := resp.Result.(map[string]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, leak := range []string{"SELECT", "SQLITE_BUSY", "users_email_key", "\n"} {
		if strings.Contains(text, leak) {
			t.Errorf("tool error leaked %q to MCP client: %s", leak, text)
		}
	}
}

func TestServer_BatchAndBodyLimits(t *testing.T) {
	s := NewServer(ServerConfig{})

	batch := make([]string, 51)
	for i := range batch {
		batch[i] = `{"jsonrpc":"2.0","id":` + string(rune('0'+i%10)) + `,"method":"ping"}`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader("["+strings.Join(batch, ",")+"]"))
	req = sessionCtx(req, "usr-1", "admin", "Admin", "org-tenant-1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("oversized batch error = %+v, want CodeInvalidRequest", resp.Error)
	}

	big := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(strings.Repeat(" ", (1<<20)+16)))
	big = sessionCtx(big, "usr-1", "admin", "Admin", "org-tenant-1")
	bigRec := httptest.NewRecorder()
	s.ServeHTTP(bigRec, big)
	if bigRec.Code != http.StatusBadRequest || !strings.Contains(bigRec.Body.String(), "max 1 MiB") {
		t.Fatalf("oversized body = %d %s, want 400 with size error", bigRec.Code, bigRec.Body.String())
	}
}
