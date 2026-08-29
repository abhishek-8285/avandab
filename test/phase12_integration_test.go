package test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestPhase12_FullIntegration(t *testing.T) {
	db := NewTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	tenantID := "1"

	// 1. Generate PNL snapshot.
	snapshot, err := svc.PNL.GenerateDailySnapshot(ctx, tenantID, time.Now().AddDate(0, 0, -1))
	require.NoError(t, err)
	assert.NotEmpty(t, snapshot.ID)

	// 2. Create + acknowledge a founder signal.
	signalID, err := svc.FounderSignals.EmitSignal(ctx, service.FounderSignal{
		TenantID:    tenantID,
		SignalType:  service.SignalRevenueMilestone,
		SignalValue: 150000,
		Direction:   service.DirectionAbove,
		Metadata:    `{"test":true}`,
	})
	require.NoError(t, err)
	require.NoError(t, svc.FounderSignals.AcknowledgeSignal(ctx, signalID, "admin-user", "admin"))

	// 3. Create + lifecycle an experiment.
	expID, err := svc.Experiments.CreateExperiment(ctx, service.Experiment{
		TenantID:     tenantID,
		Name:         "integration_test_exp",
		VariantA:     "control",
		VariantB:     "treatment",
		TrafficSplit: 50,
		CreatedBy:    "admin-user",
	})
	require.NoError(t, err)
	require.NoError(t, svc.Experiments.StartExperiment(ctx, expID))

	// 4. Deterministic variant assignment.
	variant, err := svc.Experiments.AssignVariant(ctx, tenantID, expID, "user", "user-123")
	require.NoError(t, err)
	assert.Contains(t, []string{"a", "b"}, variant)

	// 5. Feature flag evaluation is consistent for the same subject.
	flagVariant := svc.Experiments.EvaluateFeatureFlag(ctx, tenantID, "integration_test_exp", "user", "user-123")
	assert.Equal(t, variant, flagVariant)

	// 6. Create + acknowledge + resolve an ops alert.
	alertID, err := svc.OpsAlerts.CreateAlert(ctx, service.OpsAlert{
		TenantID:  tenantID,
		AlertType: service.OpsAlertSettlementDispute,
		Severity:  service.OpsAlertSeverityHigh,
		Title:     "Integration test alert",
	})
	require.NoError(t, err)
	require.NoError(t, svc.OpsAlerts.AcknowledgeAlert(ctx, alertID, "admin-user"))
	require.NoError(t, svc.OpsAlerts.ResolveAlert(ctx, alertID, "admin-user", "resolved in test"))

	// 7. Record + verify audit trail.
	require.NoError(t, svc.FounderAudit.RecordAudit(ctx, service.AuditEntry{
		TenantID:     tenantID,
		ActorID:      "admin-user",
		ActorRole:    "admin",
		Action:       service.AuditActionFounderDashboard,
		ResourceType: "dashboard",
		ResourceID:   "main",
		Details:      `{}`,
	}))
	entries, total, err := svc.FounderAudit.ListAudit(ctx, tenantID, service.AuditFilters{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.GreaterOrEqual(t, len(entries), 1)
}

func TestPhase12_FounderDashboard_HTTP(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	authAllow := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authAllow, Services: svcs}
	app.Founder = handlers.NewFounderHandlers(app, svcs.FounderSignals, svcs.FounderAudit, authAllow)
	app.Founder.RegisterRoutes(chi.NewRouter())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.Founder.RegisterRoutes(r)

	// Dashboard JSON endpoint.
	req := httptest.NewRequest("GET", "/api/v1/founder/dashboard", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestPhase12_OpsAlerts_HTTP(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	authAllow := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authAllow, Services: svcs}
	app.OpsAlerts = handlers.NewOpsAlertHandlers(app, svcs.OpsAlerts, authAllow)
	app.Founder = handlers.NewFounderHandlers(app, svcs.FounderSignals, svcs.FounderAudit, authAllow)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.OpsAlerts.RegisterRoutes(r)

	id, err := svcs.OpsAlerts.CreateAlert(shared.ContextWithTenantID(context.Background(), "1"), service.OpsAlert{
		TenantID: "1", AlertType: service.OpsAlertVehicleBreakdown, Severity: service.OpsAlertSeverityMedium, Title: "brk",
	})
	require.NoError(t, err)

	ackReq := httptest.NewRequest("POST", "/api/v1/ops-alerts/"+id+"/acknowledge", nil)
	ackRec := httptest.NewRecorder()
	r.ServeHTTP(ackRec, ackReq)
	assert.Equal(t, http.StatusOK, ackRec.Code)

	body, _ := json.Marshal(map[string]string{"note": "fixed"})
	resReq := httptest.NewRequest("POST", "/api/v1/ops-alerts/"+id+"/resolve", bytes.NewReader(body))
	resRec := httptest.NewRecorder()
	r.ServeHTTP(resRec, resReq)
	assert.Equal(t, http.StatusOK, resRec.Code)
}

func TestPhase12_PNL_HTTP(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	authAllow := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authAllow, Services: svcs}
	app.PNL = handlers.NewPNLHandlers(app, svcs.PNL, authAllow)
	app.PNL.RegisterRoutes(chi.NewRouter())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.PNL.RegisterRoutes(r)

	// Generate endpoint.
	genBody, _ := json.Marshal(map[string]string{"date": time.Now().AddDate(0, 0, -1).Format("2006-01-02")})
	genReq := httptest.NewRequest("POST", "/api/v1/pnl/generate", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genRec := httptest.NewRecorder()
	r.ServeHTTP(genRec, genReq)
	assert.Equal(t, http.StatusCreated, genRec.Code)
}

func TestPhase12_Experiments_HTTP(t *testing.T) {
	dbConn, svcs, _, _, _ := setupComplianceTestEnv(t)
	authAllow := &mockPhase6Auth{allowed: true}
	app := &handlers.App{DB: dbConn, AuthSrv: authAllow, Services: svcs}
	app.ABExperiments = handlers.NewExperimentHandlers(app, svcs.Experiments, authAllow)
	app.ABExperiments.RegisterRoutes(chi.NewRouter())

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})
			ctx = shared.ContextWithTenantID(ctx, "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	app.ABExperiments.RegisterRoutes(r)

	// Create experiment.
	createBody, _ := json.Marshal(map[string]interface{}{"name": "http_exp", "traffic_split": 50})
	createReq := httptest.NewRequest("POST", "/api/v1/experiments", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	var createResp map[string]interface{}
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &createResp))
	expID, _ := createResp["experiment_id"].(string)
	require.NotEmpty(t, expID)

	// Start.
	startReq := httptest.NewRequest("POST", "/api/v1/experiments/"+expID+"/start", nil)
	startRec := httptest.NewRecorder()
	r.ServeHTTP(startRec, startReq)
	assert.Equal(t, http.StatusOK, startRec.Code)

	// Evaluate feature flag.
	evalReq := httptest.NewRequest("GET", "/api/v1/experiments/evaluate?name=http_exp&subject_type=user&subject_id=u9", nil)
	evalRec := httptest.NewRecorder()
	r.ServeHTTP(evalRec, evalReq)
	assert.Equal(t, http.StatusOK, evalRec.Code)
}
