package datamigrate

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in   string
		want string // UTC formatted
		ok   bool
	}{
		{"2026-08-06 03:37:15", "2026-08-06T03:37:15Z", true},
		{"2026-08-27 11:27:45 +0000 UTC", "2026-08-27T11:27:45Z", true}, // Go String() shape
		{"2026-09-02 12:18:29.123456 +0000 UTC", "2026-09-02T12:18:29Z", true},
		{"2026-08-05T12:05:39Z", "2026-08-05T12:05:39Z", true},
		{"2026-08-05", "2026-08-05T00:00:00Z", true},
		{"not-a-date", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ParseTimestamp(c.in)
		if ok != c.ok {
			t.Errorf("ParseTimestamp(%q) ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got.Format(time.RFC3339) != c.want {
			t.Errorf("ParseTimestamp(%q) = %v, want %v", c.in, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestCoerceValue(t *testing.T) {
	if got := CoerceValue(int64(1), "boolean"); got != true {
		t.Errorf("int64(1)->bool = %v", got)
	}
	if got := CoerceValue(int64(0), "boolean"); got != false {
		t.Errorf("int64(0)->bool = %v", got)
	}
	if got := CoerceValue([]byte("1"), "boolean"); got != true {
		t.Errorf("[]byte 1->bool = %v", got)
	}
	if got := CoerceValue([]byte("2026-08-27 11:27:45 +0000 UTC"), "timestamp with time zone"); true {
		if _, ok := got.(time.Time); !ok {
			t.Errorf("go-ts not parsed: %T", got)
		}
	}
	if got := CoerceValue([]byte("hello"), "text"); got != "hello" {
		t.Errorf("text passthrough = %v", got)
	}
	if CoerceValue(nil, "integer") != nil {
		t.Errorf("nil must stay NULL")
	}
	if got := CoerceValue(int64(42), "integer"); got != int64(42) {
		t.Errorf("int passthrough = %v", got)
	}
}

func TestMergeRow(t *testing.T) {
	m := &IDMap{ByKey: map[string]string{"admin": "1"}, IDRemap: map[string]string{}}
	if a := MergeRow(m, "1", "admin"); a != "skip" {
		t.Errorf("same id = %q, want skip", a)
	}
	if a := MergeRow(m, "86", "experiments:read"); a != "insert" {
		t.Errorf("new key = %q, want insert", a)
	}
	// Simulate PG seed with different id for same name.
	m.ByKey["experiments:read"] = "91"
	if a := MergeRow(m, "86", "experiments:read"); a != "skip" {
		t.Errorf("colliding key = %q, want skip", a)
	}
	if m.IDRemap["86"] != "91" {
		t.Errorf("remap missing: %v", m.IDRemap)
	}
}

func TestResolveRef(t *testing.T) {
	m := &IDMap{
		ByKey:   map[string]string{"dispatcher": "2", "org_admin": "6"},
		IDRemap: map[string]string{"86": "91"},
	}
	if v, ok := ResolveRef(m, "86"); !ok || v != "91" {
		t.Errorf("remapped id: %v %v", v, ok)
	}
	if v, ok := ResolveRef(m, "4"); !ok || v != "4" {
		t.Errorf("plain id: %v %v", v, ok)
	}
	if v, ok := ResolveRef(m, "org_admin"); !ok || v != "6" {
		t.Errorf("dirty text name: %v %v", v, ok)
	}
	if _, ok := ResolveRef(m, "ghost_role"); ok {
		t.Errorf("unknown name must not resolve")
	}
}
