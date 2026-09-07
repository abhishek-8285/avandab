package errors

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	appdb "transport-app/internal/database"

	_ "modernc.org/sqlite"
)

type ErrorFilter struct {
	TenantID    string
	Severity    string
	Fingerprint string
	From        time.Time
	To          time.Time
	Limit       int
	Offset      int
}

type IncidentFilter struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

type Store interface {
	UpsertError(ctx context.Context, r ErrorReport, fingerprint string) (ErrorReport, error)
	HasOpenIncident(ctx context.Context, fingerprint, tenantID string) (bool, error)
	CreateIncident(ctx context.Context, inc Incident) error
	GetIncident(ctx context.Context, id string) (Incident, error)
	ResolveIncident(ctx context.Context, id, tenantID, status, assignedTo, rootCause string) error
	GetError(ctx context.Context, fingerprint, tenantID string) (ErrorReport, error)
	ListErrors(ctx context.Context, f ErrorFilter) ([]ErrorReport, error)
	CountErrors(ctx context.Context, f ErrorFilter) (int, error)
	ListIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error)
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

func FirstLine(msg string) string {
	line := strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0]
	if len(line) > 512 {
		line = line[:512]
	}
	return line
}

func Fingerprint(method, url, message, tenantID string) string {
	h := sha1.Sum([]byte(method + "\x00" + url + "\x00" + FirstLine(message) + "\x00" + tenantID))
	return hex.EncodeToString(h[:])
}

const upsertErrorSQL = `
INSERT INTO error_reports (
    id, fingerprint, tenant_id, user_id, url, method, status_code,
    severity, message, stack_trace, environment, app_version,
    request_id, metadata,
    occurrences, first_seen, last_seen, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, $15, $16, $17)
ON CONFLICT (fingerprint, tenant_id) DO UPDATE SET
    occurrences = error_reports.occurrences + 1,
    last_seen = excluded.last_seen,
    status_code = excluded.status_code,
    stack_trace = CASE WHEN excluded.stack_trace != '' THEN excluded.stack_trace ELSE error_reports.stack_trace END,
    user_id = CASE WHEN excluded.user_id != '' THEN excluded.user_id ELSE error_reports.user_id END,
    request_id = CASE WHEN excluded.request_id != '' THEN excluded.request_id ELSE error_reports.request_id END,
    metadata = CASE WHEN excluded.metadata != '' THEN excluded.metadata ELSE error_reports.metadata END`

func (s *SQLiteStore) UpsertError(ctx context.Context, r ErrorReport, fingerprint string) (ErrorReport, error) {
	firstSeen := r.Timestamp.UTC().Format(time.RFC3339)
	metadata := ""
	if len(r.Metadata) > 0 {
		if b, err := json.Marshal(r.Metadata); err == nil {
			metadata = string(b)
		}
	}
	_, err := s.db.ExecContext(ctx, upsertErrorSQL,
		r.ID, fingerprint, r.TenantID, r.UserID, r.URL, r.Method, r.StatusCode,
		string(r.Severity), r.Message, r.StackTrace, r.Environment, r.AppVersion,
		r.RequestID, metadata,
		firstSeen, firstSeen, firstSeen,
	)
	if err != nil {
		return ErrorReport{}, fmt.Errorf("errors: upsert failed: %w", err)
	}

	row := s.db.QueryRowContext(ctx, `
SELECT id, fingerprint, tenant_id, COALESCE(user_id,''), url, method, status_code,
       severity, message, COALESCE(stack_trace,''), environment, app_version,
       COALESCE(request_id,''), COALESCE(metadata,''),
       occurrences, first_seen, last_seen, created_at
FROM error_reports WHERE fingerprint = $1 AND tenant_id = $2`, fingerprint, r.TenantID)

	var merged ErrorReport
	var severity, created, firstSeenStr, lastSeenStr, metaStr string
	err = row.Scan(&merged.ID, &merged.Fingerprint, &merged.TenantID, &merged.UserID,
		&merged.URL, &merged.Method, &merged.StatusCode,
		&severity, &merged.Message, &merged.StackTrace, &merged.Environment, &merged.AppVersion,
		&merged.RequestID, &metaStr,
		&merged.Occurrences, &firstSeenStr, &lastSeenStr, &created)
	if err != nil {
		return ErrorReport{}, fmt.Errorf("errors: select after upsert failed: %w", err)
	}
	merged.Severity = Severity(severity)
	merged.Timestamp, _ = time.Parse(time.RFC3339, lastSeenStr)
	merged.FirstSeen, _ = time.Parse(time.RFC3339, firstSeenStr)
	if metaStr != "" {
		var m map[string]interface{}
		if json.Unmarshal([]byte(metaStr), &m) == nil {
			merged.Metadata = m
		}
	}
	return merged, nil
}

func (s *SQLiteStore) HasOpenIncident(ctx context.Context, fingerprint, tenantID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM incidents i
JOIN error_reports e ON e.id = i.error_id
WHERE e.fingerprint = $1 AND e.tenant_id = $2 AND i.status IN ('OPEN','ASSIGNED')`,
		fingerprint, tenantID).Scan(&n)
	return n > 0, err
}

func (s *SQLiteStore) CreateIncident(ctx context.Context, inc Incident) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO incidents (id, error_id, tenant_id, status, severity, assigned_to, root_cause, created, resolved_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL)`,
		inc.ID, inc.ErrorID, inc.TenantID, inc.Status, string(inc.Severity),
		inc.AssignedTo, inc.RootCause, inc.Created.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) GetIncident(ctx context.Context, id string) (Incident, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, error_id, tenant_id, status, severity, assigned_to, root_cause, created, resolved_at
FROM incidents WHERE id = $1`, id)
	return scanIncident(row.Scan)
}

func (s *SQLiteStore) ResolveIncident(ctx context.Context, id, tenantID, status, assignedTo, rootCause string) error {
	var resolvedAt any
	if status == "RESOLVED" {
		resolvedAt = time.Now().UTC().Format(time.RFC3339)
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE incidents SET status = $1, assigned_to = $2, root_cause = $3, resolved_at = $4
WHERE id = $5 AND tenant_id = $6`, status, assignedTo, rootCause, resolvedAt, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const errorReportSelect = `
SELECT id, fingerprint, tenant_id, COALESCE(user_id,''), url, method, status_code,
       severity, message, COALESCE(stack_trace,''), environment, app_version,
       COALESCE(request_id,''), COALESCE(metadata,''),
       occurrences, first_seen, last_seen, created_at
FROM error_reports`

// GetError returns one deduplicated error group. At most one row can match
// because of the uq_error_reports_fp_tenant unique index (migration 00083).
func (s *SQLiteStore) GetError(ctx context.Context, fingerprint, tenantID string) (ErrorReport, error) {
	row := s.db.QueryRowContext(ctx, errorReportSelect+`
WHERE fingerprint = ? AND tenant_id = ?`, fingerprint, tenantID)
	e, err := scanErrorReport(row.Scan)
	if err != nil {
		return ErrorReport{}, err
	}
	return e, nil
}

func (s *SQLiteStore) ListErrors(ctx context.Context, f ErrorFilter) ([]ErrorReport, error) {
	where, args := errorWhere(f)
	q := errorReportSelect + ` ` + where + `
ORDER BY last_seen DESC LIMIT ? OFFSET ?`
	args = append(args, clampLimit(f.Limit), max(f.Offset, 0))

	rebound, rerr := appdb.Rebind(q)
	if rerr != nil {
		return nil, rerr
	}
	rows, err := s.db.QueryContext(ctx, rebound, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ErrorReport
	for rows.Next() {
		e, err := scanErrorReport(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CountErrors(ctx context.Context, f ErrorFilter) (int, error) {
	where, args := errorWhere(f)
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM error_reports `+where, args...).Scan(&n)
	return n, err
}

func errorWhere(f ErrorFilter) (string, []any) {
	var conds []string
	var args []any
	if f.TenantID != "" {
		conds = append(conds, "tenant_id = ?")
		args = append(args, f.TenantID)
	}
	if f.Severity != "" {
		conds = append(conds, "severity = ?")
		args = append(args, f.Severity)
	}
	if f.Fingerprint != "" {
		conds = append(conds, "fingerprint = ?")
		args = append(args, f.Fingerprint)
	}
	if !f.From.IsZero() {
		conds = append(conds, "last_seen >= ?")
		args = append(args, f.From.UTC().Format(time.RFC3339))
	}
	if !f.To.IsZero() {
		conds = append(conds, "last_seen <= ?")
		args = append(args, f.To.UTC().Format(time.RFC3339))
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func (s *SQLiteStore) ListIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	q := `SELECT id, error_id, tenant_id, status, severity, assigned_to, root_cause, created, resolved_at FROM incidents`
	var args []any
	switch {
	case f.TenantID != "" && f.Status != "":
		q += ` WHERE tenant_id = ? AND status = ?`
		args = append(args, f.TenantID, f.Status)
	case f.TenantID != "":
		q += ` WHERE tenant_id = ?`
		args = append(args, f.TenantID)
	case f.Status != "":
		q += ` WHERE status = ?`
		args = append(args, f.Status)
	}
	q += ` ORDER BY created DESC LIMIT ? OFFSET ?`
	args = append(args, clampLimit(f.Limit), max(f.Offset, 0))

	rebound, rerr := appdb.Rebind(q)
	if rerr != nil {
		return nil, rerr
	}
	rows, err := s.db.QueryContext(ctx, rebound, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Incident{}
	for rows.Next() {
		inc, err := scanIncident(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

type scanner func(dest ...any) error

func scanErrorReport(scan scanner) (ErrorReport, error) {
	var e ErrorReport
	var severity, firstSeenStr, lastSeenStr, created, metaStr string
	if err := scan(&e.ID, &e.Fingerprint, &e.TenantID, &e.UserID,
		&e.URL, &e.Method, &e.StatusCode,
		&severity, &e.Message, &e.StackTrace, &e.Environment, &e.AppVersion,
		&e.RequestID, &metaStr,
		&e.Occurrences, &firstSeenStr, &lastSeenStr, &created); err != nil {
		return ErrorReport{}, err
	}
	e.Severity = Severity(severity)
	e.Timestamp, _ = time.Parse(time.RFC3339, lastSeenStr)
	e.FirstSeen, _ = time.Parse(time.RFC3339, firstSeenStr)
	if metaStr != "" {
		var m map[string]interface{}
		if json.Unmarshal([]byte(metaStr), &m) == nil {
			e.Metadata = m
		}
	}
	return e, nil
}

func scanIncident(scan scanner) (Incident, error) {
	var inc Incident
	var severity, created string
	var resolvedAt sql.NullString
	err := scan(&inc.ID, &inc.ErrorID, &inc.TenantID, &inc.Status, &severity,
		&inc.AssignedTo, &inc.RootCause, &created, &resolvedAt)
	if err != nil {
		return Incident{}, err
	}
	inc.Severity = Severity(severity)
	inc.Created, _ = time.Parse(time.RFC3339, created)
	if resolvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAt.String)
		inc.ResolvedAt = &t
	}
	return inc, nil
}

func clampLimit(n int) int {
	if n <= 0 || n > 500 {
		return 100
	}
	return n
}
