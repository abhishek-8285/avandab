package datamigrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Options tunes a migration run.
type Options struct {
	// DryRun executes everything inside a rolled-back transaction.
	DryRun bool
	// MaxPasses bounds the dependency-order retry loop.
	MaxPasses int
}

// Summary is the machine-readable outcome of a run.
type Summary struct {
	DryRun       bool
	TablesCopied int
	TablesTotal  int
	RowsCopied   int64
	Quarantined  []Quarantined
	Remapped     map[string]int // merge table -> remapped id count
	TableCounts  []TableCount
	Sequences    []string // reset sequences

	qkeys map[string]int // quarantine key -> index in Quarantined
}

// TableCount compares one table across engines.
type TableCount struct {
	Table        string
	SQLite, PG   int
	Copied, Skip int
}

// Run migrates all data from src (sqlite, caller opens read-only) to dst.
// It aborts before writing anything when versions differ.
func Run(ctx context.Context, src, dst *sql.DB, opt Options) (*Summary, error) {
	if opt.MaxPasses <= 0 {
		opt.MaxPasses = 12
	}
	sum := &Summary{DryRun: opt.DryRun, Remapped: map[string]int{}}

	var sv, pv int
	if err := src.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version`).Scan(&sv); err != nil {
		return nil, fmt.Errorf("datamigrate: sqlite version: %w", err)
	}
	if err := dst.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version`).Scan(&pv); err != nil {
		return nil, fmt.Errorf("datamigrate: pg version (migrate PG first): %w", err)
	}
	if sv != pv {
		return nil, fmt.Errorf("datamigrate: version mismatch sqlite=%d pg=%d — bring both to the same version, then re-run", sv, pv)
	}

	// All PG writes go through ex: *sql.DB live, *sql.Tx on dry-run.
	var ex execer = dst
	var qx queryer = dst
	var tx *sql.Tx
	if opt.DryRun {
		var err error
		tx, err = dst.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		ex = tx
		// NOTE: reads below must see the transaction's writes (merge maps
		// are rebuilt after merge tables load), so route reads there too.
		qx = txAdapter{tx}
	}
	if err := EnsureQuarantine(ctx, ex); err != nil {
		return nil, fmt.Errorf("datamigrate: quarantine table: %w", err)
	}

	tables, err := listTables(ctx, src)
	if err != nil {
		return nil, err
	}
	// Work set: non-empty tables only.
	var work []string
	counts := map[string]int{}
	for _, t := range tables {
		var n int
		if err := src.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, t)).Scan(&n); err != nil {
			return nil, fmt.Errorf("datamigrate: count %s: %w", t, err)
		}
		counts[t] = n
		if n > 0 {
			work = append(work, t)
		}
	}
	sum.TablesTotal = len(work)

	mergeSet := map[string]*MergeSpec{}
	for i := range MergeTables {
		mergeSet[MergeTables[i].Table] = &MergeTables[i]
	}
	depCols := map[string]map[string]string{} // table -> col -> refTable
	for _, d := range Dependents {
		if depCols[d.Table] == nil {
			depCols[d.Table] = map[string]string{}
		}
		depCols[d.Table][d.Column] = d.RefTable
	}

	// Phase 1: merge tables first (deterministic), recording ID remaps.
	maps, err := BuildIDMaps(ctx, qx)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	appended := map[string]bool{}
	for _, m := range MergeTables {
		if counts[m.Table] == 0 {
			continue
		}
		tc, err := copyMergeTable(ctx, src, ex, qx, &m, maps[m.Table], depCols[m.Table], sum, opt.DryRun)
		if err != nil {
			return nil, err
		}
		sum.TableCounts = append(sum.TableCounts, tc)
		appended[m.Table] = true
		done[m.Table] = true
		sum.TablesCopied++
		sum.Remapped[m.Table] = len(maps[m.Table].IDRemap)
	}
	// Refresh the in-memory maps' ByKey view so dependents see just-inserted
	// rows too. IMPORTANT: preserve IDRemap — a from-scratch rebuild would
	// discard the sqlite->PG translations recorded during the merge.
	fresh, err := BuildIDMaps(ctx, qx)
	if err != nil {
		return nil, err
	}
	for tbl, im := range maps {
		for k, v := range fresh[tbl].ByKey {
			if _, ok := im.ByKey[k]; !ok {
				im.ByKey[k] = v
			}
		}
	}

	// Phase 2: remaining tables. A table is done only when a pass skips
	// nothing; passes repeat while any table still makes progress (a
	// partial copy means FK deps may simply not be loaded yet).
	last := map[string]TableCount{}
	prevCopied := map[string]int{}
	for pass := 0; pass < opt.MaxPasses; pass++ {
		progress := false
		allDone := true
		for _, t := range work {
			if done[t] {
				continue
			}
			tc := copyTable(ctx, src, ex, qx, t, maps, depCols[t], sum, opt.DryRun)
			// Accumulate across passes: Copied sums per-pass deltas,
			// Skip/counts reflect the latest pass.
			acc := last[t]
			acc.Table = t
			acc.Copied += tc.Copied
			acc.Skip = tc.Skip
			acc.SQLite, acc.PG = tc.SQLite, tc.PG
			last[t] = acc
			if tc.Skip == 0 {
				done[t] = true
				sum.TablesCopied++
				progress = true
			} else {
				allDone = false
				if tc.Copied > prevCopied[t] {
					progress = true
				}
				prevCopied[t] = tc.Copied
			}
		}
		if allDone || !progress {
			break
		}
	}
	for _, t := range work {
		if appended[t] {
			continue // merge tables already reported in phase 1
		}
		tc, ok := last[t]
		if !ok {
			tc = TableCount{Table: t, SQLite: counts[t]}
		}
		sum.TableCounts = append(sum.TableCounts, tc)
	}

	// Phase 3: sequences.
	seqs, err := resetSequences(ctx, ex, qx)
	if err != nil {
		return nil, fmt.Errorf("datamigrate: sequences: %w", err)
	}
	sum.Sequences = seqs
	return sum, nil
}

// txAdapter routes reads into the dry-run transaction.
type txAdapter struct{ tx *sql.Tx }

func (a txAdapter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return a.tx.QueryContext(ctx, query, args...)
}

// copyMergeTable copies a seed-managed table by natural key.
// When savepoints is true (dry-run tx), each row is guarded by
// SAVEPOINT/RELEASE so one bad row cannot poison the transaction.
func copyMergeTable(ctx context.Context, src *sql.DB, ex execer, qx queryer, m *MergeSpec, im *IDMap, _ map[string]string, sum *Summary, savepoints bool) (TableCount, error) {
	tc := TableCount{Table: m.Table}
	rows, err := src.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM "%s"`, m.Table))
	if err != nil {
		return tc, err
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	types, err := ColumnTypes(ctx, qx, m.Table)
	if err != nil {
		return tc, err
	}
	keyIdx := indexOf(cols, m.Key)
	idIdx := indexOf(cols, "id")
	ph := placeholders(len(cols))
	stmt := fmt.Sprintf(`INSERT INTO "%s" ("%s") VALUES (%s) ON CONFLICT DO NOTHING`,
		m.Table, strings.Join(cols, `","`), strings.Join(ph, ","))
	for rows.Next() {
		vals := scanRow(rows, len(cols))
		sid := stringify(deref(vals[idIdx]))
		key := stringify(deref(vals[keyIdx]))
		if MergeRow(im, sid, key) == "skip" {
			tc.Skip++
			continue
		}
		args := coerceAll(vals, cols, types)
		inserted, err := execInsert(ctx, ex, qx, savepoints, stmt, args...)
		if err != nil {
			quar(ctx, ex, sum, savepoints, m.Table, cols, vals, trimErr(err))
			tc.Skip++
			continue
		}
		if inserted {
			tc.Copied++
			sum.RowsCopied++
		}
		// Clear any earlier quarantine for this row — including the
		// conflict-noop case (row already present via trigger/seed/retry).
		unquar(ctx, ex, sum, savepoints, m.Table, cols, vals)
	}
	if err := rows.Err(); err != nil {
		return tc, err
	}
	finishCount(ctx, qx, src, &tc)
	return tc, nil
}

// copyTable copies one plain table; rows that fail are quarantined.
func copyTable(ctx context.Context, src *sql.DB, ex execer, qx queryer, table string, maps map[string]*IDMap, deps map[string]string, sum *Summary, savepoints bool) TableCount {
	tc := TableCount{Table: table}
	rows, err := src.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return tc
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	types, err := ColumnTypes(ctx, qx, table)
	if err != nil {
		return tc
	}
	ph := placeholders(len(cols))
	stmt := fmt.Sprintf(`INSERT INTO "%s" ("%s") VALUES (%s) ON CONFLICT DO NOTHING`,
		table, strings.Join(cols, `","`), strings.Join(ph, ","))
	for rows.Next() {
		vals := scanRow(rows, len(cols))
		args := coerceAll(vals, cols, types)
		// Dependent remaps (role_id text names, seed ID drift).
		quarantine := false
		for i, c := range cols {
			ref, ok := deps[c]
			if !ok {
				continue
			}
			raw := stringify(args[i])
			if raw == "" {
				continue
			}
			newID, ok := ResolveRef(maps[ref], raw)
			if !ok {
				quar(ctx, ex, sum, savepoints, table, cols, vals,
					fmt.Sprintf("unresolvable %s=%q (no %s entry)", c, raw, ref))
				tc.Skip++
				quarantine = true
				break
			}
			args[i] = newID
		}
		if quarantine {
			continue
		}
		if inserted, err := execInsert(ctx, ex, qx, savepoints, stmt, args...); err != nil {
			quar(ctx, ex, sum, savepoints, table, cols, vals, trimErr(err))
			tc.Skip++
			continue
		} else if inserted {
			tc.Copied++
			sum.RowsCopied++
		}
		// Clear any earlier quarantine for this row (see copyMergeTable).
		unquar(ctx, ex, sum, savepoints, table, cols, vals)
	}
	finishCount(ctx, qx, src, &tc)
	return tc
}

// execInsert runs one row insert with ON CONFLICT DO NOTHING and reports
// whether the row was actually inserted (vs already present).
// With savepoints (dry-run inside a tx) the row is wrapped in
// SAVEPOINT/RELEASE: a failed row rolls back to the savepoint instead of
// poisoning the whole transaction.
func execInsert(ctx context.Context, ex execer, qx queryer, savepoints bool, stmt string, args ...any) (bool, error) {
	query := stmt + ` RETURNING 1`
	run := func() (bool, error) {
		rows, err := qx.QueryContext(ctx, query, args...)
		if err != nil {
			return false, err
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			return true, rows.Err()
		}
		return false, rows.Err()
	}
	if !savepoints {
		return run()
	}
	if _, err := ex.ExecContext(ctx, `SAVEPOINT dm_row`); err != nil {
		return false, err
	}
	inserted, err := run()
	if err != nil {
		if _, rb := ex.ExecContext(ctx, `ROLLBACK TO SAVEPOINT dm_row`); rb != nil {
			return false, rb
		}
		return false, err
	}
	if _, err := ex.ExecContext(ctx, `RELEASE SAVEPOINT dm_row`); err != nil {
		return false, err
	}
	return inserted, nil
}

// resetSequences advances every owned integer sequence to its column max.
func resetSequences(ctx context.Context, ex execer, q queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq.relname, tbl.relname, attr.attname
		FROM pg_class seq
		JOIN pg_depend d ON d.objid = seq.oid AND d.deptype IN ('a', 'i')
		JOIN pg_class tbl ON tbl.oid = d.refobjid
		JOIN pg_attribute attr ON attr.attrelid = tbl.oid AND attr.attnum = d.refobjsubid
		WHERE seq.relkind = 'S'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var reset []string
	for rows.Next() {
		var seq, tbl, col string
		if err := rows.Scan(&seq, &tbl, &col); err != nil {
			return nil, err
		}
		var typ string
		if err := scanOne(ctx, q, &typ,
			`SELECT data_type FROM information_schema.columns WHERE table_name=$1 AND column_name=$2`, tbl, col); err != nil || !isIntType(typ) {
			continue
		}
		if _, err := ex.ExecContext(ctx, fmt.Sprintf(
			`SELECT setval('%s', COALESCE((SELECT max("%s") FROM "%s"), 0))`, seq, col, tbl)); err != nil {
			return nil, fmt.Errorf("setval %s: %w", seq, err)
		}
		reset = append(reset, seq)
	}
	return reset, rows.Err()
}

func isIntType(t string) bool {
	switch t {
	case "integer", "bigint", "smallint":
		return true
	}
	return false
}

// --- small helpers ---

func indexOf(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func placeholders(n int) []string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	return ph
}

func scanRow(rows *sql.Rows, n int) []any {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	_ = rows.Scan(ptrs...)
	return vals
}

func deref(v any) any {
	if p, ok := v.(*any); ok {
		return *p
	}
	return v
}

func coerceAll(vals []any, cols []string, types map[string]string) []any {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = CoerceValue(deref(v), types[cols[i]])
	}
	return args
}

func quarRow(cols []string, vals []any) map[string]string {
	row := map[string]string{}
	for i, c := range cols {
		s := stringify(deref(vals[i]))
		if len(s) > 300 {
			s = s[:300] + "…"
		}
		row[c] = s
	}
	return row
}

func quarKey(table string, row map[string]string) string {
	raw, _ := json.Marshal(row)
	return table + "\x00" + string(raw)
}

func quar(ctx context.Context, ex execer, sum *Summary, savepoints bool, table string, cols []string, vals []any, reason string) {
	row := quarRow(cols, vals)
	q := Quarantined{Table: table, Row: row, Reason: reason}
	key := quarKey(table, row)
	if sum.qkeys == nil {
		sum.qkeys = map[string]int{}
	}
	if _, dup := sum.qkeys[key]; dup {
		return // already recorded (earlier pass); DB dedupes too
	}
	if savepoints {
		// SAVEPOINT fails only when the tx is already dead; keep the
		// evidence in-memory either way.
		if _, err := ex.ExecContext(ctx, `SAVEPOINT dm_q`); err != nil {
			noteQuarantined(sum, key, q)
			return
		}
		inserted, err := RecordQuarantine(ctx, ex, q)
		if err != nil {
			_, _ = ex.ExecContext(ctx, `ROLLBACK TO SAVEPOINT dm_q`)
		} else {
			_, _ = ex.ExecContext(ctx, `RELEASE SAVEPOINT dm_q`)
		}
		if err != nil || inserted {
			noteQuarantined(sum, key, q)
		}
		return
	}
	if inserted, err := RecordQuarantine(ctx, ex, q); err != nil || inserted {
		noteQuarantined(sum, key, q)
	}
}

// noteQuarantined records a quarantine entry in-memory (deduped by key).
func noteQuarantined(sum *Summary, key string, q Quarantined) {
	if _, dup := sum.qkeys[key]; dup {
		return
	}
	sum.qkeys[key] = len(sum.Quarantined)
	sum.Quarantined = append(sum.Quarantined, q)
}

// unquar removes a quarantine entry when its row later copies successfully
// (a later pass after FK deps loaded). Keeps quarantine = final truth.
func unquar(ctx context.Context, ex execer, sum *Summary, savepoints bool, table string, cols []string, vals []any) {
	if sum.qkeys == nil {
		return
	}
	key := quarKey(table, quarRow(cols, vals))
	idx, ok := sum.qkeys[key]
	if !ok {
		return
	}
	raw, _ := json.Marshal(sum.Quarantined[idx].Row)
	del := func() error {
		_, err := ex.ExecContext(ctx,
			`DELETE FROM "`+QuarantineTable+`" WHERE table_name=$1 AND row_json=$2`, table, string(raw))
		return err
	}
	if savepoints {
		if _, err := ex.ExecContext(ctx, `SAVEPOINT dm_u`); err != nil {
			return
		}
		if err := del(); err != nil {
			_, _ = ex.ExecContext(ctx, `ROLLBACK TO SAVEPOINT dm_u`)
			return
		}
		_, _ = ex.ExecContext(ctx, `RELEASE SAVEPOINT dm_u`)
	} else if err := del(); err != nil {
		return
	}
	last := len(sum.Quarantined) - 1
	sum.Quarantined[idx] = sum.Quarantined[last]
	sum.Quarantined = sum.Quarantined[:last]
	delete(sum.qkeys, key)
	if idx < len(sum.Quarantined) {
		moved := quarKey(sum.Quarantined[idx].Table, sum.Quarantined[idx].Row)
		sum.qkeys[moved] = idx
	}
}

func trimErr(err error) string {
	s := err.Error()
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func finishCount(ctx context.Context, q queryer, src *sql.DB, tc *TableCount) {
	_ = src.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, tc.Table)).Scan(&tc.SQLite)
	var n int
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, tc.Table))
	if err != nil {
		tc.PG = -1 // visible sentinel: read failed, see logs
		return
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		_ = rows.Scan(&n)
	}
	tc.PG = n
}

func scanOne(ctx context.Context, q queryer, dest *string, query string, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return rows.Scan(dest)
	}
	return sql.ErrNoRows
}
