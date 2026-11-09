// Package razorpayx is the RazorpayX payout rail (IMPS/NEFT bank payouts and
// penny-drop account validation). Plain HTTP so it runs without the Razorpay
// SDK and is fully testable with httptest.
package razorpayx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"transport-app/internal/settlement/payout"
)

// ErrNotConfigured is returned when key/secret/account are missing.
var ErrNotConfigured = errors.New("razorpayx not configured: key, secret and account number are required")

// Config holds RazorpayX credentials. BaseURL is overridable for tests.
type Config struct {
	KeyID         string
	KeySecret     string
	AccountNumber string
	BaseURL       string
}

// Client implements payout.Provider over RazorpayX's REST API.
type Client struct {
	cfg  Config
	http *http.Client
}

var _ payout.Provider = (*Client)(nil)

// New builds a client. BaseURL defaults to https://api.razorpay.com.
func New(cfg Config) (*Client, error) {
	if cfg.KeyID == "" || cfg.KeySecret == "" || cfg.AccountNumber == "" {
		return nil, ErrNotConfigured
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.razorpay.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}, nil
}

// CreatePayout POSTs /v1/payouts. Idempotent by reference_id (our payout id).
func (c *Client) CreatePayout(ctx context.Context, req payout.CreateRequest) (*payout.CreateResult, error) {
	if req.AccountNumber == "" || req.IFSC == "" || req.HolderName == "" {
		return nil, errors.New("payout account details are incomplete")
	}
	amountPaise := int64(req.AmountINR*100 + 0.5)
	if amountPaise <= 0 {
		return nil, errors.New("payout amount must be positive")
	}
	narration := req.Narration
	if narration == "" {
		narration = "Driver payout"
	}
	body := map[string]any{
		"account_number":       c.cfg.AccountNumber,
		"amount":               amountPaise,
		"currency":             "INR",
		"mode":                 "IMPS",
		"purpose":              "payout",
		"reference_id":         req.PayoutID,
		"narration":            truncate(narration, 30),
		"queue_if_low_balance": true,
		"fund_account": map[string]any{
			"account_type": "bank_account",
			"bank_account": map[string]any{
				"name":           req.HolderName,
				"ifsc":           req.IFSC,
				"account_number": req.AccountNumber,
			},
		},
	}
	var raw struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		UTR         string `json:"utr"`
		FailureDesc string `json:"failure_reason"`
		Error       *struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/payouts", body, &raw); err != nil {
		return nil, err
	}
	if raw.Error != nil && raw.Error.Description != "" {
		return nil, fmt.Errorf("razorpayx payout rejected: %s", raw.Error.Description)
	}
	return &payout.CreateResult{
		ProviderPayoutID: raw.ID,
		Status:           normalizeStatus(raw.Status),
		UTR:              raw.UTR,
		FailureReason:    raw.FailureDesc,
	}, nil
}

// VerifyBankAccount POSTs /v1/fund_accounts/validations (1 rupee penny-drop).
// The final valid/invalid result arrives via webhook.
func (c *Client) VerifyBankAccount(ctx context.Context, req payout.VerifyRequest) (*payout.VerifyResult, error) {
	if req.AccountNumber == "" || req.IFSC == "" || req.HolderName == "" {
		return nil, errors.New("payout account details are incomplete")
	}
	body := map[string]any{
		"account_number": c.cfg.AccountNumber,
		"amount":         100,
		"currency":       "INR",
		"fund_account": map[string]any{
			"account_type": "bank_account",
			"bank_account": map[string]any{
				"name":           req.HolderName,
				"ifsc":           req.IFSC,
				"account_number": req.AccountNumber,
			},
		},
	}
	var raw struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/fund_accounts/validations", body, &raw); err != nil {
		return nil, err
	}
	if raw.ID == "" {
		return nil, errors.New("razorpayx validation returned no fund_account id")
	}
	return &payout.VerifyResult{FundAccountID: raw.ID, Status: raw.Status}, nil
}

// VerifyWebhookSignature validates X-Razorpay-Signature (HMAC-SHA256 hex).
func VerifyWebhookSignature(body []byte, signature, secret string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

func (c *Client) do(ctx context.Context, method, path string, payload any, out any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode razorpayx request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.cfg.KeyID, c.cfg.KeySecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("razorpayx %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read razorpayx response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("razorpayx %s: HTTP %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode razorpayx response: %w", err)
	}
	return nil
}

func normalizeStatus(s string) payout.Status {
	switch strings.ToLower(s) {
	case "processed", "paid":
		return payout.StatusPaid
	case "failed":
		return payout.StatusFailed
	case "reversed":
		return payout.StatusReversed
	case "cancelled":
		return payout.StatusCancelled
	default:
		return payout.StatusProcessing
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
