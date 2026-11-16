package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestKharcha_ApproveReject_RejectsSelfDecision proves the server-side
// segregation-of-duties guard: the driver who filed a claim cannot approve or
// reject it even if they somehow hold kharcha:approve. A manager still can.
func TestKharcha_ApproveReject_RejectsSelfDecision(t *testing.T) {
	dbConn, svcs := openMigratedDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	_, err := dbConn.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-self','DRV-SELF','Self','Driver','+919000000001','self@driver.test','DL-SELF','2030-01-01','available','tenant-1')`)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO users
		(id, email, password_hash, name, role_id, status, tenant_id)
		VALUES ('usr-driver','self@driver.test','hash','Self Driver',5,'active','tenant-1'),
		       ('usr-manager','manager@test.local','hash','Manager',1,'active','tenant-1')`)
	require.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO driver_expenses
		(id, driver_id, expense_type, category, amount, status, tenant_id)
		VALUES ('exp-self-approve','drv-self','toll','toll',100,'pending','tenant-1'),
		       ('exp-self-reject','drv-self','toll','toll',200,'pending','tenant-1')`)
	require.NoError(t, err)

	err = svcs.Kharcha.ApproveExpense(ctx, "exp-self-approve", "usr-driver")
	require.ErrorIs(t, err, service.ErrSelfApproval)
	var status string
	require.NoError(t, dbConn.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'exp-self-approve'`).Scan(&status))
	assert.Equal(t, "pending", status, "self-approval must not transition the row")

	err = svcs.Kharcha.RejectExpense(ctx, "exp-self-reject", "usr-driver", "self")
	require.ErrorIs(t, err, service.ErrSelfApproval)
	require.NoError(t, dbConn.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'exp-self-reject'`).Scan(&status))
	assert.Equal(t, "pending", status, "self-rejection must not transition the row")

	require.NoError(t, svcs.Kharcha.ApproveExpense(ctx, "exp-self-approve", "usr-manager"))
	require.NoError(t, dbConn.QueryRow(`SELECT status FROM driver_expenses WHERE id = 'exp-self-approve'`).Scan(&status))
	assert.Equal(t, "approved", status, "a different user must still be able to approve")
}
