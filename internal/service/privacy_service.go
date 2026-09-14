package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/repository"
)

// DPDP breach-notice ledger (Digital Personal Data Protection Act, 2023
// §8(6) + DPDP Rules): intimate the Board and each affected principal without
// delay; file the detailed report within 72 hours of awareness (detected_at).
// The 72h due date derives from detected_at per query — no stored due column
// to drift. Reads are tenant-scoped fail-closed: a foreign id reads as
// not-found, never as another org's incident.
const (
	BreachStatusOpen     = "open"
	BreachStatusNotified = "notified"
	BreachStatusDetailed = "detailed"
	BreachStatusClosed   = "closed"

	// BreachDetailWindow is the DPDP Rules filing deadline after awareness.
	BreachDetailWindow = 72 * time.Hour
)

// BreachIncident is one row of breach_incidents (00154).
type BreachIncident struct {
	ID                   string
	TenantID             string
	Title                string
	Description          string
	Nature               string
	Extent               string
	AffectedCount        int64
	Status               string
	DetectedAt           time.Time
	BoardNotifiedAt      sql.NullTime
	PrincipalsNotifiedAt sql.NullTime
	DetailReport         string
	DetailedAt           sql.NullTime
}

// PrivacyService administers the breach-notice ledger. Auto-detection hooks
// (intrusion signals feeding ReportBreach) are an explicit non-goal of this
// slice — reporting starts with a human filing the incident.
type PrivacyService struct {
	baseService
}

func scanBreach(row *sql.Row, rows *sql.Rows, b *BreachIncident) error {
	if rows != nil {
		return rows.Scan(&b.ID, &b.TenantID, &b.Title, &b.Description, &b.Nature,
			&b.Extent, &b.AffectedCount, &b.Status, &b.DetectedAt,
			&b.BoardNotifiedAt, &b.PrincipalsNotifiedAt, &b.DetailReport, &b.DetailedAt)
	}
	return row.Scan(&b.ID, &b.TenantID, &b.Title, &b.Description, &b.Nature,
		&b.Extent, &b.AffectedCount, &b.Status, &b.DetectedAt,
		&b.BoardNotifiedAt, &b.PrincipalsNotifiedAt, &b.DetailReport, &b.DetailedAt)
}

const breachColumns = `id, tenant_id, title, description, nature, extent,
	affected_count, status, detected_at, board_notified_at,
	principals_notified_at, detail_report, detailed_at`

// ReportBreach files a new incident in status open, detected now.
func (s *PrivacyService) ReportBreach(ctx context.Context, tenantID, title, description, nature, extent string, affected int64) (string, error) {
	db := s.consentDB()
	if db == nil {
		return "", errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	if title == "" {
		return "", errors.New("breach title is required")
	}
	id := "brc_" + generateID()
	_, err := repository.ExecTx(ctx, db, `
		INSERT INTO breach_incidents (id, tenant_id, title, description, nature, extent, affected_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, tenantID, title, description, nature, extent, affected)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetBreach reads one incident inside the caller's tenant (fail-closed).
func (s *PrivacyService) GetBreach(ctx context.Context, tenantID, id string) (*BreachIncident, error) {
	db := s.consentDB()
	if db == nil {
		return nil, errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	var b BreachIncident
	err := scanBreach(repository.QueryRowTx(ctx, db,
		`SELECT `+breachColumns+` FROM breach_incidents WHERE tenant_id = $1 AND id = $2`,
		tenantID, id), nil, &b)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	return &b, err
}

// MarkNotified stamps whichever notifications happened; status advances to
// notified once both the Board and the principals are stamped.
func (s *PrivacyService) MarkNotified(ctx context.Context, tenantID, id string, board, principals bool) error {
	db := s.consentDB()
	if db == nil {
		return errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	if !board && !principals {
		return errors.New("nothing to mark: set board and/or principals")
	}
	set := "updated_at = CURRENT_TIMESTAMP"
	if board {
		set += ", board_notified_at = CURRENT_TIMESTAMP"
	}
	if principals {
		set += ", principals_notified_at = CURRENT_TIMESTAMP"
	}
	res, err := repository.ExecTx(ctx, db,
		`UPDATE breach_incidents SET `+set+` WHERE tenant_id = $1 AND id = $2 AND status <> 'closed'`,
		tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	_, err = repository.ExecTx(ctx, db, `
		UPDATE breach_incidents SET status = 'notified', updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2 AND status = 'open'
		  AND board_notified_at IS NOT NULL AND principals_notified_at IS NOT NULL`,
		tenantID, id)
	return err
}

// FileDetail attaches the detailed report (due ≤72h after detected_at) and
// advances status to detailed.
func (s *PrivacyService) FileDetail(ctx context.Context, tenantID, id, report string) error {
	db := s.consentDB()
	if db == nil {
		return errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	if report == "" {
		return errors.New("detail report is required")
	}
	res, err := repository.ExecTx(ctx, db, `
		UPDATE breach_incidents
		SET detail_report = $3, detailed_at = CURRENT_TIMESTAMP,
		    status = 'detailed', updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2 AND status <> 'closed'`,
		tenantID, id, report)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CloseBreach closes an incident. Reopening is out of scope: file a new
// incident referencing the old one instead, so the ledger stays append-only.
func (s *PrivacyService) CloseBreach(ctx context.Context, tenantID, id string) error {
	db := s.consentDB()
	if db == nil {
		return errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	res, err := repository.ExecTx(ctx, db, `
		UPDATE breach_incidents SET status = 'closed', updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2 AND status <> 'closed'`,
		tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListBreaches returns the tenant's incidents, newest first; status "" = all.
func (s *PrivacyService) ListBreaches(ctx context.Context, tenantID, status string) ([]BreachIncident, error) {
	db := s.consentDB()
	if db == nil {
		return nil, errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	q := `SELECT ` + breachColumns + ` FROM breach_incidents WHERE tenant_id = $1`
	args := []any{tenantID}
	if status != "" {
		q += ` AND status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY detected_at DESC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BreachIncident
	for rows.Next() {
		var b BreachIncident
		if err := scanBreach(nil, rows, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListOverdueBreaches returns open/notified incidents past the 72h filing
// deadline — the DPDP escalation watchlist. Cutoff computed in Go and passed
// as a param (same cross-engine pattern as the ETA window queries).
func (s *PrivacyService) ListOverdueBreaches(ctx context.Context, tenantID string) ([]BreachIncident, error) {
	db := s.consentDB()
	if db == nil {
		return nil, errors.New("breach reporting unavailable: storage does not support raw DB access")
	}
	cutoff := time.Now().UTC().Add(-BreachDetailWindow).Format("2006-01-02 15:04:05")
	rows, err := db.QueryContext(ctx, `
		SELECT `+breachColumns+` FROM breach_incidents
		WHERE tenant_id = $1 AND status IN ('open', 'notified') AND detected_at <= $2
		ORDER BY detected_at ASC`, tenantID, cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BreachIncident
	for rows.Next() {
		var b BreachIncident
		if err := scanBreach(nil, rows, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
