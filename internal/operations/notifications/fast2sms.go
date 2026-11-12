package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Fast2SMSConfig holds the credentials and route settings for Fast2SMS.
type Fast2SMSConfig struct {
	APIKey   string
	SenderID string // optional; defaults to "FSTSMS"
	Route    string // optional; defaults to "v3"
	Endpoint string // optional; overrides the default API URL (useful for tests)
}

// Fast2SMSSender delivers SMS via Fast2SMS Bulk SMS HTTP API.
type Fast2SMSSender struct {
	cfg    Fast2SMSConfig
	client *http.Client
}

const fast2smsDefaultEndpoint = "https://www.fast2sms.com/dev/bulkV2"

func NewFast2SMSSender(cfg Fast2SMSConfig) *Fast2SMSSender {
	if cfg.Route == "" {
		cfg.Route = "v3"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fast2smsDefaultEndpoint
	}
	return &Fast2SMSSender{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Fast2SMSSender) Configured() bool {
	return strings.TrimSpace(s.cfg.APIKey) != ""
}

type fast2smsPayload struct {
	Route    string `json:"route"`
	SenderID string `json:"sender_id,omitempty"`
	Message  string `json:"message"`
	Language string `json:"language"`
	Flash    int    `json:"flash"`
	Numbers  string `json:"numbers"`
}

type fast2smsResponse struct {
	Return    bool     `json:"return"`
	RequestID string   `json:"request_id"`
	Message   []string `json:"message"`
}

func (s *Fast2SMSSender) Send(ctx context.Context, to, message string) error {
	if !s.Configured() {
		return ErrSMSNotConfigured
	}

	// Fast2SMS requires numbers without leading '+'
	mobile := strings.TrimPrefix(strings.TrimSpace(to), "+")
	if mobile == "" {
		return fmt.Errorf("fast2sms: phone required")
	}
	for _, r := range mobile {
		if r < '0' || r > '9' {
			return fmt.Errorf("fast2sms: phone must be digits after leading '+', got %q", to)
		}
	}

	payload := fast2smsPayload{
		Route:    s.cfg.Route,
		SenderID: s.cfg.SenderID,
		Message:  message,
		Language: "english",
		Flash:    0,
		Numbers:  mobile,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", s.cfg.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fast2sms: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fast2sms gateway returned status %d: %s", resp.StatusCode, truncateBody(body))
	}

	var out fast2smsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("fast2sms gateway returned status %d with unparseable body: %s", resp.StatusCode, truncateBody(body))
	}
	if !out.Return {
		msg := strings.Join(out.Message, "; ")
		if msg == "" {
			msg = "gateway indicated failure"
		}
		return fmt.Errorf("fast2sms gateway rejected dispatch: %s", msg)
	}

	return nil
}
