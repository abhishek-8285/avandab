package privacy

import "strings"

// MaskPhone reveals only the last 4 digits of a phone number on public,
// login-free pages (ePOD certificate, public pay). Empty input stays empty.
// 10+ digits: "••••••1234". Shorter values are fully masked when non-empty.
func MaskPhone(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, p)
	if len(digits) < 8 {
		return "••••"
	}
	return "••••••" + digits[len(digits)-4:]
}

// MaskEmail reveals the first character of the local part and the full
// domain: "a***@example.com". Unparseable or short inputs collapse to "•••".
func MaskEmail(e string) string {
	e = strings.TrimSpace(e)
	if e == "" {
		return ""
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 {
		return "•••"
	}
	local, domain := e[:at], e[at:]
	prefix := string(local[0]) + "***"
	return prefix + domain
}
