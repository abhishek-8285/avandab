// Package datamigrate moves live data from the sqlite database to Postgres.
//
// The schema chains are version-locked (db/migrations vs db/migrations_pg),
// so a copy is column-for-column — but raw sqlite data is not PG-clean.
// This package handles the six known problem classes found by live dry runs:
//
//  1. Go-format timestamps ("2006-01-02 15:04:05 +0000 UTC") — parsed to time.Time.
//  2. INTEGER 0/1 in BOOLEAN columns (modernc returns int64; pgx can't encode int64→bool).
//  3. TEXT in INTEGER columns (e.g. users.role_id='org_admin') — resolved via lookup, else quarantined.
//  4. Orphan FK rows (pre-existing foreign_key_check violations) — quarantined, never silently dropped.
//  5. Seed-ID collisions on INTEGER-PK tables (permissions) — merged by natural key with ID remap.
//  6. IDENTITY sequences left behind max(id) — setval after load.
//
// Usage: check first (Precheck, read-only on both sides), then dry-run,
// then live. See cmd/sqlite2pg.
package datamigrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TimestamptzLayouts are the datetime shapes observed in sqlite TEXT columns.
var TimestamptzLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST", // Go time.String()
	"2006-01-02 15:04:05 -0700 MST",
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseTimestamp parses the datetime shapes found in sqlite and returns UTC.
func ParseTimestamp(s string) (time.Time, bool) {
	for _, l := range TimestamptzLayouts {
		if tt, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return tt.UTC(), true
		}
	}
	return time.Time{}, false
}

// IsBool reports whether a PG data_type needs Go bool coercion.
func IsBool(pgType string) bool { return pgType == "boolean" }

// IsTimestamp reports whether a PG data_type needs timestamp parsing.
func IsTimestamp(pgType string) bool {
	return strings.Contains(pgType, "timestamp") || pgType == "date"
}

// CoerceValue converts a modernc/sqlite driver value to a PG-friendly arg.
// pgType is the information_schema data_type of the target column.
func CoerceValue(v any, pgType string) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []byte:
		s := string(t)
		switch {
		case IsTimestamp(pgType):
			if tt, ok := ParseTimestamp(s); ok {
				return tt
			}
			return s // let PG try; failure quarantines the row with reason
		case IsBool(pgType):
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "1", "t", "true", "y", "yes", "on":
				return true
			case "0", "f", "false", "n", "no", "off":
				return false
			}
			return s
		default:
			return s
		}
	case int64:
		if IsBool(pgType) {
			return t != 0
		}
		return t
	case float64, string, bool, time.Time:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// ColumnTypes returns data_type per column for a PG table.
func ColumnTypes(ctx context.Context, q queryer, table string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := q.QueryContext(ctx,
		`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c, t string
		if err := rows.Scan(&c, &t); err != nil {
			return nil, err
		}
		out[c] = t
	}
	return out, rows.Err()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
