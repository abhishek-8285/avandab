package database

import (
	"strings"
	"testing"
)

func TestRebind(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare sequential", "WHERE a = ? AND b = ?", "WHERE a = $1 AND b = $2"},
		{"numbered", "WHERE a = ?1 AND b = ?2", "WHERE a = $1 AND b = $2"},
		{"repeated numbered", "LIKE '%' || lower(?2) || '%' OR x LIKE '%' || lower(?2)",
			"LIKE '%' || lower($2) || '%' OR x LIKE '%' || lower($2)"},
		{"literal untouched", "SELECT '?' WHERE a = ?", "SELECT '?' WHERE a = $1"},
		{"escaped quote", "SELECT 'it''s ?' WHERE a = ? AND b = ?",
			"SELECT 'it''s ?' WHERE a = $1 AND b = $2"},
		{"double digit", "WHERE a = ?10", "WHERE a = $10"},
		{"mixed static dollar with dynamic", "SET a = $1 WHERE id IN (?,?)",
			"SET a = $1 WHERE id IN ($2,$3)"},
		{"all static passthrough", "WHERE a = $1 AND b = $2", "WHERE a = $1 AND b = $2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Rebind(tc.in)
			if err != nil {
				t.Fatalf("Rebind = %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
	if got, err := Rebind("SELECT 1"); err != nil || got != "SELECT 1" {
		t.Errorf("placeholder-free passthrough = %q,%v", got, err)
	}
	if !strings.Contains(mustRebind(t, "SELECT ?"), "$1") {
		t.Error("sanity")
	}
}

func mustRebind(t *testing.T, q string) string {
	t.Helper()
	s, err := Rebind(q)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
