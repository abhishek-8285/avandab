package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/videotelematics"
)

func setupVideoTelematicsTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"admin-user:video:read":   true,
			"admin-user:video:write":  true,
			"viewer-user:video:read":  true,
			"viewer-user:video:write": false,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}

	app.Video = NewVideoTelematicsHandlers(app)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	app.Video.RegisterAPIRoutes(r)
	r.Route("/video", app.Video.Routes)
	return app, r
}

func TestVideoTelematicsAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupVideoTelematicsTestApp(t)

	tenantID := "tenant-vid-test"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'Video Tenant', 'video-tenant')`, tenantID)
	require.NoError(t, err)

	vehicleID := "veh-cam-test-1"
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, current_mileage)
		VALUES ($1, $2, 'DL-01-VID-1001', 'CAM-8888', 'truck', 10.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 15000.0)`,
		vehicleID, tenantID)
	require.NoError(t, err)

	driverID := "drv-vid-test-1"
	_, err = app.DB.Exec(`INSERT OR IGNORE INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ($1, $2, 'DRV-VT1', 'Vikram', 'Singh', '+919988776655', 'DL-VID-101', date('now','+2 years'), 'available')`,
		driverID, tenantID)
	require.NoError(t, err)

	// 1. RBAC check: viewer cannot configure camera profile (write permission required)
	body, _ := json.Marshal(videotelematics.ConfigureDashcamProfileInput{
		CameraMakeModel: "FleetX Pro 200",
		CameraSerial:    "FX-CAM-0012",
		DualFacing:      true,
		ADASEnabled:     true,
		DMSEnabled:      true,
	})
	req := httptest.NewRequest("POST", "/api/v1/video/cameras/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// 2. Admin successfully configures camera profile
	req = httptest.NewRequest("POST", "/api/v1/video/cameras/"+vehicleID+"/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var prof videotelematics.DashcamProfile
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prof))
	assert.Equal(t, vehicleID, prof.VehicleID)
	assert.Equal(t, "FX-CAM-0012", prof.CameraSerial)
	assert.True(t, prof.DualFacing)

	// 3. GET camera profile
	req = httptest.NewRequest("GET", "/api/v1/video/cameras/"+vehicleID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. GET list cameras
	req = httptest.NewRequest("GET", "/api/v1/video/cameras", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var listResp struct {
		Cameras []videotelematics.VideoVehicleStatus `json:"cameras"`
		Total   int                                  `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listResp))
	assert.Equal(t, 1, listResp.Total)
	assert.Equal(t, "CAM-8888", listResp.Cameras[0].VehicleNumber)

	// 5. Ingest AI Safety Event (DMS drowsiness)
	eventBody, _ := json.Marshal(videotelematics.IngestSafetyEventInput{
		VehicleID:       vehicleID,
		DriverID:        &driverID,
		Category:        videotelematics.CategoryDMS,
		EventType:       videotelematics.EventTypeDrowsiness,
		Severity:        videotelematics.SeverityCritical,
		SpeedKMH:        62.4,
		DurationSeconds: 15,
	})
	req = httptest.NewRequest("POST", "/api/v1/video/events", bytes.NewReader(eventBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var createdEvent videotelematics.VideoSafetyEvent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createdEvent))
	assert.NotEmpty(t, createdEvent.ID)
	assert.Equal(t, videotelematics.EventTypeDrowsiness, createdEvent.EventType)
	assert.Equal(t, videotelematics.ReviewPending, createdEvent.ReviewStatus)

	// 6. List events with filter
	req = httptest.NewRequest("GET", "/api/v1/video/events?category=DMS", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var eventsResp struct {
		Events []videotelematics.VideoSafetyEvent `json:"events"`
		Total  int                                `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &eventsResp))
	assert.Equal(t, 1, eventsResp.Total)
	assert.Equal(t, createdEvent.ID, eventsResp.Events[0].ID)

	// 7. Get single event
	req = httptest.NewRequest("GET", "/api/v1/video/events/"+createdEvent.ID, nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 8. Review event with coaching note
	reviewBody, _ := json.Marshal(videotelematics.ReviewSafetyEventInput{
		ReviewStatus: videotelematics.ReviewCoached,
		CoachingNote: "Mandatory 20-min fatigue break scheduled.",
		ReviewedBy:   "Admin Officer",
	})
	req = httptest.NewRequest("POST", "/api/v1/video/events/"+createdEvent.ID+"/review", bytes.NewReader(reviewBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var reviewedEvent videotelematics.VideoSafetyEvent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reviewedEvent))
	assert.Equal(t, videotelematics.ReviewCoached, reviewedEvent.ReviewStatus)
	assert.Equal(t, "Mandatory 20-min fatigue break scheduled.", reviewedEvent.CoachingNote)

	// 9. Start Livestream
	streamBody, _ := json.Marshal(videotelematics.StartLivestreamInput{
		VehicleID:      vehicleID,
		Channel:        videotelematics.ChannelRoadFacing,
		StreamProtocol: videotelematics.ProtocolWebRTC,
	})
	req = httptest.NewRequest("POST", "/api/v1/video/livestream/start", bytes.NewReader(streamBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var streamSess videotelematics.LivestreamSession
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &streamSess))
	assert.NotEmpty(t, streamSess.ID)
	assert.Equal(t, videotelematics.StreamActive, streamSess.Status)

	// 10. End Livestream
	req = httptest.NewRequest("POST", "/api/v1/video/livestream/"+streamSess.ID+"/stop", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 11. Driver Scorecard
	req = httptest.NewRequest("GET", "/api/v1/video/drivers/"+driverID+"/scorecard", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var card videotelematics.DriverSafetyScorecard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	assert.Equal(t, driverID, card.DriverID)
	assert.True(t, card.SafetyScore > 0)

	// 12. Scorecards leaderboard
	req = httptest.NewRequest("GET", "/api/v1/video/scorecards", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 13. Fleet Metrics
	req = httptest.NewRequest("GET", "/api/v1/video/metrics", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var metrics videotelematics.VideoFleetMetrics
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metrics))
	assert.Equal(t, 1, metrics.TotalCameras)
	assert.Equal(t, 1, metrics.TodayEventsCount)

	// 14. Web Video Dashboard HTML render
	req = httptest.NewRequest("GET", "/video", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	htmlBody := rec.Body.String()
	assert.Contains(t, htmlBody, "Video Telematics &amp; AI Safety")
	assert.Contains(t, htmlBody, "CAM-8888")

	// 15. Web Form Profile Configure POST
	form := url.Values{}
	form.Set("vehicle_id", vehicleID)
	form.Set("camera_serial", "FX-CAM-WEB-999")
	form.Set("camera_make_model", "FleetX Pro 200 Web")
	form.Set("dual_facing", "1")
	form.Set("adas_enabled", "1")
	form.Set("dms_enabled", "1")
	form.Set("live_stream_supported", "1")

	req = httptest.NewRequest("POST", "/video/cameras", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/video", rec.Header().Get("Location"))

	// 16. Multi-Tenant Isolation: Another tenant querying cameras gets 0
	req = httptest.NewRequest("GET", "/api/v1/video/cameras", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", "tenant-other")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var otherResp struct {
		Cameras []videotelematics.VideoVehicleStatus `json:"cameras"`
		Total   int                                  `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &otherResp))
	assert.Equal(t, 0, otherResp.Total)
}
