package audit

import (
	"context"
	"time"

	"transport-app/internal/domain/types"
)

// AuditLogRepository defines the interface for audit log persistence.
type AuditLogRepository interface {
	CreateAuditLog(ctx context.Context, log AuditLog) (AuditLog, error)
	ListAuditLogs(ctx context.Context, limit, offset int) ([]AuditLogWithUser, error)
	GetAuditLogsByRecord(ctx context.Context, tableName, recordID string, limit int) ([]AuditLogWithUser, error)
	CountAuditLogs(ctx context.Context) (int64, error)
	CountAuditLogsSince(ctx context.Context, since time.Time) (int64, error)
}

// AuditLogWithUser includes the associated user name.
type AuditLogWithUser struct {
	AuditLog
	UserName *string
}

// FileRepository defines the interface for file management.
type FileRepository interface {
	CreateFile(ctx context.Context, file types.File) (types.File, error)
	GetFileByID(ctx context.Context, id types.FileID) (types.File, error)
	GetFilesByUploadable(ctx context.Context, uploadableType string, uploadableID string) ([]types.File, error)
	DeleteFile(ctx context.Context, id types.FileID) error
	DeleteFilesByUploadable(ctx context.Context, uploadableType string, uploadableID string) error
}
