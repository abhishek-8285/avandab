package datamigrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Precheck is a read-only go/no-go report. It touches nothing — run it
// anytime against the live sqlite file to see exactly what a future
// migration would face.
type Precheck struct {
	SQLiteVersion int
	PGVersion     int
	VersionsMatch bool

	// Dirty INTEGER columns (TEXT stored in INTEGER affinity).
	DirtyColumns []DirtyColumn
	// Orphan FK rows in sqlite (would need quarantine).
	Orphans []Orphan
	// TenantOrphans are rows whose tenant_id has no tenants row. Migrations
	// 00103+ enforce this with triggers, so pragma_foreign_key_check cannot
	// see them — but the live copy quarantines them (and their FK dependents).
	TenantOrphans TenantOrphans
	// LegacyRows are sqlite-only tables with rows and no PG counterpart.
	LegacyRows []LegacyTable
	// Seed collisions preview on INTEGER-PK merge tables.
	Collisions []Collision

	// NonEmptyPG lists PG tables that already hold rows (seeds or prior run).
	NonEmptyPG []string

	// MissingPG lists sqlite tables with no PG counterpart (e.g. legacy
	// runtime tables no migration owns — harmless when empty).
	MissingPG []string
}

// DirtyColumn is one INTEGER column holding non-integer values.
type DirtyColumn struct {
	Table, Column string
	Rows          int
	Sample        string
}

// Orphan is one foreign_key_check violation group.
type Orphan struct {
	Table string
	Rows  int
}

// TenantOrphans counts rows the trigger-enforced tenant guards will reject.
// Sessions/UserRoles cascade from orphan users: the live copy quarantines the
// user first, then its dependents fail the FK.
type TenantOrphans struct {
	Users     int
	Sessions  int
	UserRoles int
}

// Blocking reports whether the copy would quarantine live tenant data.
func (t TenantOrphans) Blocking() bool {
	return t.Users > 0 || t.Sessions > 0 || t.UserRoles > 0
}

// LegacyTable is a sqlite-only table holding rows; no PG migration owns it, so
// the live copy quarantines those rows.
type LegacyTable struct {
	Table string
	Rows  int
}

// Collision is one merge-table key present on both sides with different ids.
type Collision struct {
	Table, Key, SQLiteID, PGID string
}

// RunPrecheck gathers the report. src is opened read-only by the caller.
func RunPrecheck(ctx context.Context, src, pg *sql.DB) (*Precheck, error) {
	p := &Precheck{}
	if err := src.QueryRowContext(ctx,
		`SELECT max(version_id) FROM goose_db_version`).Scan(&p.SQLiteVersion); err != nil {
		return nil, fmt.Errorf("sqlite version: %w", err)
	}
	if err := pg.QueryRowContext(ctx,
		`SELECT max(version_id) FROM goose_db_version`).Scan(&p.PGVersion); err != nil {
		return nil, fmt.Errorf("pg version (is PG migrated?): %w", err)
	}
	p.VersionsMatch = p.SQLiteVersion == p.PGVersion

	tables, err := listTables(ctx, src)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		// INTEGER columns with non-integer content.
		intCols, err := intColumns(ctx, src, t)
		if err != nil {
			return nil, err
		}
		for _, c := range intCols {
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE "%s" IS NOT NULL AND typeof("%s") NOT IN ('integer')`, t, c, c)
			if err := src.QueryRowContext(ctx, q).Scan(&n); err != nil || n == 0 {
				continue
			}
			var sample sql.NullString
			_ = src.QueryRowContext(ctx,
				fmt.Sprintf(`SELECT "%s" FROM "%s" WHERE "%s" IS NOT NULL AND typeof("%s") NOT IN ('integer') LIMIT 1`, c, t, c, c)).Scan(&sample)
			p.DirtyColumns = append(p.DirtyColumns, DirtyColumn{Table: t, Column: c, Rows: n, Sample: sample.String})
		}
		// PG non-empty tables + missing-table detection.
		var n int
		if err := pg.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT count(*) FROM "%s"`, t)).Scan(&n); err != nil {
			p.MissingPG = append(p.MissingPG, t)
			continue
		}
		if n > 0 {
			p.NonEmptyPG = append(p.NonEmptyPG, fmt.Sprintf("%s(%d)", t, n))
		}
	}
	// Orphans: per-table foreign_key_check violation counts.
	for _, t := range tables {
		var n int
		if err := src.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT count(*) FROM pragma_foreign_key_check('%s')`, t)).Scan(&n); err == nil && n > 0 {
			p.Orphans = append(p.Orphans, Orphan{Table: t, Rows: n})
		}
	}
	// Trigger-enforced tenant orphans (invisible to pragma_foreign_key_check).
	p.TenantOrphans = detectTenantOrphans(ctx, src)

	// sqlite-only tables that still hold rows would be quarantined row-by-row.
	p.LegacyRows = legacyRows(ctx, src, p.MissingPG)

	// Collisions on merge tables.
	maps, err := BuildIDMaps(ctx, pg)
	if err != nil {
		return nil, err
	}
	for _, m := range MergeTables {
		if err := collectCollisions(ctx, src, p, maps[m.Table], m); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// detectTenantOrphans counts tenant-scoped rows whose tenant/user parent is
// missing. Errors degrade to zero (old sqlite chains may lack a table); the
// live copy remains the final authority.
func detectTenantOrphans(ctx context.Context, src *sql.DB) TenantOrphans {
	var t TenantOrphans
	count := func(q string, dst *int) {
		_ = src.QueryRowContext(ctx, q).Scan(dst)
	}
	count(`SELECT count(*) FROM users WHERE tenant_id NOT IN (SELECT id FROM tenants)`, &t.Users)
	count(`SELECT count(*) FROM sessions WHERE user_id IN
		(SELECT id FROM users WHERE tenant_id NOT IN (SELECT id FROM tenants))`, &t.Sessions)
	count(`SELECT count(*) FROM user_roles WHERE user_id IN
		(SELECT id FROM users WHERE tenant_id NOT IN (SELECT id FROM tenants))`, &t.UserRoles)
	return t
}

// legacyRows counts rows in sqlite-only tables (no PG counterpart). Those rows
// cannot be migrated by the copy and are quarantined by design.
func legacyRows(ctx context.Context, src *sql.DB, missing []string) []LegacyTable {
	var out []LegacyTable
	for _, table := range missing {
		var n int
		if err := src.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, table)).Scan(&n); err == nil && n > 0 {
			out = append(out, LegacyTable{Table: table, Rows: n})
		}
	}
	return out
}

func intColumns(ctx context.Context, src *sql.DB, table string) ([]string, error) {
	rows, err := src.QueryContext(ctx,
		fmt.Sprintf(`SELECT name FROM pragma_table_info('%s') WHERE type LIKE '%%INT%%'`, table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func collectCollisions(ctx context.Context, src *sql.DB, p *Precheck, im *IDMap, m MergeSpec) error {
	rows, err := src.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, "%s" FROM "%s"`, m.Key, m.Table))
	if err != nil {
		return nil // table may not exist on old sqlite; migrate aborts later on version check
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, key any
		if err := rows.Scan(&id, &key); err != nil {
			return err
		}
		sid, k := stringify(id), stringify(key)
		if pgID, ok := im.ByKey[k]; ok && pgID != sid {
			p.Collisions = append(p.Collisions, Collision{Table: m.Table, Key: k, SQLiteID: sid, PGID: pgID})
		}
	}
	return rows.Err()
}

// Report renders the precheck for humans.
func (p *Precheck) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "sqlite v%d  vs  pg v%d  -> %s\n", p.SQLiteVersion, p.PGVersion,
		map[bool]string{true: "MATCH (safe to copy)", false: "MISMATCH — migrate sqlite to PG version first"}[p.VersionsMatch])
	fmt.Fprintf(&b, "dirty INTEGER columns: %d\n", len(p.DirtyColumns))
	for _, d := range p.DirtyColumns {
		fmt.Fprintf(&b, "  %s.%s: %d rows (e.g. %q) — auto-mapped where lookup exists, else quarantined\n", d.Table, d.Column, d.Rows, d.Sample)
	}
	n := 0
	for _, o := range p.Orphans {
		n += o.Rows
	}
	fmt.Fprintf(&b, "orphan FK rows: %d\n", n)
	for _, o := range p.Orphans {
		fmt.Fprintf(&b, "  %s: %d rows -> quarantine\n", o.Table, o.Rows)
	}
	fmt.Fprintf(&b, "tenant orphans (trigger-enforced, invisible to FK check): users=%d sessions=%d user_roles=%d\n",
		p.TenantOrphans.Users, p.TenantOrphans.Sessions, p.TenantOrphans.UserRoles)
	if p.TenantOrphans.Blocking() {
		fmt.Fprintf(&b, "  -> live copy WILL quarantine these rows; fix or delete them before cutover (users first, then sessions/user_roles)\n")
	}
	fmt.Fprintf(&b, "seed collisions: %d\n", len(p.Collisions))
	for _, c := range p.Collisions {
		fmt.Fprintf(&b, "  %s %q: sqlite=%s pg=%s -> merged by name, dependents remapped\n", c.Table, c.Key, c.SQLiteID, c.PGID)
	}
	fmt.Fprintf(&b, "non-empty PG tables (seeds/prior run): %d\n", len(p.NonEmptyPG))
	if len(p.NonEmptyPG) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(p.NonEmptyPG, ", "))
	}
	fmt.Fprintf(&b, "sqlite tables missing on PG: %d\n", len(p.MissingPG))
	for _, t := range p.MissingPG {
		fmt.Fprintf(&b, "  %s (purge probes skip absent tables; migrate data manually if non-empty)\n", t)
	}
	fmt.Fprintf(&b, "sqlite-only tables with rows (will be quarantined): %d\n", len(p.LegacyRows))
	for _, l := range p.LegacyRows {
		fmt.Fprintf(&b, "  %s: %d rows -> no PG owner; archive/drop before cutover\n", l.Table, l.Rows)
	}
	return b.String()
}

func listTables(ctx context.Context, src *sql.DB) ([]string, error) {
	rows, err := src.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name!='goose_db_version' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
