package application

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared/id"
)

func newDSARService(t *testing.T, db *sql.DB) *DSARService {
	t.Helper()
	return NewDSARService(db, id.NewUUIDGenerator())
}

func TestDSARService_Export_EndToEnd(t *testing.T) {
	ctx := context.Background()
	db := newPrivacyTestDB(t)
	svc := newDSARService(t, db)

	tenantID := "tenant_dsar_1"
	seedPrivacyTenant(t, db, tenantID)

	userID := "usr_dsar_test"
	userEmail := "rahul.sharma@example.com"
	userPhone := "9876543210"

	// 1. Seed user
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, tenant_id, name, email, phone, password_hash, role_id, status, timezone, theme_preference)
		VALUES ($1, $2, 'Rahul Sharma', $3, $4, 'hash123', 2, 'active', 'Asia/Kolkata', 'system')`,
		userID, tenantID, userEmail, userPhone)
	require.NoError(t, err)

	// 2. Seed consent
	_, err = db.ExecContext(ctx, `
		INSERT INTO user_consents (id, tenant_id, user_id, purpose, notice_version, granted_at)
		VALUES ('consent_1', $1, $2, 'platform_use', 'v1', CURRENT_TIMESTAMP)`,
		tenantID, userID)
	require.NoError(t, err)

	// 3. Seed driver
	driverID := "drv_dsar_1"
	_, err = db.ExecContext(ctx, `
		INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, email, license_number, aadhaar, pan, bank_details, status)
		VALUES ($1, $2, 'DRV001', 'Rahul', 'Sharma', $3, $4, 'DL-MH-1234567890', '987654321098', 'ABCDE1234F', '1234567890123456', 'available')`,
		driverID, tenantID, userPhone, userEmail)
	require.NoError(t, err)

	// 4. Seed vehicle & assignment
	vehID := "veh_dsar_1"
	_, err = db.ExecContext(ctx, `
		INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, status)
		VALUES ($1, $2, 'MH12AB1234', 'MH12AB1234', 'truck', 25000, 'available')`,
		vehID, tenantID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO driver_preferred_vehicles (id, tenant_id, driver_id, vehicle_id, is_primary, assigned_at)
		VALUES ('dpv_1', $1, $2, $3, 1, CURRENT_TIMESTAMP)`,
		tenantID, driverID, vehID)
	require.NoError(t, err)

	// 5. Seed driver expense
	_, err = db.ExecContext(ctx, `
		INSERT INTO driver_expenses (id, tenant_id, driver_id, expense_type, category, amount, description, status)
		VALUES ('exp_1', $1, $2, 'toll', 'toll', 500.0, 'Toll payment at tollway', 'pending')`,
		tenantID, driverID)
	require.NoError(t, err)

	// 6. Seed audit log
	_, err = db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, user_id, action, table_name, record_id, created_at)
		VALUES ('aud_1', $1, $2, 'login.success', 'users', $2, CURRENT_TIMESTAMP)`,
		tenantID, userID)
	require.NoError(t, err)

	// Act: Run ExportDataPrincipal
	bundle, err := svc.ExportDataPrincipal(ctx, tenantID, userID, userID)
	require.NoError(t, err)
	require.NotNil(t, bundle)

	// Assert: Check bundle structure
	assert.Equal(t, tenantID, bundle.TenantID)
	assert.Equal(t, userID, bundle.RequestedBy)
	assert.Equal(t, "Rahul Sharma", bundle.User.Name)
	assert.Equal(t, userEmail, bundle.User.Email)
	assert.Equal(t, userPhone, bundle.User.Phone)

	require.Len(t, bundle.Consents, 1)
	assert.Equal(t, "platform_use", bundle.Consents[0].Purpose)

	require.NotNil(t, bundle.Driver)
	assert.Equal(t, "Rahul", bundle.Driver.FirstName)
	assert.Equal(t, "•••• •••• 1098", bundle.Driver.AadhaarMasked, "Aadhaar must be masked in DSAR bundle")
	assert.Equal(t, "••••••234F", bundle.Driver.PANMasked, "PAN must be masked in DSAR bundle")
	assert.Equal(t, "••••••••3456", bundle.Driver.BankDetailsMasked, "Bank details must be masked in DSAR bundle")

	require.Len(t, bundle.Assignments, 1)
	assert.Equal(t, vehID, bundle.Assignments[0].VehicleID)
	assert.True(t, bundle.Assignments[0].IsPrimary)

	require.Len(t, bundle.Expenses, 1)
	assert.Equal(t, 500.0, bundle.Expenses[0].Amount)

	require.NotEmpty(t, bundle.AuditHistory)
	assert.Equal(t, "login.success", bundle.AuditHistory[0].Action)

	require.NotEmpty(t, bundle.RetentionSchedule)
	require.NotEmpty(t, bundle.SubProcessors)

	// Verify DSAR request row was recorded
	var dsarCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM privacy_dsar_requests WHERE tenant_id = $1 AND user_id = $2 AND request_type = 'export'`,
		tenantID, userID).Scan(&dsarCount)
	require.NoError(t, err)
	assert.Equal(t, 1, dsarCount, "DSAR export must be logged in privacy_dsar_requests")
}

func TestDSARService_Erase_EndToEnd(t *testing.T) {
	ctx := context.Background()
	db := newPrivacyTestDB(t)
	svc := newDSARService(t, db)

	tenantID := "tenant_dsar_2"
	seedPrivacyTenant(t, db, tenantID)

	userID := "usr_erase_test"
	userEmail := "vikram.singh@example.com"
	userPhone := "9123456789"

	// 1. Seed user
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, tenant_id, name, email, phone, password_hash, role_id, status)
		VALUES ($1, $2, 'Vikram Singh', $3, $4, 'secure_hash_123', 2, 'active')`,
		userID, tenantID, userEmail, userPhone)
	require.NoError(t, err)

	// 2. Seed active session
	_, err = db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES ('sess_erase_1', $1, 'tokenhash123', datetime('now', '+1 hour'))`,
		userID)
	require.NoError(t, err)

	// 3. Seed consent
	_, err = db.ExecContext(ctx, `
		INSERT INTO user_consents (id, tenant_id, user_id, purpose, notice_version, granted_at)
		VALUES ('consent_erase_1', $1, $2, 'platform_use', 'v1', CURRENT_TIMESTAMP)`,
		tenantID, userID)
	require.NoError(t, err)

	// 4. Seed driver
	driverID := "drv_erase_1"
	_, err = db.ExecContext(ctx, `
		INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, email, license_number, emergency_contact_name, status)
		VALUES ($1, $2, 'DRV002', 'Vikram', 'Singh', $3, $4, 'DL-DL-9876543210', 'Emergency Contact', 'available')`,
		driverID, tenantID, userPhone, userEmail)
	require.NoError(t, err)

	// Act: Run EraseDataPrincipal
	result, err := svc.EraseDataPrincipal(ctx, tenantID, userID, userID)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success)
	assert.Equal(t, userID, result.UserID)
	assert.NotEmpty(t, result.RequestID)
	assert.Contains(t, result.RetainedRecordsNotice, "CGST Act")

	// Verify User Table Anonymization
	var uName, uEmail, uStatus, uPass string
	var uPhone sql.NullString
	err = db.QueryRowContext(ctx, `SELECT name, email, phone, status, password_hash FROM users WHERE id = $1`, userID).Scan(
		&uName, &uEmail, &uPhone, &uStatus, &uPass)
	require.NoError(t, err)

	assert.Equal(t, "[ERASED DATA PRINCIPAL]", uName)
	assert.True(t, strings.HasPrefix(uEmail, "erased-"))
	assert.True(t, strings.HasSuffix(uEmail, "@redacted.local"))
	assert.False(t, uPhone.Valid, "User phone must be set to NULL")
	assert.Equal(t, "inactive", uStatus)
	assert.Equal(t, "*ERASED*", uPass)

	// Verify sessions deleted
	var sessCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = $1`, userID).Scan(&sessCount)
	require.NoError(t, err)
	assert.Equal(t, 0, sessCount, "All active sessions must be deleted")

	// Verify consent marked withdrawn
	var withdrawnAt sql.NullTime
	err = db.QueryRowContext(ctx, `SELECT withdrawn_at FROM user_consents WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID).Scan(&withdrawnAt)
	require.NoError(t, err)
	assert.True(t, withdrawnAt.Valid, "Consent must be stamped withdrawn_at")

	// Verify driver table anonymization
	var dFirst, dLast, dPhone, dLicense, dStatus string
	var dEmail, dEmContact sql.NullString
	err = db.QueryRowContext(ctx, `SELECT first_name, last_name, phone, email, license_number, emergency_contact_name, status FROM drivers WHERE id = $1`, driverID).Scan(
		&dFirst, &dLast, &dPhone, &dEmail, &dLicense, &dEmContact, &dStatus)
	require.NoError(t, err)

	assert.Equal(t, "[ERASED]", dFirst)
	assert.Empty(t, dLast)
	assert.Empty(t, dPhone)
	assert.False(t, dEmail.Valid)
	assert.Equal(t, "[ERASED]", dLicense)
	assert.False(t, dEmContact.Valid)
	assert.Equal(t, "inactive", dStatus)

	// Verify privacy_dsar_requests record
	var dsarType, dsarStatus string
	err = db.QueryRowContext(ctx, `SELECT request_type, status FROM privacy_dsar_requests WHERE id = $1`, result.RequestID).Scan(
		&dsarType, &dsarStatus)
	require.NoError(t, err)
	assert.Equal(t, "erasure", dsarType)
	assert.Equal(t, "completed", dsarStatus)

	// Verify audit_logs record
	var audCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE tenant_id = $1 AND action = 'privacy.dsar.erasure'`, tenantID).Scan(&audCount)
	require.NoError(t, err)
	assert.Equal(t, 1, audCount, "Erasure must be recorded in audit_logs")
}

func TestDSARService_FailClosed(t *testing.T) {
	ctx := context.Background()
	db := newPrivacyTestDB(t)
	svc := newDSARService(t, db)

	tenantA := "tenant_a"
	tenantB := "tenant_b"
	seedPrivacyTenant(t, db, tenantA)
	seedPrivacyTenant(t, db, tenantB)

	// Seed user in tenant A
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, tenant_id, name, email, password_hash)
		VALUES ('usr_tenant_a', $1, 'User A', 'usera@example.com', 'hash')`, tenantA)
	require.NoError(t, err)

	// Calling export for user A from tenant B must fail (ErrNoRows)
	_, err = svc.ExportDataPrincipal(ctx, tenantB, "usr_tenant_a", "usr_tenant_a")
	assert.ErrorIs(t, err, sql.ErrNoRows, "foreign tenant export must return sql.ErrNoRows")

	// Calling erase for user A from tenant B must fail (ErrNoRows)
	_, err = svc.EraseDataPrincipal(ctx, tenantB, "usr_tenant_a", "usr_tenant_a")
	assert.ErrorIs(t, err, sql.ErrNoRows, "foreign tenant erase must return sql.ErrNoRows")

	// Empty tenant ID must error
	_, err = svc.ExportDataPrincipal(ctx, "", "usr_tenant_a", "usr_tenant_a")
	assert.Error(t, err)
}
