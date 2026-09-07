package db

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readDirNames(t *testing.T, dir string, readDir func(string) ([]fs.DirEntry, error)) []string {
	t.Helper()
	entries, err := readDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// TestPGChainParity is structural only: every version present on both sides,
// and each PG file carries its `PG port of` header plus Up/Down sections.
// Bodies intentionally drift (e.g. 00126: sqlite rebuild vs PG ALTER), so no
// semantic SQL compare — that would over-build and rot on every rewrite.
func TestPGChainParity(t *testing.T) {
	sqlite := readDirNames(t, "migrations", Migrations.ReadDir)
	pg := readDirNames(t, "migrations_pg", MigrationsPG.ReadDir)

	require.Equal(t, sqlite, pg, "version sets must match 1:1 (same file names both sides)")

	for _, name := range pg {
		raw, err := MigrationsPG.ReadFile(path.Join("migrations_pg", name))
		require.NoError(t, err, name)
		body := string(raw)
		require.Contains(t, body, "PG port of", "%s: missing `PG port of` header", name)
		require.Contains(t, body, "+goose Up", "%s: missing Up section", name)
		require.Contains(t, body, "+goose Down", "%s: missing Down section", name)
	}
}
