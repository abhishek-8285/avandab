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

// Config drives SMS provider selection in NewSMSSenderFromConfig; empty
// Provider keeps the provider-agnostic webhook sender.
type SMSConfig struct {
	Provider string // "" | "webhook" | "msg91"

	WebhookURL   string
	WebhookToken string

	MSG91AuthKey  string
	MSG91SenderID string
	MSG91Route    string
	MSG91Endpoint string
}

// NewSMSSenderFromConfig picks the SMS delivery engine per config. Unknown
// providers never degrade to a success-looking sender: they return an
// unknownSMSSender that stays unconfigured and fails loudly, exactly like the
// P0 fail-closed gate for every other provider seam.
func NewSMSSenderFromConfig(cfg SMSConfig) SMSSender {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "msg91":
		return NewMSG91SMSSender(MSG91Config{
			AuthKey:  cfg.MSG91AuthKey,
			SenderID: cfg.MSG91SenderID,
			Route:    cfg.MSG91Route,
			Endpoint: cfg.MSG91Endpoint,
		})
	case "", "webhook":
		return NewWebhookSMSSender(cfg.WebhookURL, cfg.WebhookToken)
	default:
		return &unknownSMSSender{provider: strings.TrimSpace(cfg.Provider)}
	}
}

// unknownSMSSender is returned for unrecognized SMS_PROVIDER values: honest
// failures instead of a silently undelivered message.
type unknownSMSSender struct {
	provider string
}

func (u *unknownSMSSender) Configured() bool { return false }

func (u *unknownSMSSender) Send(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("sms delivery: unknown SMS_PROVIDER %q — supported: webhook, msg91", u.provider)
}

// MSG91SMSSender delivers SMS through the MSG91 HTTP API v5
// (POST {Endpoint}/api/v5/sms/ with the authkey header). Message text is
// passed verbatim, so the content must be approved for the sender on the
// MSG91/DLT side; delivery failures are surfaced, never swallowed.
type MSG91SMSSender struct {
	cfg    MSG91Config
	client *http.Client
}

// MSG91Config holds the credentials for the MSG91 HTTP API v5.
type MSG91Config struct {
	AuthKey  string
	SenderID string
	Route    string // optional; defaults to "4" (transactional)
	Endpoint string // optional; overrides the default API URL (tests)
}

const msg91DefaultEndpoint = "https://control.msg91.com/api/v5/sms/"

func NewMSG91SMSSender(cfg MSG91Config) *MSG91SMSSender {
	if cfg.Route == "" {
		cfg.Route = "4"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = msg91DefaultEndpoint
	}
	return &MSG91SMSSender{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *MSG91SMSSender) Configured() bool {
	return s.cfg.AuthKey != "" && s.cfg.SenderID != ""
}

type msg91SMSList struct {
	Sender string         `json:"sender"`
	Route  string         `json:"route"`
	SMS    []msg91SMSPart `json:"sms"`
}

type msg91SMSPart struct {
	Message string   `json:"message"`
	To      []string `json:"to"`
}

type msg91Response struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (s *MSG91SMSSender) Send(ctx context.Context, to, message string) error {
	if !s.Configured() {
		return ErrSMSNotConfigured
	}
	// MSG91 wants digits only, country code included, no leading "+".
	mobile := strings.TrimPrefix(strings.TrimSpace(to), "+")
	if mobile == "" {
		return fmt.Errorf("msg91: phone required")
	}
	for _, r := range mobile {
		if r < '0' || r > '9' {
			return fmt.Errorf("msg91: phone must be digits after the leading '+', got %q", to)
		}
	}

	payload, err := json.Marshal(msg91SMSList{
		Sender: s.cfg.SenderID,
		Route:  s.cfg.Route,
		SMS: []msg91SMSPart{{
			Message: message,
			To:      []string{mobile},
		}},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authkey", s.cfg.AuthKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("msg91: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("msg91 gateway returned status %d: %s", resp.StatusCode, truncateBody(body))
	}
	var out msg91Response
	if err := json.Unmarshal(body, &out); err != nil || strings.EqualFold(strings.TrimSpace(out.Type), "error") {
		return fmt.Errorf("msg91 gateway returned status %d: %s", resp.StatusCode, truncateBody(body))
	}
	return nil
}

// truncateBody keeps gateway error text readable without dumping megabytes.
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 256 {
		s = s[:256] + "…"
	}
	if s == "" {
		return "<empty body>"
	}
	return s
}
