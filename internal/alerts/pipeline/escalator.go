package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
)

// EscalationStep defines a scheduled escalation target and channel.
// Recipients optionally pins explicit destinations (emails for the email
// channel, phones for sms/whatsapp, user ids for in_app); when empty the step
// resolves TargetRole to active users of the alert's tenant.
type EscalationStep struct {
	AfterSeconds int      `json:"after_seconds"`
	TargetRole   string   `json:"target_role"`
	Channel      string   `json:"channel"`
	Recipients   []string `json:"recipients,omitempty"`
}

// Escalator periodically checks for unresolved alerts that need escalation.
type Escalator struct {
	repo     repository.AlertRepository
	channels map[string]channels.Provider
	logger   *slog.Logger
	clock    Clock
}

// NewEscalator creates a new Escalator.
func NewEscalator(repo repository.AlertRepository, channelMap map[string]channels.Provider, logger *slog.Logger) *Escalator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Escalator{
		repo:     repo,
		channels: channelMap,
		logger:   logger,
		clock:    realClock{},
	}
}

// SetClock allows injecting a mock clock for testing.
func (e *Escalator) SetClock(c Clock) {
	e.clock = c
}

// Run starts the background escalation loop.
func (e *Escalator) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.Tick(ctx); err != nil {
				e.logger.Error("escalator tick failed", "error", err)
			}
		}
	}
}

// Tick evaluates pending escalations at the current time.
func (e *Escalator) Tick(ctx context.Context) error {
	now := e.clock.Now()
	alerts, err := e.repo.ListPendingEscalations(ctx, now)
	if err != nil {
		return err
	}

	for _, a := range alerts {
		if a.RuleID == nil {
			continue
		}

		rule, err := e.repo.GetRule(ctx, *a.RuleID)
		if err != nil || rule == nil || rule.EscalationSchedule == nil || *rule.EscalationSchedule == "" {
			continue
		}

		var schedule []EscalationStep
		if err := json.Unmarshal([]byte(*rule.EscalationSchedule), &schedule); err != nil || len(schedule) == 0 {
			continue
		}

		if a.EscalationStep < len(schedule) {
			currentStep := schedule[a.EscalationStep]

			e.dispatchStep(ctx, &a, currentStep)

			nextIndex := a.EscalationStep + 1
			if nextIndex < len(schedule) {
				nextStep := schedule[nextIndex]
				nextAt := now.Add(time.Duration(nextStep.AfterSeconds) * time.Second)
				_ = e.repo.UpdateEscalation(ctx, a.ID, nextIndex, &nextAt, domain.StatusEscalated)
			} else {
				_ = e.repo.UpdateEscalation(ctx, a.ID, nextIndex, nil, domain.StatusEscalated)
			}

			e.logger.Info("escalated alert", "alert_id", a.ID, "step", a.EscalationStep+1, "target_role", currentStep.TargetRole, "channel", currentStep.Channel)
		}
	}

	return nil
}

// dispatchStep delivers one escalation step. Per-recipient channels (email,
// sms, whatsapp, in_app) send one message per resolved recipient; channels
// without per-user routing (telegram) send once with the resolved recipients
// recorded in metadata. When neither explicit recipients nor target-role users
// exist, the step logs honestly and falls back to the channel's configured
// destination instead of silently dropping the escalation.
func (e *Escalator) dispatchStep(ctx context.Context, a *domain.Alert, step EscalationStep) {
	p, ok := e.channels[step.Channel]
	if !ok || p == nil {
		e.logger.Warn("escalation channel not configured; step skipped",
			"alert_id", a.ID, "channel", step.Channel)
		return
	}
	base := channels.Message{
		AlertID:      a.ID,
		SeverityRank: a.SeverityRank,
		Title:        fmt.Sprintf("[Escalation Step %d] %s", a.EscalationStep+1, a.Title),
		Body:         fmt.Sprintf("Alert unresolved for role %s: %s", step.TargetRole, a.Message),
		Severity:     a.Severity,
		Meta: map[string]any{
			"escalation_step": a.EscalationStep + 1,
			"target_role":     step.TargetRole,
		},
	}

	recipients := e.resolveRecipients(ctx, a, step)
	if len(recipients) == 0 {
		e.logger.Warn("escalation step has no resolvable recipients; using channel default",
			"alert_id", a.ID, "target_role", step.TargetRole, "channel", step.Channel)
		e.sendToChannel(ctx, p, base)
		return
	}

	switch step.Channel {
	case "email", "sms", "whatsapp", "in_app":
		for _, r := range recipients {
			msg := base
			switch step.Channel {
			case "email":
				if r.Email == "" {
					e.logger.Warn("escalation recipient has no email", "alert_id", a.ID, "user_id", r.UserID)
					continue
				}
				msg.Email = r.Email
			case "sms", "whatsapp":
				if r.Phone == "" {
					e.logger.Warn("escalation recipient has no phone", "alert_id", a.ID, "user_id", r.UserID)
					continue
				}
				msg.Phone = r.Phone
			case "in_app":
				if r.UserID == "" {
					continue
				}
				msg.UserID = r.UserID
			}
			e.sendToChannel(ctx, p, msg)
		}
	default:
		base.Meta["target_recipients"] = recipientLabels(recipients)
		e.sendToChannel(ctx, p, base)
	}
}

// resolveRecipients returns the destinations for a step: explicit recipients
// first, then active tenant users holding the target role. An empty slice means
// the step has no resolvable recipient (caller falls back honestly).
func (e *Escalator) resolveRecipients(ctx context.Context, a *domain.Alert, step EscalationStep) []domain.AlertRecipient {
	if len(step.Recipients) > 0 {
		out := make([]domain.AlertRecipient, 0, len(step.Recipients))
		for _, raw := range step.Recipients {
			v := strings.TrimSpace(raw)
			if v == "" {
				continue
			}
			switch step.Channel {
			case "email":
				out = append(out, domain.AlertRecipient{Email: v})
			case "sms", "whatsapp":
				out = append(out, domain.AlertRecipient{Phone: v})
			default:
				out = append(out, domain.AlertRecipient{UserID: v})
			}
		}
		return out
	}
	if strings.TrimSpace(step.TargetRole) == "" {
		return nil
	}
	users, err := e.repo.ListTenantUsersByRole(ctx, a.TenantID, step.TargetRole)
	if err != nil {
		e.logger.Warn("escalation role lookup failed",
			"alert_id", a.ID, "tenant_id", a.TenantID, "target_role", step.TargetRole, "error", err)
		return nil
	}
	return users
}

func (e *Escalator) sendToChannel(ctx context.Context, p channels.Provider, msg channels.Message) {
	if err := p.Send(ctx, msg); err != nil {
		e.logger.Warn("failed to send escalation message", "channel", p.Name(), "alert_id", msg.AlertID, "error", err)
	}
}

func recipientLabels(recipients []domain.AlertRecipient) []string {
	out := make([]string, 0, len(recipients))
	for _, r := range recipients {
		switch {
		case r.UserID != "":
			out = append(out, r.UserID)
		case r.Email != "":
			out = append(out, r.Email)
		case r.Phone != "":
			out = append(out, r.Phone)
		}
	}
	return out
}
