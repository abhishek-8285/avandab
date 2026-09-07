package fastag

import (
	"testing"
	"time"
)

// LoadConfig with nil DB must return safe defaults without touching storage.
func TestLoadConfig_NilDBDefaults(t *testing.T) {
	cfg := LoadConfig(nil)
	if !cfg.AutoKharcha {
		t.Error("AutoKharcha should default to true")
	}
	if cfg.Provider != "MOCK" {
		t.Errorf("Provider should default to %q, got %q", "MOCK", cfg.Provider)
	}
}

// Constructor must wire config without requiring DB/client connections.
func TestNewFASTagService_Constructor(t *testing.T) {
	cfg := Config{AutoKharcha: false, MerchantID: "M123", Provider: "MOCK"}
	s := NewFASTagService(nil, nil, cfg)
	if s == nil {
		t.Fatal("NewFASTagService returned nil")
	}
	if s.config != cfg {
		t.Errorf("config not wired: got %+v", s.config)
	}
}

// parseTimeFlexible must handle provider timestamp formats without infra.
func TestParseTimeFlexible(t *testing.T) {
	cases := map[string]string{
		time.RFC3339:          "2026-09-01T10:30:00Z",
		"2006-01-02 15:04:05": "2026-09-01 10:30:00",
		"2006-01-02":          "2026-09-01",
	}
	for format, input := range cases {
		got := parseTimeFlexible(input)
		want, err := time.Parse(format, input)
		if err != nil {
			t.Fatalf("bad test case %q: %v", input, err)
		}
		if !got.Equal(want) {
			t.Errorf("parseTimeFlexible(%q) = %v, want %v", input, got, want)
		}
	}

	before := time.Now()
	got := parseTimeFlexible("not-a-timestamp")
	if got.Before(before) || time.Since(got) > time.Minute {
		t.Errorf("unparseable input should fall back to now, got %v", got)
	}
}
