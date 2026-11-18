package database_test

import (
	"context"
	"database/sql"
	"testing"

	"transport-app/internal/database"

	_ "modernc.org/sqlite"
)

// A crash mid-00126 leaves either a stale temp table (base still present) or a
// temp table with no base (base already dropped). Both must be recoverable.
func TestRecoverInterruptedRebuild(t *testing.T) {
	ctx := context.Background()

	t.Run("stale temp dropped when base exists", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE vehicles (id TEXT); CREATE TABLE vehicles_rebuild_00126 (id TEXT)`); err != nil {
			t.Fatal(err)
		}
		if err := database.RecoverInterruptedRebuild(ctx, db, nil); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='vehicles_rebuild_00126'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("stale temp table survived")
		}
	})

	t.Run("temp promoted when base missing", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE vehicles_rebuild_00126 (id TEXT); INSERT INTO vehicles_rebuild_00126 VALUES ('v1')`); err != nil {
			t.Fatal(err)
		}
		if err := database.RecoverInterruptedRebuild(ctx, db, nil); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := db.QueryRow(`SELECT id FROM vehicles LIMIT 1`).Scan(&id); err != nil {
			t.Fatalf("vehicles not restored: %v", err)
		}
		if id != "v1" {
			t.Fatalf("restored id = %q", id)
		}
	})
}

// Every NO TRANSACTION table-rebuild migration must survive a crash at the
// same two points as 00126. Regression for H56.
func TestRecoverInterruptedRebuild_AllRebuildMigrations(t *testing.T) {
	ctx := context.Background()
	cases := []struct{ base, tmp string }{
		{"drivers", "drivers_rebuild_00131"},
		{"vehicles", "vehicles_rebuild_00132"},
		{"vehicles", "vehicles_rebuild_00135"},
	}
	for _, tc := range cases {
		t.Run(tc.tmp+"_stale", func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE ` + tc.base + ` (id TEXT); CREATE TABLE ` + tc.tmp + ` (id TEXT)`); err != nil {
				t.Fatal(err)
			}
			if err := database.RecoverInterruptedRebuild(ctx, db, nil); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name=?`, tc.tmp).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("stale %s survived", tc.tmp)
			}
		})
		t.Run(tc.tmp+"_promote", func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE ` + tc.tmp + ` (id TEXT); INSERT INTO ` + tc.tmp + ` VALUES ('v1')`); err != nil {
				t.Fatal(err)
			}
			if err := database.RecoverInterruptedRebuild(ctx, db, nil); err != nil {
				t.Fatal(err)
			}
			var id string
			if err := db.QueryRow(`SELECT id FROM ` + tc.base + ` LIMIT 1`).Scan(&id); err != nil {
				t.Fatalf("%s not restored: %v", tc.base, err)
			}
			if id != "v1" {
				t.Fatalf("restored id = %q", id)
			}
		})
	}
}
