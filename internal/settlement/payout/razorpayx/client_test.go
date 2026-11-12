package razorpayx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/settlement/payout"
)

func TestCreatePayout_SendsRazorpayXContract(t *testing.T) {
	var gotUser, gotPass, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pout_rzp_1","status":"processing"}`))
	}))
	defer srv.Close()

	c, err := New(Config{KeyID: "rzp_key", KeySecret: "rzp_secret", AccountNumber: "1234567890", BaseURL: srv.URL})
	require.NoError(t, err)

	res, err := c.CreatePayout(context.Background(), payout.CreateRequest{
		PayoutID: "pout_local_1", HolderName: "Sunil Sharma",
		AccountNumber: "1122334455", IFSC: "SBIN0001234", AmountINR: 123.45,
	})
	require.NoError(t, err)

	assert.Equal(t, "/v1/payouts", gotPath)
	assert.Equal(t, "rzp_key", gotUser)
	assert.Equal(t, "rzp_secret", gotPass)
	assert.Equal(t, float64(12345), gotBody["amount"], "amount must be paise")
	assert.Equal(t, "INR", gotBody["currency"])
	assert.Equal(t, "pout_local_1", gotBody["reference_id"])
	assert.Equal(t, "1234567890", gotBody["account_number"])
	fund := gotBody["fund_account"].(map[string]any)["bank_account"].(map[string]any)
	assert.Equal(t, "1122334455", fund["account_number"])
	assert.Equal(t, "SBIN0001234", fund["ifsc"])

	assert.Equal(t, "pout_rzp_1", res.ProviderPayoutID)
	assert.Equal(t, payout.StatusProcessing, res.Status)
}

func TestCreatePayout_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"description":"insufficient balance"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{KeyID: "k", KeySecret: "s", AccountNumber: "1", BaseURL: srv.URL})
	require.NoError(t, err)
	_, err = c.CreatePayout(context.Background(), payout.CreateRequest{
		PayoutID: "p", HolderName: "H", AccountNumber: "1", IFSC: "SBIN0001", AmountINR: 10,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Contains(t, err.Error(), "insufficient balance")
}

func TestVerifyBankAccount_PennyDropContract(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_, _ = w.Write([]byte(`{"id":"fa_123","status":"created"}`))
	}))
	defer srv.Close()

	c, err := New(Config{KeyID: "k", KeySecret: "s", AccountNumber: "1", BaseURL: srv.URL})
	require.NoError(t, err)
	res, err := c.VerifyBankAccount(context.Background(), payout.VerifyRequest{
		HolderName: "Sunil Sharma", AccountNumber: "1122334455", IFSC: "SBIN0001234",
	})
	require.NoError(t, err)

	assert.Equal(t, "/v1/fund_accounts/validations", gotPath)
	assert.Equal(t, float64(100), gotBody["amount"], "penny-drop is 1 rupee in paise")
	assert.Equal(t, "fa_123", res.FundAccountID)
}

func TestVerifyWebhookSignature(t *testing.T) {
	body := []byte(`{"event":"payout.processed"}`)
	// HMAC-SHA256 of body with secret "whsec".
	if VerifyWebhookSignature(body, "", "whsec") {
		t.Fatal("empty signature must fail")
	}
	if VerifyWebhookSignature(body, "deadbeef", "whsec") {
		t.Fatal("wrong signature must fail")
	}
	// Compute a real signature via the same helper to avoid a brittle constant.
	if !VerifyWebhookSignature(body, hmacHex(body, "whsec"), "whsec") {
		t.Fatal("valid HMAC must pass")
	}
	if VerifyWebhookSignature(body, hmacHex(body, "other"), "whsec") {
		t.Fatal("signature from a different secret must fail")
	}
}

func hmacHex(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
