package channels

import "context"

// Message defines the payload sent across notification channels.
type Message struct {
	AlertID      string         `json:"alert_id"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Severity     string         `json:"severity"`
	SeverityRank int            `json:"severity_rank,omitempty"` // 1=critical..5=info (Spec 22 §5.1)
	UserID       string         `json:"user_id,omitempty"`       // in_app target
	Phone        string         `json:"phone,omitempty"`         // sms/whatsapp
	Email        string         `json:"email,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

// Provider represents a destination channel for operational alerts.
type Provider interface {
	Name() string
	Send(ctx context.Context, msg Message) error
}
