package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inventedStatPattern catches fabricated marketing metrics ("95% fewer
// claims", "3x faster", "100% coverage"). The public feature pages must
// describe capabilities, never invented outcomes (AGENTS.md prime directive).
func inventedStatPattern() *regexp.Regexp {
	return regexp.MustCompile(`\b\d+(\.\d+)?\s*%|\b\d+x\b`)
}

// TestFeatureRegistry_NoUnverifiableClaims is the truth ratchet for the
// login-free /features/<slug> explainer pages.
func TestFeatureRegistry_NoUnverifiableClaims(t *testing.T) {
	for slug, f := range featureRegistry {
		var b strings.Builder
		add := func(parts ...string) {
			for _, p := range parts {
				b.WriteString(p)
				b.WriteByte('\n')
			}
		}
		add(f.Title, f.Tagline, f.Audience, f.Summary, f.Lead, f.WhatItIs, f.WhoFor)
		for _, c := range f.Capabilities {
			add(c.Title, c.Text)
		}
		for _, benefit := range f.Benefits {
			add(benefit.Text)
		}
		add(f.Steps...)
		add(f.UseCases...)
		for _, q := range f.FAQ {
			add(q.Question, q.Answer)
		}
		text := b.String()

		assert.NotRegexp(t, inventedStatPattern(), text, "%s: marketing copy must not carry invented stats", slug)
		for _, phrase := range []string{
			"guarantee", "zero gaps", "dispute-free", "pass 100%",
			"SAP ", "Oracle", "ISO 27001", "SOC 2", "certified secure",
		} {
			assert.NotContains(t, strings.ToLower(text), strings.ToLower(phrase),
				"%s: unverifiable claim %q", slug, phrase)
		}
	}
}

// TestHomeCopy_NoAbsoluteClaims guards the landing-page copy in both
// languages: no hardware-free claim (GPS needs devices) and no invented
// compliance/stat claims.
func TestHomeCopy_NoAbsoluteClaims(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	for _, lang := range []string{"en", "hi"} {
		raw, err := os.ReadFile(filepath.Join("internal", "i18n", lang+".json"))
		require.NoError(t, err)
		var copyMap map[string]string
		require.NoError(t, json.Unmarshal(raw, &copyMap), "%s.json must be valid", lang)

		assert.NotContains(t, strings.ToLower(copyMap["home.f_hw"]), "no hardware",
			"%s: home.f_hw overclaims hardware-free operation", lang)
		assert.NotContains(t, strings.ToLower(copyMap["home.g_comp"]), "built-in",
			"%s: home.g_comp overclaims compliance", lang)

		for key, value := range copyMap {
			if strings.HasPrefix(key, "home.") {
				assert.NotRegexp(t, inventedStatPattern(), value,
					"%s: home copy %s must not carry invented stats", lang, key)
			}
		}
	}
}
