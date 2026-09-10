package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TurnstileVerifier validates Cloudflare Turnstile CAPTCHA tokens.
type TurnstileVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// CloudflareTurnstileVerifier implements TurnstileVerifier using Cloudflare siteverify API.
type CloudflareTurnstileVerifier struct {
	secretKey  string
	httpClient *http.Client
}

// NewTurnstileVerifier constructs a verifier with a 5-second HTTP timeout.
func NewTurnstileVerifier(secretKey string) *CloudflareTurnstileVerifier {
	return &CloudflareTurnstileVerifier{
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

// Verify checks the client token against Cloudflare's validation endpoint.
// Returns true when secretKey is empty (disabled in development/testing).
func (v *CloudflareTurnstileVerifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if v == nil || v.secretKey == "" {
		return true, nil // self-heal / pass-through when unconfigured
	}
	if token == "" {
		return false, nil
	}

	form := url.Values{}
	form.Set("secret", v.secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result turnstileVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}
	return result.Success, nil
}
