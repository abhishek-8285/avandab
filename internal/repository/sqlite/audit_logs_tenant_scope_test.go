package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

func auditStrPtr(s string) *string { return &s }

// TestAuditLogs_TenantScope proves the org-isolation rule: the platform admin
// (role admin) sees every org's rows; org roles see only their own tenant;
// platform-level tenants-table rows never surface on an org feed; a
// tenant-less non-admin context fails closed.
func TestAuditLogs_TenantScope(t *testing.T) {
	dbConn := setupUsersTestDB(t)
	repo := NewRepository(dbConn)

	_, err := dbConn.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-a', 'A', 'a'), ('t-b', 'B', 'b')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES ('u-a', 'a@x.com', 'h', 'A User', 6, 'active', 't-a'),
		       ('u-b', 'b@x.com', 'h', 'B User', 6, 'active', 't-b'),
		       ('u-plat', 'plat@x.com', 'h', 'Admin User', 1, 'active', '1')`)
	require.NoError(t, err)

	orgACtx := context.WithValue(
		shared.ContextWithTenantID(context.Background(), "t-a"),
		auth.ContextUser, &auth.SessionData{UserID: "u-a", Role: string(domain.RoleOrgAdmin)})
	orgBCtx := context.WithValue(
		shared.ContextWithTenantID(context.Background(), "t-b"),
		auth.ContextUser, &auth.SessionData{UserID: "u-b", Role: string(domain.RoleOrgAdmin)})
	adminCtx := context.WithValue(
		shared.ContextWithTenantID(context.Background(), "1"),
		auth.ContextUser, &auth.SessionData{UserID: "u-plat", Role: string(domain.RoleAdmin)})

	uidA := domain.UserID("u-a")
	uidB := domain.UserID("u-b")
	uidPlat := domain.UserID("u-plat")
	// Writes carry no explicit tenant: attribution resolves from ctx.
	_, err = repo.CreateAuditLog(orgACtx, domain.AuditLog{ID: "scope-a1", UserID: &uidA, Action: "login", TableName: "users", RecordID: auditStrPtr("u-a")})
	require.NoError(t, err)
	_, err = repo.CreateAuditLog(orgBCtx, domain.AuditLog{ID: "scope-b1", UserID: &uidB, Action: "login", TableName: "users", RecordID: auditStrPtr("u-b")})
	require.NoError(t, err)
	// Platform-level row lands on the platform tenant.
	_, err = repo.CreateAuditLog(adminCtx, domain.AuditLog{ID: "scope-plat1", UserID: &uidPlat, Action: "tenant.suspend", TableName: "tenants", RecordID: auditStrPtr("t-b")})
	require.NoError(t, err)
	// Pre-auth style write (login: user but no request tenant) resolves via the actor's org.
	_, err = repo.CreateAuditLog(context.Background(), domain.AuditLog{ID: "scope-a2", UserID: &uidA, Action: "login", TableName: "users", RecordID: auditStrPtr("u-a")})
	require.NoError(t, err)

	// List: org A sees its two rows only; admin sees all four.
	logsA, err := repo.ListAuditLogs(orgACtx, 10, 0)
	require.NoError(t, err)
	require.Len(t, logsA, 2)
	for _, l := range logsA {
		assert.Equal(t, shared.TenantID("t-a"), l.TenantID)
		assert.NotEqual(t, "tenant.suspend", l.Action)
	}
	logsAdmin, err := repo.ListAuditLogs(adminCtx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, logsAdmin, 4)
	// Org B sees only its own row.
	logsB, err := repo.ListAuditLogs(orgBCtx, 10, 0)
	require.NoError(t, err)
	require.Len(t, logsB, 1)
	assert.Equal(t, "scope-b1", string(logsB[0].ID))

	// Tenant-less non-admin context fails closed.
	_, err = repo.ListAuditLogs(context.Background(), 10, 0)
	require.Error(t, err, "missing tenant must fail closed")

	// Filtered list + count follow the same scope.
	fA, totalA, err := repo.ListAuditLogsDateRange(orgACtx, "login", "", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, totalA)
	assert.Len(t, fA, 2)
	fAdmin, totalAdmin, err := repo.ListAuditLogsDateRange(adminCtx, "", "", "", 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 4, totalAdmin)
	assert.Len(t, fAdmin, 4)

	// Counts follow the same scope.
	countA, err := repo.CountAuditLogs(orgACtx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, countA)
	countAdmin, err := repo.CountAuditLogs(adminCtx)
	require.NoError(t, err)
	assert.EqualValues(t, 4, countAdmin)
	sinceA, err := repo.CountAuditLogsSince(orgACtx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 2, sinceA)
	sinceAdmin, err := repo.CountAuditLogsSince(adminCtx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 4, sinceAdmin)

	// By-record history is tenant-scoped too.
	byRecA, err := repo.GetAuditLogsByRecord(orgACtx, "users", "u-a", 10)
	require.NoError(t, err)
	assert.Len(t, byRecA, 2)
	byRecCross, err := repo.GetAuditLogsByRecord(orgACtx, "users", "u-b", 10)
	require.NoError(t, err)
	assert.Empty(t, byRecCross, "org A must not see org B record history")
	byRecAdmin, err := repo.GetAuditLogsByRecord(adminCtx, "users", "u-b", 10)
	require.NoError(t, err)
	assert.Len(t, byRecAdmin, 1)
}
