package handlers

import (
	"testing"
	"unicode/utf8"
)

// TestAbbrString_KeepsMultiByteRunesIntact guards low finding 2
// (2026-11-12): the template `abbr` helper byte-sliced strings, which can cut
// a UTF-8 rune in half and render replacement characters in Hindi text.
func TestAbbrString_KeepsMultiByteRunesIntact(t *testing.T) {
	in := "मुंबई डिपो"

	got := abbrString(in, 5)
	if !utf8.ValidString(got) {
		t.Fatalf("abbr produced invalid UTF-8: %q", got)
	}
	want := string([]rune(in)[:2]) + "..."
	if got != want {
		t.Fatalf("abbr(%q, 5) = %q, want %q", in, got, want)
	}

	short := abbrString("Mumbai", 10)
	if short != "Mumbai" {
		t.Fatalf("short strings must pass through unchanged, got %q", short)
	}
}
