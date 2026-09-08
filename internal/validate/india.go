// Package validate holds India-specific format validators shared by web
// forms (HTML pattern attrs) and server-side handlers.
package validate

import "regexp"

// GSTIN: 2-digit state code + PAN-shaped block + entity/blank/Z + checksum char.
var gstinRe = regexp.MustCompile(`^\d{2}[A-Z]{5}\d{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

// PAN: 5 letters, 4 digits, 1 letter.
var panRe = regexp.MustCompile(`^[A-Z]{5}\d{4}[A-Z]$`)

// Indian mobile: optional +91/0/91 prefix, then 10 digits starting 6-9.
// Spaces/dashes accepted between groups.
var phoneRe = regexp.MustCompile(`^(?:\+?91[\s-]?|0)?[6-9]\d{4}[\s-]?\d{5}$`)

// Vehicle registration (loose, all India RTO styles): MH01AB1234, DL8C1234,
// KA05E9988 etc. Two letters, 1-2 digits, up to 3 letters, 4 digits.
var vehicleRegRe = regexp.MustCompile(`^[A-Z]{2}\s?\d{1,2}\s?[A-Z]{0,3}\s?\d{4}$`)

func ValidGSTIN(s string) bool      { return gstinRe.MatchString(s) }
func ValidPAN(s string) bool        { return panRe.MatchString(s) }
func ValidPhoneIN(s string) bool    { return phoneRe.MatchString(s) }
func ValidVehicleReg(s string) bool { return vehicleRegRe.MatchString(s) }

// gstinAlphabet is the base-36 code set for the GSTIN check digit.
const gstinAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// ValidGSTINChecksum verifies the 15th GSTIN character against the GSTN
// mod-36 check-digit algorithm over the first 14: each char value (0-35)
// weighted by alternating factors 1,2, products digit-summed in base 36,
// check = (36 - sum%36) % 36. Catches typos/transpositions a regex waves
// through (e.g. ...1ZV vs ...1ZX). It proves the number is well-formed,
// NOT that the taxpayer is registered — only the GST portal confirms that.
func ValidGSTINChecksum(s string) bool {
	if len(s) != 15 {
		return false
	}
	total := 0
	for i := 0; i < 14; i++ {
		c := s[i]
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
		default:
			return false
		}
		if i%2 == 1 {
			v *= 2
		}
		total += v/36 + v%36
	}
	return gstinAlphabet[(36-total%36)%36] == s[14]
}

// GSTINPattern / PANPattern / PhonePattern / VehicleRegPattern are the HTML
// input `pattern` attribute equivalents of the validators above.
const (
	GSTINPattern      = `\d{2}[A-Z]{5}\d{4}[A-Z][1-9A-Z]Z[0-9A-Z]`
	PANPattern        = `[A-Z]{5}\d{4}[A-Z]`
	PhonePattern      = `(\+?91[\s-]?|0)?[6-9][0-9]{4}[\s-]?[0-9]{5}`
	VehicleRegPattern = `[A-Z]{2}\s?\d{1,2}\s?[A-Z]{0,3}\s?\d{4}`
)
