package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRAGSearchTool_EnforcesRESTPermission: the support sub-agent tool must
// carry the same rag:read gate as the knowledge-base REST endpoint.
func TestRAGSearchTool_EnforcesRESTPermission(t *testing.T) {
	tool := ragSearchTool(nil)
	if tool.Resource != "rag" || tool.Action != "read" {
		t.Fatalf("knowledge_search RBAC = %s:%s, want rag:read", tool.Resource, tool.Action)
	}
	ctx := WithPermissionChecker(context.Background(), denyPermissionChecker{})
	_, err := tool.Execute(ctx, json.RawMessage(`{"query":"cancellation policy"}`))
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("denied caller error = %v, want permission denied", err)
	}
}
