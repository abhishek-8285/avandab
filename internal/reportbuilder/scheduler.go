package reportbuilder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"transport-app/internal/shared"
)

// Scheduler scans and triggers due report delivery schedules across tenants.
type Scheduler struct {
	svc    *Service
	logger *slog.Logger
}

// NewScheduler creates a background report delivery scheduler.
func NewScheduler(svc *Service, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		svc:    svc,
		logger: logger,
	}
}

// RunOnce evaluates all due and enabled schedules once.
// It executes each schedule with its tenant context, marks status honestly,
// and returns the number of processed schedules and any accumulated errors.
func (s *Scheduler) RunOnce(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	due, err := s.svc.repo.ListDueSchedules(ctx, now, 100)
	if err != nil {
		return 0, fmt.Errorf("failed to list due schedules: %w", err)
	}

	var errs []error
	processed := 0
	for _, sched := range due {
		tenantCtx := shared.ContextWithTenantID(ctx, shared.TenantID(sched.TenantID))
		_, trigErr := s.svc.TriggerSchedule(tenantCtx, sched.TenantID, sched.ID)
		processed++
		if trigErr != nil {
			s.logger.Error("schedule execution failed",
				"schedule_id", sched.ID,
				"tenant_id", sched.TenantID,
				"error", trigErr,
			)
			errs = append(errs, trigErr)
		}
	}

	if len(errs) > 0 {
		return processed, errors.Join(errs...)
	}
	return processed, nil
}

// Run executes the scheduler ticker loop until context is canceled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				s.logger.Error("scheduled report run error", "error", err)
			}
		}
	}
}
