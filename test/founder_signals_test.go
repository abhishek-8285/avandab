package test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chi "github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/handlers"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func founderServices(t *testing.T) (*service.FounderSignalsService, *service.FounderAuditService) {
	t.Helper()
	svc := NewTestServices(t, NewTestDB(t))
	return svc.FounderSignals, svc.FounderAudit
}

func TestFounderSignal_Emit(t *testing.T) {
	sig, _ := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	id, err := sig.EmitSignal(ctx, service.FounderSignal{
		TenantID:    "1",
		SignalType:  service.SignalRevenueMilestone,
		SignalValue: 120000,
		Direction:   service.DirectionAbove,
		Metadata:    `{"monthly_revenue":120000.00}`,
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	signals, _, err := sig.ListSignals(ctx, "1", service.SignalFilters{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.False(t, signals[0].Acknowledged, "new signal must be unacknowledged")
	assert.Equal(t, service.DirectionAbove, signals[0].Direction)
}

func TestFounderSignal_EmitIfThreshold(t *testing.T) {
	sig, _ := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Above threshold → emitted.
	ok, err := sig.EmitIfThreshold(ctx, "1", service.SignalRevenueMilestone, 150000, 100000, `{}`)
	require.NoError(t, err)
	assert.True(t, ok)

	// Below threshold → emitted with direction below.
	ok, err = sig.EmitIfThreshold(ctx, "1", service.SignalCashFlowAlert, -500, 0, `{}`)
	require.NoError(t, err)
	assert.True(t, ok)

	// Exactly at threshold → no signal.
	ok, err = sig.EmitIfThreshold(ctx, "1", service.SignalRevenueMilestone, 100000, 100000, `{}`)
	require.NoError(t, err)
	assert.False(t, ok, "value exactly at threshold must not emit")

	// Duplicate within 1 hour → deduped (same direction "above").
	ok, err = sig.EmitIfThreshold(ctx, "1", service.SignalRevenueMilestone, 200000, 100000, `{}`)
	require.NoError(t, err)
	assert.False(t, ok, "duplicate within 1h must be deduplicated")
}

func TestFounderSignal_Acknowledge(t *testing.T) {
	sig, _ := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	id, err := sig.EmitSignal(ctx, service.FounderSignal{
		TenantID:    "1",
		SignalType:  service.SignalDriverChurnRisk,
		SignalValue: 5,
		Direction:   service.DirectionAbove,
	})
	require.NoError(t, err)

	require.NoError(t, sig.AcknowledgeSignal(ctx, id, "admin-1", "admin"))

	signals, _, err := sig.ListSignals(ctx, "1", service.SignalFilters{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.True(t, signals[0].Acknowledged)
	require.NotNil(t, signals[0].AcknowledgedBy)
	assert.Equal(t, "admin-1", *signals[0].AcknowledgedBy)

	// Second acknowledge must error.
	err = sig.AcknowledgeSignal(ctx, id, "admin-1", "admin")
	assert.Error(t, err)
}

func TestFounderSignal_ListFilters(t *testing.T) {
	sig, _ := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	_, _ = sig.EmitSignal(ctx, service.FounderSignal{TenantID: "1", SignalType: service.SignalRevenueMilestone, SignalValue: 1, Direction: service.DirectionAbove})
	_, _ = sig.EmitSignal(ctx, service.FounderSignal{TenantID: "1", SignalType: service.SignalCashFlowAlert, SignalValue: -1, Direction: service.DirectionBelow})

	// Filter by signal_type.
	rev, _, err := sig.ListSignals(ctx, "1", service.SignalFilters{SignalType: service.SignalRevenueMilestone})
	require.NoError(t, err)
	require.Len(t, rev, 1)
	assert.Equal(t, service.SignalRevenueMilestone, rev[0].SignalType)

	// Filter by unacknowledged_only (both still unacknowledged).
	unack, _, err := sig.ListSignals(ctx, "1", service.SignalFilters{UnacknowledgedOnly: true})
	require.NoError(t, err)
	assert.Len(t, unack, 2)

	// Acknowledge one and re-filter.
	require.NoError(t, sig.AcknowledgeSignal(ctx, rev[0].ID, "u", "admin"))
	unack, _, err = sig.ListSignals(ctx, "1", service.SignalFilters{UnacknowledgedOnly: true})
	require.NoError(t, err)
	assert.Len(t, unack, 1)
}

func TestFounderAudit_Record(t *testing.T) {
	sig, audit := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	require.NoError(t, audit.RecordAudit(ctx, service.AuditEntry{
		TenantID:     "1",
		ActorID:      "admin-9",
		ActorRole:    "admin",
		Action:       service.AuditActionExperimentStart,
		ResourceType: "experiment",
		ResourceID:   "exp-1",
		Details:      `{"name":"x"}`,
		IPAddress:    "10.0.0.1",
		UserAgent:    "test-agent",
	}))

	entries, _, err := audit.ListAudit(ctx, "1", service.AuditFilters{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, "admin-9", e.ActorID)
	assert.Equal(t, "experiment", e.ResourceType)
	assert.Equal(t, "exp-1", e.ResourceID)
	assert.Equal(t, `{"name":"x"}`, e.Details)
	assert.Equal(t, "10.0.0.1", e.IPAddress)
	assert.Equal(t, "test-agent", e.UserAgent)
	_ = sig
}

func TestFounderAudit_ListFilters(t *testing.T) {
	_, audit := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	_ = audit.RecordAudit(ctx, service.AuditEntry{TenantID: "1", ActorID: "u1", ActorRole: "admin", Action: service.AuditActionExperimentStart, ResourceType: "experiment", ResourceID: "e1", Details: "{}"})
	_ = audit.RecordAudit(ctx, service.AuditEntry{TenantID: "1", ActorID: "u2", ActorRole: "admin", Action: service.AuditActionSignalAcknowledge, ResourceType: "founder_signal", ResourceID: "s1", Details: "{}"})
	_ = audit.RecordAudit(ctx, service.AuditEntry{TenantID: "1", ActorID: "u3", ActorRole: "admin", Action: service.AuditActionExperimentStart, ResourceType: "experiment", ResourceID: "e2", Details: "{}"})

	byAction, _, err := audit.ListAudit(ctx, "1", service.AuditFilters{Action: service.AuditActionExperimentStart})
	require.NoError(t, err)
	assert.Len(t, byAction, 2)

	byResource, _, err := audit.ListAudit(ctx, "1", service.AuditFilters{ResourceType: "founder_signal"})
	require.NoError(t, err)
	assert.Len(t, byResource, 1)
}

func TestFounderSignal_HooksFire(t *testing.T) {
	sig, _ := founderServices(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Revenue milestone above threshold.
	ok, err := sig.EmitRevenueMilestone(ctx, "1", 250000, 100000)
	require.NoError(t, err)
	assert.True(t, ok)

	// Driver churn risk with 4 inactive drivers (>= 3).
	ok, err = sig.EmitDriverChurnRisk(ctx, "1", 4, 3)
	require.NoError(t, err)
	assert.True(t, ok)

	// Vehicle utilization below threshold.
	ok, err = sig.EmitVehicleUtilization(ctx, "1", 0.25, 0.5, 5, 20)
	require.NoError(t, err)
	assert.True(t, ok)

	// Settlement dispute spike with 4 disputes (strictly > 3 threshold).
	ok, err = sig.EmitSettlementDisputeSpike(ctx, "1", 4, 3)
	require.NoError(t, err)
	assert.True(t, ok)

	// Cash flow alert when net profit negative.
	ok, err = sig.EmitCashFlowAlert(ctx, "1", -1200.5, "2026-08-20")
	require.NoError(t, err)
	assert.True(t, ok)

	// Compliance score below threshold.
	ok, err = sig.EmitComplianceScore(ctx, "1", 72.5, 80)
	require.NoError(t, err)
	assert.True(t, ok)

	// Threshold exactly met (no crossing) → no signal.
	ok, err = sig.EmitRevenueMilestone(ctx, "1", 100000, 100000)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFounderDashboard_Aggregation(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t))
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	sig := svc.FounderSignals

	// Seed: 2 unacknowledged signals, 1 running experiment, 1 open ops alert, 1 PNL snapshot.
	_, _ = sig.EmitSignal(ctx, service.FounderSignal{TenantID: "1", SignalType: service.SignalRevenueMilestone, SignalValue: 1, Direction: service.DirectionAbove})
	_, _ = sig.EmitSignal(ctx, service.FounderSignal{TenantID: "1", SignalType: service.SignalCashFlowAlert, SignalValue: -1, Direction: service.DirectionBelow})

	expID, _ := svc.Experiments.CreateExperiment(ctx, service.Experiment{TenantID: "1", Name: "agg", TrafficSplit: 50})
	require.NoError(t, svc.Experiments.StartExperiment(ctx, expID))

	_, _ = svc.OpsAlerts.CreateAlert(ctx, service.OpsAlert{
		TenantID:  "1",
		AlertType: service.OpsAlertVehicleBreakdown,
		Severity:  service.OpsAlertSeverityMedium,
		Title:     "breakdown",
	})

	_, err := svc.PNL.GenerateDailySnapshot(ctx, "1", time.Now())
	require.NoError(t, err)

	unack, err := sig.CountUnacknowledged(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 2, unack)

	activeExp, err := svc.Experiments.CountByStatus(ctx, "1", service.ExperimentStatusRunning)
	require.NoError(t, err)
	assert.Equal(t, 1, activeExp)

	openAlerts, err := svc.OpsAlerts.CountByStatus(ctx, "1", service.OpsAlertStatusOpen)
	require.NoError(t, err)
	assert.Equal(t, 1, openAlerts)

	latest, err := svc.PNL.GetLatest(ctx, "1")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "1", latest.TenantID)
}

// TestFounder_RBAC_Allowed verifies admin access to founder endpoints (200).
func TestFounder_RBAC_Allowed(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)

	authAllow := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authAllow, Services: svcs}
	app.Founder = handlers.NewFounderHandlers(app, svcs.FounderSignals, svcs.FounderAudit, authAllow)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.Founder.RegisterRoutes(r)

	for _, path := range []string{
		"/api/v1/founder/signals",
		"/api/v1/founder/audit",
		"/api/v1/founder/dashboard",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "admin should access %s", path)
	}
}

// TestFounder_RBAC_Forbidden verifies non-granted users get 403 (Spec 16 §6 RBAC).
func TestFounder_RBAC_Forbidden(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)

	authDeny := &mockPhase6Auth{allowed: false}
	app := &handlers.App{DB: dbConn, AuthSrv: authDeny, Services: svcs}
	app.Founder = handlers.NewFounderHandlers(app, svcs.FounderSignals, svcs.FounderAudit, authDeny)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "guest-1", Role: "guest"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.Founder.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/api/v1/founder/signals", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Acknowledge requires founder:update.
	ackReq := httptest.NewRequest("POST", "/api/v1/founder/signals/xyz/acknowledge", bytes.NewReader([]byte(`{}`)))
	ackRec := httptest.NewRecorder()
	r.ServeHTTP(ackRec, ackReq)
	assert.Equal(t, http.StatusForbidden, ackRec.Code)
}
