package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func newComplianceAgg(now time.Time) *VehicleAggregate {
	return NewVehicleAggregate(
		"v1", shared.TenantID("t1"), "MH01AB1234", "VN-1",
		VehicleTypeTruck, 10, FuelTypeDiesel,
		now.Add(365*24*time.Hour), now.Add(365*24*time.Hour), now.Add(365*24*time.Hour),
		VehicleAvailable, nil, now,
	)
}

func TestVehicleAggregate_CanAssign_Valid(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	require.NoError(t, agg.CanAssign(now))
}

func TestVehicleAggregate_CanAssign_Blocked(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	agg.ApplyCompliance(true, "RC expired", nil, nil, 0, now)
	err := agg.CanAssign(now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Dispatch blocked")
}

func TestVehicleAggregate_CanAssign_BlockedStatus(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	agg.Status = VehicleBlocked
	err := agg.CanAssign(now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Dispatch blocked")
}

func TestVehicleAggregate_CanAssign_ExpiredRC(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	past := now.Add(-24 * time.Hour)
	agg.ApplyCompliance(false, "", &past, nil, 100, now)
	err := agg.CanAssign(now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RC expired")
}

func TestVehicleAggregate_CanAssign_ExpiredPUC(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	past := now.Add(-24 * time.Hour)
	agg.ApplyCompliance(false, "", nil, &past, 100, now)
	err := agg.CanAssign(now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PUC expired")
}

func TestVehicleAggregate_ApplyCompliance_RoundTrip(t *testing.T) {
	now := time.Now()
	agg := newComplianceAgg(now)
	rc := now.Add(30 * 24 * time.Hour)
	puc := now.Add(60 * 24 * time.Hour)
	agg.ApplyCompliance(false, "", &rc, &puc, 12345.5, now)
	assert.False(t, agg.Blocked)
	assert.Equal(t, 12345.5, agg.Odometer)
	require.NotNil(t, agg.RCExpiry)
	require.NotNil(t, agg.PUCExpiry)
	require.NoError(t, agg.CanAssign(now))
}
