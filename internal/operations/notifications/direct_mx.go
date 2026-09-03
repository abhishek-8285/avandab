package notifications

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultFromEmail is the fallback sender when SMTP_FROM is unconfigured.
	DefaultFromEmail = "billing@avandab.com"
	// DefaultHeloDomain is the default EHLO identity.
	DefaultHeloDomain = "avandab.com"
)

// DirectMXConfig holds settings and injection hooks for Direct MX email delivery.
type DirectMXConfig struct {
	From       string
	HeloDomain string
	LocalRelay string // when set (e.g. "localhost:25"), sends via local relay instead of resolving MX
	Timeout    time.Duration
	LookupMX   func(ctx context.Context, domain string) ([]*net.MX, error)
	Dialer     func(ctx context.Context, network, addr string) (net.Conn, error)
	TLSConfig  *tls.Config
}

// DirectMXEmailSender delivers emails directly to recipient MX servers or local Postfix relay.
type DirectMXEmailSender struct {
	cfg DirectMXConfig
}

// NewDirectMXEmailSender instantiates a direct MX email delivery engine.
func NewDirectMXEmailSender(cfg DirectMXConfig) *DirectMXEmailSender {
	if cfg.From == "" {
		cfg.From = DefaultFromEmail
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.LookupMX == nil {
		cfg.LookupMX = func(ctx context.Context, domain string) ([]*net.MX, error) {
			return net.DefaultResolver.LookupMX(ctx, domain)
		}
	}
	if cfg.Dialer == nil {
		netDialer := &net.Dialer{Timeout: cfg.Timeout}
		cfg.Dialer = netDialer.DialContext
	}
	if cfg.HeloDomain == "" {
		if _, dom, err := extractDomain(cfg.From); err == nil && dom != "" {
			cfg.HeloDomain = dom
		} else {
			cfg.HeloDomain = DefaultHeloDomain
		}
	}
	return &DirectMXEmailSender{cfg: cfg}
}

// Configured reports whether direct MX sending is active.
func (s *DirectMXEmailSender) Configured() bool {
	return true
}

// Send delivers a plain text email (implements EmailSender).
func (s *DirectMXEmailSender) Send(ctx context.Context, to, subject, body string) error {
	return s.SendRich(ctx, EmailMessage{
		To:       to,
		Subject:  subject,
		TextBody: body,
	})
}

// SendHTML delivers an email with both plain text fallback and HTML body.
func (s *DirectMXEmailSender) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return s.SendRich(ctx, EmailMessage{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	})
}

// SendWithAttachments delivers an email with text, HTML, and binary file attachments.
func (s *DirectMXEmailSender) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []Attachment) error {
	return s.SendRich(ctx, EmailMessage{
		To:          to,
		Subject:     subject,
		TextBody:    textBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	})
}

// SendRich builds a compliant MIME payload and delivers directly to recipient MX servers.
func (s *DirectMXEmailSender) SendRich(ctx context.Context, msg EmailMessage) error {
	if strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("direct_mx: missing recipient email address")
	}

	from := s.cfg.From
	if from == "" {
		from = DefaultFromEmail
	}

	// 1. Local Relay Mode (e.g. Postfix on localhost:25)
	if s.cfg.LocalRelay != "" {
		relayAddr := s.cfg.LocalRelay
		if !strings.Contains(relayAddr, ":") {
			relayAddr = net.JoinHostPort(relayAddr, "25")
		}
		host, _, err := net.SplitHostPort(relayAddr)
		if err != nil {
			host = relayAddr
		}
		return s.deliverToHost(ctx, host, relayAddr, from, msg)
	}

	// 2. Direct MX Mode: Extract domain
	_, domain, err := extractDomain(msg.To)
	if err != nil {
		return fmt.Errorf("direct_mx: %w", err)
	}

	// 3. Resolve DNS MX records
	lookupMX := s.cfg.LookupMX
	if lookupMX == nil {
		lookupMX = func(c context.Context, d string) ([]*net.MX, error) {
			return net.DefaultResolver.LookupMX(c, d)
		}
	}

	mxRecords, err := lookupMX(ctx, domain)
	if err != nil || len(mxRecords) == 0 {
		// RFC 5321 §5.1: Fall back to domain hostname A/AAAA record if MX is absent
		mxRecords = []*net.MX{{Host: domain, Pref: 0}}
	} else {
		// Sort by lowest preference first (highest priority)
		sort.SliceStable(mxRecords, func(i, j int) bool {
			return mxRecords[i].Pref < mxRecords[j].Pref
		})
	}

	// 4. Attempt delivery across MX hosts in priority order
	var deliveryErrors []string
	for _, mx := range mxRecords {
		targetHost := strings.TrimSuffix(strings.TrimSpace(mx.Host), ".")
		if targetHost == "" {
			continue
		}
		addr := net.JoinHostPort(targetHost, "25")
		if err := s.deliverToHost(ctx, targetHost, addr, from, msg); err == nil {
			return nil
		} else {
			deliveryErrors = append(deliveryErrors, fmt.Sprintf("%s (%v)", targetHost, err))
		}
	}

	return fmt.Errorf("direct_mx: failed to deliver to %s across %d MX hosts: %s",
		msg.To, len(mxRecords), strings.Join(deliveryErrors, "; "))
}

// deliverToHost connects to an SMTP host on port 25, executes opportunistic STARTTLS and transmits MIME data.
func (s *DirectMXEmailSender) deliverToHost(ctx context.Context, host, addr, from string, msg EmailMessage) error {
	dialer := s.cfg.Dialer
	if dialer == nil {
		netDialer := &net.Dialer{Timeout: s.cfg.Timeout}
		dialer = netDialer.DialContext
	}

	conn, err := dialer(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if s.cfg.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client %s: %w", host, err)
	}
	defer func() { _ = c.Close() }()

	// EHLO / HELO
	heloDomain := s.cfg.HeloDomain
	if heloDomain == "" {
		heloDomain = DefaultHeloDomain
	}
	if err := c.Hello(heloDomain); err != nil {
		return fmt.Errorf("smtp hello (%s): %w", heloDomain, err)
	}

	// Opportunistic STARTTLS
	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsConf := s.cfg.TLSConfig
		if tlsConf == nil {
			tlsConf = &tls.Config{
				ServerName: host,
				MinVersion: tls.VersionTLS12,
			}
		}
		if terr := c.StartTLS(tlsConf); terr != nil {
			return fmt.Errorf("smtp starttls %s: %w", host, terr)
		}
	}

	// Clean email addresses for envelope commands
	cleanFrom := parseEnvelopeAddress(from)
	cleanTo := parseEnvelopeAddress(msg.To)

	if err := c.Mail(cleanFrom); err != nil {
		return fmt.Errorf("smtp mail from <%s>: %w", cleanFrom, err)
	}
	if err := c.Rcpt(cleanTo); err != nil {
		return fmt.Errorf("smtp rcpt to <%s>: %w", cleanTo, err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	mimeData := buildMIMEMessage(from, msg.To, msg.Subject, msg)
	if _, err := w.Write(mimeData); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write data: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}

	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}

	return nil
}

// extractDomain parses an email address and returns the local part and normalized domain.
func extractDomain(emailAddr string) (localPart, domain string, err error) {
	emailAddr = strings.TrimSpace(emailAddr)
	if emailAddr == "" {
		return "", "", fmt.Errorf("empty email address")
	}
	// Parse RFC 5322 formatted address (e.g. "Full Name <user@domain.com>")
	if parsed, parseErr := mail.ParseAddress(emailAddr); parseErr == nil {
		emailAddr = parsed.Address
	}
	parts := strings.Split(emailAddr, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid email address format: %q", emailAddr)
	}
	domain = strings.ToLower(strings.Trim(parts[1], ". \t\r\n"))
	if domain == "" {
		return "", "", fmt.Errorf("invalid domain in email address: %q", emailAddr)
	}
	return parts[0], domain, nil
}

// parseEnvelopeAddress extracts the bare email address suitable for SMTP MAIL FROM / RCPT TO commands.
func parseEnvelopeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if parsed, err := mail.ParseAddress(addr); err == nil {
		return parsed.Address
	}
	return addr
}
