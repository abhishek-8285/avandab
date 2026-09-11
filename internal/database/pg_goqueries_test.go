//go:build pg_integration

// Postgres Go-SQL gate: every static DML string literal in non-test,
// non-generated Go source (SELECT/INSERT/UPDATE/DELETE/WITH), rebound via
// database.Rebind, must PREPARE cleanly on a real PostgreSQL carrying the
// full ported chain. Dynamic queries (Sprintf %s, concatenation) and DDL
// are skipped — the former are covered by Rebind-at-execution, the latter
// cannot be PREPAREd. Run with:
//
//	DATABASE_URL="postgres://postgres:fix@localhost:5544/mvtms_pg?sslmode=disable" \
//	    go test -tags pg_integration ./internal/database/ -run TestPostgresGoSQL -v
package database_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appdb "transport-app/internal/database"
)

func TestPostgresGoSQL(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	goLitRe := regexp.MustCompile("(?s)`[^`]*`")
	dmlRe := regexp.MustCompile(`(?i)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	fromRe := regexp.MustCompile(`\bFROM\b`)
	dynamicR := regexp.MustCompile(`%[sdv]|%\d+d`)
	var files []string
	sidecar := func(p string) bool {
		// Own-database sidecars (never run on the main PG): skip.
		if strings.Contains(p, "/agent/rl/") || strings.Contains(p, "/rag/") {
			return true
		}
		// datamigrate is sqlite→PG tooling by design: sqlite_master probes
		// and concatenated DDL/DML that only run against the source/target
		// explicitly. Covered by its own pg_integration test instead.
		return strings.Contains(p, "/datamigrate/")
	}
	// All previously-dead queries were fixed (00127 session: trip_stops
	// pod_required/otp_required, company_settings logo_path, audit_logs
	// column mapping, eway_bills updated_at, payout verification_status,
	// behaviour metadata, controltower started_at/completed_at, customer
	// registration_number). No allowlist remains — every literal must pass.
	_ = filepath.Walk("../../internal", func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".go") &&
			!strings.HasSuffix(p, "_test.go") &&
			!strings.Contains(p, "/generated/") && !sidecar(p) {
			files = append(files, p)
		}
		return nil
	})
	_ = filepath.Walk("../../cmd", func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})

	s := &testSettings{driver: "postgres", url: url, maxOpen: 4, maxIdle: 2}
	db, err := appdb.Open(context.Background(), s, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	total, skipped, failed := 0, 0, 0
	prepN := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fOK, fFail := 0, 0
		for _, m := range goLitRe.FindAllString(string(raw), -1) {
			body := strings.TrimSpace(m[1 : len(m)-1])
			if !dmlRe.MatchString(body) {
				continue
			}
			if dynamicR.MatchString(body) {
				continue // Sprintf-built: covered by Rebind-at-execution
			}
			upper := strings.ToUpper(body)
			if strings.HasPrefix(upper, "SELECT") && !fromRe.MatchString(upper) {
				continue // column-list fragment, completed by concatenation
			}
			rebound, err := appdb.Rebind(body)
			if err != nil {
				rebound = body
			}
			total++
			name := fmt.Sprintf("pgg_%d", prepN)
			prepN++
			if _, err := db.ExecContext(ctx, "PREPARE "+name+" AS "+rebound); err != nil {
				// Dynamic fragments (literals ending mid-statement, e.g.
				// "IN (", "SELECT COUNT(*) FROM") cannot PREPARE by
				// construction — count as skipped, not failed.
				if strings.Contains(err.Error(), "end of input") {
					skipped++
					continue
				}
				t.Logf("FAIL %s: %.80q: %v", f, body, err)
				fFail++
				failed++
				continue
			}
			fOK++
			_, _ = db.ExecContext(ctx, "DEALLOCATE "+name)
		}
		if fFail > 0 {
			t.Logf("%s: %d ok, %d FAIL", f, fOK, fFail)
		}
	}
	t.Logf("go-sql literals: %d checked, %d skipped (dynamic fragments), %d failed", total, skipped, failed)
	if failed > 0 {
		t.Fatalf("%d Go SQL literals failed PREPARE on postgres", failed)
	}
}
