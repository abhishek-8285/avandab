package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DPDP §8(6) follow-through: incidents past the 72h detail-report deadline
// (PrivacyService.ListOverdueBreaches) must page ops, not sit in a ledger.
// SweepOverdue is the leadered-cron body — one ops alert per overdue
// incident via the EXISTING ops_alerts raise path (no new table).
//
// Dedupe key: (tenant_id, alert_type='compliance_breach',
// entity_type='breach', entity_id=breach id) restricted to
// status IN ('open','acknowledged'). No UNIQUE constraint backs it (no new
// migrations by rule) so two concurrent sweepers could double-raise; the
// leadered cron is the single-writer that makes this safe in practice.
// Resolved/dismissed alerts deliberately do NOT suppress: the incident is
// still overdue, so the next sweep re-raises.
const (
	breachWatchSweepName = "breach_overdue_watch"
	breachWatchEntity    = "breach"
)

// BreachWatchService pushes overdue DPDP breach incidents into ops_alerts.
type BreachWatchService struct {
	baseService
	db      *sql.DB
	privacy *PrivacyService
	alerts  *OpsAlertService
}

// NewBreachWatchService wires the watch over the shared raw DB. Nil deps are
// legal at construction; SweepOverdue fails fast if any is missing.
func NewBreachWatchService(bs baseService, db *sql.DB, privacy *PrivacyService, alerts *OpsAlertService) *BreachWatchService {
	return &BreachWatchService{baseService: bs, db: db, privacy: privacy, alerts: alerts}
}

// SweepName is the leader-election lease name for the cron.
func (s *BreachWatchService) SweepName() string { return breachWatchSweepName }

// SweepOverdue raises one ops alert per overdue breach in every tenant.
// Returns the count raised. Per-tenant/per-breach failures warn and continue
// (radar-sweep precedent); only a missing dependency or a failed tenant
// listing aborts the pass.
func (s *BreachWatchService) SweepOverdue(ctx context.Context) (int, error) {
	if s.db == nil || s.privacy == nil || s.alerts == nil {
		return 0, errors.New("breach watch unavailable: missing db, privacy, or alerts service")
	}
	tenants, err := s.listTenantIDs(ctx)
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, tid := range tenants {
		n, err := s.SweepTenant(ctx, tid)
		if err != nil {
			s.log.Warn("breach watch tenant sweep failed", "tenant", tid, "error", err)
			continue
		}
		raised += n
	}
	return raised, nil
}

// SweepTenant raises alerts for one tenant's overdue breaches. Returns count raised.
func (s *BreachWatchService) SweepTenant(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, errors.New("breach watch: tenant required")
	}
	overdue, err := s.privacy.ListOverdueBreaches(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, b := range overdue {
		seen, err := s.alreadyAlerted(ctx, tenantID, b.ID)
		if err != nil {
			s.log.Warn("breach watch dedupe check failed", "tenant", tenantID, "breach", b.ID, "error", err)
			continue
		}
		if seen {
			continue
		}
		due := b.DetectedAt.Add(BreachDetailWindow)
		_, err = s.alerts.CreateAlert(ctx, OpsAlert{
			TenantID:  tenantID,
			AlertType: OpsAlertComplianceBreach,
			Severity:  OpsAlertSeverityCritical,
			Title:     "Breach detail report overdue: " + b.Title,
			Description: fmt.Sprintf("DPDP §8(6): breach %q passed the 72h detail-report deadline (detected %s, due %s). File the detailed report.",
				b.Title, b.DetectedAt.Format("02 Jan 15:04"), due.Format("02 Jan 15:04")),
			EntityType: StrPtr(breachWatchEntity),
			EntityID:   StrPtr(b.ID),
		})
		if err != nil {
			s.log.Warn("breach watch raise failed", "tenant", tenantID, "breach", b.ID, "error", err)
			continue
		}
		raised++
	}
	return raised, nil
}

// alreadyAlerted reports whether an open/acknowledged compliance_breach alert
// already points at this breach incident.
func (s *BreachWatchService) alreadyAlerted(ctx context.Context, tenantID, breachID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ops_alerts
		WHERE tenant_id = $1 AND alert_type = $2
		  AND entity_type = $3 AND entity_id = $4
		  AND status IN ('open', 'acknowledged')`,
		tenantID, OpsAlertComplianceBreach, breachWatchEntity, breachID).Scan(&n)
	return n > 0, err
}

// listTenantIDs enumerates tenants as the sweep scope. breach_incidents rows
// always carry a tenants(id) row (00154 FK trigger), so the tenants table —
// not trips — is the correct scope.
func (s *BreachWatchService) listTenantIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}
