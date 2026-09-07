package datamigrate

import (
	"context"
	"fmt"
	"strconv"
)

// MergeSpec marks a table merged by natural key instead of blind insert:
// when a PG row with the same key exists (a migration seed), the sqlite
// row is skipped and dependents are remapped to the PG id. Nothing is lost.
type MergeSpec struct {
	Table string
	Key   string
}

// MergeTables are seed-managed tables whose INTEGER ids may collide.
var MergeTables = []MergeSpec{
	{Table: "roles", Key: "name"},
	{Table: "permissions", Key: "name"},
}

// DependentSpec remaps table.column through a merged table's ID map.
type DependentSpec struct {
	Table, Column, RefTable string
}

// Dependents must be remapped after their RefTable merge completes.
var Dependents = []DependentSpec{
	{Table: "role_permissions", Column: "role_id", RefTable: "roles"},
	{Table: "role_permissions", Column: "permission_id", RefTable: "permissions"},
	{Table: "users", Column: "role_id", RefTable: "roles"},
	{Table: "user_roles", Column: "role_id", RefTable: "roles"},
}

// IDMap holds a merged table's translations.
type IDMap struct {
	ByKey   map[string]string // natural key -> PG id
	IDRemap map[string]string // sqlite id -> PG id (only when different)
}

// BuildIDMaps reads the current PG state of every merge table.
func BuildIDMaps(ctx context.Context, q queryer) (map[string]*IDMap, error) {
	out := map[string]*IDMap{}
	for _, m := range MergeTables {
		im, err := scanMergeTable(ctx, q, m)
		if err != nil {
			return nil, err
		}
		out[m.Table] = im
	}
	return out, nil
}

func scanMergeTable(ctx context.Context, q queryer, m MergeSpec) (*IDMap, error) {
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, "%s" FROM "%s"`, m.Key, m.Table))
	if err != nil {
		return nil, fmt.Errorf("merge scan %s: %w", m.Table, err)
	}
	defer func() { _ = rows.Close() }()
	im := &IDMap{ByKey: map[string]string{}, IDRemap: map[string]string{}}
	for rows.Next() {
		var id, key any
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		im.ByKey[stringify(key)] = stringify(id)
	}
	return im, rows.Err()
}

// MergeRow decides a merge-table row's fate.
// Returns action "skip" (seed already has it — record remap) or "insert".
func MergeRow(m *IDMap, sqliteID, key string) (action string) {
	if pgID, ok := m.ByKey[key]; ok {
		if pgID != sqliteID {
			m.IDRemap[sqliteID] = pgID
		}
		return "skip"
	}
	m.ByKey[key] = sqliteID // will exist after insert
	return "insert"
}

// ResolveRef remaps a dependent column value through a merged table's map.
// Handles both INTEGER ids ("4") and dirty TEXT names ("org_admin").
func ResolveRef(m *IDMap, raw string) (string, bool) {
	if pgID, ok := m.IDRemap[raw]; ok {
		return pgID, true
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return raw, true // plain id, no remap needed
	}
	pgID, ok := m.ByKey[raw] // TEXT name in an INTEGER column
	return pgID, ok
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}
