package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

var (
	ErrEmailNotConfigured = fmt.Errorf("email delivery not configured: set SMTP_HOST / SMTP_FROM")
	ErrSMSNotConfigured   = fmt.Errorf("sms delivery not configured: set SMS_WEBHOOK_URL")
)

// EmailSender delivers one plain-text email.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// SMSSender delivers one SMS text message.
type SMSSender interface {
	Send(ctx context.Context, to, message string) error
}

// SMTPConfig holds the SMTP relay settings loaded from env.
type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
}

// SMTPEmailSender sends email through an SMTP relay using STARTTLS when the
// server advertises it and AUTH PLAIN when credentials are set. Unconfigured
// (empty host) sends fail honestly with ErrEmailNotConfigured.
type SMTPEmailSender struct {
	cfg SMTPConfig
}

func NewSMTPEmailSender(cfg SMTPConfig) *SMTPEmailSender {
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	return &SMTPEmailSender{cfg: cfg}
}

func (s *SMTPEmailSender) Configured() bool { return s.cfg.Host != "" && s.cfg.From != "" }

func (s *SMTPEmailSender) Send(ctx context.Context, to, subject, body string) error {
	if !s.Configured() {
		return ErrEmailNotConfigured
	}
	addr := s.cfg.Host + ":" + s.cfg.Port

	var msg bytes.Buffer
	writeHeader(&msg, "From", s.cfg.From)
	writeHeader(&msg, "To", to)
	writeHeader(&msg, "Subject", mime.QEncoding.Encode("utf-8", subject))
	writeHeader(&msg, "MIME-Version", "1.0")
	writeHeader(&msg, "Content-Type", `text/plain; charset="utf-8"`)
	msg.WriteString("\r\n")
	msg.WriteString(body)

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConf := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	var c *smtp.Client
	if s.cfg.Port == "465" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsConf}
		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("smtp dial %s (implicit tls): %w", addr, err)
		}
		c, err = smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("smtp dial %s: %w", addr, err)
		}
		c, err = smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if terr := c.StartTLS(tlsConf); terr != nil {
				_ = c.Close()
				return fmt.Errorf("smtp starttls: %w", terr)
			}
		}
	}
	defer func() { _ = c.Close() }()

	if s.cfg.User != "" {
		auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
		if aerr := c.Auth(auth); aerr != nil {
			return fmt.Errorf("smtp auth: %w", aerr)
		}
	}
	if aerr := c.Mail(s.cfg.From); aerr != nil {
		return fmt.Errorf("smtp mail from: %w", aerr)
	}
	if aerr := c.Rcpt(to); aerr != nil {
		return fmt.Errorf("smtp rcpt: %w", aerr)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg.Bytes()); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

// WebhookSMSSender posts {to, message} JSON to a configured webhook URL —
// provider-agnostic so any gateway (MSG91 bridge, Twilio proxy, custom) works.
// Non-2xx responses are errors; unconfigured sends fail with ErrSMSNotConfigured.
type WebhookSMSSender struct {
	url    string
	secret string
	client *http.Client
}

func NewWebhookSMSSender(url, secret string) *WebhookSMSSender {
	return &WebhookSMSSender{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *WebhookSMSSender) Configured() bool { return s.url != "" }

func (s *WebhookSMSSender) Send(ctx context.Context, to, message string) error {
	if !s.Configured() {
		return ErrSMSNotConfigured
	}
	payload, err := json.Marshal(map[string]string{"to": to, "message": message})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.secret != "" {
		req.Header.Set("X-SMS-Secret", s.secret)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sms webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(sanitizeHeaderValue(value))
	buf.WriteString("\r\n")
}

// sanitizeHeaderValue strips CR/LF to prevent header injection via user data.
func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}
