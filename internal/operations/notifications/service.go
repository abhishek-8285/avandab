package notifications

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"transport-app/internal/shared/id"
	"transport-app/internal/shared/ports"
)

// Ids must NOT be minted from time.Now(): on Windows the clock steps in ~0.5ms
// chunks, so two ids generated inside one tick are identical and the second
// INSERT fails on a UNIQUE/PK column. Invisible on Linux CI, intermittent on
// Windows. See docs/10-FRONTEND-UX-AUDIT.md §7.4.
//
// GenerateUUID, not GenerateDisplayID: the latter truncates a UUID to 8 hex
// chars (32 bits), which collides by the birthday bound at ~77k rows — fine
// for a booking number, not for a notification table.
var idGen = id.NewUUIDGenerator()

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
		ID:        "notif_" + idGen.GenerateUUID(),
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
