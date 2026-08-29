package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

// setupSequencesTestDB creates an in-memory SQLite DB with all migrations
// applied, so the invoice_sequences table from 00048 exists.
func setupSequencesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_sequences_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../../db/migrations"))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
		('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'), ('tenant-loop','Test Tenant Loop','tenant-loop'),
		('tenant-1','Test Tenant 1','tenant-1'), ('1','Default','default')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestFinancialYear proves the Indian April–March financial year label used as
// the invoice sequence partition key: March closes the old FY, April opens the
// new one. The second component is a two-digit year (2027 → "27"), so century
// boundaries must pad correctly.
func TestFinancialYear(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"last day of march closes previous FY", time.Date(2026, time.March, 31, 23, 59, 59, 0, time.UTC), "2025-26"},
		{"first day of april opens new FY", time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), "2026-27"},
		{"mid-year august stays in current FY", time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC), "2026-27"},
		{"january belongs to previous FY", time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), "2025-26"},
		{"december 2099 sits in FY apr2099-mar2100", time.Date(2099, time.December, 31, 0, 0, 0, 0, time.UTC), "2099-00"},
		{"year 2100 april renders 2100-01", time.Date(2100, time.April, 1, 0, 0, 0, 0, time.UTC), "2100-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FinancialYear(tc.at))
		})
	}
}

// TestNextInvoiceNumber_IncrementsSequentially proves gap-free 1,2,3 numbering
// for one tenant — the core GST sequential-invoice requirement.
func TestNextInvoiceNumber_IncrementsSequentially(t *testing.T) {
	dbConn := setupSequencesTestDB(t)
	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-seq")

	fy := FinancialYear(time.Now())
	for want := int64(1); want <= 3; want++ {
		num, err := repo.NextInvoiceNumber(ctx, "tenant-seq", "INV")
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("INV/%s/%04d", fy, want), num)
	}

	var lastNumber int64
	var storedFY string
	require.NoError(t, dbConn.QueryRow(
		`SELECT last_number, financial_year FROM invoice_sequences WHERE tenant_id = ?`, "tenant-seq",
	).Scan(&lastNumber, &storedFY))
	assert.Equal(t, int64(3), lastNumber)
	assert.Equal(t, fy, storedFY)
}

// TestNextInvoiceNumber_TenantIsolation proves two tenants keep independent
// counters on the same financial-year row key.
func TestNextInvoiceNumber_TenantIsolation(t *testing.T) {
	dbConn := setupSequencesTestDB(t)
	repo := NewRepository(dbConn)
	ctxA := shared.ContextWithTenantID(context.Background(), "tenant-A")
	ctxB := shared.ContextWithTenantID(context.Background(), "tenant-B")

	for i := 1; i <= 2; i++ {
		numA, err := repo.NextInvoiceNumber(ctxA, "tenant-A", "INV")
		require.NoError(t, err)
		assert.Contains(t, numA, fmt.Sprintf("/%04d", i), "tenant A counter must advance independently")

		numB, err := repo.NextInvoiceNumber(ctxB, "tenant-B", "INV")
		require.NoError(t, err)
		assert.Contains(t, numB, fmt.Sprintf("/%04d", i), "tenant B must start at 1 regardless of tenant A")
	}

	var seqRows int
	require.NoError(t, dbConn.QueryRow(
		`SELECT COUNT(*) FROM invoice_sequences WHERE financial_year = ?`, FinancialYear(time.Now()),
	).Scan(&seqRows))
	assert.Equal(t, 2, seqRows, "each tenant gets its own sequence row")
}

// TestNextInvoiceNumber_FormatAndLength proves GST compliance: the number is
// ≤16 characters and matches {prefix}/{FY}/{seq:04d}.
func TestNextInvoiceNumber_FormatAndLength(t *testing.T) {
	dbConn := setupSequencesTestDB(t)
	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-fmt")

	pattern := regexp.MustCompile(`^INV/\d{4}-\d{2}/\d{4}$`)
	for i := 0; i < 5; i++ {
		num, err := repo.NextInvoiceNumber(ctx, "tenant-fmt", "INV")
		require.NoError(t, err)
		assert.LessOrEqual(t, len(num), 16, "GST caps invoice numbers at 16 chars: %q", num)
		assert.True(t, pattern.MatchString(num), "number %q must match INV/FY/seq pattern", num)
	}
}

// TestNextInvoiceNumber_SequentialLoop hammers the allocator in a tight loop
// to prove strictly increasing, duplicate-free allocation (SQLite serialises
// writers, so the upsert+RETURNING is concurrency-safe).
func TestNextInvoiceNumber_SequentialLoop(t *testing.T) {
	dbConn := setupSequencesTestDB(t)
	repo := NewRepository(dbConn)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-loop")

	prev := 0
	seen := make(map[int]bool)
	for i := 0; i < 50; i++ {
		num, err := repo.NextInvoiceNumber(ctx, "tenant-loop", "INV")
		require.NoError(t, err)
		seqStr := num[strings.LastIndex(num, "/")+1:]
		seq, err := strconv.Atoi(seqStr)
		require.NoError(t, err)
		assert.Equal(t, prev+1, seq, "sequence must be gap-free in loop iteration %d", i)
		require.False(t, seen[seq], "duplicate sequence %d", seq)
		seen[seq] = true
		prev = seq
	}
}
