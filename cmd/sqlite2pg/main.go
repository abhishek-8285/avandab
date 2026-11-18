// Command sqlite2pg migrates live data from sqlite to Postgres.
//
// The schema chains are version-locked, so this tool moves DATA only.
// Workflow for a future cutover:
//
//	sqlite2pg --check                 # read-only go/no-go report
//	sqlite2pg --dry-run               # full copy inside a rolled-back txn
//	sqlite2pg                         # live migration
//	sqlite2pg --check                 # verify counts + quarantine review
//
// Preconditions: both engines at the same goose version (run the server
// once against each engine — it auto-migrates), PG reachable.
// sqlite is always opened read-only; the source file is never modified.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
	dbmigr "transport-app/db"
	appdb "transport-app/internal/database"
	"transport-app/internal/datamigrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sqlite2pg: %v\n", err)
		os.Exit(1)
	}
}

// run owns the DB lifecycle so deferred closes execute on every exit path
// instead of being skipped by os.Exit.
func run() error {
	sqlitePath := flag.String("sqlite", "transport.db", "sqlite source file (opened read-only)")
	pgURL := flag.String("pg-url", os.Getenv("DATABASE_URL"), "postgres URL (or DATABASE_URL env)")
	checkOnly := flag.Bool("check", false, "read-only precheck report, migrate nothing")
	dryRun := flag.Bool("dry-run", false, "full copy inside a rolled-back transaction")
	autoSchema := flag.Bool("auto-schema", true, "automatically apply goose migrations to PG if unmigrated")
	flag.Parse()

	if *pgURL == "" {
		return errors.New("pg-url or DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	src, err := sql.Open("sqlite", "file:"+*sqlitePath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer func() { _ = src.Close() }()
	dst, err := sql.Open("pgx-rebind", *pgURL)
	if err != nil {
		return fmt.Errorf("open pg: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if *autoSchema {
		if err := ensurePGSchema(ctx, dst); err != nil {
			return fmt.Errorf("auto-schema on pg: %w", err)
		}
	}

	pre, err := datamigrate.RunPrecheck(ctx, src, dst)
	if err != nil {
		return fmt.Errorf("precheck: %w", err)
	}
	fmt.Print(pre.Report())
	if !pre.VersionsMatch {
		return fmt.Errorf("version mismatch — sqlite=%d pg=%d", pre.SQLiteVersion, pre.PGVersion)
	}
	if *checkOnly {
		// Tenant-isolation triggers hide orphan rows from pragma_foreign_key_check;
		// the live copy would quarantine them. Fail the gate so cutover stops.
		if pre.TenantOrphans.Blocking() {
			fmt.Fprintln(os.Stderr, "sqlite2pg: --check failed: tenant orphans would be quarantined (see report above)")
			os.Exit(1)
		}
		return nil
	}

	sum, err := datamigrate.Run(ctx, src, dst, datamigrate.Options{DryRun: *dryRun})
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	mode := "LIVE"
	if sum.DryRun {
		mode = "DRY-RUN (rolled back)"
	}
	fmt.Printf("\n== %s summary ==\ntables: %d/%d  rows copied: %d  quarantined: %d  sequences reset: %d\n",
		mode, sum.TablesCopied, sum.TablesTotal, sum.RowsCopied, len(sum.Quarantined), len(sum.Sequences))
	for tbl, n := range sum.Remapped {
		fmt.Printf("  merged %s: %d ids remapped\n", tbl, n)
	}
	seen := map[string]int{}
	for _, q := range sum.Quarantined {
		seen[q.Table+" | "+q.Reason]++
	}
	for k, n := range seen {
		fmt.Printf("  quarantine x%d: %s\n", n, k)
	}
	fmt.Println("per-table counts (sqlite -> pg, copied+skipped-this-run):")
	for _, tc := range sum.TableCounts {
		marker := ""
		if tc.SQLite != tc.PG {
			marker = "  <-- DIFF (seeds, merge-skips or quarantine — see above)"
		}
		fmt.Printf("  %-28s %d -> %d (copied %d, skipped %d)%s\n", tc.Table, tc.SQLite, tc.PG, tc.Copied, tc.Skip, marker)
	}
	return nil
}

func ensurePGSchema(ctx context.Context, pg *sql.DB) error {
	sub, err := fs.Sub(dbmigr.MigrationsPG, "migrations_pg")
	if err != nil {
		return fmt.Errorf("read pg migrations: %w", err)
	}
	provider, err := goose.NewProvider(appdb.GooseDialect("postgres"), pg, sub)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}

	// Two-phase migrate: UpTo(72) -> resync -> Up
	if _, err := provider.UpTo(ctx, int64(appdb.PreGooseCutVersion)); err != nil {
		return fmt.Errorf("pg up-to %d: %w", appdb.PreGooseCutVersion, err)
	}
	if err := appdb.ResyncIdentitySequences(ctx, pg); err != nil {
		return fmt.Errorf("pg resync: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("pg full up: %w", err)
	}
	return nil
}
