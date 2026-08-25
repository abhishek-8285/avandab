package gstin

import "testing"

func TestValid(t *testing.T) {
	cases := []struct {
		gstin string
		want  bool
		why   string
	}{
		{"27AABCU9603R1ZX", true, "classic GSTN doc example (company)"},
		{"29AABCB1234C1Z7", true, "valid company GSTIN, different state"},
		{"07AAACP0000M1Z9", true, "valid proprietor GSTIN"},
		{"07KUKPS5477RDAF", false, "real-world bad value: idx12 'D' not digit, idx13 'A' not Z"},
		{"27PQRSX5678K1Z2", false, "entity letter idx5 'S' not in P/F/C/H/A/T/B/L/J/G"},
		{"27AABCU9603R1Z", false, "too short"},
		{"27AABCU9603R1ZX1", false, "too long"},
		{"X7AABCU9603R1ZX", false, "state code not numeric"},
		{"27AABCU9603R1Z7", true, "alnum check digit"},
		{"", false, "empty"},
		{" 27AABCU9603R1ZX ", true, "whitespace tolerated"},
		{"27aabcu9603r1zx", true, "lowercase tolerated"},
	}
	for _, tc := range cases {
		if got := Valid(tc.gstin); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v (%s)", tc.gstin, got, tc.want, tc.why)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(" 27aabcu9603r1zx "); got != "27AABCU9603R1ZX" {
		t.Errorf("Normalize = %q", got)
	}
}
