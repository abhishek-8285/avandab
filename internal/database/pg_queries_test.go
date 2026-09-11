//go:build pg_integration

// Postgres query-compatibility gate: every sqlc-generated sqlite query,
// rebound ? -> $N via database.Rebind, must PREPARE cleanly on a real
// PostgreSQL carrying the full ported chain. Catches non-portable SQL
// (unknown functions, bad casts) at migration/query change time:
//
//	DATABASE_URL="postgres://postgres:fix@localhost:5544/mvtms_pg?sslmode=disable" \
//	    go test -tags pg_integration ./internal/database/ -run TestPostgresQueryCompat -v
package database_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	appdb "transport-app/internal/database"
)

func TestPostgresQueryCompat(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	queryConstRe := regexp.MustCompile("(?s)const \\w+ = `-- name: (\\S+) [^\\n]*\\n(.*?)`")
	files, err := filepath.Glob("../../db/generated/sqlite/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("generated files: %v %d", err, len(files))
	}
	type q struct {
		file, name, sql string
	}
	var queries []q
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range queryConstRe.FindAllStringSubmatch(string(raw), -1) {
			queries = append(queries, q{filepath.Base(f), m[1], m[2]})
		}
	}
	if len(queries) == 0 {
		t.Fatal("no queries extracted")
	}
	t.Logf("extracted %d queries from %d files", len(queries), len(files))

	s := &testSettings{driver: "postgres", url: url, maxOpen: 4, maxIdle: 2}
	db, err := appdb.Open(context.Background(), s, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	var failed []string
	for i, qq := range queries {
		rebound, err := appdb.Rebind(qq.sql)
		if err != nil {
			// Placeholder-free queries (e.g. ListRoles) run verbatim.
			rebound = qq.sql
		}
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("PREPARE pgq_%d AS %s", i, rebound)); err != nil {
			failed = append(failed, fmt.Sprintf("%s/%s: %v", qq.file, qq.name, err))
			continue
		}
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DEALLOCATE pgq_%d", i))
	}
	for _, f := range failed {
		t.Log("FAIL:", f)
	}
	if len(failed) > 0 {
		t.Fatalf("%d/%d queries failed PREPARE on postgres", len(failed), len(queries))
	}
	t.Logf("all %d queries PREPARE cleanly on postgres", len(queries))
}
