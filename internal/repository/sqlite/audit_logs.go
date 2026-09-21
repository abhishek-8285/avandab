package sqlite

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/shared"
)

// AuditLogRepository implementation

// auditScope resolves the tenant predicate for audit reads: the platform
// admin (role admin) sees all orgs; every other role sees only its own
// tenant. Missing tenant on a non-admin context fails closed with an error.
func auditScope(ctx context.Context) (tenantID string, global bool, err error) {
	if sess, ok := ctx.Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil &&
		sess.Role == string(domain.RoleAdmin) {
		return "", true, nil
	}
	t, err := shared.RequireTenantID(ctx)
	if err != nil {
		return "", false, err
	}
	return string(t), false, nil
}

// resolveAuditTenant attributes a new audit row: explicit value wins, then
// the request tenant, then the actor's org (covers pre-auth writes like
// login that carry no request tenant). Unattributable system rows fall back
// to the platform tenant so they stay admin-visible, never cross-org.
func (r *SQLRepository) resolveAuditTenant(ctx context.Context, log domain.AuditLog) string {
	if log.TenantID != "" {
		return string(log.TenantID)
	}
	if t := shared.TenantIDFromContext(ctx); t != "" {
		return string(t)
	}
	if log.UserID != nil {
		var tenant sql.NullString
		if err := r.queryRow(ctx, `SELECT tenant_id FROM users WHERE id = ?`,
			string(*log.UserID)).Scan(&tenant); err == nil && tenant.Valid && tenant.String != "" {
			return tenant.String
		}
	}
	//nolint:tenant-default // platform attribution for unattributable system rows: admin-visible only, never cross-org
	return string(shared.DefaultTenant)
}

func (r *SQLRepository) CreateAuditLog(ctx context.Context, log domain.AuditLog) (domain.AuditLog, error) {
	var userID sql.NullString
	if log.UserID != nil {
		userID = sql.NullString{String: string(*log.UserID), Valid: true}
	}

	created, err := r.Q(ctx).CreateAuditLog(ctx, db.CreateAuditLogParams{
		ID:        string(log.ID),
		UserID:    userID,
		Action:    log.Action,
		TableName: log.TableName,
		RecordID:  nullString(log.RecordID),
		OldValues: nullString(log.OldValues),
		NewValues: nullString(log.NewValues),
		IpAddress: nullString(log.IPAddress),
		Location:  nullString(log.Location),
		TenantID:  sql.NullString{String: r.resolveAuditTenant(ctx, log), Valid: true},
	})
	if err != nil {
		return domain.AuditLog{}, err
	}

	var user *domain.UserID
	if created.UserID.Valid {
		uid := domain.UserID(created.UserID.String)
		user = &uid
	}

	return domain.AuditLog{
		ID:        domain.FileID(created.ID),
		TenantID:  shared.TenantID(created.TenantID.String),
		UserID:    user,
		Action:    created.Action,
		TableName: created.TableName,
		RecordID:  fromNullString(created.RecordID),
		OldValues: fromNullString(created.OldValues),
		NewValues: fromNullString(created.NewValues),
		IPAddress: fromNullString(created.IpAddress),
		Location:  fromNullString(created.Location),
		CreatedAt: created.CreatedAt,
	}, nil
}

func (r *SQLRepository) ListAuditLogs(ctx context.Context, limit, offset int) ([]repository.AuditLogWithUser, error) {
	tenantID, global, err := auditScope(ctx)
	if err != nil {
		return nil, err
	}
	if global {
		rows, err := r.Q(ctx).GetAuditLogsGlobal(ctx, db.GetAuditLogsGlobalParams{
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, err
		}
		result := make([]repository.AuditLogWithUser, len(rows))
		for i, row := range rows {
			result[i] = auditLogRowToWithUser(
				row.ID, row.UserID, row.Action, row.TableName,
				row.RecordID, row.OldValues, row.NewValues,
				row.IpAddress, row.Location, row.TenantID, row.CreatedAt, row.UserName,
			)
		}
		return result, nil
	}
	rows, err := r.Q(ctx).GetAuditLogs(ctx, db.GetAuditLogsParams{
		TenantID: sql.NullString{String: tenantID, Valid: true},
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.AuditLogWithUser, len(rows))
	for i, row := range rows {
		result[i] = auditLogRowToWithUser(
			row.ID, row.UserID, row.Action, row.TableName,
			row.RecordID, row.OldValues, row.NewValues,
			row.IpAddress, row.Location, row.TenantID, row.CreatedAt, row.UserName,
		)
	}
	return result, nil
}

func (r *SQLRepository) GetAuditLogsByRecord(ctx context.Context, tableName, recordID string, limit int) ([]repository.AuditLogWithUser, error) {
	if limit <= 0 {
		limit = 10
	}
	tenantID, global, err := auditScope(ctx)
	if err != nil {
		return nil, err
	}
	if global {
		rows, err := r.Q(ctx).GetAuditLogsByRecordGlobal(ctx, db.GetAuditLogsByRecordGlobalParams{
			TableName: tableName,
			RecordID:  sql.NullString{String: recordID, Valid: true},
			Limit:     int64(limit),
		})
		if err != nil {
			return nil, err
		}
		result := make([]repository.AuditLogWithUser, len(rows))
		for i, row := range rows {
			result[i] = auditLogRowToWithUser(
				row.ID, row.UserID, row.Action, row.TableName,
				row.RecordID, row.OldValues, row.NewValues,
				row.IpAddress, row.Location, row.TenantID, row.CreatedAt, row.UserName,
			)
		}
		return result, nil
	}
	rows, err := r.Q(ctx).GetAuditLogsByRecord(ctx, db.GetAuditLogsByRecordParams{
		TenantID:  sql.NullString{String: tenantID, Valid: true},
		TableName: tableName,
		RecordID:  sql.NullString{String: recordID, Valid: true},
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, err
	}
	result := make([]repository.AuditLogWithUser, len(rows))
	for i, row := range rows {
		result[i] = auditLogRowToWithUser(
			row.ID, row.UserID, row.Action, row.TableName,
			row.RecordID, row.OldValues, row.NewValues,
			row.IpAddress, row.Location, row.TenantID, row.CreatedAt, row.UserName,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountAuditLogs(ctx context.Context) (int64, error) {
	tenantID, global, err := auditScope(ctx)
	if err != nil {
		return 0, err
	}
	if global {
		return r.Q(ctx).CountAuditLogsGlobal(ctx)
	}
	return r.Q(ctx).CountAuditLogs(ctx, sql.NullString{String: tenantID, Valid: true})
}

func (r *SQLRepository) CountAuditLogsSince(ctx context.Context, since time.Time) (int64, error) {
	tenantID, global, err := auditScope(ctx)
	if err != nil {
		return 0, err
	}
	// Bind as a UTC space-format string: the driver serializes time.Time with
	// a monotonic suffix that SQLite datetime() cannot parse (always NULL).
	sinceStr := since.UTC().Format("2006-01-02 15:04:05")
	if global {
		return r.Q(ctx).CountAuditLogsSinceGlobal(ctx, sinceStr)
	}
	return r.Q(ctx).CountAuditLogsSince(ctx, db.CountAuditLogsSinceParams{
		TenantID: sql.NullString{String: tenantID, Valid: true},
		Column2:  sinceStr,
	})
}

// auditLogDateClause filters created_at against the inclusive UTC instant
// bounds from shared.DayBoundsUTC. Storage mixes RFC3339 and CURRENT_TIMESTAMP
// text; SQLite datetime() normalizes both, so comparing instants applies the
// fleet-local (IST) day window instead of silently filing 00:00–05:30 IST
// events under the previous UTC day.
const auditLogDateClause = `
  AND (? = '' OR datetime(CAST(a.created_at AS TEXT)) >= datetime(?))
  AND (? = '' OR datetime(CAST(a.created_at AS TEXT)) <= datetime(?))`

// ListAuditLogsDateRange mirrors ListAuditLogs and additionally filters by a
// free-text query over action/table/record/user and a created_at window
// (optional interface asserted by AuditLogService).
func (r *SQLRepository) ListAuditLogsDateRange(ctx context.Context, query string, from string, to string, limit int, offset int) ([]repository.AuditLogWithUser, int64, error) {
	tenantID, global, err := auditScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	// Platform admin reads global; org roles add the tenant predicate.
	scopeClause := `AND a.tenant_id = ?`
	scopeArgs := []any{tenantID}
	if global {
		scopeClause = ``
		scopeArgs = nil
	}
	qPattern := "%" + query + "%"
	dateFrom, dateTo := shared.DayBoundsUTC(from, to)
	rows, err := r.query(ctx, `
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.location, a.tenant_id, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE ($1 = '' OR a.action LIKE $2 OR a.table_name LIKE $3 OR a.record_id LIKE $4 OR u.name LIKE $5)`+auditLogDateClause+scopeClause+`
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?`,
		append([]any{query, qPattern, qPattern, qPattern, qPattern,
			dateFrom, dateFrom, dateTo, dateTo}, append(scopeArgs, limit, offset)...)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.AuditLogWithUser, 0)
	for rows.Next() {
		var id, action, tableName string
		var userID, recordID, oldValues, newValues, ipAddress, location, tenant, userName sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &action, &tableName, &recordID,
			&oldValues, &newValues, &ipAddress, &location, &tenant, &createdAt, &userName); err != nil {
			return nil, 0, err
		}
		result = append(result, auditLogRowToWithUser(
			id, userID, action, tableName,
			recordID, oldValues, newValues,
			ipAddress, location, tenant, createdAt, userName,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var count int64
	err = r.queryRow(ctx, `
SELECT COUNT(*)
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE ($1 = '' OR a.action LIKE $2 OR a.table_name LIKE $3 OR a.record_id LIKE $4 OR u.name LIKE $5)`+auditLogDateClause+scopeClause,
		append([]any{query, qPattern, qPattern, qPattern, qPattern,
			dateFrom, dateFrom, dateTo, dateTo}, scopeArgs...)...,
	).Scan(&count)
	if err != nil {
		return nil, 0, err
	}
	return result, count, nil
}
