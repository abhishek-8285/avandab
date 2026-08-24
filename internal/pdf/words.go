package pdf

import (
	"fmt"
	"strings"
)

var ones = []string{"", "One", "Two", "Three", "Four", "Five", "Six", "Seven",
	"Eight", "Nine", "Ten", "Eleven", "Twelve", "Thirteen", "Fourteen",
	"Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen"}
var tens = []string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty",
	"Seventy", "Eighty", "Ninety"}

// twoDigits spells 0-99 in words.
func twoDigits(n int) string {
	switch {
	case n < 20:
		return ones[n]
	case n%10 == 0:
		return tens[n/10]
	default:
		return tens[n/10] + " " + ones[n%10]
	}
}

// threeDigits spells 0-999.
func threeDigits(n int) string {
	if n < 100 {
		return twoDigits(n)
	}
	h, rest := n/100, n%100
	out := ones[h] + " Hundred"
	if rest > 0 {
		out += " " + twoDigits(rest)
	}
	return out
}

// indianWords spells a non-negative integer using the Indian system:
// unlimited crore groups (arab/kharab fall out of the recursion),
// then lakh, thousand and hundreds.
func indianWords(n int) string {
	switch {
	case n >= 10000000:
		rest := n % 10000000
		out := indianWords(n/10000000) + " Crore"
		if rest > 0 {
			out += " " + indianWords(rest)
		}
		return out
	case n >= 100000:
		rest := n % 100000
		out := twoDigits(n/100000) + " Lakh"
		if rest > 0 {
			out += " " + indianWords(rest)
		}
		return out
	case n >= 1000:
		rest := n % 1000
		out := twoDigits(n/1000) + " Thousand"
		if rest > 0 {
			out += " " + threeDigits(rest)
		}
		return out
	default:
		return threeDigits(n)
	}
}

// AmountInWordsIndian converts an amount to words using the Indian
// numbering system as required on GST tax invoices. Paise are appended
// when non-zero, e.g. "Rupees One Lakh Fifty Thousand and Paise Fifty Only".
func AmountInWordsIndian(amount float64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	rupees := int(amount)
	paise := int((amount-float64(rupees))*100 + 0.5)

	if rupees == 0 && paise == 0 {
		return "Rupees Zero Only"
	}

	var out string
	switch {
	case rupees > 0 && paise > 0:
		out = fmt.Sprintf("Rupees %s and Paise %s", indianWords(rupees), twoDigits(paise))
	case rupees > 0:
		out = fmt.Sprintf("Rupees %s", indianWords(rupees))
	default:
		out = fmt.Sprintf("Paise %s", twoDigits(paise))
	}
	if neg {
		out = "Minus " + out
	}
	return strings.TrimSpace(out) + " Only"
}
