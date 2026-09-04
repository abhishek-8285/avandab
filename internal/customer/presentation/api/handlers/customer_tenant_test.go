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
// fell back to tenant "1" and served another org's data.
func TestGetTenantAndCustomer_NoTenantFailsClosed(t *testing.T) {
	h := NewCustomerHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/customer/bookings", nil)
	ctx := context.WithValue(context.Background(), auth.ContextUser, &auth.SessionData{UserID: "cust-1"})
	req = req.WithContext(ctx)

	_, _, ok := h.getTenantAndCustomer(req)
	assert.False(t, ok)
}

func TestGetTenantAndCustomer_TenantPreserved(t *testing.T) {
	h := NewCustomerHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/customer/bookings", nil)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-9")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "cust-1"})
	req = req.WithContext(ctx)

	tenantID, customerID, ok := h.getTenantAndCustomer(req)
	assert.True(t, ok)
	assert.Equal(t, "tenant-9", tenantID)
	assert.Equal(t, "cust-1", customerID)
}
