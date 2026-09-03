package channels

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// LoggingProvider decorates a channel so every delivery attempt is
// persisted (Spec 22 §2.9): notification_log rows are the delivery
// record; failures are stored, never swallowed.
type LoggingProvider struct {
	next Provider
	db   *sql.DB
	log  *slog.Logger
}

func NewLoggingProvider(next Provider, db *sql.DB, log *slog.Logger) *LoggingProvider {
	return &LoggingProvider{next: next, db: db, log: log}
}

func (l *LoggingProvider) Name() string { return l.next.Name() }

func (l *LoggingProvider) Send(ctx context.Context, msg Message) error {
	err := l.next.Send(ctx, msg)
	status, detail := "sent", ""
	if err != nil {
		status, detail = "failed", err.Error()
	}
	l.record(ctx, msg, status, detail)
	return err
}

// SendWhatsApp forwards direct WhatsApp delivery to downstream provider if supported.
func (l *LoggingProvider) SendWhatsApp(ctx context.Context, phone, text string) error {
	if s, ok := l.next.(interface {
		SendWhatsApp(ctx context.Context, phone, text string) error
	}); ok {
		err := s.SendWhatsApp(ctx, phone, text)
		status, detail := "sent", ""
		if err != nil {
			status, detail = "failed", err.Error()
		}
		l.record(ctx, Message{Phone: phone, Body: text}, status, detail)
		return err
	}
	return l.Send(ctx, Message{Phone: phone, Body: text})
}

func (l *LoggingProvider) record(ctx context.Context, msg Message, status, detail string) {
	target := msg.Phone
	if target == "" {
		target = msg.Email
	}
	if target == "" {
		target = msg.UserID
	}
	_, err := l.db.ExecContext(ctx, `
		INSERT INTO notification_log (id, channel, alert_id, target, status, error, created_at)
		VALUES ('nlog-' || lower(hex(randomblob(8))), ?, ?, ?, ?, ?, ?)`,
		l.next.Name(), msg.AlertID, target, status, detail, time.Now().UTC())
	if err != nil {
		l.log.Warn("notification_log write failed", "channel", l.next.Name(),
			"alert_id", msg.AlertID, "error", err)
	}
}
