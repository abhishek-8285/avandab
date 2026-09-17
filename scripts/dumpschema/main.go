// Command dumpschema migrates a fresh SQLite DB to head using the repo's
// goose migrations (library, no external binary needed). Used by
// scripts/dump-schema.sh to produce db/schema.sql.
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: dumpschema <migrations-dir> <db-path>")
		os.Exit(2)
	}
	dir, path := os.Args[1], os.Args[2]
	db, err := sql.Open("sqlite", "file:"+path+"?cache=shared&_pragma=journal_mode(DELETE)")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	if err := goose.SetDialect("sqlite"); err != nil {
		fmt.Fprintln(os.Stderr, "dialect:", err)
		os.Exit(1)
	}
	if err := goose.Up(db, dir); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}
