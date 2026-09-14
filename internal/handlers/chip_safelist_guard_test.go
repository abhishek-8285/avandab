package handlers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guard for the Go-emitted chip seam (docs/10 §6b, chips emitted from Go).
//
// Classes built in Go string literals are invisible to Tailwind's template
// scanner, so they only reach the compiled bundle via the `@source inline`
// safelist in src/input.css. Adding a chip class in Go without adding it
// there renders silently unstyled — no error, no test failure, just a broken
// control in production. This pins the two sides together:
//
//  1. Every tone the badge helpers can return must be safelisted.
//  2. Every chip-family token inside a Go `class="..."` attribute must be
//     safelisted (dashboard `badge-*` template DATA keys and prose like
//     "one-line" live outside class attributes and are correctly ignored).
var (
	safelistRe  = regexp.MustCompile(`@source inline\("([^"]+)"\)`)
	classAttrRe = regexp.MustCompile(`class="([^"]+)"`)
)

func safelistedClasses(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "src", "input.css"))
	if err != nil {
		t.Fatalf("reading src/input.css: %v", err)
	}
	set := map[string]bool{}
	for _, m := range safelistRe.FindAllSubmatch(raw, -1) {
		for _, cls := range strings.Fields(string(m[1])) {
			set[cls] = true
		}
	}
	if len(set) == 0 {
		t.Fatal("no @source inline safelist found in src/input.css")
	}
	return set
}

func TestGoEmittedBadgeTonesSafelisted(t *testing.T) {
	allowed := safelistedClasses(t)

	statuses := []string{
		"pending", "confirmed", "completed", "cancelled", "draft",
		"scheduled", "assigned", "started", "reached_pickup", "in_transit",
		"delivered", "available", "on_trip", "maintenance", "running",
		"inactive", "paid", "partially_paid", "something-new",
	}
	for _, s := range statuses {
		if cls := statusBadgeClass(s); !allowed[cls] {
			t.Errorf("statusBadgeClass(%q) = %q, missing from @source inline safelist in src/input.css", s, cls)
		}
	}
	for _, tier := range []string{"A", "B", "C", "Z"} {
		if cls := string(tierBadgeClass(tier)); !allowed[cls] {
			t.Errorf("tierBadgeClass(%q) = %q, missing from @source inline safelist in src/input.css", tier, cls)
		}
	}
}

func TestGoEmittedChipClassesSafelisted(t *testing.T) {
	allowed := safelistedClasses(t)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	isChip := func(tok string) bool {
		if strings.HasPrefix(tok, "badge-") {
			return true
		}
		for _, fam := range []string{"-soft", "-line", "-strong", "-mid"} {
			if strings.HasSuffix(tok, fam) {
				return true
			}
		}
		return false
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range classAttrRe.FindAllSubmatch(raw, -1) {
			for _, tok := range strings.Fields(string(m[1])) {
				// Strip variant prefixes (dark:, hover:) and trailing opacity slash.
				if i := strings.LastIndex(tok, ":"); i >= 0 {
					tok = tok[i+1:]
				}
				tok = strings.TrimSuffix(tok, "/")
				if !isChip(tok) {
					continue
				}
				checked++
				if !allowed[tok] {
					t.Errorf("%s: Go-emitted chip class %q missing from @source inline safelist in src/input.css", f, tok)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found zero Go-emitted chip classes — scanner broken, guard vacuous")
	}
	t.Logf("pinned %d Go-emitted chip class usages to the safelist", checked)
}
