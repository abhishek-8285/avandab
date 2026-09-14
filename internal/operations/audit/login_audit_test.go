package audit_test

import (
	"context"
	"testing"

	"transport-app/internal/operations/audit"
	"transport-app/internal/operations/notifications"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
)

func TestLoginAuditService_AdaptiveSecurityNotification(t *testing.T) {
	notifSvc := notifications.NewService(id.NewUUIDGenerator(), clock.NewRealClock())
	policy := audit.SecurityPolicy{
		NotifyOnNewDevice: true,
	}

	svc := audit.NewLoginAuditService(notifSvc, policy)
	ctx := context.Background()

	// First login on Chrome
	err := svc.RecordLogin(ctx, audit.LoginRecord{
		UserID:    "user_101",
		UserEmail: "user@example.com",
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0 Chrome/120.0",
		Success:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error recording login: %v", err)
	}

	history := svc.GetLoginHistory("user_101")
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
}
