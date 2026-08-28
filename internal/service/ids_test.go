package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// setupIDsTestStore builds a real SQLRepository over an in-memory SQLite DB
// with all migrations applied, so generateInvoiceNumber exercises the real
// invoice_sequences allocator.
func setupIDsTestStore(t *testing.T) Store {
	t.Helper()
	name := fmt.Sprintf("test_ids_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = db.Close() })
	return sqlite.NewRepository(db)
}

// failingSeqStore overrides only NextInvoiceNumber to simulate a sequence
// allocation failure; everything else delegates to the embedded Store.
type failingSeqStore struct {
	Store
}

func (f *failingSeqStore) NextInvoiceNumber(ctx context.Context, tenantID string, prefix string) (string, error) {
	return "", errors.New("sequence table unavailable")
}

// TestGenerateInvoiceNumber_GSTCompliant proves the generated number is
// sequential, ≤16 characters, and matches {prefix}/{FY}/{seq:04d} — e.g.
// "INV/2026-27/0001".
func TestGenerateInvoiceNumber_GSTCompliant(t *testing.T) {
	bs := baseService{store: setupIDsTestStore(t), log: slog.Default()}
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	pattern := regexp.MustCompile(`^INV/\d{4}-\d{2}/\d{4}$`)
	prev := 0
	for i := 0; i < 3; i++ {
		num := bs.generateInvoiceNumber(ctx)
		assert.LessOrEqual(t, len(num), 16, "GST caps invoice numbers at 16 chars: %q", num)
		require.True(t, pattern.MatchString(num), "number %q must match INV/FY/seq", num)

		seq, err := strconv.Atoi(num[strings.LastIndex(num, "/")+1:])
		require.NoError(t, err)
		assert.Equal(t, prev+1, seq, "invoice numbers must be strictly sequential")
		prev = seq
	}
}

// TestGenerateInvoiceNumber_FallsBackOnSequenceError proves the legacy random
// scheme is used only when sequence allocation fails — and the number still
// carries the configured prefix.
func TestGenerateInvoiceNumber_FallsBackOnSequenceError(t *testing.T) {
	realStore := setupIDsTestStore(t)
	bs := baseService{store: &failingSeqStore{Store: realStore}, log: slog.New(slog.DiscardHandler)}
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	fallbackPattern := regexp.MustCompile(`^INV-[0-9a-f]{8}$`)
	num := bs.generateInvoiceNumber(ctx)
	assert.True(t, fallbackPattern.MatchString(num), "fallback %q must use legacy INV-{uuid8} scheme", num)
}
