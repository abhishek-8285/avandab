package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPublicPayData_MasksCustomerPII guards audit finding M2 (2026-09-17):
// /pay/{invoiceId} is intentionally public so a customer can settle an
// invoice from a link, but the rendered page + Razorpay prefill previously
// carried the customer's raw phone number and email address. Both must be
// masked at the handler boundary before reaching the template.
func TestPublicPayData_MasksCustomerPII(t *testing.T) {
	t.Run("phone reveals only last 4 digits", func(t *testing.T) {
		assert.Equal(t, "••••••3210", maskPhone("+919876543210"))
		assert.Equal(t, "", maskPhone(""), "empty stays empty")
	})
	t.Run("email reveals only first char + domain", func(t *testing.T) {
		assert.Equal(t, "r***@example.com", maskEmail("ramesh@example.com"))
		assert.Equal(t, "", maskEmail(""))
	})
	t.Run("short/unparseable values fully collapse", func(t *testing.T) {
		assert.Equal(t, "••••", maskPhone("123"))
		assert.Equal(t, "•••", maskEmail("notanemail"))
	})
}
