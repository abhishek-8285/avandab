package razorpay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fix #13: 19.99 INR must become 1999 paise, not 1998 (float truncation).
func TestCreateOrder_RoundsPaiseInsteadOfTruncating(t *testing.T) {
	api := &fakeOrderAPI{}
	client := NewRazorpayClient("key_id", "key_secret")
	client.orderAPI = api

	order, err := client.CreateOrder("inv_paise_13", 19.99, "INR")
	require.NoError(t, err)
	require.NotNil(t, order)

	assert.Equal(t, int64(1999), order.Amount, "19.99 INR must become 1999 paise")
	assert.Equal(t, int64(1999), api.got["amount"], "payload must carry rounded paise")
}
