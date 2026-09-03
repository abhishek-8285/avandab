package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpDoer builds the shared HTTP client used by the BSP transports.
func httpDoer(creds map[string]string) *http.Client {
	_ = creds
	return &http.Client{Timeout: 15 * time.Second}
}

// gupshupSender posts to the Gupshup messages API.
type gupshupSender struct {
	apiKey string
	http   *http.Client
}

func (g gupshupSender) send(ctx context.Context, phone, text string) error {
	if g.apiKey == "" {
		return fmt.Errorf("gupshup: missing api key")
	}
	form := bytes.NewBufferString("channel=whatsapp&source=&destination=" + phone +
		"&message=" + urlQueryEscape(text))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.gupshup.io/wa/api/v1/msg", form)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", g.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doWhatsAppHTTP(g.http, req)
}

// metaSender posts to the WhatsApp Cloud API (Meta).
type metaSender struct {
	token         string
	phoneNumberID string
	http          *http.Client
}

func (m metaSender) send(ctx context.Context, phone, text string) error {
	if m.token == "" || m.phoneNumberID == "" {
		return fmt.Errorf("meta: missing token or phone_number_id")
	}
	payload, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                phone,
		"type":              "text",
		"text":              map[string]string{"body": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://graph.facebook.com/v20.0/"+m.phoneNumberID+"/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Content-Type", "application/json")
	return doWhatsAppHTTP(m.http, req)
}

// evolutionSender posts to Evolution API v1/v2 endpoint.
type evolutionSender struct {
	baseURL  string
	instance string
	apiKey   string
	http     *http.Client
}

func (e evolutionSender) send(ctx context.Context, phone, text string) error {
	if e.baseURL == "" {
		return fmt.Errorf("evolution: missing base url")
	}
	instance := e.instance
	if instance == "" {
		instance = "default"
	}
	base := strings.TrimRight(e.baseURL, "/")
	var endpoint string
	if strings.Contains(base, "/message/sendText") {
		endpoint = base
	} else {
		endpoint = fmt.Sprintf("%s/message/sendText/%s", base, instance)
	}
	payload, err := json.Marshal(map[string]any{
		"number": phone,
		"text":   text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("apikey", e.apiKey)
	}
	return doWhatsAppHTTP(e.http, req)
}

// webhookSender posts to a generic webhook HTTP adapter.
type webhookSender struct {
	url    string
	apiKey string
	token  string
	http   *http.Client
}

func (w webhookSender) send(ctx context.Context, phone, text string) error {
	if w.url == "" {
		return fmt.Errorf("generic webhook: missing url")
	}
	payload, err := json.Marshal(map[string]any{
		"recipient": phone,
		"phone":     phone,
		"number":    phone,
		"to":        phone,
		"message":   text,
		"text":      text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.apiKey != "" {
		req.Header.Set("apikey", w.apiKey)
	}
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}
	return doWhatsAppHTTP(w.http, req)
}

func doWhatsAppHTTP(c *http.Client, req *http.Request) error {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp transport status %d", resp.StatusCode)
	}
	return nil
}

func urlQueryEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('+')
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}
