// Package comm — durable outbound communication queue (migration 00118).
//
// Producers enqueue in the SAME transaction as the business write (true
// outbox — no lost sends); the Worker delivers email through notifications.EmailSender
// and WhatsApp through WhatsAppSender, applying exponential backoff via
// attempts and dead-lettering after MaxAttempts. Anti-ban rate limiting for
// WhatsApp enforces human jitter (3–5 seconds delay between consecutive sends).
// Restart/replica-safe: rows are claimed with a compare-and-set on status, and
// stale 'sending' claims (crashed worker) are reaped back for retry.
package comm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"transport-app/internal/operations/notifications"
	"transport-app/internal/repository"
	idpkg "transport-app/internal/shared/id"
)

const (
	DefaultPollInterval = 5 * time.Second
	DefaultBackoffBase  = time.Minute
	DefaultMaxAttempts  = 5
	// StaleAfter — a row stuck in 'sending' longer than this was claimed by a
	// worker that died mid-send; the reaper returns it to the queue.
	DefaultStaleAfter = 5 * time.Minute
	// sendTimeout bounds each dial+send so a hung relay/API can't stall the poll loop.
	sendTimeout = 30 * time.Second
	// batchSize caps rows claimed per tick — one slow provider can't starve the rest.
	batchSize = 100

	// Anti-ban rate limiting for WhatsApp (3-5s human jitter).
	DefaultWAJitterMin = 3 * time.Second
	DefaultWAJitterMax = 5 * time.Second
)

// WhatsAppSender delivers outbound WhatsApp messages.
type WhatsAppSender interface {
	SendWhatsApp(ctx context.Context, phone, text string) error
}

// LogWhatsAppSender logs WhatsApp messages without sending (for dev/test).
type LogWhatsAppSender struct {
	logger *slog.Logger
}

func NewLogWhatsAppSender(logger *slog.Logger) *LogWhatsAppSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogWhatsAppSender{logger: logger}
}

func (l *LogWhatsAppSender) SendWhatsApp(ctx context.Context, phone, text string) error {
	l.logger.Info("comm outbox: whatsapp logged (mock mode)", "to", phone, "text", text)
	return nil
}

// EmailPayload represents the serialized JSON payload for an email in comm_outbox.
type EmailPayload struct {
	Subject     string                     `json:"subject"`
	Body        string                     `json:"body,omitempty"`
	HTMLBody    string                     `json:"html_body,omitempty"`
	Attachments []notifications.Attachment `json:"attachments,omitempty"`
}

// WhatsAppPayload represents the serialized JSON payload for a WhatsApp message in comm_outbox.
type WhatsAppPayload struct {
	Body     string            `json:"body,omitempty"`
	Text     string            `json:"text,omitempty"`
	Template string            `json:"template,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	MediaURL string            `json:"media_url,omitempty"`
}

// EnqueueRichEmail inserts an email with optional HTML and binary attachments into comm_outbox.
func EnqueueRichEmail(ctx context.Context, db *sql.DB, tenantID, to, template string, payload EmailPayload) (string, error) {
	if db == nil {
		return "", fmt.Errorf("comm: nil database")
	}
	if tenantID == "" {
		return "", fmt.Errorf("comm: tenantID required (derive via shared.TenantIDFromContext)")
	}
	if to == "" {
		return "", fmt.Errorf("comm: recipient required")
	}
	if template == "" {
		template = "generic"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("comm: marshal payload: %w", err)
	}
	idv := idpkg.NewUUIDGenerator().GenerateUUID()

	const q = `INSERT INTO comm_outbox (id, tenant_id, channel, recipient, template, payload_json)
	           VALUES (?, ?, 'email', ?, ?, ?)`
	if tx := repository.TxFromContext(ctx); tx != nil {
		if _, err := tx.ExecContext(ctx, q, idv, tenantID, to, template, string(data)); err != nil {
			return "", fmt.Errorf("comm: enqueue email (tx): %w", err)
		}
	} else {
		if _, err := db.ExecContext(ctx, q, idv, tenantID, to, template, string(data)); err != nil {
			return "", fmt.Errorf("comm: enqueue email: %w", err)
		}
	}
	return idv, nil
}

// EnqueueEmail inserts one plain-text email into comm_outbox.
func EnqueueEmail(ctx context.Context, db *sql.DB, tenantID, to, template, subject, body string) (string, error) {
	return EnqueueRichEmail(ctx, db, tenantID, to, template, EmailPayload{
		Subject: subject,
		Body:    body,
	})
}

// EnqueueRichWhatsApp inserts a structured WhatsApp message into comm_outbox.
func EnqueueRichWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, template string, payload WhatsAppPayload) (string, error) {
	if db == nil {
		return "", fmt.Errorf("comm: nil database")
	}
	if tenantID == "" {
		return "", fmt.Errorf("comm: tenantID required (derive via shared.TenantIDFromContext)")
	}
	if phone == "" {
		return "", fmt.Errorf("comm: recipient phone required")
	}
	if template == "" {
		template = "generic"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("comm: marshal payload: %w", err)
	}
	idv := idpkg.NewUUIDGenerator().GenerateUUID()

	const q = `INSERT INTO comm_outbox (id, tenant_id, channel, recipient, template, payload_json)
	           VALUES (?, ?, 'whatsapp', ?, ?, ?)`
	if tx := repository.TxFromContext(ctx); tx != nil {
		if _, err := tx.ExecContext(ctx, q, idv, tenantID, phone, template, string(data)); err != nil {
			return "", fmt.Errorf("comm: enqueue whatsapp (tx): %w", err)
		}
	} else {
		if _, err := db.ExecContext(ctx, q, idv, tenantID, phone, template, string(data)); err != nil {
			return "", fmt.Errorf("comm: enqueue whatsapp: %w", err)
		}
	}
	return idv, nil
}

// EnqueueWhatsApp inserts one plain-text WhatsApp message into comm_outbox.
func EnqueueWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, template, message string) (string, error) {
	return EnqueueRichWhatsApp(ctx, db, tenantID, phone, template, WhatsAppPayload{
		Text: message,
		Body: message,
	})
}

// EnqueueTripDispatchWhatsApp queues a trip dispatch notification for the driver.
func EnqueueTripDispatchWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, origin, destination, vehicleID string) (string, error) {
	msg := FormatTripDispatchMessage(origin, destination, vehicleID)
	return EnqueueRichWhatsApp(ctx, db, tenantID, phone, "trip_dispatched", WhatsAppPayload{
		Text:     msg,
		Body:     msg,
		Template: "trip_dispatched",
		Params: map[string]string{
			"origin":      origin,
			"destination": destination,
			"vehicle_id":  vehicleID,
		},
	})
}

// EnqueueTripTrackingWhatsApp queues a trip tracking notification for the customer.
func EnqueueTripTrackingWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, tripNum, origin, destination, vehicleID string) (string, error) {
	msg := FormatTripTrackingMessage(tripNum, origin, destination, vehicleID)
	return EnqueueRichWhatsApp(ctx, db, tenantID, phone, "trip_tracking", WhatsAppPayload{
		Text:     msg,
		Body:     msg,
		Template: "trip_tracking",
		Params: map[string]string{
			"trip_number": tripNum,
			"origin":      origin,
			"destination": destination,
			"vehicle_id":  vehicleID,
		},
	})
}

// EnqueueBookingTrackingWhatsApp queues a booking confirmed notification for the customer.
func EnqueueBookingTrackingWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, bookingNum, origin, destination, trackingURL string) (string, error) {
	msg := FormatBookingConfirmedMessage(bookingNum, origin, destination, trackingURL)
	return EnqueueRichWhatsApp(ctx, db, tenantID, phone, "booking_confirmed", WhatsAppPayload{
		Text:     msg,
		Body:     msg,
		Template: "booking_confirmed",
		Params: map[string]string{
			"booking_number": bookingNum,
			"origin":         origin,
			"destination":    destination,
			"tracking_url":   trackingURL,
		},
	})
}

// EnqueuePODWhatsApp queues an e-POD receipt notification for the customer.
func EnqueuePODWhatsApp(ctx context.Context, db *sql.DB, tenantID, phone, tripNum, podURL string) (string, error) {
	msg := FormatPODReceiptMessage(tripNum, podURL)
	return EnqueueRichWhatsApp(ctx, db, tenantID, phone, "pod_receipt", WhatsAppPayload{
		Text:     msg,
		Body:     msg,
		Template: "pod_receipt",
		MediaURL: podURL,
		Params: map[string]string{
			"trip_number": tripNum,
			"pod_url":     podURL,
		},
	})
}

// EnqueueInvoiceEmail formats and queues an invoice email with HTML body and attached PDF.
func EnqueueInvoiceEmail(ctx context.Context, db *sql.DB, tenantID, to, invoiceNum string, total float64, dueDate string, pdfBytes []byte) (string, error) {
	subject := fmt.Sprintf("Invoice #%s from Avandab Fleet", invoiceNum)
	textBody := fmt.Sprintf("Dear Customer,\n\nYour invoice #%s for ₹%.2f is ready.\nDue Date: %s\n\nThank you for choosing Avandab Fleet.",
		invoiceNum, total, dueDate)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #0f172a; color: #f8fafc; padding: 24px; margin: 0;">
  <div style="max-width: 600px; margin: 0 auto; background-color: #1e293b; border-radius: 12px; border: 1px solid #334155; overflow: hidden; padding: 32px;">
    <div style="border-bottom: 1px solid #334155; padding-bottom: 16px; margin-bottom: 24px;">
      <h1 style="color: #38bdf8; font-size: 24px; margin: 0;">Avandab Fleet</h1>
      <p style="color: #94a3b8; font-size: 14px; margin: 4px 0 0 0;">Automated Billing &amp; Invoicing</p>
    </div>
    <div style="background-color: #0f172a; border-radius: 8px; padding: 20px; margin-bottom: 24px;">
      <h2 style="color: #f8fafc; font-size: 18px; margin: 0 0 12px 0;">Invoice Issued: <span style="color: #38bdf8;">#%s</span></h2>
      <table style="width: 100%%; border-collapse: collapse; font-size: 14px; color: #cbd5e1;">
        <tr>
          <td style="padding: 6px 0; color: #94a3b8;">Amount Due:</td>
          <td style="padding: 6px 0; text-align: right; font-weight: bold; color: #4ade80; font-size: 16px;">₹%.2f</td>
        </tr>
        <tr>
          <td style="padding: 6px 0; color: #94a3b8;">Due Date:</td>
          <td style="padding: 6px 0; text-align: right; font-weight: 600;">%s</td>
        </tr>
      </table>
    </div>
    <p style="color: #94a3b8; font-size: 14px; line-height: 1.5;">Please find the invoice details above. The complete invoice PDF is attached to this email.</p>
    <div style="margin-top: 32px; border-top: 1px solid #334155; padding-top: 16px; font-size: 12px; color: #64748b;">
      Avandab Fleet Logistics • Automated Transactional Communication
    </div>
  </div>
</body>
</html>`, invoiceNum, total, dueDate)

	var attachments []notifications.Attachment
	if len(pdfBytes) > 0 {
		attachments = append(attachments, notifications.Attachment{
			Filename:    fmt.Sprintf("invoice_%s.pdf", invoiceNum),
			ContentType: "application/pdf",
			Data:        pdfBytes,
		})
	}

	return EnqueueRichEmail(ctx, db, tenantID, to, "invoice_issued", EmailPayload{
		Subject:     subject,
		Body:        textBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	})
}

// EnqueuePODEmail formats and queues an e-POD delivery receipt email.
func EnqueuePODEmail(ctx context.Context, db *sql.DB, tenantID, to, tripNum, podURL string, podBytes []byte) (string, error) {
	subject := fmt.Sprintf("Delivery Confirmed - Trip #%s Proof of Delivery", tripNum)
	textBody := fmt.Sprintf("Dear Customer,\n\nYour shipment for Trip #%s has been delivered successfully.\nProof of Delivery URL: %s\n\nThank you for choosing Avandab Fleet.",
		tripNum, podURL)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #0f172a; color: #f8fafc; padding: 24px; margin: 0;">
  <div style="max-width: 600px; margin: 0 auto; background-color: #1e293b; border-radius: 12px; border: 1px solid #334155; overflow: hidden; padding: 32px;">
    <div style="border-bottom: 1px solid #334155; padding-bottom: 16px; margin-bottom: 24px;">
      <h1 style="color: #4ade80; font-size: 24px; margin: 0;">Avandab Fleet</h1>
      <p style="color: #94a3b8; font-size: 14px; margin: 4px 0 0 0;">Proof of Delivery &amp; Receipt</p>
    </div>
    <div style="background-color: #0f172a; border-radius: 8px; padding: 20px; margin-bottom: 24px;">
      <h2 style="color: #f8fafc; font-size: 18px; margin: 0 0 12px 0;">Delivery Confirmed: <span style="color: #4ade80;">Trip #%s</span></h2>
      <p style="color: #cbd5e1; font-size: 14px; margin: 0 0 16px 0;">All consignments have been handed over and verified via digital e-POD.</p>
      %s
    </div>
    <div style="margin-top: 32px; border-top: 1px solid #334155; padding-top: 16px; font-size: 12px; color: #64748b;">
      Avandab Fleet Logistics • Automated Transactional Communication
    </div>
  </div>
</body>
</html>`, tripNum, func() string {
		if podURL != "" {
			return fmt.Sprintf(`<a href="%s" style="display: inline-block; background-color: #059669; color: #ffffff; padding: 10px 20px; border-radius: 6px; text-decoration: none; font-weight: 600; font-size: 14px;">View Signed e-POD</a>`, podURL)
		}
		return `<span style="color: #94a3b8; font-size: 13px;">e-POD document attached</span>`
	}())

	var attachments []notifications.Attachment
	if len(podBytes) > 0 {
		attachments = append(attachments, notifications.Attachment{
			Filename:    fmt.Sprintf("epod_%s.pdf", tripNum),
			ContentType: "application/pdf",
			Data:        podBytes,
		})
	}

	return EnqueueRichEmail(ctx, db, tenantID, to, "trip_completed", EmailPayload{
		Subject:     subject,
		Body:        textBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	})
}

// Worker polls comm_outbox and delivers pending emails and WhatsApp messages through configured transports.
type Worker struct {
	db          *sql.DB
	email       notifications.EmailSender
	whatsapp    WhatsAppSender
	logger      *slog.Logger
	interval    time.Duration
	backoff     time.Duration
	staleAfter  time.Duration
	maxAttempts int
	jitterMin   time.Duration
	jitterMax   time.Duration
	sleeper     func(ctx context.Context, d time.Duration) error
}

func NewWorker(db *sql.DB, email notifications.EmailSender, whatsapp WhatsAppSender, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		db:          db,
		email:       email,
		whatsapp:    whatsapp,
		logger:      logger,
		interval:    DefaultPollInterval,
		backoff:     DefaultBackoffBase,
		staleAfter:  DefaultStaleAfter,
		maxAttempts: DefaultMaxAttempts,
		jitterMin:   DefaultWAJitterMin,
		jitterMax:   DefaultWAJitterMax,
	}
}

// SetJitter configures anti-ban human jitter bounds (useful for test suites).
func (w *Worker) SetJitter(min, max time.Duration) {
	w.jitterMin = min
	w.jitterMax = max
}

// SetSleeper overrides the sleep implementation (useful for deterministic tests).
func (w *Worker) SetSleeper(sleeper func(ctx context.Context, d time.Duration) error) {
	w.sleeper = sleeper
}

// Run blocks until ctx is cancelled, polling on interval (outbox.Relay pattern).
func (w *Worker) Run(ctx context.Context) {
	if w.db == nil {
		w.logger.Warn("comm outbox worker disabled: nil database")
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.logger.Info("comm outbox worker started",
		"interval", w.interval.String(),
		"backoff", w.backoff.String(),
		"max_attempts", w.maxAttempts,
		"email_configured", w.email != nil,
		"whatsapp_configured", w.whatsapp != nil)

	w.process(ctx)
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("comm outbox worker stopped")
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

// process runs one full tick: reap stale claims, then deliver due rows.
func (w *Worker) process(ctx context.Context) {
	w.reapStale(ctx)

	rows, err := w.db.QueryContext(ctx, `
		SELECT id, channel FROM comm_outbox
		WHERE status IN ('pending','failed')
		  AND next_attempt_at <= datetime('now')
		  AND attempts < ?
		ORDER BY created_at
		LIMIT ?`, w.maxAttempts, batchSize)
	if err != nil {
		w.logger.Error("comm outbox worker: poll failed", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	type task struct {
		id      string
		channel string
	}
	var tasks []task
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.id, &t.channel); err != nil {
			w.logger.Error("comm outbox worker: scan failed", "error", err)
			break
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		w.logger.Error("comm outbox worker: row iteration failed", "error", err)
		return
	}

	for i, t := range tasks {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := w.deliver(ctx, t.id); err != nil {
			w.logger.Error("comm outbox worker: delivery failed", "id", t.id, "channel", t.channel, "error", err)
		}
		// If delivering WhatsApp, apply jitter delay between consecutive messages
		if t.channel == "whatsapp" && i < len(tasks)-1 {
			jitter := w.calculateWAJitter()
			if err := w.sleepJitter(ctx, jitter); err != nil {
				return
			}
		}
	}
}

func (w *Worker) calculateWAJitter() time.Duration {
	if w.jitterMin <= 0 && w.jitterMax <= 0 {
		return 0
	}
	if w.jitterMax <= w.jitterMin {
		return w.jitterMin
	}
	diff := w.jitterMax - w.jitterMin
	return w.jitterMin + time.Duration(rand.Int63n(int64(diff)+1))
}

func (w *Worker) sleepJitter(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if w.sleeper != nil {
		return w.sleeper(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// reapStale returns rows stuck in 'sending' (crashed worker) to the queue.
func (w *Worker) reapStale(ctx context.Context) {
	res, err := w.db.ExecContext(ctx, `
		UPDATE comm_outbox SET status='failed',
		       last_error='stale sending claim reaped by worker'
		WHERE status='sending'
		  AND next_attempt_at < datetime('now', ?)`,
		fmt.Sprintf("-%d seconds", int(w.staleAfter.Seconds())))
	if err != nil {
		w.logger.Error("comm outbox worker: stale reaper failed", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		w.logger.Warn("comm outbox worker: reaped stale sending claims", "count", n)
	}
}

// deliver claims one row (compare-and-set), sends it, and records the outcome.
func (w *Worker) deliver(ctx context.Context, idv string) error {
	var attempts int
	if err := w.db.QueryRowContext(ctx,
		`SELECT attempts FROM comm_outbox WHERE id = ?`, idv).Scan(&attempts); err != nil {
		return fmt.Errorf("load attempts: %w", err)
	}

	res, err := w.db.ExecContext(ctx, `
		UPDATE comm_outbox SET status='sending', attempts = attempts + 1,
		       next_attempt_at = datetime('now')
		WHERE id = ? AND status IN ('pending','failed')`, idv)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // claimed by another worker — skip silently (replica-safe)
	}

	var channel, to, template, payload string
	err = w.db.QueryRowContext(ctx,
		`SELECT channel, recipient, template, payload_json FROM comm_outbox WHERE id = ?`, idv).
		Scan(&channel, &to, &template, &payload)
	if err != nil {
		return fmt.Errorf("load claimed row: %w", err)
	}

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	sendErr := w.send(sendCtx, channel, to, template, payload)

	if sendErr == nil {
		if _, uerr := w.db.ExecContext(ctx, `
			UPDATE comm_outbox SET status='sent', sent_at=datetime('now'), last_error=NULL
			WHERE id = ? AND status='sending'`, idv); uerr != nil {
			return fmt.Errorf("mark sent: %w", uerr)
		}

		if channel == "whatsapp" {
			jitter := w.calculateWAJitter()
			// Anti-ban rate limiting: push back next_attempt_at for all remaining pending/failed WhatsApp rows
			if jitter > 0 {
				seconds := int(math.Ceil(jitter.Seconds()))
				if seconds < 1 {
					seconds = 1
				}
				_, _ = w.db.ExecContext(ctx, `
					UPDATE comm_outbox
					SET next_attempt_at = datetime('now', ?)
					WHERE channel = 'whatsapp'
					  AND status IN ('pending', 'failed')
					  AND next_attempt_at <= datetime('now', ?)`,
					fmt.Sprintf("+%d seconds", seconds),
					fmt.Sprintf("+%d seconds", seconds))
			}
		}

		w.logger.Info("comm outbox worker: message delivered", "id", idv, "channel", channel, "to", to, "template", template)
		return nil
	}

	// attempts+1 = this attempt. Exponential backoff: base * 2^(n-1), capped 24h.
	backoff := w.backoff
	for i := 1; i < attempts+1; i++ {
		backoff *= 2
		if backoff > 24*time.Hour {
			backoff = 24 * time.Hour
			break
		}
	}
	dead := attempts+1 >= w.maxAttempts
	if _, uerr := w.db.ExecContext(ctx, `
		UPDATE comm_outbox
		SET status = CASE WHEN ? THEN 'dead' ELSE 'failed' END,
		    next_attempt_at = CASE WHEN ? THEN next_attempt_at
		                           ELSE datetime('now', ?) END,
		    last_error = ?
		WHERE id = ? AND status='sending'`,
		dead, dead, fmt.Sprintf("+%d seconds", int(backoff.Seconds())), sendErr.Error(), idv); uerr != nil {
		return fmt.Errorf("mark failed: %w (send error: %v)", uerr, sendErr)
	}
	return sendErr
}

// send renders the payload and hands it to the appropriate channel sender.
func (w *Worker) send(ctx context.Context, channel, to, template, payload string) error {
	switch channel {
	case "email":
		return w.sendEmail(ctx, to, template, payload)
	case "whatsapp":
		return w.sendWhatsApp(ctx, to, template, payload)
	default:
		return fmt.Errorf("comm outbox: unsupported channel %q", channel)
	}
}

func (w *Worker) sendEmail(ctx context.Context, to, template, payload string) error {
	var p struct {
		Subject     string                     `json:"subject"`
		Body        string                     `json:"body"`
		HTMLBody    string                     `json:"html_body"`
		Attachments []notifications.Attachment `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("render email payload: %w", err)
	}
	subject := p.Subject
	if subject == "" {
		subject = template
	}
	if w.email == nil {
		return fmt.Errorf("email sender not wired (set SMTP_HOST / SMTP_FROM)")
	}
	if rich, ok := w.email.(notifications.RichEmailSender); ok && (p.HTMLBody != "" || len(p.Attachments) > 0) {
		return rich.SendRich(ctx, notifications.EmailMessage{
			To:          to,
			Subject:     subject,
			TextBody:    p.Body,
			HTMLBody:    p.HTMLBody,
			Attachments: p.Attachments,
		})
	}
	body := p.Body
	if body == "" {
		body = p.HTMLBody
	}
	return w.email.Send(ctx, to, subject, body)
}

func (w *Worker) sendWhatsApp(ctx context.Context, to, template, payload string) error {
	var p WhatsAppPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("render whatsapp payload: %w", err)
	}
	text := p.Text
	if text == "" {
		text = p.Body
	}
	if text == "" {
		text = template
	}
	if w.whatsapp == nil {
		return fmt.Errorf("whatsapp sender not wired (set WHATSAPP_PROVIDER)")
	}
	return w.whatsapp.SendWhatsApp(ctx, to, text)
}
