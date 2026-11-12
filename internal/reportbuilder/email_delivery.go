package reportbuilder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/operations/notifications"
)

// EmailDelivery delivers scheduled report artifacts through the existing email
// adapters (SMTP pool, direct MX, or the honest log-only sender).
type EmailDelivery struct {
	sender notifications.RichEmailSender
	logger *slog.Logger
}

// NewEmailDelivery builds the delivery seam. When the sender is unconfigured,
// Configured() is false and TriggerSchedule marks the run PENDING_DELIVERY
// instead of pretending the report was delivered.
func NewEmailDelivery(sender notifications.RichEmailSender, logger *slog.Logger) *EmailDelivery {
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailDelivery{sender: sender, logger: logger}
}

// Configured reports whether a real email transport is available.
func (d *EmailDelivery) Configured() bool {
	return d != nil && d.sender != nil && d.sender.Configured()
}

// Deliver emails the artifact to every schedule recipient. A channel other than
// EMAIL is refused (no silent fallback), and zero recipients is an error.
func (d *EmailDelivery) Deliver(ctx context.Context, tenantID string, sched *ReportSchedule, artifact ReportArtifact) error {
	if d == nil || d.sender == nil {
		return errors.New("email delivery: sender not configured")
	}
	if sched == nil {
		return errors.New("email delivery: schedule is required")
	}
	channel := strings.ToUpper(strings.TrimSpace(sched.Channel))
	if channel != "" && channel != "EMAIL" {
		return fmt.Errorf("email delivery: channel %q is not supported", sched.Channel)
	}
	recipients := make([]string, 0, len(sched.Recipients))
	for _, r := range sched.Recipients {
		to := strings.TrimSpace(r)
		if to == "" {
			continue
		}
		if !strings.Contains(to, "@") {
			return fmt.Errorf("email delivery: invalid recipient %q", to)
		}
		recipients = append(recipients, to)
	}
	if len(recipients) == 0 {
		return errors.New("email delivery: schedule has no recipients")
	}
	if len(artifact.Data) == 0 {
		return errors.New("email delivery: report artifact is empty")
	}

	ext, contentType := "csv", "text/csv"
	if strings.EqualFold(strings.TrimSpace(artifact.Format), "JSON") {
		ext, contentType = "json", "application/json"
	}
	name := sanitizeReportFilename(artifact.ReportName)
	if name == "" {
		name = "report"
	}
	filename := fmt.Sprintf("%s-%s.%s", name, time.Now().UTC().Format("20060102-150405"), ext)
	subject := fmt.Sprintf("Scheduled report: %s", sched.Name)
	body := fmt.Sprintf("Report %q was generated at %s with %d rows.\nThe %s attachment contains the data.\n",
		artifact.ReportName, time.Now().UTC().Format(time.RFC3339), artifact.RowCount, strings.ToUpper(ext))
	attachment := notifications.Attachment{Filename: filename, ContentType: contentType, Data: artifact.Data}

	var errs []error
	for _, to := range recipients {
		if err := d.sender.SendWithAttachments(ctx, to, subject, body, "", []notifications.Attachment{attachment}); err != nil {
			errs = append(errs, fmt.Errorf("send to %s: %w", to, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("email delivery failed for %d/%d recipients: %w", len(errs), len(recipients), errors.Join(errs...))
	}
	d.logger.Info("scheduled report delivered by email",
		"tenant_id", tenantID, "schedule_id", sched.ID, "recipients", len(recipients), "filename", filename)
	return nil
}

// sanitizeReportFilename keeps letters, digits, dash and underscore; other
// characters become dashes so attachment names stay safe across mail clients.
func sanitizeReportFilename(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '\\':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
