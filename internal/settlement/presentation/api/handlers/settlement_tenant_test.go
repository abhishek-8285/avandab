package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// No resolved tenant → fail closed, even with a valid session. Previously
// fell back to tenant "1" and served another org's wallet.
func TestGetTenantAndUser_NoTenantFailsClosed(t *testing.T) {
	h := NewSettlementHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/drivers/me/wallet", nil)
	ctx := context.WithValue(context.Background(), auth.ContextUser, &auth.SessionData{UserID: "drv-1"})
	req = req.WithContext(ctx)

	_, _, ok := h.getTenantAndUser(req)
	assert.False(t, ok)
}

func TestGetTenantAndUser_TenantPreserved(t *testing.T) {
	h := NewSettlementHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/drivers/me/wallet", nil)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-9")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "drv-1"})
	req = req.WithContext(ctx)

	tenantID, userID, ok := h.getTenantAndUser(req)
	assert.True(t, ok)
	assert.Equal(t, "tenant-9", tenantID)
	assert.Equal(t, "drv-1", userID)
}
