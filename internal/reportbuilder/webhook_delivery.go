package reportbuilder

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebhookDelivery posts scheduled report artifacts to caller-supplied endpoint
// URLs (the schedule recipients). When a secret is configured every request is
// signed with HMAC-SHA256 so receivers can verify authenticity.
type WebhookDelivery struct {
	client *http.Client
	secret string
	logger *slog.Logger
}

// NewWebhookDelivery builds the webhook seam. A nil client uses a 10s timeout.
func NewWebhookDelivery(client *http.Client, secret string, logger *slog.Logger) *WebhookDelivery {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookDelivery{client: client, secret: secret, logger: logger}
}

// Configured reports whether webhook delivery can run (an HTTP client exists).
func (d *WebhookDelivery) Configured() bool {
	return d != nil && d.client != nil
}

// webhookPayload is the JSON body posted to every endpoint.
type webhookPayload struct {
	Event        string `json:"event"`
	TenantID     string `json:"tenant_id"`
	ScheduleID   string `json:"schedule_id"`
	ScheduleName string `json:"schedule_name"`
	ReportID     string `json:"report_id"`
	ReportName   string `json:"report_name"`
	ExecutionID  string `json:"execution_id"`
	Format       string `json:"format"`
	ContentType  string `json:"content_type"`
	RowCount     int    `json:"row_count"`
	GeneratedAt  string `json:"generated_at"`
	DataBase64   string `json:"data_base64"`
}

// Deliver posts the artifact to every recipient URL. Any non-2xx response,
// invalid URL or transport error fails the delivery (caller marks FAILED);
// partial failures are reported, never swallowed.
func (d *WebhookDelivery) Deliver(ctx context.Context, tenantID string, sched *ReportSchedule, artifact ReportArtifact) error {
	if d == nil || d.client == nil {
		return errors.New("webhook delivery: client not configured")
	}
	if sched == nil {
		return errors.New("webhook delivery: schedule is required")
	}
	channel := strings.ToUpper(strings.TrimSpace(sched.Channel))
	if channel != "" && channel != "WEBHOOK" {
		return fmt.Errorf("webhook delivery: channel %q is not supported", sched.Channel)
	}
	if len(artifact.Data) == 0 {
		return errors.New("webhook delivery: report artifact is empty")
	}
	targets := make([]string, 0, len(sched.Recipients))
	for _, raw := range sched.Recipients {
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}
		parsed, err := url.Parse(target)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("webhook delivery: invalid endpoint %q", target)
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return errors.New("webhook delivery: schedule has no endpoint URLs")
	}

	contentType := "text/csv"
	if strings.EqualFold(strings.TrimSpace(artifact.Format), "JSON") {
		contentType = "application/json"
	}
	payload := webhookPayload{
		Event:        "report.scheduled",
		TenantID:     tenantID,
		ScheduleID:   sched.ID,
		ScheduleName: sched.Name,
		ReportID:     artifact.ReportID,
		ReportName:   artifact.ReportName,
		ExecutionID:  artifact.ExecutionID,
		Format:       strings.ToUpper(artifact.Format),
		ContentType:  contentType,
		RowCount:     artifact.RowCount,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		DataBase64:   base64.StdEncoding.EncodeToString(artifact.Data),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook delivery: encode payload: %w", err)
	}
	signature := ""
	if d.secret != "" {
		mac := hmac.New(sha256.New, []byte(d.secret))
		mac.Write(body)
		signature = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	var errs []error
	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", target, err))
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "avandab-report-delivery/1.0")
		req.Header.Set("X-Report-Event", "report.scheduled")
		if signature != "" {
			req.Header.Set("X-Report-Signature", signature)
		}
		resp, err := d.client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", target, err))
			continue
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Errorf("%s: HTTP %d: %s", target, resp.StatusCode, strings.TrimSpace(string(snippet))))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("webhook delivery failed for %d/%d endpoints: %w", len(errs), len(targets), errors.Join(errs...))
	}
	d.logger.Info("scheduled report delivered by webhook",
		"tenant_id", tenantID, "schedule_id", sched.ID, "endpoints", len(targets))
	return nil
}

// ChannelRouter routes a schedule to the delivery implementation for its
// channel. It exposes per-channel readiness so the service can mark an
// unconfigured channel PENDING_DELIVERY instead of FAILED or fake SUCCESS.
type ChannelRouter struct {
	email   ScheduleDelivery
	webhook ScheduleDelivery
}

// NewChannelRouter wires the per-channel deliveries (nil = not configured).
func NewChannelRouter(email, webhook ScheduleDelivery) *ChannelRouter {
	return &ChannelRouter{email: email, webhook: webhook}
}

func (r *ChannelRouter) channelDelivery(channel string) ScheduleDelivery {
	switch strings.ToUpper(strings.TrimSpace(channel)) {
	case "", "EMAIL":
		return r.email
	case "WEBHOOK":
		return r.webhook
	default:
		return nil
	}
}

// Configured reports whether any channel can deliver.
func (r *ChannelRouter) Configured() bool {
	return r.ConfiguredFor("EMAIL") || r.ConfiguredFor("WEBHOOK")
}

// ConfiguredFor reports whether the delivery for this channel is ready.
func (r *ChannelRouter) ConfiguredFor(channel string) bool {
	d := r.channelDelivery(channel)
	return d != nil && d.Configured()
}

// Deliver routes the artifact to the channel-specific delivery.
func (r *ChannelRouter) Deliver(ctx context.Context, tenantID string, sched *ReportSchedule, artifact ReportArtifact) error {
	if sched == nil {
		return errors.New("channel router: schedule is required")
	}
	if !r.ConfiguredFor(sched.Channel) {
		return fmt.Errorf("channel router: channel %q is not configured", sched.Channel)
	}
	d := r.channelDelivery(sched.Channel)
	return d.Deliver(ctx, tenantID, sched, artifact)
}
