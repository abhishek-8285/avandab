package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"transport-app/internal/shared"
)

// Unresolvable tenant must come back empty so callers skip instead of
// filing the alert under the bootstrap tenant (alerts:read is held by
// every org's viewer/dispatcher).
func TestResolveTenantID_FailsClosed(t *testing.T) {
	e := &Engine{}

	assert.Equal(t, "", e.resolveTenantID(context.Background(), IngestEvent{}))

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-7")
	assert.Equal(t, "tenant-7", e.resolveTenantID(ctx, IngestEvent{}))

	assert.Equal(t, "tenant-8", e.resolveTenantID(context.Background(), IngestEvent{TenantID: "tenant-8"}))

	assert.Equal(t, "tenant-9", e.resolveTenantID(context.Background(),
		IngestEvent{Metadata: map[string]interface{}{"tenant_id": "tenant-9"}}))
}
