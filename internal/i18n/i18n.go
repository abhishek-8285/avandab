// Package i18n serves web-UI translations. Progressive coverage: templates
// call {{t "key"}}; missing keys fall back to English, then to the key itself,
// so untranslated pages keep working while translations land.
package i18n

import (
	"embed"
	"encoding/json"
	"log"
	"strings"
)

//go:embed en.json hi.json
var files embed.FS

var bundles = map[string]map[string]string{}

func init() {
	for _, lang := range []string{"en", "hi"} {
		b, err := files.ReadFile(lang + ".json")
		if err != nil {
			log.Printf("i18n: cannot read %s bundle: %v", lang, err)
			continue
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			log.Printf("i18n: cannot parse %s bundle: %v", lang, err)
			continue
		}
		bundles[lang] = m
	}
}

// Normalize maps any Accept-Language-ish value to a known bundle.
func Normalize(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(l, "hi") || strings.Contains(lang, "हिन्दी") {
		return "hi"
	}
	return "en"
}

// Available reports whether a bundle exists for the normalized language.
func Available(lang string) bool {
	_, ok := bundles[Normalize(lang)]
	return ok
}

// T looks up key in lang, then English, then returns the key itself.
// A key present-but-empty in the requested language returns "" — that is
// an intentional blank (e.g. Hindi word order drops an English prefix),
// not a missing translation. English fallback applies only to absent keys.
func T(lang, key string) string {
	if m := bundles[Normalize(lang)]; m != nil {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if m := bundles["en"]; m != nil {
		if v, ok := m[key]; ok && v != "" {
			return v
		}
	}
	return key
}
