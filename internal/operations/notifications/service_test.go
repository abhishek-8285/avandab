package notifications

import (
	"context"
	"errors"
	"testing"

	"transport-app/internal/shared/ports"
)

func TestNotificationService_SendEmailAndInApp(t *testing.T) {
	svc := NewService()

	ctx := context.Background()

	emailMsg := ports.NotificationMessage{
		Recipient: "admin@flyfleet.io",
		Subject:   "Test Alert",
		Body:      "Email body test",
		Type:      ports.NotificationTypeEmail,
	}

	err := svc.SendEmail(ctx, emailMsg)
	if !errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("unconfigured channel must fail honestly with ErrEmailNotConfigured, got: %v", err)
	}

	inAppMsg := ports.NotificationMessage{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Recipient: "user-1",
		Subject:   "In-App Welcome",
		Body:      "Welcome to FlyFleet",
		Type:      ports.NotificationTypeInApp,
	}

	err = svc.SendInApp(ctx, inAppMsg)
	if err != nil {
		t.Fatalf("expected nil error sending in-app notif, got: %v", err)
	}
}
