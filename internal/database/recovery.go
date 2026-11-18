package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// RecoverInterruptedRebuild repairs crash artifacts of the NO TRANSACTION
// table-rebuild migrations (00126, 00131, 00132, 00135). If the process died
// between the copy / drop / rename steps, the next startup would fail creating
// the rebuild table again and the database could never migrate forward.
//
// This runs before goose and is deliberately limited to the exact temp table
// names those migrations use. Both tables carry the same data by construction
// (INSERT completes before DROP), so promoting the temp table when the base is
// missing is safe.
func RecoverInterruptedRebuild(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	type pair struct{ base, tmp string }
	pairs := []pair{
		{"vehicles", "vehicles_rebuild_00126"},
		{"vehicles", "vehicles_down_00126"},
		{"drivers", "drivers_rebuild_00131"},
		{"drivers", "drivers_down_00131"},
		{"vehicles", "vehicles_rebuild_00132"},
		{"vehicles", "vehicles_down_00132"},
		{"vehicles", "vehicles_rebuild_00135"},
		{"vehicles", "vehicles_down_00135"},
	}
	for _, p := range pairs {
		baseOK, err := tableExists(ctx, db, p.base)
		if err != nil {
			return err
		}
		tmpOK, err := tableExists(ctx, db, p.tmp)
		if err != nil {
			return err
		}
		if !tmpOK {
			continue
		}
		switch {
		case baseOK:
			// Crash after the copy (Up) or after re-creating vehicles (Down):
			// the temp table is a stale duplicate; 00126 can run again.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "%s"`, p.tmp)); err != nil {
				return fmt.Errorf("recover interrupted rebuild: drop stale %s: %w", p.tmp, err)
			}
			if logger != nil {
				logger.Warn("recovered interrupted rebuild: dropped stale temp table", "table", p.tmp)
			}
		case !baseOK:
			// Crash after dropping/renaming the base table: promote the temp
			// copy so goose can retry from a complete base table.
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, p.tmp, p.base)); err != nil {
				return fmt.Errorf("recover interrupted rebuild: restore %s from %s: %w", p.base, p.tmp, err)
			}
			if logger != nil {
				logger.Warn("recovered interrupted rebuild: restored base table", "table", p.base, "from", p.tmp)
			}
		}
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("recover interrupted rebuild: inspect %s: %w", name, err)
	}
	return n > 0, nil
}
