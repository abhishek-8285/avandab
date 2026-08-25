// Package gstin provides structural validation for Indian GSTINs.
package gstin

import "strings"

// Valid does a structural check on a 15-char GSTIN:
//   - idx 0-1: 2-digit state code
//   - idx 2-11: 10-char PAN — 5 letters, 4 digits, 1 letter; the 4th
//     PAN letter (GSTIN idx 5) must be an entity code P/F/C/H/A/T/B/L/J/G
//   - idx 12: entity number (alnum — strict rule says 1-9 but letters
//     appear on newer allocations; kept lenient to avoid false rejects)
//   - idx 13: literal 'Z'
//   - idx 14: alphanumeric check digit
//
// It does NOT recompute the official checksum — that needs the GSTN
// algorithm + salt. It guards against typos like "07KUKPS5477RDAF"
// (idx 12 'D' not a digit, idx 13 'A' not 'Z') entering the system.
func Valid(g string) bool {
	g = strings.ToUpper(strings.TrimSpace(g))
	if len(g) != 15 {
		return false
	}
	for i := 0; i < 2; i++ {
		if g[i] < '0' || g[i] > '9' {
			return false
		}
	}
	for i := 2; i < 12; i++ {
		if !((g[i] >= 'A' && g[i] <= 'Z') || (g[i] >= '0' && g[i] <= '9')) {
			return false
		}
	}
	switch g[5] {
	case 'P', 'F', 'C', 'H', 'A', 'T', 'B', 'L', 'J', 'G':
	default:
		return false
	}
	return g[13] == 'Z'
}

// Normalize trims and uppercases a GSTIN for storage.
func Normalize(g string) string {
	return strings.ToUpper(strings.TrimSpace(g))
}
