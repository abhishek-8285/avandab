package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/alerts/domain"
	"transport-app/internal/alerts/pipeline"
	"transport-app/internal/shared"
)

// Radar thresholds (Spec 22 §5.4): document expiry alerts at 30/7/1 days,
// e-way-bill expiry at 12/4 hours. Emitted through the EXISTING alert
// pipeline — no new alert types, dedup/cooldown handled there.
const (
	docWarnDays  = 30
	docUrgentDay = 7
	docFinalDay  = 1

	ewbWarnHours = 12
	ewbCritHours = 4

	radarSweepName = "compliance_radar_sweep"
)

// ComplianceRadarService scans document vaults and e-way bills for
// approaching expiry, surfaces a one-screen radar payload, and emits
// ranked alerts through the pipeline.
type ComplianceRadarService struct {
	db     *sql.DB
	engine *pipeline.Engine
	log    *slog.Logger
}

func NewComplianceRadarService(db *sql.DB, engine *pipeline.Engine, log *slog.Logger) *ComplianceRadarService {
	if log == nil {
		log = slog.Default()
	}
	return &ComplianceRadarService{db: db, engine: engine, log: log}
}

// SweepName is the leader-election lease name for the nightly sweep.
func SweepName() string { return radarSweepName }

// docHit is one expiring document row.
type docHit struct {
	EntityKind string // vehicle | driver
	EntityID   string
	TenantID   string
	DocType    string
	ExpiresOn  time.Time
	DaysLeft   int
}

// EwayBillHit is one expiring e-way bill.
type EwayBillHit struct {
	ID        string
	EwbNumber string
	TripID    string
	TenantID  string
	ValidUnti time.Time
	HoursLeft float64
}

// Radar is the §2.8 API payload.
type Radar struct {
	ExpiringSoon     []map[string]any `json:"expiring_soon"`
	EwaybillExpiring []map[string]any `json:"ewaybill_expiring"`
}

// Radar returns everything inside the warning windows for one tenant.
func (s *ComplianceRadarService) Radar(ctx context.Context, tenantID string) (*Radar, error) {
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	out := &Radar{ExpiringSoon: []map[string]any{}, EwaybillExpiring: []map[string]any{}}

	docs, err := s.expiringDocs(ctx, tenantID, docWarnDays)
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		out.ExpiringSoon = append(out.ExpiringSoon, map[string]any{
			"entity": d.EntityKind, "id": d.EntityID, "kind": d.DocType,
			"expires_on": d.ExpiresOn.Format("2006-01-02"), "days_left": d.DaysLeft,
		})
	}

	ewbs, err := s.expiringEwbs(ctx, tenantID, ewbWarnHours)
	if err != nil {
		return nil, err
	}
	for _, e := range ewbs {
		out.EwaybillExpiring = append(out.EwaybillExpiring, map[string]any{
			"id": e.ID, "ewb_number": e.EwbNumber,
			"expires_at": e.ValidUnti.Format(time.RFC3339), "hours_left": int(e.HoursLeft),
		})
	}
	return out, nil
}

// Sweep runs one pass over all tenants, emitting alerts per bucket. Safe
// to run repeatedly — the pipeline's dedup key includes the bucket so a
// doc re-alerts as it crosses 30d→7d→1d but not within a bucket.
func (s *ComplianceRadarService) Sweep(ctx context.Context) error {
	docs, err := s.expiringDocsAllTenants(ctx, docWarnDays)
	if err != nil {
		return fmt.Errorf("radar docs: %w", err)
	}
	for _, d := range docs {
		bucket := docBucket(d.DaysLeft)
		if bucket == "" {
			continue // outside every alert window
		}
		if err := s.emitDocAlert(ctx, d, bucket); err != nil {
			s.log.Warn("radar doc alert failed", "entity", d.EntityID, "error", err)
		}
	}

	ewbs, err := s.expiringEwbsAllTenants(ctx, ewbWarnHours)
	if err != nil {
		return fmt.Errorf("radar ewbs: %w", err)
	}
	for _, e := range ewbs {
		bucket := ewbBucket(e.HoursLeft)
		if bucket == "" {
			continue
		}
		if err := s.emitEwbAlert(ctx, e, bucket); err != nil {
			s.log.Warn("radar ewb alert failed", "ewb", e.EwbNumber, "error", err)
		}
	}
	s.log.Info("compliance radar sweep complete",
		"docs_in_window", len(docs), "ewbs_in_window", len(ewbs))
	return nil
}

// docBucket maps days-left onto the alert bucket label ("" = silent).
func docBucket(daysLeft int) string {
	switch {
	case daysLeft <= docFinalDay:
		return "1d"
	case daysLeft <= docUrgentDay:
		return "7d"
	case daysLeft <= docWarnDays:
		return "30d"
	default:
		return ""
	}
}

// ewbBucket maps hours-left onto the EWB bucket label ("" = silent).
func ewbBucket(hoursLeft float64) string {
	switch {
	case hoursLeft <= ewbCritHours:
		return "4h"
	case hoursLeft <= ewbWarnHours:
		return "12h"
	default:
		return ""
	}
}

func (s *ComplianceRadarService) emitDocAlert(ctx context.Context, d docHit, bucket string) error {
	rank := domain.RankUrgent // §5.1: doc expiry ≤1d is rank 2 urgent; buckets pre-warn same rank
	sev := domain.SeverityWarning
	if bucket == "1d" {
		rank = domain.RankCritical
		sev = domain.SeverityCritical
	}
	title := fmt.Sprintf("%s %s expires in %sd", docLabel(d.EntityKind), d.DocType, bucketTrim(bucket))
	return s.engine.Ingest(ctx, pipeline.IngestEvent{
		Source:       "compliance",
		AlertType:    "doc_expiry_" + bucket,
		Severity:     sev,
		Title:        title,
		Message:      fmt.Sprintf("%s %s expires %s (%s)", docLabel(d.EntityKind), d.DocType, d.ExpiresOn.Format("02 Jan 2006"), d.EntityID),
		EntityType:   d.EntityKind,
		EntityID:     d.EntityID,
		TenantID:     d.TenantID,
		SeverityRank: &rank,
	})
}

func (s *ComplianceRadarService) emitEwbAlert(ctx context.Context, e EwayBillHit, bucket string) error {
	rank := domain.RankCritical // §5.1: EWB <4h is rank-1 critical
	sev := domain.SeverityCritical
	if bucket == "12h" {
		rank = domain.RankUrgent
		sev = domain.SeverityWarning
	}
	return s.engine.Ingest(ctx, pipeline.IngestEvent{
		Source:       "compliance",
		AlertType:    "ewb_expiry_" + bucket,
		Severity:     sev,
		Title:        fmt.Sprintf("E-way bill expires in ~%sh (%s…)", bucketTrim(bucket), clipStr(e.EwbNumber, 8)),
		Message:      fmt.Sprintf("EWB %s validity ends %s — extend from the console or face penalty", e.EwbNumber, e.ValidUnti.Format("02 Jan 15:04")),
		EntityType:   "ewaybill",
		EntityID:     e.ID,
		TenantID:     e.TenantID,
		SeverityRank: &rank,
	})
}

// ── queries ──────────────────────────────────────────────────────────

func (s *ComplianceRadarService) expiringDocs(ctx context.Context, tenantID string, withinDays int) ([]docHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'vehicle' AS kind, vd.vehicle_id AS entity_id, v.tenant_id,
		       vd.doc_type, vd.expiry_date
		FROM vehicle_documents vd JOIN vehicles v ON v.id = vd.vehicle_id
		WHERE v.tenant_id = ? AND vd.status != 'rejected'
		  AND vd.expiry_date IS NOT NULL
		  AND date(vd.expiry_date) <= date('now', '+' || ? || ' days')
		UNION ALL
		SELECT 'driver', dd.driver_id, d.tenant_id, dd.doc_type, dd.expiry_date
		FROM driver_documents dd JOIN drivers d ON d.id = dd.driver_id
		WHERE d.tenant_id = ? AND dd.status != 'rejected'
		  AND dd.expiry_date IS NOT NULL
		  AND date(dd.expiry_date) <= date('now', '+' || ? || ' days')`,
		tenantID, withinDays, tenantID, withinDays)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []docHit
	for rows.Next() {
		var h docHit
		expTime, ok := scanExpiry(rows, &h.EntityKind, &h.EntityID, &h.TenantID, &h.DocType)
		if !ok {
			continue
		}
		h.ExpiresOn = expTime
		h.DaysLeft = int(time.Until(expTime).Hours() / 24)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *ComplianceRadarService) expiringDocsAllTenants(ctx context.Context, withinDays int) ([]docHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'vehicle', vd.vehicle_id, COALESCE(v.tenant_id, ''),
		       vd.doc_type, vd.expiry_date
		FROM vehicle_documents vd JOIN vehicles v ON v.id = vd.vehicle_id
		WHERE vd.status != 'rejected' AND vd.expiry_date IS NOT NULL
		  AND date(vd.expiry_date) <= date('now', '+' || ? || ' days')
		UNION ALL
		SELECT 'driver', dd.driver_id, COALESCE(d.tenant_id, ''), dd.doc_type, dd.expiry_date
		FROM driver_documents dd JOIN drivers d ON d.id = dd.driver_id
		WHERE dd.status != 'rejected' AND dd.expiry_date IS NOT NULL
		  AND date(dd.expiry_date) <= date('now', '+' || ? || ' days')`,
		withinDays, withinDays)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []docHit
	for rows.Next() {
		var h docHit
		expTime, ok := scanExpiry(rows, &h.EntityKind, &h.EntityID, &h.TenantID, &h.DocType)
		if !ok {
			continue
		}
		h.ExpiresOn = expTime
		h.DaysLeft = int(time.Until(expTime).Hours() / 24)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *ComplianceRadarService) expiringEwbs(ctx context.Context, tenantID string, withinHours int) ([]EwayBillHit, error) {
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	rows, qerr := s.db.QueryContext(ctx, `
		SELECT e.id, e.ewb_number, COALESCE(e.trip_id, ''), COALESCE(t.tenant_id, ''),
		       e.valid_until
		FROM eway_bills e LEFT JOIN trips t ON t.id = e.trip_id
		WHERE e.status = 'active'
		  AND COALESCE(t.tenant_id, '') = ?
		  AND julianday(e.valid_until) - julianday('now') <= ? / 24.0`,
		tenantID, withinHours)
	if qerr != nil {
		return nil, qerr
	}
	return scanEwbs(rows)
}

func (s *ComplianceRadarService) expiringEwbsAllTenants(ctx context.Context, withinHours int) ([]EwayBillHit, error) {
	rows, qerr := s.db.QueryContext(ctx, `
		SELECT e.id, e.ewb_number, COALESCE(e.trip_id, ''), COALESCE(t.tenant_id, ''),
		       e.valid_until
		FROM eway_bills e LEFT JOIN trips t ON t.id = e.trip_id
		WHERE e.status = 'active'
		  AND julianday(e.valid_until) - julianday('now') <= ? / 24.0`,
		withinHours)
	if qerr != nil {
		return nil, qerr
	}
	return scanEwbs(rows)
}

func scanEwbs(rows *sql.Rows) ([]EwayBillHit, error) {
	defer func() { _ = rows.Close() }()
	var out []EwayBillHit
	for rows.Next() {
		var h EwayBillHit
		var until time.Time
		if err := rows.Scan(&h.ID, &h.EwbNumber, &h.TripID, &h.TenantID, &until); err != nil {
			continue
		}
		h.ValidUnti = until
		h.HoursLeft = time.Until(until).Hours()
		out = append(out, h)
	}
	return out, rows.Err()
}

func docLabel(kind string) string {
	if kind == "vehicle" {
		return "Vehicle document"
	}
	return "Driver document"
}

func bucketTrim(b string) string { return b[:len(b)-1] }

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// scanExpiry scans the trailing DATE column of a doc query. Drivers
// differ on DATE representation (modernc returns time.Time, some paths
// store TEXT) so both are accepted; anything else skips the row.
func scanExpiry(rows *sql.Rows, dst ...any) (time.Time, bool) {
	var raw any
	all := make([]any, 0, len(dst)+1)
	all = append(all, dst...)
	all = append(all, &raw)
	if err := rows.Scan(all...); err != nil {
		return time.Time{}, false
	}
	switch v := raw.(type) {
	case time.Time:
		return v.Truncate(24 * time.Hour), true
	case []byte:
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(string(v))); err == nil {
			return t, true
		}
	case string:
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(v)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
