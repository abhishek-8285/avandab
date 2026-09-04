package features

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

func gateTestRequest(tenant string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if tenant != "" {
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	}
	return req
}

// Unresolved tenant must be denied, never served defaults or tenant "1".
func TestGate_NoTenantDenied(t *testing.T) {
	reg := testRegistry(t, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	rec := httptest.NewRecorder()
	Gate(reg, "fastag")(next).ServeHTTP(rec, gateTestRequest(""))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	Gate(reg, "ewaybill")(next).ServeHTTP(rec, gateTestRequest(""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Resolved tenant with explicit grant passes through.
func TestGate_GrantedTenantPasses(t *testing.T) {
	reg := testRegistry(t, nil)
	ctx := context.Background()
	require.NoError(t, reg.Set(ctx, "tenant-g", "fastag", true, "admin"))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	Gate(reg, "fastag")(next).ServeHTTP(rec, gateTestRequest("tenant-g"))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}
