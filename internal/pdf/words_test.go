package pdf

import (
	"strings"
	"testing"
)

func TestAmountInWordsIndian(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "Rupees Zero Only"},
		{1, "Rupees One Only"},
		{99, "Rupees Ninety Nine Only"},
		{100, "Rupees One Hundred Only"},
		{1150.50, "Rupees One Thousand One Hundred Fifty and Paise Fifty Only"},
		{25000, "Rupees Twenty Five Thousand Only"},
		{150000, "Rupees One Lakh Fifty Thousand Only"},
		{1234567, "Rupees Twelve Lakh Thirty Four Thousand Five Hundred Sixty Seven Only"},
		{100000000, "Rupees Ten Crore Only"},
		{12345678901, "Rupees One Thousand Two Hundred Thirty Four Crore Fifty Six Lakh Seventy Eight Thousand Nine Hundred One Only"},
	}
	for _, c := range cases {
		if got := AmountInWordsIndian(c.in); got != c.want {
			t.Errorf("AmountInWordsIndian(%v) = %q, want %q", c.in, got, c.want)
		}
	}

	if got := AmountInWordsIndian(0.99); !strings.HasPrefix(got, "Paise Ninety Nine") || !strings.Contains(got, "Only") {
		t.Errorf("paise-only amount = %q", got)
	}
	if got := AmountInWordsIndian(-500); !strings.HasPrefix(got, "Minus Rupees Five Hundred") {
		t.Errorf("negative = %q", got)
	}
}
