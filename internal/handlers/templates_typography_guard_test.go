package handlers

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The 2026-09-14 frontend sweep removed every text-[9px]/text-[10px] caption
// (docs/10 §6b: "Zero sub-11px text remains"). Later dashboards (video, cold
// chain, control tower) reintroduced the class, so the floor needs a guard, not
// just a one-time cleanup: sub-11px text is a readability/contrast defect that
// no test was catching.
//
// This is a ratchet. It has no allowlist: new and existing templates must stay
// >= 11px. If a future design truly needs smaller text, the decision has to
// show up as a change to this test.
func TestTemplates_NoTextBelow11px(t *testing.T) {
	dir := templatesDirForTest(t)
	sizeRe := regexp.MustCompile(`text-\[(\d+)px\]`)

	var violations []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range sizeRe.FindAllStringSubmatch(string(b), -1) {
			px, convErr := strconv.Atoi(m[1])
			if convErr != nil || px >= 11 {
				continue
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = path
			}
			violations = append(violations, fmt.Sprintf("%s: %s", rel, m[0]))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("%d text size(s) below the 11px readability floor (docs/10 §6b):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

// templatesDirForTest mirrors parseTemplatesLang's directory resolution so the
// guard works from the package directory, the repo root, or CI.
func templatesDirForTest(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("TEMPLATES_DIR"); d != "" {
		return d
	}
	for _, cand := range []string{
		"internal/templates",
		"../../internal/templates",
		"../internal/templates",
		"./internal/templates",
	} {
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
	}
	t.Fatal("templates directory not found; set TEMPLATES_DIR")
	return ""
}
