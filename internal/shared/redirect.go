package shared

import (
	"net/url"
	"strings"
)

// SafeRedirect returns raw only when it is a same-origin absolute path safe
// for a redirect. It rejects:
//   - protocol-relative ("//host") and absolute URLs,
//   - backslashes (browsers normalize "\" to "/", so "/\evil.com" is treated
//     as "//evil.com"),
//   - percent-encoded separators ("%2f", "%5c") that a downstream decoder
//     could reinterpret,
//   - control characters and whitespace that header parsing may strip.
//
// The returned value is re-serialized from the parsed URL so callers redirect
// to exactly the path/query that was validated.
func SafeRedirect(raw string) string {
	if raw == "" {
		return ""
	}
	// Reject every raw control byte (browsers strip several of them, turning
	// "/\x0b/evil.com" into "//evil.com") and backslashes.
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' || raw[i] < 0x20 || raw[i] == 0x7f {
			return ""
		}
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Scheme != "" || u.Host != "" {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	// Percent-encoded control characters decode into u.Path; reject them too.
	if strings.IndexFunc(u.Path, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return ""
	}
	return u.RequestURI()
}
