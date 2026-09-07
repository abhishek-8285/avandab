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
	"flag"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
	_ "transport-app/internal/database"
	"transport-app/internal/datamigrate"
)

func main() {
	sqlitePath := flag.String("sqlite", "transport.db", "sqlite source file (opened read-only)")
	pgURL := flag.String("pg-url", os.Getenv("DATABASE_URL"), "postgres URL (or DATABASE_URL env)")
	checkOnly := flag.Bool("check", false, "read-only precheck report, migrate nothing")
	dryRun := flag.Bool("dry-run", false, "full copy inside a rolled-back transaction")
	flag.Parse()

	if *pgURL == "" {
		fatal("pg-url or DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	src, err := sql.Open("sqlite", "file:"+*sqlitePath+"?mode=ro")
	if err != nil {
		fatal("open sqlite: %v", err)
	}
	defer func() { _ = src.Close() }()
	dst, err := sql.Open("pgx-rebind", *pgURL)
	if err != nil {
		fatal("open pg: %v", err)
	}
	defer func() { _ = dst.Close() }()

	pre, err := datamigrate.RunPrecheck(ctx, src, dst)
	if err != nil {
		fatal("precheck: %v", err)
	}
	fmt.Print(pre.Report())
	if !pre.VersionsMatch {
		fatal("version mismatch — run the server once against each engine, then re-run")
	}
	if *checkOnly {
		return
	}

	sum, err := datamigrate.Run(ctx, src, dst, datamigrate.Options{DryRun: *dryRun})
	if err != nil {
		fatal("migrate: %v", err)
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
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "sqlite2pg: "+f+"\n", a...)
	os.Exit(1)
}
