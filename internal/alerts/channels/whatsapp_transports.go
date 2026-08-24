package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
