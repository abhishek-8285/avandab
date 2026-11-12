package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TwilioConfig holds the credentials and sender phone for Twilio SMS.
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
	FromNumber string
	Endpoint   string // optional; overrides the default API URL (useful for tests)
}

// TwilioSMSSender delivers SMS via Twilio REST API.
type TwilioSMSSender struct {
	cfg    TwilioConfig
	client *http.Client
}

func NewTwilioSMSSender(cfg TwilioConfig) *TwilioSMSSender {
	return &TwilioSMSSender{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *TwilioSMSSender) Configured() bool {
	return s.cfg.AccountSID != "" && s.cfg.AuthToken != "" && s.cfg.FromNumber != ""
}

type twilioErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func (s *TwilioSMSSender) Send(ctx context.Context, to, message string) error {
	if !s.Configured() {
		return ErrSMSNotConfigured
	}

	trimmedTo := strings.TrimSpace(to)
	if trimmedTo == "" || !strings.HasPrefix(trimmedTo, "+") {
		return fmt.Errorf("twilio: recipient phone must be in E.164 format with leading '+', got %q", to)
	}
	for _, r := range trimmedTo[1:] {
		if r < '0' || r > '9' {
			return fmt.Errorf("twilio: phone must contain only digits after '+', got %q", to)
		}
	}

	endpoint := s.cfg.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", s.cfg.AccountSID)
	}

	data := url.Values{}
	data.Set("To", trimmedTo)
	data.Set("From", s.cfg.FromNumber)
	data.Set("Body", message)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.cfg.AccountSID, s.cfg.AuthToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var twErr twilioErrorResponse
		if jsonErr := json.Unmarshal(body, &twErr); jsonErr == nil && twErr.Message != "" {
			return fmt.Errorf("twilio gateway returned status %d: %s", resp.StatusCode, twErr.Message)
		}
		return fmt.Errorf("twilio gateway returned status %d: %s", resp.StatusCode, truncateBody(body))
	}
	return nil
}
