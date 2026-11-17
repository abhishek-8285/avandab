package shared_test

import (
	"testing"

	"transport-app/internal/shared"
)

// TestSafeRedirect_RejectsControlAndSeparatorPayloads guards audit finding H5
// (2026-11-12): the old check only rejected "//", absolute URLs and non-paths.
// Browsers normalize "\" to "/" and strip several control characters before
// URL parsing, so "/\evil.com" and "/\x0b/evil.com" became "//evil.com".
// Percent-encoded separators/controls must not survive either.
func TestSafeRedirect_RejectsControlAndSeparatorPayloads(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain path", "/trips/123", "/trips/123"},
		{"query kept", "/trips/123?tab=costs", "/trips/123?tab=costs"},

		{"protocol relative", "//evil.com/x", ""},
		{"backslash", "/\\evil.com/x", ""},
		{"encoded slash", "/%2f%2fevil.com/x", ""},
		{"encoded backslash", "/%5cevil.com/x", ""},
		{"raw vertical tab", "/\x0b/evil.com", ""},
		{"encoded vertical tab", "/%0b/evil.com", ""},
		{"absolute url", "https://evil.com/x", ""},
		{"relative without slash", "trips/123", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shared.SafeRedirect(tc.in); got != tc.want {
				t.Fatalf("SafeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
