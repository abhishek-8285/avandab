// Package monitor watches money-movement state that the provider webhook may
// never resolve: payouts parked in initiated/processing past a threshold are
// surfaced as escalating settlement.payout_stuck alerts instead of vanishing.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/settlement/domain"
	"transport-app/internal/shared"
)

// StuckPayoutReader is the narrow read seam the sweeper needs.
type StuckPayoutReader interface {
	ListPayoutsAwaitingCompletion(ctx context.Context, olderThan time.Time, limit int) ([]domain.PayoutInstruction, error)
}

// Sweeper periodically publishes payout_stuck alerts for stale payouts.
type Sweeper struct {
	repo     StuckPayoutReader
	bus      events.EventBus
	logger   *slog.Logger
	after    time.Duration
	interval time.Duration
	limit    int
	now      func() time.Time
}

// NewSweeper builds the sweep. Zero values fall back to 24h stuck threshold,
// 1h tick and 100 rows per pass.
func NewSweeper(repo StuckPayoutReader, bus events.EventBus, after, interval time.Duration, logger *slog.Logger) *Sweeper {
	if after <= 0 {
		after = 24 * time.Hour
	}
	if interval <= 0 {
		interval = time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{repo: repo, bus: bus, logger: logger, after: after, interval: interval, limit: 100, now: time.Now}
}

// Run executes a sweep immediately, then on every interval until ctx is done.
func (s *Sweeper) Run(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	if _, err := s.SweepOnce(ctx); err != nil && ctx.Err() == nil {
		s.logger.Warn("stuck payout sweep failed", "error", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepOnce(ctx); err != nil && ctx.Err() == nil {
				s.logger.Warn("stuck payout sweep failed", "error", err)
			}
		}
	}
}

// SweepOnce publishes one alert event per stale payout and returns the count.
func (s *Sweeper) SweepOnce(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	cutoff := s.now().UTC().Add(-s.after)
	stale, err := s.repo.ListPayoutsAwaitingCompletion(ctx, cutoff, s.limit)
	if err != nil {
		return 0, fmt.Errorf("list stale payouts: %w", err)
	}
	published := 0
	for _, p := range stale {
		age := s.now().UTC().Sub(p.InitiatedAt)
		if p.InitiatedAt.IsZero() {
			age = s.now().UTC().Sub(p.CreatedAt)
		}
		providerRef := ""
		if p.ProviderPayoutID != nil {
			providerRef = *p.ProviderPayoutID
		}
		payload := map[string]any{
			"source":     "settlement",
			"alert_type": "payout_stuck",
			"severity":   "critical",
			"title":      fmt.Sprintf("Driver payout stuck for %s", age.Round(time.Minute)),
			"details": fmt.Sprintf(
				"Payout %s of %.2f %s for driver %s has been %q for %s (provider ref %q). Verify the transfer with the bank/provider before retrying or compensating.",
				p.ID, p.Amount, p.Currency, p.DriverID, p.Status, age.Round(time.Minute), providerRef),
			"driver_id": p.DriverID,
			"payout_id": p.ID,
			"amount":    p.Amount,
			"currency":  p.Currency,
			"status":    string(p.Status),
		}
		if s.bus == nil {
			s.logger.Warn("stuck payout detected but no event bus wired", "payout_id", p.ID, "tenant_id", p.TenantID)
			continue
		}
		tctx := shared.ContextWithTenantID(ctx, shared.TenantID(p.TenantID))
		if err := s.bus.Publish(tctx, events.Event{Type: "AlertEvent", Payload: payload}); err != nil {
			s.logger.Warn("failed to publish stuck payout alert", "payout_id", p.ID, "error", err)
			continue
		}
		published++
		s.logger.Warn("stuck payout alert published",
			"payout_id", p.ID, "tenant_id", p.TenantID, "driver_id", p.DriverID, "age", age.Round(time.Minute))
	}
	return published, nil
}
