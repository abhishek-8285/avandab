package privacy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskPhone(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"9876543210", "••••••3210"},
		{"+91 98765 43210", "••••••3210"},
		{"12345", "••••"},
		{"+91-9876543210", "••••••3210"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			assert.Equal(t, tt.want, MaskPhone(tt.in))
		})
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"amit.patel@example.com", "a***@example.com"},
		{"x@y.co", "x***@y.co"},
		{"no-at-sign", "•••"},
		{"@example.com", "•••"},
		{"user@", "•••"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			assert.Equal(t, tt.want, MaskEmail(tt.in))
		})
	}
}

// TestMasksCustomerPII guards audit finding M2 (2026-09-17):
// /pay/{invoiceId} is intentionally public so a customer can settle an
// invoice from a link, but the rendered page + Razorpay prefill previously
// carried the customer's raw phone number and email address. Both must be
// masked at the handler boundary before reaching the template.
func TestMasksCustomerPII(t *testing.T) {
	t.Run("phone reveals only last 4 digits", func(t *testing.T) {
		assert.Equal(t, "••••••3210", MaskPhone("+919876543210"))
		assert.Equal(t, "", MaskPhone(""), "empty stays empty")
	})
	t.Run("email reveals only first char + domain", func(t *testing.T) {
		assert.Equal(t, "r***@example.com", MaskEmail("ramesh@example.com"))
		assert.Equal(t, "", MaskEmail(""))
	})
	t.Run("short/unparseable values fully collapse", func(t *testing.T) {
		assert.Equal(t, "••••", MaskPhone("123"))
		assert.Equal(t, "•••", MaskEmail("notanemail"))
	})
}
