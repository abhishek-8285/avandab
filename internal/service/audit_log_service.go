package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/repository"
)

// AuditLogService handles audit log operations.
type AuditLogService struct {
	baseService
}

// ListAuditLogs retrieves audit logs with pagination.
func (s *AuditLogService) ListAuditLogs(ctx context.Context, limit, offset int) ([]repository.AuditLogWithUser, int64, error) {
	logs, err := s.store.ListAuditLogs(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountAuditLogs(ctx)
	if err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// ListAuditLogsByRecord retrieves the most recent audit entries for a single record.
func (s *AuditLogService) ListAuditLogsByRecord(ctx context.Context, tableName, recordID string, limit int) ([]repository.AuditLogWithUser, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.store.GetAuditLogsByRecord(ctx, tableName, recordID, limit)
}

// CountUnread counts audit entries created after the given time.
func (s *AuditLogService) CountUnread(ctx context.Context, since time.Time) (int64, error) {
	return s.store.CountAuditLogsSince(ctx, since)
}

// LogAction creates an audit log entry for a significant action.
func (s *AuditLogService) LogAction(ctx context.Context, userID *domain.UserID, action, tableName, recordID string, oldValues, newValues *string) error {
	var uid *string
	if userID != nil {
		uid = new(string)
		*uid = string(*userID)
	}

	_, err := s.store.CreateAuditLog(ctx, domain.AuditLog{
		ID:        domain.FileID(uuid.NewString()),
		UserID:    userID,
		Action:    action,
		TableName: tableName,
		RecordID:  strPtr(recordID),
		OldValues: oldValues,
		NewValues: newValues,
		IPAddress: getUserIP(ctx),
	})
	return err
}

// getUserIP extracts the user IP from the request context.
func getUserIP(ctx context.Context) *string {
	if ip, ok := ctx.Value(auth.ContextIP).(string); ok && ip != "" {
		return &ip
	}
	return nil
}

// logAudit is a helper for services to create audit log entries.
func (s *baseService) logAudit(ctx context.Context, userID *domain.UserID, action, table, recordID string, oldValues, newValues *string) {
	_, _ = s.store.CreateAuditLog(ctx, domain.AuditLog{
		ID:        domain.FileID(generateID()),
		UserID:    userID,
		Action:    action,
		TableName: table,
		RecordID:  strPtr(recordID),
		OldValues: oldValues,
		NewValues: newValues,
		IPAddress: getUserIP(ctx),
	})
}
