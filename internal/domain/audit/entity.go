package audit

import (
	"time"

	"transport-app/internal/domain/types"
)

// AuditLog represents a record of a significant action.
type AuditLog struct {
	ID        types.FileID
	UserID    *types.UserID
	Action    string
	TableName string
	RecordID  *string
	OldValues *string
	NewValues *string
	IPAddress *string
	Location  *string
	CreatedAt time.Time
}
