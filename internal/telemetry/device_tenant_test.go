package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeviceAssign_RejectsForeignTenantVehicle: a device bound to a vehicle in
// another org makes that org's telemetry appear under the caller's tenant.
func TestDeviceAssign_RejectsForeignTenantVehicle(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)

	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('2', 'Other Tenant', 'other-tenant')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v-foreign', '2', 'REG-F', 'MH-02-F', 'truck', 15, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`)
	require.NoError(t, err)

	_, err = svc.RegisterDevice(tenantCtx(), RegisterDeviceCommand{IMEI: "ACTX"})
	require.NoError(t, err)

	err = svc.AssignDevice(tenantCtx(), "ACTX", "v-foreign")
	require.Error(t, err, "assigning a foreign vehicle must fail")

	d, err := NewDeviceStore(db).GetByIMEI(tenantCtx(), "ACTX")
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, DeviceStatusInventory, d.Status)
	assert.Nil(t, d.VehicleID)
}
