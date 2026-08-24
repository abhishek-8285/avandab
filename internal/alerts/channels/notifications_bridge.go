package channels

import (
	"context"

	"transport-app/internal/shared/ports"
)

// NotificationBridge adapts the operations notifications service (SMTP email,
// SMS webhook) to the alert Provider interface. When a recipient field is
// absent the send fails honestly — the alert pipeline records the failure.
type NotificationBridge struct {
	name      string
	send      func(ctx context.Context, msg ports.NotificationMessage) error
	recipient func(msg Message) string
}

func NewNotificationBridge(name string, send func(ctx context.Context, msg ports.NotificationMessage) error, recipient func(msg Message) string) *NotificationBridge {
	return &NotificationBridge{name: name, send: send, recipient: recipient}
}

func (b *NotificationBridge) Name() string { return b.name }

func (b *NotificationBridge) Send(ctx context.Context, msg Message) error {
	to := ""
	if b.recipient != nil {
		to = b.recipient(msg)
	}
	return b.send(ctx, ports.NotificationMessage{
		Recipient: to,
		Subject:   msg.Title,
		Body:      msg.Body,
		Type:      ports.NotificationTypeEmail,
	})
}

func NewEmailBridge(svc ports.NotificationService) *NotificationBridge {
	return NewNotificationBridge("email", svc.SendEmail, func(m Message) string { return m.Email })
}

func NewSMSBridge(svc ports.NotificationService) *NotificationBridge {
	return NewNotificationBridge("sms", svc.SendSMS, func(m Message) string { return m.Phone })
}
