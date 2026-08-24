package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/repository"
)

// EscalationStep defines a scheduled escalation target and channel.
type EscalationStep struct {
	AfterSeconds int    `json:"after_seconds"`
	TargetRole   string `json:"target_role"`
	Channel      string `json:"channel"`
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

			msg := channels.Message{
				AlertID:      a.ID,
				SeverityRank: a.SeverityRank,
				Title:        fmt.Sprintf("[Escalation Step %d] %s", a.EscalationStep+1, a.Title),
				Body:         fmt.Sprintf("Alert unresolved for role %s: %s", currentStep.TargetRole, a.Message),
				Severity:     a.Severity,
				Meta: map[string]any{
					"escalation_step": a.EscalationStep + 1,
					"target_role":     currentStep.TargetRole,
				},
			}

			if p, ok := e.channels[currentStep.Channel]; ok && p != nil {
				if err := p.Send(ctx, msg); err != nil {
					e.logger.Warn("failed to send escalation message", "channel", currentStep.Channel, "alert_id", a.ID, "error", err)
				}
			}

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
