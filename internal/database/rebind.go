package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// Rebind converts a query using `?` placeholders (sqlite/mysql style,
// including sqlc's numbered `?N` form) into Postgres `$N` form for the pgx
// driver, which rejects `?`. Question marks inside single-quoted string
// literals are left untouched.
//
// Rules:
//   - `?N` (explicit number) becomes `$N`.
//   - Pre-existing `$N` markers are honored as claimed: bare `?` becomes
//     the next free `$K` in scan order, skipping every number already
//     present (whether written `?N` or `$N`). This keeps mixed static/dynamic
//     queries correct: static `$1` keeps arg 1, appended `?` take $2.. .
//
// An error is returned only for a dangling `?0` marker. Queries without
// any placeholder pass through unchanged (DDL probes, SELECT 1, already
// rebound $N text) so callers can wrap unconditionally.
func Rebind(query string) (string, error) {
	used := map[int]bool{}
	// Pre-claim every $N already present (outside string literals).
	inStr := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if inStr {
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
				} else {
					inStr = false
				}
			}
			continue
		}
		if c == '\'' {
			inStr = true
			continue
		}
		if c == '$' {
			j, num := i+1, 0
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				num = num*10 + int(query[j]-'0')
				j++
			}
			if j > i+1 && num >= 1 {
				used[num] = true
			}
		}
	}
	next := 1
	var out strings.Builder
	out.Grow(len(query) + 8)
	inStr = false
	np := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		if inStr {
			out.WriteByte(c)
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					out.WriteByte('\'')
					i++
				} else {
					inStr = false
				}
			}
			continue
		}
		switch {
		case c == '\'':
			inStr = true
			out.WriteByte(c)
		case c == '?':
			j := i + 1
			num := 0
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				num = num*10 + int(query[j]-'0')
				j++
			}
			if j == i+1 {
				// Bare marker: next free number.
				for used[next] {
					next++
				}
				num = next
				next++
			}
			if num < 1 {
				return "", fmt.Errorf("database: invalid placeholder ?0")
			}
			used[num] = true
			np++
			fmt.Fprintf(&out, "$%d", num)
			i = j - 1
		default:
			out.WriteByte(c)
		}
	}
	if np == 0 {
		return query, nil
	}
	return out.String(), nil
}

// RowidOrder returns the insertion-order tiebreaker for ORDER BY: SQLite's
// rowid, or Postgres' ctid (closest equivalent — reverse physical order
// approximates reverse insertion order, the same heuristic rowid gives).
// Use it wherever sqlite code said ORDER BY ... rowid ASC/DESC.
func RowidOrder(db *sql.DB, asc bool) string {
	dir := "DESC"
	if asc {
		dir = "ASC"
	}
	if IsPostgres(db) {
		return "ctid " + dir
	}
	return "rowid " + dir
}
