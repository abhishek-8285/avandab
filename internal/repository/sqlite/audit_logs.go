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
