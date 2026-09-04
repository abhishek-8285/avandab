package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

// setupCompanyTenantTestDB builds an in-memory SQLite DB with all migrations
// applied (including 00125 tenant_company_profiles).
func setupCompanyTenantTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_company_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	conn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(conn, "../../../db/migrations"))
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestCompanySettings_TenantIsolation proves per-tenant settings: writes in
// one org never leak into another, rowless tenants get a blank profile (so
// onboarding triggers), and tenant-less contexts keep the legacy global row.
func TestCompanySettings_TenantIsolation(t *testing.T) {
	conn := setupCompanyTenantTestDB(t)
	repo := NewRepository(conn)

	_, err := conn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-a', 'A Fleet', 'a-fleet'), ('tenant-b', 'B Fleet', 'b-fleet')`)
	require.NoError(t, err)
	_, err = conn.Exec(`UPDATE company_settings SET company_name = 'GLOBAL', email = 'global@x.com' WHERE id = 1`)
	require.NoError(t, err)

	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-a")
	ctxB := shared.ContextWithTenantID(context.Background(), "tenant-b")
	ctxC := shared.ContextWithTenantID(context.Background(), "tenant-rowless")

	mkSettings := func(name, email string) domain.CompanySettings {
		return domain.CompanySettings{
			CompanyName: name, Currency: "INR", Timezone: "Asia/Kolkata",
			BookingPrefix: "BK", TripPrefix: "TR", InvoicePrefix: "INV",
			Address: strPtrTest(email + "-addr"), Phone: strPtrTest("9000000001"),
			Email: strPtrTest(email), StateCode: "27",
		}
	}

	_, err = repo.UpdateCompanySettings(ctxA, mkSettings("A Fleet", "a@x.com"))
	require.NoError(t, err)
	_, err = repo.UpdateCompanySettings(ctxB, mkSettings("B Fleet", "b@x.com"))
	require.NoError(t, err)

	gotA, err := repo.GetCompanySettings(ctxA)
	require.NoError(t, err)
	assert.Equal(t, "A Fleet", gotA.CompanyName)
	require.NotNil(t, gotA.Email)
	assert.Equal(t, "a@x.com", *gotA.Email)

	gotB, err := repo.GetCompanySettings(ctxB)
	require.NoError(t, err)
	assert.Equal(t, "B Fleet", gotB.CompanyName)
	require.NotNil(t, gotB.Email)
	assert.Equal(t, "b@x.com", *gotB.Email)

	// Overwrite A again — B must not move.
	_, err = repo.UpdateCompanySettings(ctxA, mkSettings("A Fleet v2", "a2@x.com"))
	require.NoError(t, err)
	gotB, err = repo.GetCompanySettings(ctxB)
	require.NoError(t, err)
	assert.Equal(t, "B Fleet", gotB.CompanyName)

	// Rowless tenant: blank identity (forces /company/onboard) over table
	// defaults so prefix/currency consumers keep working.
	gotC, err := repo.GetCompanySettings(ctxC)
	require.NoError(t, err)
	assert.Equal(t, "", gotC.CompanyName)
	assert.Equal(t, "INR", gotC.Currency)
	assert.Equal(t, "Asia/Kolkata", gotC.Timezone)
	assert.Equal(t, "BK", gotC.BookingPrefix)
	assert.Equal(t, "27", gotC.StateCode)

	// No tenant in context: legacy global singleton.
	gotG, err := repo.GetCompanySettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "GLOBAL", gotG.CompanyName)

	// Bootstrap tenant stays global-backed (no backfill row for '1').
	ctxBoot := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	gotBoot, err := repo.GetCompanySettings(ctxBoot)
	require.NoError(t, err)
	assert.Equal(t, "GLOBAL", gotBoot.CompanyName)

	// Bootstrap writes land in the global row, never in a profile row.
	_, err = repo.UpdateCompanySettings(ctxBoot, mkSettings("Boot Co", "boot@x.com"))
	require.NoError(t, err)
	gotG, err = repo.GetCompanySettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Boot Co", gotG.CompanyName)
	var profileCount int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM tenant_company_profiles WHERE tenant_id = '1'`).Scan(&profileCount))
	assert.Equal(t, 0, profileCount)
}

func strPtrTest(s string) *string { return &s }
