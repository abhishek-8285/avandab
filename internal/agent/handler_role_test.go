package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"transport-app/internal/auth"
)

// TestHandleChat_DenyNonStaffRoles pins the role allowlist fix: the agent's
// read tools expose customer PII, revenue and unpaid invoices, so anything
// outside the staff allowlist — driver (mobile API accounts), viewer, or an
// unknown role — must be rejected before any tool runs.
func TestHandleChat_DenyNonStaffRoles(t *testing.T) {
	h := &Handler{} // orch/env must never be touched when the role check rejects
	for _, role := range []string{"driver", "viewer", "", "superuser"} {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/chat", nil)
		ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "u1", Role: role})
		rec := httptest.NewRecorder()
		h.handleChat(rec, req.WithContext(ctx))
		if rec.Code != http.StatusForbidden {
			t.Errorf("role %q: got %d, want 403", role, rec.Code)
		}
	}
}

func TestHandleChat_RequiresSession(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/chat", nil)
	rec := httptest.NewRecorder()
	h.handleChat(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: got %d, want 401", rec.Code)
	}
}
