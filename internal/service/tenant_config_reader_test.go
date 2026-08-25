package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// setupTenantConfigTestDB opens a fresh in-memory SQLite with every migration
// applied (mirrors setupIDsTestStore) and hands back the raw DB so tests can
// seed company_config rows directly.
func setupTenantConfigTestDB(t *testing.T) (*sql.DB, Store) {
	t.Helper()
	name := fmt.Sprintf("test_tenantcfg_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	return db, sqlite.NewRepository(db)
}

// seedTenantConfig inserts company_config rows for the overlay tests.
func seedTenantConfig(t *testing.T, db *sql.DB, tenantID, key, value string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT OR REPLACE INTO company_config (tenant_id, key, value) VALUES (?, ?, ?)`,
		tenantID, key, value)
	require.NoError(t, err)
}

// TestTenantConfigReader_OverlayFallsBackWhenRowMissing proves the chain
// tenant row → supplied company_settings default: 'acme' has overrides,
// 'beta' has no rows at all (migrations only ever seeded tenant '1').
func TestTenantConfigReader_OverlayFallsBackWhenRowMissing(t *testing.T) {
	db, _ := setupTenantConfigTestDB(t)
	reader := NewTenantConfigReader(db)
	ctx := context.Background()

	seedTenantConfig(t, db, "acme", ConfigKeyBookingPrefix, "AC")
	seedTenantConfig(t, db, "acme", ConfigKeyGSTRate, "18")
	seedTenantConfig(t, db, "acme", ConfigKeyGSTEnabled, "true")

	assert.Equal(t, "AC", reader.Overlay(ctx, "acme", ConfigKeyBookingPrefix, "BK"),
		"tenant override must win over the global default")
	assert.Equal(t, "BK", reader.Overlay(ctx, "beta", ConfigKeyBookingPrefix, "BK"),
		"missing rows must fall back to the global default")
	assert.Equal(t, "TR", reader.Overlay(ctx, "beta", ConfigKeyTripPrefix, "TR"))

	assert.True(t, reader.GetBool(ctx, "acme", ConfigKeyGSTEnabled, false))
	assert.True(t, reader.GetBool(ctx, "beta", ConfigKeyGSTEnabled, true),
		"missing rows must flow through the supplied company_settings default")
	assert.InDelta(t, 18.0, reader.GetFloat(ctx, "acme", ConfigKeyGSTRate, 0), 0.0001)
	assert.InDelta(t, 5.5, reader.GetFloat(ctx, "beta", ConfigKeyGSTRate, 5.5), 0.0001)

	seedTenantConfig(t, db, "acme", ConfigKeyTripPrefix, "")
	assert.Equal(t, "TR", reader.Overlay(ctx, "acme", ConfigKeyTripPrefix, "TR"),
		"blank override values must be treated as a miss")
}

// TestTenantConfigReader_ConcurrentTenantsNeverCrossValues hammers both tenants
// from concurrent goroutines while snapshots refresh — the sharded cache must
// never serve one tenant's row to the other (the single-snapshot geofence/fuel
// readers would fail this under contention).
func TestTenantConfigReader_ConcurrentTenantsNeverCrossValues(t *testing.T) {
	db, _ := setupTenantConfigTestDB(t)
	reader := NewTenantConfigReader(db)
	ctx := context.Background()

	seedTenantConfig(t, db, "acme", ConfigKeyBookingPrefix, "AC")

	const iterations = 200
	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)

	for _, tc := range []struct {
		tenantID string
		want     string
	}{
		{tenantID: "acme", want: "AC"},
		{tenantID: "beta", want: "BK"},
	} {
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func(tenantID, want string) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					if got := reader.Overlay(ctx, tenantID, ConfigKeyBookingPrefix, "BK"); got != want {
						errs <- fmt.Errorf("tenant %s got %q, want %q", tenantID, got, want)
						return
					}
					if rate := reader.GetFloat(ctx, tenantID, ConfigKeyGSTRate, 0); rate != 0 && rate != 18 {
						errs <- fmt.Errorf("tenant %s got gst_rate %v", tenantID, rate)
						return
					}
				}
			}(tc.tenantID, tc.want)
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	assert.Empty(t, errs, "no wrong-tenant value may ever leak across shards")
}

// TestTenantConfigReader_InvalidateBeatsTTL proves cached staleness semantics:
// within the TTL an updated row keeps serving the old value until
// Invalidate(tenantID) forces the next read to reload.
func TestTenantConfigReader_InvalidateBeatsTTL(t *testing.T) {
	db, _ := setupTenantConfigTestDB(t)
	reader := NewTenantConfigReader(db)
	ctx := context.Background()

	seedTenantConfig(t, db, "acme", ConfigKeyBookingPrefix, "AC")
	assert.Equal(t, "AC", reader.Overlay(ctx, "acme", ConfigKeyBookingPrefix, "BK"), "prime the cache")

	_, err := db.Exec(`UPDATE company_config SET value = 'ACME2' WHERE tenant_id = 'acme' AND key = ?`, ConfigKeyBookingPrefix)
	require.NoError(t, err)

	assert.Equal(t, "AC", reader.Overlay(ctx, "acme", ConfigKeyBookingPrefix, "BK"),
		"within TTL the stale snapshot must still be served")

	reader.Invalidate("acme")
	assert.Equal(t, "ACME2", reader.Overlay(ctx, "acme", ConfigKeyBookingPrefix, "BK"),
		"Invalidate must force an immediate reload")
}

// TestGenerateInvoiceNumber_TenantOverlaySequentialPerTenant is the ids.go
// integration proof: interleaved generations across two tenants each yield a
// gap-free subsequence, with 'acme' carrying its branding.invoice_prefix
// override ("AC") and row-less 'beta' falling back to the global default
// ("INV"). The invoice_sequences allocator stays untouched.
func TestGenerateInvoiceNumber_TenantOverlaySequentialPerTenant(t *testing.T) {
	db, store := setupTenantConfigTestDB(t)
	bs := baseService{
		store:     store,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		tenantCfg: NewTenantConfigReader(db),
	}
	ctxAcme := shared.ContextWithTenantID(context.Background(), shared.TenantID("acme"))
	ctxBeta := shared.ContextWithTenantID(context.Background(), shared.TenantID("beta"))

	seedTenantConfig(t, db, "acme", ConfigKeyInvoicePrefix, "AC")

	acmeNums := make([]string, 0, 4)
	betaNums := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		acmeNums = append(acmeNums, bs.generateInvoiceNumber(ctxAcme))
		betaNums = append(betaNums, bs.generateInvoiceNumber(ctxBeta))
	}

	seqOf := func(num string) int {
		t.Helper()
		n, err := strconv.Atoi(num[strings.LastIndex(num, "/")+1:])
		require.NoError(t, err)
		return n
	}
	for i, num := range acmeNums {
		assert.True(t, strings.HasPrefix(num, "AC/"), "acme number %q must use the tenant override prefix", num)
		assert.LessOrEqual(t, len(num), 16, "GST caps invoice numbers at 16 chars: %q", num)
		assert.Equal(t, i+1, seqOf(num), "acme sequence must stay gap-free across interleaving")
	}
	for i, num := range betaNums {
		assert.True(t, strings.HasPrefix(num, "INV/"), "beta number %q must fall back to the global prefix", num)
		assert.Equal(t, i+1, seqOf(num), "beta sequence must stay gap-free across interleaving")
	}
}
