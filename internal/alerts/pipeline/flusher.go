package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/repository"
)

// Flusher flushes consolidated notifications for storm-batched alerts.
type Flusher struct {
	repo           repository.AlertRepository
	channels       map[string]channels.Provider
	logger         *slog.Logger
	clock          Clock
	stormWindowSec int
}

// NewFlusher creates a new Flusher.
func NewFlusher(repo repository.AlertRepository, channelMap map[string]channels.Provider, logger *slog.Logger) *Flusher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Flusher{
		repo:           repo,
		channels:       channelMap,
		logger:         logger,
		clock:          realClock{},
		stormWindowSec: 60,
	}
}

// SetClock allows injecting a mock clock for testing.
func (f *Flusher) SetClock(c Clock) {
	f.clock = c
}

// SetStormWindow allows overriding the storm window duration.
func (f *Flusher) SetStormWindow(seconds int) {
	if seconds > 0 {
		f.stormWindowSec = seconds
	}
}

// Run starts the background flusher loop.
func (f *Flusher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(f.stormWindowSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := f.Flush(ctx); err != nil {
				f.logger.Error("storm flusher failed", "error", err)
			}
		}
	}
}

// Flush scans and emits batch notifications for completed storm windows.
func (f *Flusher) Flush(ctx context.Context) error {
	now := f.clock.Now()
	cutoff := now.Add(-time.Duration(f.stormWindowSec) * time.Second)

	alerts, err := f.repo.ListUnflushedStormAlerts(ctx, cutoff)
	if err != nil {
		return err
	}

	for _, a := range alerts {
		var meta map[string]any
		if a.Metadata != "" {
			_ = json.Unmarshal([]byte(a.Metadata), &meta)
		}
		if meta == nil {
			meta = make(map[string]any)
		}

		if flushed, ok := meta["flushed"].(bool); ok && flushed {
			continue
		}

		stormTitle := fmt.Sprintf("⚠️ Storm: %s", a.Title)
		stormBody := fmt.Sprintf("%s occurred %d times since %s", a.Title, a.Occurrences, a.FirstSeenAt.Format(time.RFC3339))

		msg := channels.Message{
			AlertID:      a.ID,
			SeverityRank: a.SeverityRank,
			Title:        stormTitle,
			Body:         stormBody,
			Severity:     a.Severity,
			Meta:         meta,
		}

		// Send to channels
		for _, provider := range f.channels {
			if provider != nil {
				_ = provider.Send(ctx, msg)
			}
		}

		meta["flushed"] = true
		meta["flushed_occurrences"] = a.Occurrences
		metaBytes, _ := json.Marshal(meta)
		_ = f.repo.UpdateMetadata(ctx, a.ID, string(metaBytes))

		f.logger.Info("flushed storm alert batch", "alert_id", a.ID, "occurrences", a.Occurrences)
	}

	return nil
}
