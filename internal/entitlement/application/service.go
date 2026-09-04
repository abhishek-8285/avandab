package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/entitlement/domain"
	"transport-app/internal/shared"
)

// Service defines the interface for commercial entitlements and subscriptions.
type Service interface {
	GetSubscription(ctx context.Context, tenantID shared.TenantID) (*domain.TenantSubscription, error)
	HasFeature(ctx context.Context, tenantID shared.TenantID, featureKey domain.FeatureKey) (bool, error)
	CheckQuota(ctx context.Context, tenantID shared.TenantID, quotaKey domain.QuotaKey) (*domain.QuotaStatus, error)
	ReserveQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int, idempotencyKey, entityType, entityID string) error
	CommitQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int, idempotencyKey, entityType, entityID string) error
	ReleaseQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int) error
	CanExecuteOperation(ctx context.Context, tenantID shared.TenantID, opCode domain.OpCode, resourceID string) (bool, error)
	CanCreateBooking(ctx context.Context, tenantID shared.TenantID) (bool, error)
	ReserveBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
	CommitBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
	ReleaseBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
	CreateSubscription(ctx context.Context, tenantID shared.TenantID, planID domain.PlanID, status domain.SubscriptionStatus, periodStart, periodEnd time.Time) (*domain.TenantSubscription, error)
	HandleSubscriptionWebhook(ctx context.Context, providerSubID string, status domain.SubscriptionStatus, periodStart, periodEnd time.Time) error
	ProcessSubscriptionWebhook(ctx context.Context, p WebhookEventPayload) error
	SetEntitlementOverride(ctx context.Context, tenantID shared.TenantID, entType, keyName, value, reason string, expiresAt *time.Time) error
}

type service struct {
	db *sql.DB
}

// NewService constructs a new Entitlement application service.
func NewService(db *sql.DB) Service {
	return &service{db: db}
}

func (s *service) GetSubscription(ctx context.Context, tenantID shared.TenantID) (*domain.TenantSubscription, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, plan_id, status, current_period_start, current_period_end, trial_end, COALESCE(provider_subscription_id, ''), created_at, updated_at
		FROM tenant_subscriptions
		WHERE tenant_id = ?
	`, string(tenantID))

	var sub domain.TenantSubscription
	var tid, startStr, endStr, crStr, upStr string
	var trialStr sql.NullString
	if err := row.Scan(&sub.ID, &tid, &sub.PlanID, &sub.Status, &startStr, &endStr, &trialStr, &sub.ProviderSubscriptionID, &crStr, &upStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrSubscriptionNotFound
		}
		return nil, err
	}
	sub.TenantID = shared.TenantID(tid)
	sub.CurrentPeriodStart, _ = parseTime(startStr)
	sub.CurrentPeriodEnd, _ = parseTime(endStr)
	sub.CreatedAt, _ = parseTime(crStr)
	sub.UpdatedAt, _ = parseTime(upStr)
	if trialStr.Valid && trialStr.String != "" {
		t, _ := parseTime(trialStr.String)
		sub.TrialEnd = &t
	}
	return &sub, nil
}

func (s *service) HasFeature(ctx context.Context, tenantID shared.TenantID, featureKey domain.FeatureKey) (bool, error) {
	// 1. Check explicit overrides first
	var overrideVal string
	var expiresStr sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT override_value, expires_at
		FROM tenant_entitlement_overrides
		WHERE tenant_id = ? AND entitlement_type = 'FEATURE' AND key_name = ?
	`, string(tenantID), string(featureKey)).Scan(&overrideVal, &expiresStr)
	if err == nil {
		if expiresStr.Valid && expiresStr.String != "" {
			if exp, err := parseTime(expiresStr.String); err == nil && time.Now().UTC().After(exp) {
				// Expired override, fallback to plan
			} else {
				return overrideVal == "true" || overrideVal == "enabled" || overrideVal == "1", nil
			}
		} else {
			return overrideVal == "true" || overrideVal == "enabled" || overrideVal == "1", nil
		}
	}

	// 2. Lookup subscription
	sub, err := s.GetSubscription(ctx, tenantID)
	if err != nil {
		if err == domain.ErrSubscriptionNotFound {
			return false, nil
		}
		return false, err
	}

	// Inactive subscriptions cannot use paid features
	if sub.Status == domain.SubReadOnly || sub.Status == domain.SubAccountClosed || sub.Status == domain.SubOperationallyTerminated {
		return false, nil
	}

	// 3. Lookup plan features
	var featuresJSON string
	err = s.db.QueryRowContext(ctx, `SELECT features_json FROM subscription_plans WHERE id = ?`, string(sub.PlanID)).Scan(&featuresJSON)
	if err != nil {
		return false, domain.ErrPlanNotFound
	}

	var features []string
	if err := json.Unmarshal([]byte(featuresJSON), &features); err != nil {
		return false, err
	}

	for _, f := range features {
		if f == string(featureKey) {
			return true, nil
		}
	}
	return false, nil
}

func (s *service) CheckQuota(ctx context.Context, tenantID shared.TenantID, quotaKey domain.QuotaKey) (*domain.QuotaStatus, error) {
	return s.checkQuotaTx(ctx, nil, tenantID, quotaKey)
}

func (s *service) checkQuotaTx(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey) (*domain.QuotaStatus, error) {
	sub, err := s.GetSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var meter domain.TenantUsageMeter
	var stStr, endStr, upStr string
	var queryRow func(ctx context.Context, query string, args ...any) *sql.Row
	var execCtx func(ctx context.Context, query string, args ...any) (sql.Result, error)
	if tx != nil {
		queryRow = tx.QueryRowContext
		execCtx = tx.ExecContext
	} else {
		queryRow = s.db.QueryRowContext
		execCtx = s.db.ExecContext
	}
	err = queryRow(ctx, `
		SELECT id, tenant_id, quota_key, period_start, period_end, used_quantity, reserved_quantity, max_quantity, updated_at
		FROM tenant_usage_meters
		WHERE tenant_id = ? AND quota_key = ? AND period_start <= ? AND period_end >= ?
	`, string(tenantID), string(quotaKey), now.Format(time.RFC3339), now.Format(time.RFC3339)).Scan(
		&meter.ID, &meter.TenantID, &meter.QuotaKey, &stStr, &endStr, &meter.UsedQuantity, &meter.ReservedQuantity, &meter.MaxQuantity, &upStr,
	)

	if err == sql.ErrNoRows {
		// Initialize meter from plan & overrides
		maxQty, err := s.resolveMaxQuota(ctx, tenantID, sub.PlanID, quotaKey)
		if err != nil {
			return nil, err
		}
		meter = domain.TenantUsageMeter{
			ID:               uuid.NewString(),
			TenantID:         tenantID,
			QuotaKey:         quotaKey,
			PeriodStart:      sub.CurrentPeriodStart,
			PeriodEnd:        sub.CurrentPeriodEnd,
			UsedQuantity:     0,
			ReservedQuantity: 0,
			MaxQuantity:      maxQty,
			UpdatedAt:        now,
		}
		_, err = execCtx(ctx, `
			INSERT INTO tenant_usage_meters (id, tenant_id, quota_key, period_start, period_end, used_quantity, reserved_quantity, max_quantity, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, meter.ID, string(tenantID), string(quotaKey), meter.PeriodStart.Format(time.RFC3339), meter.PeriodEnd.Format(time.RFC3339), 0, 0, maxQty, now.Format(time.RFC3339))
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		meter.PeriodStart, _ = parseTime(stStr)
		meter.PeriodEnd, _ = parseTime(endStr)
		meter.UpdatedAt, _ = parseTime(upStr)
	}

	return &domain.QuotaStatus{
		QuotaKey:    quotaKey,
		Used:        meter.UsedQuantity,
		Reserved:    meter.ReservedQuantity,
		Max:         meter.MaxQuantity,
		Remaining:   meter.RemainingCapacity(),
		PeriodStart: meter.PeriodStart,
		PeriodEnd:   meter.PeriodEnd,
	}, nil
}

func (s *service) resolveMaxQuota(ctx context.Context, tenantID shared.TenantID, planID domain.PlanID, quotaKey domain.QuotaKey) (int, error) {
	// 1. Check override
	var overrideVal string
	err := s.db.QueryRowContext(ctx, `
		SELECT override_value FROM tenant_entitlement_overrides
		WHERE tenant_id = ? AND entitlement_type = 'QUOTA' AND key_name = ?
	`, string(tenantID), string(quotaKey)).Scan(&overrideVal)
	if err == nil {
		var val int
		if _, err := fmt.Sscanf(overrideVal, "%d", &val); err == nil {
			return val, nil
		}
	}

	// 2. Check plan
	var quotasJSON string
	err = s.db.QueryRowContext(ctx, `SELECT quotas_json FROM subscription_plans WHERE id = ?`, string(planID)).Scan(&quotasJSON)
	if err != nil {
		return 0, domain.ErrPlanNotFound
	}
	var quotas map[string]int
	if err := json.Unmarshal([]byte(quotasJSON), &quotas); err != nil {
		return 0, err
	}
	if v, ok := quotas[string(quotaKey)]; ok {
		return v, nil
	}
	return 0, nil
}

func (s *service) ReserveQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int, idempotencyKey, entityType, entityID string) error {
	// Check if already executed idempotently
	var existingID string
	var queryRunner interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	} = s.db
	if tx != nil {
		queryRunner = tx
	}

	err := queryRunner.QueryRowContext(ctx, `
		SELECT id FROM tenant_usage_events WHERE tenant_id = ? AND idempotency_key = ?
	`, string(tenantID), idempotencyKey).Scan(&existingID)
	if err == nil {
		return nil // Already consumed
	}

	// Ensure meter exists
	_, err = s.checkQuotaTx(ctx, tx, tenantID, quotaKey)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	res, err := queryRunner.ExecContext(ctx, `
		UPDATE tenant_usage_meters
		SET reserved_quantity = reserved_quantity + ?, updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND quota_key = ? AND period_start <= ? AND period_end >= ?
		  AND (used_quantity + reserved_quantity + ?) <= max_quantity
	`, quantity, string(tenantID), string(quotaKey), now.Format(time.RFC3339), now.Format(time.RFC3339), quantity)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrQuotaExceeded
	}
	return nil
}

func (s *service) CommitQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int, idempotencyKey, entityType, entityID string) error {
	var queryRunner interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	} = s.db
	if tx != nil {
		queryRunner = tx
	}

	now := time.Now().UTC()
	// Decrement reserved, increment used
	_, err := queryRunner.ExecContext(ctx, `
		UPDATE tenant_usage_meters
		SET reserved_quantity = MAX(0, reserved_quantity - ?),
		    used_quantity = used_quantity + ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND quota_key = ? AND period_start <= ? AND period_end >= ?
	`, quantity, quantity, string(tenantID), string(quotaKey), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return err
	}

	// Record immutable audit usage event
	eventID := uuid.NewString()
	_, err = queryRunner.ExecContext(ctx, `
		INSERT OR IGNORE INTO tenant_usage_events (id, tenant_id, quota_key, quantity, idempotency_key, source_entity_type, source_entity_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, eventID, string(tenantID), string(quotaKey), quantity, idempotencyKey, entityType, entityID, now.Format(time.RFC3339))
	return err
}

func (s *service) ReleaseQuota(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, quotaKey domain.QuotaKey, quantity int) error {
	var queryRunner interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	} = s.db
	if tx != nil {
		queryRunner = tx
	}

	now := time.Now().UTC()
	_, err := queryRunner.ExecContext(ctx, `
		UPDATE tenant_usage_meters
		SET reserved_quantity = MAX(0, reserved_quantity - ?),
		    updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = ? AND quota_key = ? AND period_start <= ? AND period_end >= ?
	`, quantity, string(tenantID), string(quotaKey), now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

func (s *service) CanExecuteOperation(ctx context.Context, tenantID shared.TenantID, opCode domain.OpCode, resourceID string) (bool, error) {
	return s.canExecuteOperation(ctx, tenantID, opCode, resourceID)
}

// CanCreateBooking is the booking module's narrow gate: READ_ONLY/CLOSED
// orgs are blocked, everyone else (including subscription-less legacy
// tenants) may create.
func (s *service) CanCreateBooking(ctx context.Context, tenantID shared.TenantID) (bool, error) {
	ok, err := s.canExecuteOperation(ctx, tenantID, domain.OpCreateBooking, "")
	if err != nil {
		if errors.Is(err, domain.ErrSubscriptionNotFound) {
			return true, nil
		}
		return false, err
	}
	return ok, nil
}

// reserveKey namespaces booking metering events so retries dedupe.
func reserveKey(bookingID, phase string) string {
	return "booking:" + bookingID + ":" + phase
}

// ReserveBooking holds one monthly-trip unit for a new booking. Missing
// subscriptions are a no-op (legacy tenants meter nothing).
func (s *service) ReserveBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error {
	if _, err := s.GetSubscription(ctx, tenantID); err != nil {
		if errors.Is(err, domain.ErrSubscriptionNotFound) {
			return nil
		}
		return err
	}
	return s.ReserveQuota(ctx, tx, tenantID, domain.QuotaMaxTripsPerMonth, 1, reserveKey(bookingID, "reserve"), "booking", bookingID)
}

// CommitBooking converts the hold into usage at trip completion. Idempotent:
// a retried completion must not double-count.
func (s *service) CommitBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error {
	if _, err := s.GetSubscription(ctx, tenantID); err != nil {
		if errors.Is(err, domain.ErrSubscriptionNotFound) {
			return nil
		}
		return err
	}
	var done string
	qr := s.db
	if tx != nil {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tenant_usage_events WHERE tenant_id = ? AND idempotency_key = ?`,
			string(tenantID), reserveKey(bookingID, "commit")).Scan(&done); err == nil {
			return nil
		}
	} else {
		if err := qr.QueryRowContext(ctx, `SELECT id FROM tenant_usage_events WHERE tenant_id = ? AND idempotency_key = ?`,
			string(tenantID), reserveKey(bookingID, "commit")).Scan(&done); err == nil {
			return nil
		}
	}
	return s.CommitQuota(ctx, tx, tenantID, domain.QuotaMaxTripsPerMonth, 1, reserveKey(bookingID, "commit"), "booking", bookingID)
}

// ReleaseBooking returns the hold when a booking is cancelled before use.
func (s *service) ReleaseBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error {
	if _, err := s.GetSubscription(ctx, tenantID); err != nil {
		if errors.Is(err, domain.ErrSubscriptionNotFound) {
			return nil
		}
		return err
	}
	return s.ReleaseQuota(ctx, tx, tenantID, domain.QuotaMaxTripsPerMonth, 1)
}

func (s *service) canExecuteOperation(ctx context.Context, tenantID shared.TenantID, opCode domain.OpCode, resourceID string) (bool, error) {
	sub, err := s.GetSubscription(ctx, tenantID)
	if err != nil {
		if err == domain.ErrSubscriptionNotFound {
			return false, domain.ErrSubscriptionNotFound
		}
		return false, err
	}

	switch sub.Status {
	case domain.SubTrial, domain.SubActive, domain.SubPastDue, domain.SubGrace:
		return true, nil

	case domain.SubReadOnly, domain.SubAccountClosed:
		// Ingress operations blocked
		switch opCode {
		case domain.OpCreateBooking, domain.OpCreateDispatch, domain.OpCreateVehicle, domain.OpCreateDriver:
			return false, domain.ErrOperationBlocked
		default:
			// In-flight trip operations, settlements, invoicing, and reading permitted
			return true, nil
		}

	case domain.SubOperationallyTerminated:
		// Complete security freeze except read
		if opCode == domain.OpReadResource {
			return true, nil
		}
		return false, domain.ErrOperationBlocked

	default:
		return false, domain.ErrOperationBlocked
	}
}

func (s *service) CreateSubscription(ctx context.Context, tenantID shared.TenantID, planID domain.PlanID, status domain.SubscriptionStatus, periodStart, periodEnd time.Time) (*domain.TenantSubscription, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	trialEnd := now.Add(14 * 24 * time.Hour)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, trial_end, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id) DO UPDATE SET
			plan_id = excluded.plan_id,
			status = excluded.status,
			current_period_start = excluded.current_period_start,
			current_period_end = excluded.current_period_end,
			updated_at = CURRENT_TIMESTAMP
	`, id, string(tenantID), string(planID), string(status), periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339), trialEnd.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return s.GetSubscription(ctx, tenantID)
}

// WebhookEventPayload models incoming Razorpay subscription webhooks.
type WebhookEventPayload struct {
	EventID                string    `json:"event_id"`
	EventType              string    `json:"event_type"`
	Provider               string    `json:"provider"`
	ProviderSubscriptionID string    `json:"provider_subscription_id"`
	PlanID                 string    `json:"plan_id"`
	PayloadJSON            string    `json:"payload_json"`
	EventTimestamp         time.Time `json:"event_timestamp"`
	PeriodStart            time.Time `json:"period_start"`
	PeriodEnd              time.Time `json:"period_end"`
}

func (s *service) ProcessSubscriptionWebhook(ctx context.Context, p WebhookEventPayload) error {
	if p.Provider == "" {
		p.Provider = "RAZORPAY"
	}
	if p.EventTimestamp.IsZero() {
		p.EventTimestamp = time.Now().UTC()
	}

	// 1. Replay Idempotency Check
	var existingID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM subscription_webhook_events WHERE provider = ? AND event_id = ?
	`, p.Provider, p.EventID).Scan(&existingID)
	if err == nil {
		return nil // Already processed idempotently
	}

	// 2. Lookup Tenant Subscription
	var tenantIDStr, currentStatus string
	err = s.db.QueryRowContext(ctx, `
		SELECT tenant_id, status FROM tenant_subscriptions WHERE provider_subscription_id = ?
	`, p.ProviderSubscriptionID).Scan(&tenantIDStr, &currentStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.ErrSubscriptionNotFound
		}
		return err
	}

	// 3. Out-of-Order Protection: Prevent older events from downgrading newer active state
	var latestTsStr sql.NullString
	_ = s.db.QueryRowContext(ctx, `
		SELECT MAX(event_timestamp) FROM subscription_webhook_events WHERE provider_subscription_id = ?
	`, p.ProviderSubscriptionID).Scan(&latestTsStr)
	if latestTsStr.Valid && latestTsStr.String != "" {
		if latestTS, err := parseTime(latestTsStr.String); err == nil {
			if p.EventTimestamp.Before(latestTS) {
				// Record as IGNORED_OUT_OF_ORDER
				recID := uuid.NewString()
				_, _ = s.db.ExecContext(ctx, `
					INSERT INTO subscription_webhook_events (id, tenant_id, provider, event_id, event_type, payload_json, provider_subscription_id, event_timestamp, processed_at, status)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'IGNORED_OUT_OF_ORDER')
				`, recID, tenantIDStr, p.Provider, p.EventID, p.EventType, p.PayloadJSON, p.ProviderSubscriptionID, p.EventTimestamp.Format(time.RFC3339))
				return nil
			}
		}
	}

	// 4. Determine Target Status from Event Type
	var newStatus domain.SubscriptionStatus
	switch p.EventType {
	case "subscription.authenticated", "subscription.activated", "subscription.resumed":
		newStatus = domain.SubActive

	case "payment.captured", "subscription.charged":
		newStatus = domain.SubActive

	case "payment.failed":
		newStatus = domain.SubPastDue

	case "subscription.paused":
		newStatus = domain.SubReadOnly

	case "subscription.cancelled", "subscription.expired", "subscription.completed":
		newStatus = domain.SubReadOnly

	default:
		newStatus = domain.SubscriptionStatus(currentStatus)
	}

	// 5. Update Tenant Subscription (status + period, and plan when the
	// payload carries a plan reference matching an active local plan —
	// unknown references are ignored, never written).
	now := time.Now().UTC()
	planID := ""
	if p.PlanID != "" {
		var exists string
		if err := s.db.QueryRowContext(ctx, `SELECT id FROM subscription_plans WHERE id = ? AND is_active = 1`, p.PlanID).Scan(&exists); err == nil {
			planID = exists
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if !p.PeriodStart.IsZero() && !p.PeriodEnd.IsZero() {
		if planID != "" {
			_, err = tx.ExecContext(ctx, `
				UPDATE tenant_subscriptions
				SET status = ?, plan_id = ?, current_period_start = ?, current_period_end = ?, updated_at = ?
				WHERE provider_subscription_id = ?
			`, string(newStatus), planID, p.PeriodStart.Format(time.RFC3339), p.PeriodEnd.Format(time.RFC3339), now.Format(time.RFC3339), p.ProviderSubscriptionID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE tenant_subscriptions
				SET status = ?, current_period_start = ?, current_period_end = ?, updated_at = ?
				WHERE provider_subscription_id = ?
			`, string(newStatus), p.PeriodStart.Format(time.RFC3339), p.PeriodEnd.Format(time.RFC3339), now.Format(time.RFC3339), p.ProviderSubscriptionID)
		}
	} else if planID != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE tenant_subscriptions
			SET status = ?, plan_id = ?, updated_at = ?
			WHERE provider_subscription_id = ?
		`, string(newStatus), planID, now.Format(time.RFC3339), p.ProviderSubscriptionID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE tenant_subscriptions
			SET status = ?, updated_at = ?
			WHERE provider_subscription_id = ?
		`, string(newStatus), now.Format(time.RFC3339), p.ProviderSubscriptionID)
	}
	if err != nil {
		return err
	}

	// 6. Record Processed Webhook Event
	recID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscription_webhook_events (id, tenant_id, provider, event_id, event_type, payload_json, provider_subscription_id, event_timestamp, processed_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'PROCESSED')
	`, recID, tenantIDStr, p.Provider, p.EventID, p.EventType, p.PayloadJSON, p.ProviderSubscriptionID, p.EventTimestamp.Format(time.RFC3339))
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *service) HandleSubscriptionWebhook(ctx context.Context, providerSubID string, status domain.SubscriptionStatus, periodStart, periodEnd time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE tenant_subscriptions
		SET status = ?, current_period_start = ?, current_period_end = ?, updated_at = ?
		WHERE provider_subscription_id = ?
	`, string(status), periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339), now.Format(time.RFC3339), providerSubID)
	return err
}

func (s *service) SetEntitlementOverride(ctx context.Context, tenantID shared.TenantID, entType, keyName, value, reason string, expiresAt *time.Time) error {
	id := uuid.NewString()
	var expStr sql.NullString
	if expiresAt != nil {
		expStr = sql.NullString{String: expiresAt.Format(time.RFC3339), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_entitlement_overrides (id, tenant_id, entitlement_type, key_name, override_value, reason, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, entitlement_type, key_name) DO UPDATE SET
			override_value = excluded.override_value,
			reason = excluded.reason,
			expires_at = excluded.expires_at
	`, id, string(tenantID), entType, keyName, value, reason, expStr)
	return err
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}
