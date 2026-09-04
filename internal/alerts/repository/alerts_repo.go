package repository

import (
	"context"
	"time"

	"transport-app/internal/alerts/domain"
)

// AlertRepository defines storage operations for rules, overrides, alerts, and preferences.
type AlertRepository interface {
	FindOpenByDedupKey(ctx context.Context, dedupKey string) (*domain.Alert, error)
	CreateAlert(ctx context.Context, a *domain.Alert) error
	IncrementOccurrences(ctx context.Context, alertID string, lastSeen time.Time) error
	ListRulesBySource(ctx context.Context, source string) ([]domain.Rule, error)
	GetRule(ctx context.Context, ruleID string) (*domain.Rule, error)
	GetActiveRuleForType(ctx context.Context, source, alertType string, entityID string) (*domain.Rule, *domain.RuleOverride, error)
	GetOverrides(ctx context.Context, ruleID string, entityID string) (*domain.RuleOverride, error)
	ListAlerts(ctx context.Context, status string, limit, offset int) ([]domain.Alert, error)
	ListPendingEscalations(ctx context.Context, now time.Time) ([]domain.Alert, error)
	UpdateEscalation(ctx context.Context, alertID string, nextStep int, nextAt *time.Time, status string) error
	ListUnflushedStormAlerts(ctx context.Context, windowCutoff time.Time) ([]domain.Alert, error)
	UpdateMetadata(ctx context.Context, alertID string, metadata string) error
	UnreadCount(ctx context.Context, userID string) (int, error)
	Recent(ctx context.Context, userID string, limit int) ([]domain.Alert, error)
	Ack(ctx context.Context, alertID string, userID string) error
	Resolve(ctx context.Context, alertID string, userID string) error
	MarkAllRead(ctx context.Context, userID string) error

	// Inbox operations (Spec 22 S1). status is one of open|snoozed|acked|
	// resolved|all; snoozed rows count as visible-open once snoozed_until
	// has passed. Ack/Snooze return false when the guard matched no row
	// (already handled — Spec 22 edge case 10).
	ListInbox(ctx context.Context, tenantID, status string, limit int) ([]domain.Alert, error)
	// InboxCounts returns (visible-open, rank-1-critical) counts using the
	// same visibility rule as ListInbox status=open (expired snoozes count).
	InboxCounts(ctx context.Context, tenantID string) (open int, critical int, err error)
	InboxAck(ctx context.Context, alertID, userID string) (bool, error)
	InboxSnooze(ctx context.Context, alertID, userID string, until time.Time) (bool, error)
	InboxSnoozeAll(ctx context.Context, ids []string, userID string, until time.Time) (int64, error)
	ReopenExpiredSnoozes(ctx context.Context, now time.Time) (int64, error)
	ReopenExpiredSnoozesForTenant(ctx context.Context, tenantID string, now time.Time) (int64, error)
}
