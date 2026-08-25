package sqlite

import (
	"context"
	"database/sql"
	"time"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
)

// AuditLogRepository implementation

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
		UserID:    user,
		Action:    created.Action,
		TableName: created.TableName,
		RecordID:  fromNullString(created.RecordID),
		OldValues: fromNullString(created.OldValues),
		NewValues: fromNullString(created.NewValues),
		IPAddress: fromNullString(created.IpAddress),
		CreatedAt: created.CreatedAt,
	}, nil
}

func (r *SQLRepository) ListAuditLogs(ctx context.Context, limit, offset int) ([]repository.AuditLogWithUser, error) {
	rows, err := r.Q(ctx).GetAuditLogs(ctx, db.GetAuditLogsParams{
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
			row.IpAddress, row.CreatedAt, row.UserName,
		)
	}
	return result, nil
}

func (r *SQLRepository) GetAuditLogsByRecord(ctx context.Context, tableName, recordID string, limit int) ([]repository.AuditLogWithUser, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.Q(ctx).GetAuditLogsByRecord(ctx, db.GetAuditLogsByRecordParams{
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
			row.IpAddress, row.CreatedAt, row.UserName,
		)
	}
	return result, nil
}

func (r *SQLRepository) CountAuditLogs(ctx context.Context) (int64, error) {
	return r.Q(ctx).CountAuditLogs(ctx)
}

func (r *SQLRepository) CountAuditLogsSince(ctx context.Context, since time.Time) (int64, error) {
	return r.Q(ctx).CountAuditLogsSince(ctx, since)
}

// auditLogDateClause filters on created_at using date(substr(...)) because
// SQLite stores timestamps as text in mixed formats (RFC3339 from Go,
// 'YYYY-MM-DD HH:MM:SS' from datetime('now')) — only the prefix is stable.
const auditLogDateClause = `
  AND (? = '' OR date(substr(a.created_at,1,10)) >= date(?))
  AND (? = '' OR date(substr(a.created_at,1,10)) <= date(?))`

// ListAuditLogsDateRange mirrors ListAuditLogs and additionally filters by a
// free-text query over action/table/record/user and a created_at window
// (optional interface asserted by AuditLogService).
func (r *SQLRepository) ListAuditLogsDateRange(ctx context.Context, query string, from string, to string, limit int, offset int) ([]repository.AuditLogWithUser, int64, error) {
	qPattern := "%" + query + "%"
	rows, err := r.query(ctx, `
SELECT a.id, a.user_id, a.action, a.table_name, a.record_id, a.old_values, a.new_values, a.ip_address, a.created_at,
       u.name AS user_name
FROM audit_logs a
LEFT JOIN users u ON a.user_id = u.id
WHERE (? = '' OR a.action LIKE ? OR a.table_name LIKE ? OR a.record_id LIKE ? OR u.name LIKE ?)`+auditLogDateClause+`
ORDER BY a.created_at DESC
LIMIT ? OFFSET ?`,
		query, qPattern, qPattern, qPattern, qPattern,
		from, from, to, to,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.AuditLogWithUser, 0)
	for rows.Next() {
		var id, action, tableName string
		var userID, recordID, oldValues, newValues, ipAddress, userName sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &action, &tableName, &recordID,
			&oldValues, &newValues, &ipAddress, &createdAt, &userName); err != nil {
			return nil, 0, err
		}
		result = append(result, auditLogRowToWithUser(
			id, userID, action, tableName,
			recordID, oldValues, newValues,
			ipAddress, createdAt, userName,
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
WHERE (? = '' OR a.action LIKE ? OR a.table_name LIKE ? OR a.record_id LIKE ? OR u.name LIKE ?)`+auditLogDateClause,
		query, qPattern, qPattern, qPattern, qPattern,
		from, from, to, to,
	).Scan(&count)
	if err != nil {
		return nil, 0, err
	}
	return result, count, nil
}
