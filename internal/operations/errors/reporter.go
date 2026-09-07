package errors

import (
	"context"
	"fmt"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
)

type ErrorReport struct {
	ID          string                 `json:"id"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	RequestID   string                 `json:"request_id"`
	UserID      string                 `json:"user_id"`
	TenantID    string                 `json:"tenant_id"`
	URL         string                 `json:"url"`
	Method      string                 `json:"method"`
	StatusCode  int                    `json:"status_code"`
	StackTrace  string                 `json:"stack_trace"`
	Message     string                 `json:"message"`
	Severity    Severity               `json:"severity"`
	Environment string                 `json:"environment"`
	AppVersion  string                 `json:"app_version"`
	UserAgent   string                 `json:"user_agent"`
	IPAddress   string                 `json:"ip_address"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Occurrences int                    `json:"occurrences,omitempty"`
	FirstSeen   time.Time              `json:"first_seen,omitempty"`
}

type Incident struct {
	ID         string     `json:"id"`
	ErrorID    string     `json:"error_id"`
	TenantID   string     `json:"tenant_id"`
	Status     string     `json:"status"` // OPEN, ASSIGNED, RESOLVED
	Severity   Severity   `json:"severity"`
	Created    time.Time  `json:"created"`
	AssignedTo string     `json:"assigned_to"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	RootCause  string     `json:"root_cause,omitempty"`
}

type Reporter struct {
	store       Store
	notifSvc    ports.NotificationService
	environment string
	appVersion  string
}

func NewReporter(notifSvc ports.NotificationService, store Store, env, appVersion string) *Reporter {
	return &Reporter{
		store:       store,
		notifSvc:    notifSvc,
		environment: env,
		appVersion:  appVersion,
	}
}

func (r *Reporter) Report(ctx context.Context, report ErrorReport) (ErrorReport, error) {
	if report.ID == "" {
		report.ID = fmt.Sprintf("err_%d", time.Now().UnixNano())
	}
	if report.Timestamp.IsZero() {
		report.Timestamp = time.Now()
	}
	if report.Environment == "" {
		report.Environment = r.environment
	}
	if report.AppVersion == "" {
		report.AppVersion = r.appVersion
	}
	if report.Severity == "" {
		report.Severity = SeverityMedium
	}
	if report.TenantID == "" {
		report.TenantID = string(shared.TenantIDFromContext(ctx))
	}
	if report.TenantID == "" {
		report.TenantID = string(shared.DefaultTenant) //nolint:tenant-default // ops triage bucket for tenant-less system errors; ctx-derived above when present
	}
	if r.store == nil {
		return report, nil
	}

	fp := Fingerprint(report.Method, report.URL, report.Message, report.TenantID)
	merged, err := r.store.UpsertError(ctx, report, fp)
	if err != nil {
		return report, err
	}
	merged.RequestID = report.RequestID
	merged.UserAgent = report.UserAgent
	merged.IPAddress = report.IPAddress
	merged.Metadata = report.Metadata
	merged.StackTrace = report.StackTrace

	if merged.Severity == SeverityCritical || merged.Severity == SeverityHigh {
		open, err := r.store.HasOpenIncident(ctx, fp, merged.TenantID)
		if err == nil && !open {
			_ = r.store.CreateIncident(ctx, Incident{
				ID:       fmt.Sprintf("inc_%d", time.Now().UnixNano()),
				ErrorID:  merged.ID,
				TenantID: merged.TenantID,
				Status:   "OPEN",
				Severity: merged.Severity,
				Created:  time.Now(),
			})
		}
	}

	if merged.Severity == SeverityCritical && r.notifSvc != nil {
		_ = r.notifSvc.SendEmail(ctx, ports.NotificationMessage{
			Recipient: "tech-alerts@flyfleet.io",
			Subject:   fmt.Sprintf("[CRITICAL ALERT] %s - %s", report.Method, report.URL),
			Body: fmt.Sprintf("Error ID: %s\nMessage: %s\nStack Trace:\n%s",
				merged.ID, merged.Message, report.StackTrace),
			Type: ports.NotificationTypeEmail,
		})
	}

	return merged, nil
}

func (r *Reporter) GetError(ctx context.Context, fingerprint, tenantID string) (ErrorReport, error) {
	if r.store == nil {
		return ErrorReport{}, fmt.Errorf("errors: store not configured")
	}
	return r.store.GetError(ctx, fingerprint, tenantID)
}

func (r *Reporter) ListErrors(ctx context.Context, f ErrorFilter) ([]ErrorReport, error) {
	if r.store == nil {
		return []ErrorReport{}, nil
	}
	return r.store.ListErrors(ctx, f)
}

func (r *Reporter) CountErrors(ctx context.Context, f ErrorFilter) (int, error) {
	if r.store == nil {
		return 0, nil
	}
	return r.store.CountErrors(ctx, f)
}

func (r *Reporter) ListIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	if r.store == nil {
		return []Incident{}, nil
	}
	return r.store.ListIncidents(ctx, f)
}

func (r *Reporter) ResolveIncident(ctx context.Context, id, tenantID, status, assignedTo, rootCause string) error {
	if r.store == nil {
		return nil
	}
	return r.store.ResolveIncident(ctx, id, tenantID, status, assignedTo, rootCause)
}
