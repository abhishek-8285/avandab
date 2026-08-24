package notifications

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"transport-app/internal/shared/ports"
)

type Notification struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	Recipient string    `json:"recipient"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	mu         sync.Mutex
	inAppStore map[string][]Notification
	email      EmailSender
	sms        SMSSender
}

const maxInAppPerKey = 100

func NewService() *Service {
	return &Service{
		inAppStore: make(map[string][]Notification),
	}
}

// NewServiceWithChannels wires real delivery adapters. Pass nil for a channel
// to keep it unconfigured — its Send then fails honestly instead of faking
// success.
func NewServiceWithChannels(email EmailSender, sms SMSSender) *Service {
	return &Service{
		inAppStore: make(map[string][]Notification),
		email:      email,
		sms:        sms,
	}
}

// EmailConfigured reports whether real SMTP delivery is wired.
func (s *Service) EmailConfigured() bool {
	e, ok := s.email.(interface{ Configured() bool })
	return ok && e.Configured()
}

// SMSConfigured reports whether real SMS delivery is wired.
func (s *Service) SMSConfigured() bool {
	x, ok := s.sms.(interface{ Configured() bool })
	return ok && x.Configured()
}

func (s *Service) SendEmail(ctx context.Context, msg ports.NotificationMessage) error {
	if s.email == nil {
		log.Printf("[NOTIFICATION:EMAIL:UNCONFIGURED] To: %s | Subject: %s", msg.Recipient, msg.Subject)
		return ErrEmailNotConfigured
	}
	return s.email.Send(ctx, msg.Recipient, msg.Subject, msg.Body)
}

func (s *Service) SendInApp(ctx context.Context, msg ports.NotificationMessage) error {
	notif := Notification{
		ID:        fmt.Sprintf("notif_%d", time.Now().UnixNano()),
		TenantID:  msg.TenantID,
		UserID:    msg.UserID,
		Type:      string(ports.NotificationTypeInApp),
		Recipient: msg.Recipient,
		Subject:   msg.Subject,
		Body:      msg.Body,
		Read:      false,
		CreatedAt: time.Now(),
	}

	key := msg.UserID
	if key == "" {
		key = msg.TenantID
	}

	s.mu.Lock()
	if len(s.inAppStore[key]) >= maxInAppPerKey {
		s.inAppStore[key] = s.inAppStore[key][1:]
	}
	s.inAppStore[key] = append(s.inAppStore[key], notif)
	s.mu.Unlock()

	log.Printf("[NOTIFICATION:IN_APP] User/Tenant: %s | Subject: %s", key, msg.Subject)
	return nil
}

func (s *Service) SendSMS(ctx context.Context, msg ports.NotificationMessage) error {
	if s.sms == nil {
		return ErrSMSNotConfigured
	}
	return s.sms.Send(ctx, msg.Recipient, msg.Body)
}

func (s *Service) SendPush(ctx context.Context, msg ports.NotificationMessage) error {
	return fmt.Errorf("push notification channel not configured yet")
}

func (s *Service) SendWebhook(ctx context.Context, msg ports.NotificationMessage) error {
	return fmt.Errorf("webhook notification channel not configured yet")
}

var _ ports.NotificationService = (*Service)(nil)
