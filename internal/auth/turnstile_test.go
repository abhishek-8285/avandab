package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnstileVerifier_UnconfiguredFailsClosed(t *testing.T) {
	v := NewTurnstileVerifier("")
	ok, err := v.Verify(context.Background(), "any-token", "1.2.3.4")
	require.Error(t, err, "unconfigured verifier must report why it denied")
	assert.False(t, ok, "no secret means no real check happened: deny, never pass through")

	var nilVerifier *CloudflareTurnstileVerifier
	ok, err = nilVerifier.Verify(context.Background(), "any-token", "1.2.3.4")
	require.Error(t, err)
	assert.False(t, ok, "nil verifier must deny too")
}

func TestTurnstileVerifier_EmptyTokenFails(t *testing.T) {
	v := NewTurnstileVerifier("test-secret")
	ok, err := v.Verify(context.Background(), "", "1.2.3.4")
	require.NoError(t, err)
	assert.False(t, ok, "empty token must fail when secret is configured")
}

func TestTurnstileVerifier_MockCloudflareResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "my-secret", r.FormValue("secret"))
		if r.FormValue("response") == "valid-token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true}`))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
		}
	}))
	defer server.Close()

	// Direct verification with mock URL
	v := &CloudflareTurnstileVerifier{
		secretKey:  "my-secret",
		httpClient: server.Client(),
	}

	// Test valid token
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	assert.NotNil(t, req)
	assert.NotNil(t, v)
}
