package errors

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/errors_test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
CREATE TABLE error_reports (
    id          TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    tenant_id   TEXT NOT NULL DEFAULT '1',
    user_id     TEXT,
    url         TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 0,
    severity    TEXT NOT NULL DEFAULT 'MEDIUM',
    message     TEXT NOT NULL,
    stack_trace TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    app_version TEXT NOT NULL DEFAULT '',
    request_id  TEXT NOT NULL DEFAULT '',
    metadata    TEXT NOT NULL DEFAULT '',
    occurrences INTEGER NOT NULL DEFAULT 1,
    first_seen  TEXT NOT NULL,
    last_seen   TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX uq_error_reports_fp_tenant ON error_reports (fingerprint, tenant_id);
CREATE INDEX idx_error_reports_tenant_sev ON error_reports (tenant_id, severity, last_seen);
CREATE TABLE incidents (
    id          TEXT PRIMARY KEY,
    error_id    TEXT NOT NULL REFERENCES error_reports(id),
    tenant_id   TEXT NOT NULL DEFAULT '1',
    status      TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','ASSIGNED','RESOLVED')),
    severity    TEXT NOT NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    root_cause  TEXT NOT NULL DEFAULT '',
    created     TEXT NOT NULL,
    resolved_at TEXT
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReporterDedupsByFingerprint(t *testing.T) {
	db := testDB(t)
	rep := NewReporter(nil, NewSQLiteStore(db), "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	base := ErrorReport{
		URL: "/api/v1/bookings", Method: "POST",
		Message: "insert failed: db locked\nat handler.go:10",
	}
	first, err := rep.Report(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rep.Report(ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	if second.ID != first.ID {
		t.Fatalf("expected same row id, got %s vs %s", first.ID, second.ID)
	}
	if second.Occurrences != 2 {
		t.Fatalf("occurrences = %d, want 2", second.Occurrences)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM error_reports`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rows = %d, want 1 (dedup failed)", n)
	}
}

func TestReporterSeparatesTenants(t *testing.T) {
	db := testDB(t)
	rep := NewReporter(nil, NewSQLiteStore(db), "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctxA := context.Background()
	ctxB := tenantCtx("7")

	r1, _ := rep.Report(ctxA, ErrorReport{URL: "/x", Method: "GET", Message: "boom"})
	r2, _ := rep.Report(ctxB, ErrorReport{URL: "/x", Method: "GET", Message: "boom"})

	if r1.TenantID != "1" || r2.TenantID != "7" {
		t.Fatalf("tenants = %q / %q", r1.TenantID, r2.TenantID)
	}
	if r1.Fingerprint == r2.Fingerprint {
		t.Fatal("fingerprints must differ across tenants")
	}

	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM error_reports`).Scan(&n)
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestReporterAutoCreatesSingleIncident(t *testing.T) {
	db := testDB(t)
	rep := NewReporter(nil, NewSQLiteStore(db), "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	crit := ErrorReport{URL: "/pay", Method: "POST", Message: "gateway down", Severity: SeverityCritical}
	got, err := rep.Report(ctx, crit)
	if err != nil {
		t.Fatal(err)
	}

	open, err := NewSQLiteStore(db).HasOpenIncident(ctx, got.Fingerprint, got.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Fatal("CRITICAL must auto-create an incident")
	}

	if _, err := rep.Report(ctx, crit); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM incidents`).Scan(&n)
	if n != 1 {
		t.Fatalf("incidents = %d, want 1 (no duplicates)", n)
	}
}

func TestResolveIncidentClosesOpenState(t *testing.T) {
	db := testDB(t)
	store := NewSQLiteStore(db)
	rep := NewReporter(nil, store, "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	got, _ := rep.Report(ctx, ErrorReport{
		URL: "/x", Method: "GET", Message: "high sev", Severity: SeverityHigh,
	})
	if ok, _ := store.HasOpenIncident(ctx, got.Fingerprint, got.TenantID); !ok {
		t.Fatal("HIGH should create incident")
	}

	incs, err := rep.ListIncidents(ctx, IncidentFilter{TenantID: "1", Status: "OPEN"})
	if err != nil || len(incs) != 1 {
		t.Fatalf("open incidents = %d err=%v", len(incs), err)
	}

	if err := rep.ResolveIncident(ctx, incs[0].ID, "1", "RESOLVED", "ops", "bad deploy"); err != nil {
		t.Fatal(err)
	}

	resolved, err := store.GetIncident(ctx, incs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != "RESOLVED" || resolved.ResolvedAt == nil || resolved.RootCause != "bad deploy" {
		t.Fatalf("incident not resolved properly: %+v", resolved)
	}
	if ok, _ := store.HasOpenIncident(ctx, got.Fingerprint, got.TenantID); ok {
		t.Fatal("resolved incident must not count as open")
	}

	if err := rep.ResolveIncident(ctx, "missing", "1", "RESOLVED", "", ""); err == nil {
		t.Fatal("resolving unknown incident must fail")
	}
}

func TestGetError(t *testing.T) {
	db := testDB(t)
	store := NewSQLiteStore(db)
	rep := NewReporter(nil, store, "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	got, err := rep.Report(ctx, ErrorReport{
		URL: "/pay", Method: "POST", Message: "gateway down\nsecond line",
		Severity: SeverityHigh, RequestID: "req-42", StackTrace: "at pay (handler.go:9)",
		Metadata: map[string]interface{}{"source": "server"},
	})
	if err != nil {
		t.Fatal(err)
	}

	detail, err := store.GetError(ctx, got.Fingerprint, "1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Message != "gateway down\nsecond line" || detail.StackTrace != "at pay (handler.go:9)" {
		t.Fatalf("detail mismatch: %+v", detail)
	}
	if detail.RequestID != "req-42" || detail.Occurrences != 1 || detail.Metadata["source"] != "server" {
		t.Fatalf("detail fields mismatch: %+v", detail)
	}

	// Wrong tenant must not see the group even with the right fingerprint.
	if _, err := store.GetError(ctx, got.Fingerprint, "7"); err != sql.ErrNoRows {
		t.Fatalf("cross-tenant GetError err = %v, want sql.ErrNoRows", err)
	}
	if _, err := store.GetError(ctx, "deadbeef", "1"); err != sql.ErrNoRows {
		t.Fatalf("unknown fingerprint err = %v, want sql.ErrNoRows", err)
	}
}

func TestListErrorsFilters(t *testing.T) {
	db := testDB(t)
	store := NewSQLiteStore(db)
	rep := NewReporter(nil, store, "test", "v1", id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	for _, sev := range []Severity{SeverityLow, SeverityMedium, SeverityCritical} {
		if _, err := rep.Report(ctx, ErrorReport{
			URL: "/f", Method: "GET", Message: "m-" + string(sev), Severity: sev,
		}); err != nil {
			t.Fatal(err)
		}
	}

	crits, err := rep.ListErrors(ctx, ErrorFilter{TenantID: "1", Severity: "CRITICAL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(crits) != 1 || crits[0].Severity != SeverityCritical {
		t.Fatalf("severity filter broken: %+v", crits)
	}

	all, _ := rep.ListErrors(ctx, ErrorFilter{TenantID: "1"})
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
	if all[0].Timestamp.Before(all[2].Timestamp) && all[0].Message == all[2].Message {
		t.Log("ordering by last_seen with identical timestamps is nondeterministic — acceptable")
	}

	count, err := rep.CountErrors(ctx, ErrorFilter{TenantID: "9"})
	if err != nil || count != 0 {
		t.Fatalf("tenant scoping broken: %d %v", count, err)
	}
}

func TestFirstLineAndFingerprintStability(t *testing.T) {
	if got := FirstLine("one\ntwo"); got != "one" {
		t.Fatalf("first line = %q", got)
	}
	a := Fingerprint("POST", "/u", "msg\nstack", "1")
	b := Fingerprint("POST", "/u", "msg\nother-stack", "1")
	if a != b {
		t.Fatal("stack lines beyond the first must not change fingerprint")
	}
	c := Fingerprint("GET", "/u", "msg", "1")
	if a == c {
		t.Fatal("method change must change fingerprint")
	}
}

func tenantCtx(id string) context.Context {
	return shared.ContextWithTenantID(context.Background(), shared.TenantID(id))
}
