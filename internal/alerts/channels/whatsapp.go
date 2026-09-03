package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"transport-app/internal/alerts/domain"
)

// WhatsAppMaxRank gates the channel: Spec 22 §10 — only rank 1–3
// (critical/urgent/money) reach WhatsApp by default; waste and info
// (4/5) never do.
const WhatsAppMaxRank = 3

// whatsappSender is the transport behind the gated provider.
type whatsappSender interface {
	send(ctx context.Context, phone, text string) error
}

// WhatsAppProvider delivers rank ≤3 alerts over WhatsApp. Transport is
// config-selected (mock | gupshup | meta); unconfigured → mock, which
// logs the delivery honestly instead of pretending it happened.
type WhatsAppProvider struct {
	gate   bool
	sender whatsappSender
	logger *slog.Logger
}

func NewWhatsAppProvider(provider string, creds map[string]string, logger *slog.Logger) *WhatsAppProvider {
	if logger == nil {
		logger = slog.Default()
	}
	var s whatsappSender = mockWASender{logger: logger}
	switch strings.ToLower(provider) {
	case "gupshup":
		s = gupshupSender{apiKey: creds["api_key"], http: httpDoer(creds)}
	case "meta":
		s = metaSender{token: creds["token"], phoneNumberID: creds["phone_number_id"], http: httpDoer(creds)}
	case "evolution":
		s = evolutionSender{
			baseURL:  creds["url"],
			instance: creds["instance"],
			apiKey:   creds["api_key"],
			http:     httpDoer(creds),
		}
	case "webhook", "generic":
		s = webhookSender{
			url:    creds["url"],
			apiKey: creds["api_key"],
			token:  creds["token"],
			http:   httpDoer(creds),
		}
	default:
		logger.Info("whatsapp provider unconfigured — mock (log-only) sender active")
	}
	return &WhatsAppProvider{gate: true, sender: s, logger: logger}
}

func (p *WhatsAppProvider) Name() string { return "whatsapp" }

func (p *WhatsAppProvider) Send(ctx context.Context, msg Message) error {
	rank := msg.SeverityRank
	if rank == 0 {
		rank = domain.SeverityToRank(msg.Severity)
	}
	if p.gate && rank > WhatsAppMaxRank {
		p.logger.Debug("whatsapp suppressed: rank above channel policy",
			"alert_id", msg.AlertID, "rank", rank)
		return nil
	}
	text := fmt.Sprintf("%s\n%s", msg.Title, msg.Body)
	return p.sender.send(ctx, msg.Phone, text)
}

// SendWhatsApp sends a direct WhatsApp message without alert rank filtering.
func (p *WhatsAppProvider) SendWhatsApp(ctx context.Context, phone, text string) error {
	return p.sender.send(ctx, phone, text)
}

// mockWASender logs the delivery — the S10 exit gate's "mock send logged".
type mockWASender struct{ logger *slog.Logger }

func (m mockWASender) send(ctx context.Context, phone, text string) error {
	m.logger.Info("whatsapp mock send",
		"phone", maskPhone(phone), "chars", len(text))
	return ctx.Err()
}

func maskPhone(p string) string {
	if len(p) <= 4 {
		return "***"
	}
	return "…" + p[len(p)-4:]
}
