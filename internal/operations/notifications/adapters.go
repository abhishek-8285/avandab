package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
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

// Attachment represents a file attached to an email (e.g., invoice PDF, e-POD receipt).
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
}

// Bytes returns the decoded binary payload of the attachment.
func (a *Attachment) Bytes() []byte {
	if len(a.Data) > 0 {
		return a.Data
	}
	if a.DataBase64 != "" {
		data, err := base64.StdEncoding.DecodeString(a.DataBase64)
		if err == nil {
			return data
		}
	}
	return nil
}

// EmailMessage contains full plain text, HTML body, and optional binary attachments.
type EmailMessage struct {
	To          string       `json:"to"`
	Subject     string       `json:"subject"`
	TextBody    string       `json:"text_body,omitempty"`
	HTMLBody    string       `json:"html_body,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// EmailSender delivers one plain-text email.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// RichEmailSender delivers multipart MIME emails with HTML and optional attachments.
type RichEmailSender interface {
	EmailSender
	Configured() bool
	SendRich(ctx context.Context, msg EmailMessage) error
	SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error
	SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []Attachment) error
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
	Direct   bool
}

// NewEmailSenderFromConfig instantiates the appropriate email delivery engine:
// - Direct MX delivery when Host == "direct" or Direct is true.
// - Localhost delivery on port 25 without auth when Host == "localhost" or "127.0.0.1".
// - Standard SMTP relay otherwise (e.g. smtp.gmail.com).
func NewEmailSenderFromConfig(cfg SMTPConfig) RichEmailSender {
	host := strings.ToLower(strings.TrimSpace(cfg.Host))
	if host == "direct" || cfg.Direct {
		from := cfg.From
		if from == "" {
			from = DefaultFromEmail
		}
		return NewDirectMXEmailSender(DirectMXConfig{
			From: from,
		})
	}
	if host == "localhost" || host == "127.0.0.1" {
		port := cfg.Port
		if port == "" {
			port = "25"
		}
		from := cfg.From
		if from == "" {
			from = DefaultFromEmail
		}
		return NewSMTPEmailSender(SMTPConfig{
			Host:     host,
			Port:     port,
			User:     "", // no auth for local postfix relay
			Password: "",
			From:     from,
		})
	}
	return NewSMTPEmailSender(cfg)
}

// SMTPEmailSender sends email through an SMTP relay using STARTTLS when the
// server advertises it and AUTH PLAIN when credentials are set. Unconfigured
// (empty host) sends fail honestly with ErrEmailNotConfigured.
type SMTPEmailSender struct {
	cfg SMTPConfig
}

func NewSMTPEmailSender(cfg SMTPConfig) *SMTPEmailSender {
	if cfg.Port == "" {
		if cfg.Host == "localhost" || cfg.Host == "127.0.0.1" {
			cfg.Port = "25"
		} else {
			cfg.Port = "587"
		}
	}
	return &SMTPEmailSender{cfg: cfg}
}

func (s *SMTPEmailSender) Configured() bool { return s.cfg.Host != "" && s.cfg.From != "" }

// Send delivers a plain text email (implements EmailSender).
func (s *SMTPEmailSender) Send(ctx context.Context, to, subject, body string) error {
	return s.SendRich(ctx, EmailMessage{
		To:       to,
		Subject:  subject,
		TextBody: body,
	})
}

// SendHTML delivers an email with both plain text fallback and HTML body.
func (s *SMTPEmailSender) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return s.SendRich(ctx, EmailMessage{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	})
}

// SendWithAttachments delivers an email with text, HTML, and binary file attachments.
func (s *SMTPEmailSender) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []Attachment) error {
	return s.SendRich(ctx, EmailMessage{
		To:          to,
		Subject:     subject,
		TextBody:    textBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	})
}

// SendRich builds a compliant MIME payload and delivers via SMTP.
func (s *SMTPEmailSender) SendRich(ctx context.Context, msg EmailMessage) error {
	if !s.Configured() {
		return ErrEmailNotConfigured
	}
	addr := s.cfg.Host + ":" + s.cfg.Port
	mimeData := buildMIMEMessage(s.cfg.From, msg.To, msg.Subject, msg)

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
	if aerr := c.Rcpt(msg.To); aerr != nil {
		return fmt.Errorf("smtp rcpt: %w", aerr)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(mimeData); err != nil {
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

// LogEmailSender captures emails by logging to slog in development or test environments.
type LogEmailSender struct {
	logger *slog.Logger
}

func NewLogEmailSender(logger *slog.Logger) *LogEmailSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogEmailSender{logger: logger}
}

func (l *LogEmailSender) Configured() bool { return true }

func (l *LogEmailSender) Send(ctx context.Context, to, subject, body string) error {
	l.logger.Info("[DEV EMAIL] captured plain email",
		"to", to,
		"subject", subject,
		"body_len", len(body),
	)
	return nil
}

func (l *LogEmailSender) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return l.SendRich(ctx, EmailMessage{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	})
}

func (l *LogEmailSender) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []Attachment) error {
	return l.SendRich(ctx, EmailMessage{
		To:          to,
		Subject:     subject,
		TextBody:    textBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	})
}

func (l *LogEmailSender) SendRich(ctx context.Context, msg EmailMessage) error {
	l.logger.Info("[DEV EMAIL] captured rich email",
		"to", msg.To,
		"subject", msg.Subject,
		"text_len", len(msg.TextBody),
		"html_len", len(msg.HTMLBody),
		"attachments_count", len(msg.Attachments),
	)
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

func makeBoundary(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("==_mime_%s_%d_%s_==", prefix, time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

func writeBase64(buf *bytes.Buffer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		buf.WriteString(enc[:76])
		buf.WriteString("\r\n")
		enc = enc[76:]
	}
	if len(enc) > 0 {
		buf.WriteString(enc)
		buf.WriteString("\r\n")
	}
}

func buildMIMEMessage(from, to, subject string, msg EmailMessage) []byte {
	var buf bytes.Buffer
	writeHeader(&buf, "From", from)
	writeHeader(&buf, "To", to)
	writeHeader(&buf, "Subject", mime.QEncoding.Encode("utf-8", subject))
	writeHeader(&buf, "MIME-Version", "1.0")

	hasAttachments := len(msg.Attachments) > 0
	hasHTML := strings.TrimSpace(msg.HTMLBody) != ""
	hasText := strings.TrimSpace(msg.TextBody) != ""

	// Default to TextBody if neither text nor HTML is set
	if !hasHTML && !hasText {
		hasText = true
	}

	if hasAttachments {
		mixedBoundary := makeBoundary("mixed")
		writeHeader(&buf, "Content-Type", fmt.Sprintf(`multipart/mixed; boundary="%s"`, mixedBoundary))
		buf.WriteString("\r\n")

		// Body part
		buf.WriteString("--" + mixedBoundary + "\r\n")
		if hasText && hasHTML {
			altBoundary := makeBoundary("alt")
			buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altBoundary))

			// Plain text part
			buf.WriteString("--" + altBoundary + "\r\n")
			buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
			buf.WriteString(msg.TextBody + "\r\n\r\n")

			// HTML part
			buf.WriteString("--" + altBoundary + "\r\n")
			buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
			buf.WriteString(msg.HTMLBody + "\r\n\r\n")

			buf.WriteString("--" + altBoundary + "--\r\n")
		} else if hasHTML {
			buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
			buf.WriteString(msg.HTMLBody + "\r\n")
		} else {
			buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
			buf.WriteString(msg.TextBody + "\r\n")
		}

		// Attachments
		for _, att := range msg.Attachments {
			attBytes := att.Bytes()
			if len(attBytes) == 0 && len(att.Data) > 0 {
				attBytes = att.Data
			}
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			filename := att.Filename
			if filename == "" {
				filename = "attachment.dat"
			}

			buf.WriteString("\r\n--" + mixedBoundary + "\r\n")
			buf.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", ct, sanitizeHeaderValue(filename)))
			buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", sanitizeHeaderValue(filename)))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			writeBase64(&buf, attBytes)
		}

		buf.WriteString("\r\n--" + mixedBoundary + "--\r\n")
	} else if hasText && hasHTML {
		altBoundary := makeBoundary("alt")
		writeHeader(&buf, "Content-Type", fmt.Sprintf(`multipart/alternative; boundary="%s"`, altBoundary))
		buf.WriteString("\r\n")

		// Plain text part
		buf.WriteString("--" + altBoundary + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
		buf.WriteString(msg.TextBody + "\r\n\r\n")

		// HTML part
		buf.WriteString("--" + altBoundary + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
		buf.WriteString(msg.HTMLBody + "\r\n\r\n")

		buf.WriteString("--" + altBoundary + "--\r\n")
	} else if hasHTML {
		writeHeader(&buf, "Content-Type", `text/html; charset="utf-8"`)
		buf.WriteString("\r\n")
		buf.WriteString(msg.HTMLBody)
	} else {
		writeHeader(&buf, "Content-Type", `text/plain; charset="utf-8"`)
		buf.WriteString("\r\n")
		buf.WriteString(msg.TextBody)
	}

	return buf.Bytes()
}
