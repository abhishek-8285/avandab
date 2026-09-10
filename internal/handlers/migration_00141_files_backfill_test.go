package handlers

// 00141 roundtrip: files uploadable_type CHECK widening (driver_issue) +
// files.tenant_id backfill from the owner entity (00140 follow-up).

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func migrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	name := fmt.Sprintf("test_mig_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Up(db, "db/migrations"))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// exec00141BackfillUp extracts and executes the real 00141 migration Up body
// over seeded orphan data (the migration also ran at goose-up time on the
// empty DB; this replays it so the test exercises the shipped SQL, not a copy).
func exec00141BackfillUp(t *testing.T, db *sql.DB) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		cwd = filepath.Join(cwd, "..", "..")
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "db", "migrations", "00141_files_backfill_tenant_from_owner.sql"))
	require.NoError(t, err)

	up := strings.Split(string(raw), "-- +goose Up")[1]
	up = strings.Split(up, "-- +goose Down")[0]

	var stmts []string
	var curr strings.Builder
	inBlock := false
	for _, line := range strings.Split(up, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "+goose StatementBegin") {
			inBlock = true
			continue
		}
		if strings.Contains(trimmed, "+goose StatementEnd") {
			inBlock = false
			s := strings.TrimSpace(curr.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			curr.Reset()
			continue
		}
		if !inBlock && strings.HasPrefix(trimmed, "--") {
			continue
		}
		curr.WriteString(line + "\n")
		if !inBlock && strings.HasSuffix(trimmed, ";") {
			s := strings.TrimSpace(curr.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			curr.Reset()
		}
	}
	if s := strings.TrimSpace(curr.String()); s != "" {
		stmts = append(stmts, s)
	}
	for _, stmt := range stmts {
		_, err := db.Exec(stmt)
		require.NoError(t, err, "00141 statement failed: %s", stmt)
	}
}

func seed00141Orphans(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('2', 'Org2', 'org2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r1', 'Delhi', 'Jaipur', 280, 5, 5000, '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('d2', 'DRV-2', 'Sunil', 'Kumar', '9876500002', 'DL-2', date('now','+1 year'), 'available', '2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t2', 'TRIP-2', 'r1', '2026-08-19 09:00:00', 'completed', '2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO trips (id, trip_number, route_id, departure_time, status, tenant_id)
		VALUES ('t1', 'TRIP-1', 'r1', '2026-08-19 09:00:00', 'completed', '1')`)
	require.NoError(t, err)

	files := []struct {
		id, kind, ref string
	}{
		{"f1", "driver_issue", "d2"},       // -> tenant '2' via drivers
		{"f2", "trip_pod", "t2"},           // -> tenant '2' via trips
		{"f3", "expense_receipt", "t2"},    // -> tenant '2' via trips
		{"f4", "driver_issue", "dangling"}, // dangling ref -> stays '1'
		{"f5", "trip_pod", "t1"},           // bootstrap owner -> stays '1'
		{"f6", "driver_license", "d2"},     // unmapped type -> stays '1'
	}
	for _, f := range files {
		_, err := db.Exec(`INSERT INTO files (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id)
			VALUES (?, ?, ?, 'uploads/x.jpg', 10, 'image/jpeg', ?, ?, '1')`,
			f.id, f.id+".jpg", f.id+".jpg", f.kind, f.ref)
		require.NoError(t, err)
	}
}

func assert00141Stamps(t *testing.T, db *sql.DB) {
	t.Helper()
	type kv struct{ id, tenant string }
	rows, err := db.Query(`SELECT id, tenant_id FROM files ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var k kv
		require.NoError(t, rows.Scan(&k.id, &k.tenant))
		got[k.id] = k.tenant
	}
	assert.Equal(t, "2", got["f1"], "driver_issue file must inherit owner driver tenant")
	assert.Equal(t, "2", got["f2"], "trip_pod file must inherit trip owner tenant")
	assert.Equal(t, "2", got["f3"], "expense_receipt file must inherit trip owner tenant")
	assert.Equal(t, "1", got["f4"], "dangling reference must stay at bootstrap tenant")
	assert.Equal(t, "1", got["f5"], "bootstrap-owned file must stay at '1'")
	assert.Equal(t, "1", got["f6"], "unmapped uploadable_type must stay at bootstrap tenant")
}

func TestMigration00141_FilesBackfill_RebuildAndStamps(t *testing.T) {
	db := migrationTestDB(t)

	seed00141Orphans(t, db)

	// (A) CHECK widening: driver_issue is now a valid uploadable_type.
	_, err := db.Exec(`INSERT INTO files (id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id)
		VALUES ('f7', 'issue.jpg', 'issue.jpg', 'uploads/issue.jpg', 10, 'image/jpeg', 'driver_issue', 'd2', '2')`)
	require.NoError(t, err, "driver_issue uploadable_type must pass the widened CHECK (drivers.go upload path)")

	exec00141BackfillUp(t, db)
	assert00141Stamps(t, db)

	// Re-run (idempotency): stamped rows are not touched again.
	exec00141BackfillUp(t, db)
	assert00141Stamps(t, db)
}

func TestMigration00141_GooseDownUp_RoundTrip(t *testing.T) {
	db := migrationTestDB(t)
	seed00141Orphans(t, db)
	exec00141BackfillUp(t, db)

	// Down (documented no-op) then Up again must not change stamps.
	require.NoError(t, goose.Down(db, "db/migrations"), "00141 down must apply (no-op)")
	require.NoError(t, goose.Up(db, "db/migrations"), "re-up must apply")
	assert00141Stamps(t, db)
}
