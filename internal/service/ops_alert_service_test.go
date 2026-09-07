package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func openOpsAlertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_foreign_keys=off")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE IF NOT EXISTS ops_alerts (
		id              TEXT PRIMARY KEY,
		tenant_id       TEXT NOT NULL DEFAULT '1',
		alert_type      TEXT NOT NULL,
		severity        TEXT NOT NULL DEFAULT 'medium',
		title           TEXT NOT NULL,
		description     TEXT,
		entity_type     TEXT,
		entity_id       TEXT,
		status          TEXT NOT NULL DEFAULT 'open',
		acknowledged_by TEXT,
		acknowledged_at DATETIME,
		resolved_by     TEXT,
		resolved_at     DATETIME,
		resolution_note TEXT,
		created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestOpsAlert_CreateAndList(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	id, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:    string(shared.DefaultTenant),
		AlertType:   service.OpsAlertVehicleBreakdown,
		Severity:    service.OpsAlertSeverityHigh,
		Title:       "Vehicle broken down on highway",
		Description: "Engine overheat",
		EntityType:  service.StrPtr("vehicle"),
		EntityID:    service.StrPtr("v-123"),
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty alert id")
	}

	alerts, total, err := svc.ListAlerts(ctx, "1", service.OpsAlertFilters{})
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if total != 1 || len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d (total: %d)", len(alerts), total)
	}

	a := alerts[0]
	if a.ID != id {
		t.Errorf("expected id %s, got %s", id, a.ID)
	}
	if a.Status != service.OpsAlertStatusOpen {
		t.Errorf("expected status open, got %s", a.Status)
	}
	if a.AcknowledgedBy != nil {
		t.Errorf("expected nil acknowledged_by, got %v", a.AcknowledgedBy)
	}
	if a.EntityType == nil || *a.EntityType != "vehicle" {
		t.Errorf("expected entity_type vehicle, got %v", a.EntityType)
	}
	if a.EntityID == nil || *a.EntityID != "v-123" {
		t.Errorf("expected entity_id v-123, got %v", a.EntityID)
	}
}

func TestOpsAlert_Acknowledge(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	id, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertDriverAbsence,
		Severity:  service.OpsAlertSeverityMedium,
		Title:     "Driver absence reported",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	// 1. Acknowledge open alert
	err = svc.AcknowledgeAlert(ctx, id, "user-456")
	if err != nil {
		t.Fatalf("acknowledge alert: %v", err)
	}

	alert, err := svc.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if alert.Status != service.OpsAlertStatusAcknowledged {
		t.Errorf("expected status acknowledged, got %s", alert.Status)
	}
	if alert.AcknowledgedBy == nil || *alert.AcknowledgedBy != "user-456" {
		t.Errorf("expected acknowledged_by user-456, got %v", alert.AcknowledgedBy)
	}
	if alert.AcknowledgedAt == nil {
		t.Error("expected non-nil acknowledged_at")
	}

	// 2. Double-acknowledge should return ErrAlertNotFoundOrAlreadyAcknowledged
	err = svc.AcknowledgeAlert(ctx, id, "user-456")
	if !errors.Is(err, service.ErrAlertNotFoundOrAlreadyAcknowledged) {
		t.Fatalf("expected ErrAlertNotFoundOrAlreadyAcknowledged, got %v", err)
	}
}

func TestOpsAlert_Resolve(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	id, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertRouteDisruption,
		Severity:  service.OpsAlertSeverityMedium,
		Title:     "Route disrupted due to roadwork",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	// Resolve from open state
	note := "Alternative route assigned"
	err = svc.ResolveAlert(ctx, id, "dispatcher-1", note)
	if err != nil {
		t.Fatalf("resolve alert: %v", err)
	}

	alert, err := svc.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if alert.Status != service.OpsAlertStatusResolved {
		t.Errorf("expected status resolved, got %s", alert.Status)
	}
	if alert.ResolvedBy == nil || *alert.ResolvedBy != "dispatcher-1" {
		t.Errorf("expected resolved_by dispatcher-1, got %v", alert.ResolvedBy)
	}
	if alert.ResolutionNote == nil || *alert.ResolutionNote != note {
		t.Errorf("expected resolution note %q, got %v", note, alert.ResolutionNote)
	}
	if alert.ResolvedAt == nil {
		t.Error("expected non-nil resolved_at")
	}

	// Already resolved alert cannot be resolved or acknowledged again
	err = svc.ResolveAlert(ctx, id, "dispatcher-1", "another note")
	if !errors.Is(err, service.ErrAlertNotFoundOrAlreadyResolved) {
		t.Fatalf("expected ErrAlertNotFoundOrAlreadyResolved, got %v", err)
	}
}

func TestOpsAlert_Dismiss(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	id, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertPaymentDelay,
		Severity:  service.OpsAlertSeverityLow,
		Title:     "Payment delay warning",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	err = svc.DismissAlert(ctx, id, "finance-admin", "False alarm - payment arrived via NEFT")
	if err != nil {
		t.Fatalf("dismiss alert: %v", err)
	}

	alert, err := svc.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("get alert: %v", err)
	}
	if alert.Status != service.OpsAlertStatusDismissed {
		t.Errorf("expected status dismissed, got %s", alert.Status)
	}

	// Already dismissed alert cannot be dismissed again
	err = svc.DismissAlert(ctx, id, "finance-admin", "duplicate dismiss")
	if !errors.Is(err, service.ErrAlertNotFoundOrAlreadyDismissed) {
		t.Fatalf("expected ErrAlertNotFoundOrAlreadyDismissed, got %v", err)
	}
}

func TestOpsAlert_ListFilters(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	// Create diverse alerts
	id1, _ := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertVehicleBreakdown,
		Severity:  service.OpsAlertSeverityCritical,
		Title:     "Breakdown 1",
	})
	id2, _ := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertDriverAbsence,
		Severity:  service.OpsAlertSeverityLow,
		Title:     "Absence 1",
	})
	id3, _ := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertVehicleBreakdown,
		Severity:  service.OpsAlertSeverityMedium,
		Title:     "Breakdown 2",
	})

	// Acknowledge id2
	_ = svc.AcknowledgeAlert(ctx, id2, "u1")

	// Filter by status=open
	openAlerts, totalOpen, err := svc.ListAlerts(ctx, "1", service.OpsAlertFilters{Status: service.OpsAlertStatusOpen})
	if err != nil {
		t.Fatalf("filter by status: %v", err)
	}
	if totalOpen != 2 || len(openAlerts) != 2 {
		t.Errorf("expected 2 open alerts, got %d (total: %d)", len(openAlerts), totalOpen)
	}

	// Filter by type=vehicle_breakdown
	breakdownAlerts, totalBreakdown, err := svc.ListAlerts(ctx, "1", service.OpsAlertFilters{Type: service.OpsAlertVehicleBreakdown})
	if err != nil {
		t.Fatalf("filter by type: %v", err)
	}
	if totalBreakdown != 2 || len(breakdownAlerts) != 2 {
		t.Errorf("expected 2 breakdown alerts, got %d (total: %d)", len(breakdownAlerts), totalBreakdown)
	}

	// Filter by severity=critical
	criticalAlerts, totalCrit, err := svc.ListAlerts(ctx, "1", service.OpsAlertFilters{Severity: service.OpsAlertSeverityCritical})
	if err != nil {
		t.Fatalf("filter by severity: %v", err)
	}
	if totalCrit != 1 || len(criticalAlerts) != 1 || criticalAlerts[0].ID != id1 {
		t.Errorf("expected 1 critical alert (id %s), got %v", id1, criticalAlerts)
	}

	// Different tenant should see 0
	otherTenantAlerts, totalOther, err := svc.ListAlerts(ctx, "tenant-999", service.OpsAlertFilters{})
	if err != nil {
		t.Fatalf("other tenant list: %v", err)
	}
	if totalOther != 0 || len(otherTenantAlerts) != 0 {
		t.Errorf("expected 0 alerts for other tenant, got %d", totalOther)
	}
	_ = id3
}

func TestOpsAlert_CriticalPublishesEvent(t *testing.T) {
	db := openOpsAlertTestDB(t)
	bus := events.NewInMemoryBus()
	svc := service.NewOpsAlertServiceForTest(db, bus)
	ctx := context.Background()

	var receivedEvent *events.Event
	bus.Subscribe("ops.alert.critical", func(ctx context.Context, e events.Event) error {
		receivedEvent = &e
		return nil
	})

	id, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:    string(shared.DefaultTenant),
		AlertType:   service.OpsAlertComplianceBreach,
		Severity:    service.OpsAlertSeverityCritical,
		Title:       "Dispatch blocked: Expired RC",
		Description: "Vehicle RC expired on 2025-01-01",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	// Give async/sync event handler time
	time.Sleep(10 * time.Millisecond)

	if receivedEvent == nil {
		t.Fatal("expected ops.alert.critical event to be published, got nil")
	}
	payload, ok := receivedEvent.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map payload, got %T", receivedEvent.Payload)
	}
	if payload["alert_id"] != id {
		t.Errorf("expected alert_id %s, got %v", id, payload["alert_id"])
	}
	if payload["alert_type"] != service.OpsAlertComplianceBreach {
		t.Errorf("expected alert_type %s, got %v", service.OpsAlertComplianceBreach, payload["alert_type"])
	}
}

func TestOpsAlert_NonCriticalNoEvent(t *testing.T) {
	db := openOpsAlertTestDB(t)
	bus := events.NewInMemoryBus()
	svc := service.NewOpsAlertServiceForTest(db, bus)
	ctx := context.Background()

	eventFired := false
	bus.Subscribe("ops.alert.critical", func(ctx context.Context, e events.Event) error {
		eventFired = true
		return nil
	})

	// Medium severity alert should NOT publish critical event
	_, err := svc.CreateAlert(ctx, service.OpsAlert{
		TenantID:  string(shared.DefaultTenant),
		AlertType: service.OpsAlertPaymentDelay,
		Severity:  service.OpsAlertSeverityMedium,
		Title:     "Invoice delayed by 3 days",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if eventFired {
		t.Error("event should NOT have been published for medium severity alert")
	}
}

func TestOpsAlert_GetNotFound(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	_, err := svc.GetAlert(ctx, "non-existent-id")
	if !errors.Is(err, service.ErrAlertNotFound) {
		t.Fatalf("expected ErrAlertNotFound, got %v", err)
	}
}

func TestOpsAlert_CountsRequireTenant(t *testing.T) {
	db := openOpsAlertTestDB(t)
	svc := service.NewOpsAlertServiceForTest(db, nil)
	ctx := context.Background()

	if _, err := svc.CountAlertsSince(ctx, "", service.OpsAlertSettlementDispute, time.Hour); err == nil {
		t.Error("expected error for empty tenant in CountAlertsSince, got nil")
	}
	if _, err := svc.CountByStatus(ctx, "", service.OpsAlertStatusOpen); err == nil {
		t.Error("expected error for empty tenant in CountByStatus, got nil")
	}
}
