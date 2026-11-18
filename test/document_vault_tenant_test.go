package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestDocumentVault_TenantScopedListAndVerify: the list/verify APIs take only
// an entity id, so without a tenant predicate any org can read or approve
// another org's KYC documents.
func TestDocumentVault_TenantScopedListAndVerify(t *testing.T) {
	dbConn, svcs, _, _, _ := setupDocVaultEnv(t)
	defer dbConn.Close()
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	_, err := dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-doc-x', 'Doc X', 'tenant-doc-x')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-doc-x', 'DRV-X', 'X', 'Driver', '+919900000088', 'DL-X', '2029-01-01', 'available', 'tenant-doc-x')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO driver_documents (id, driver_id, doc_type, file_url, status)
		VALUES ('doc-x', 'drv-doc-x', 'aadhaar', '/uploads/x.pdf', 'pending_review')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('veh-doc-x', 'MH-99-XX-9999', 'MH99XX9999', 'truck', 6000, '2029-01-01', '2029-01-01', '2029-01-01', 'available', 'tenant-doc-x')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, status)
		VALUES ('vdoc-x', 'veh-doc-x', 'rc', '/uploads/vx.pdf', 'pending_review')`)
	require.NoError(t, err)

	driverDocs, err := svcs.Documents.ListDriverDocs(ctx, "drv-doc-x")
	require.NoError(t, err)
	assert.Empty(t, driverDocs)
	vehicleDocs, err := svcs.Documents.ListVehicleDocs(ctx, "veh-doc-x")
	require.NoError(t, err)
	assert.Empty(t, vehicleDocs)

	err = svcs.Documents.VerifyDocument(ctx, "driver", "drv-doc-x", "doc-x", "admin-1")
	assert.ErrorIs(t, err, service.ErrDocumentNotFound)
	err = svcs.Documents.VerifyDocument(ctx, "vehicle", "veh-doc-x", "vdoc-x", "admin-1")
	assert.ErrorIs(t, err, service.ErrDocumentNotFound)

	var status string
	require.NoError(t, dbConn.QueryRow(`SELECT status FROM driver_documents WHERE id='doc-x'`).Scan(&status))
	assert.Equal(t, "pending_review", status)
}
