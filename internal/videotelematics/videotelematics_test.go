package videotelematics_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/videotelematics"
)

func newTestVideoDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_video_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	// Seed tenants
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-vid-a', 'Video Telematics Tenant A', 'tenant-vid-a'),
		('tenant-vid-b', 'Video Telematics Tenant B', 'tenant-vid-b')`)
	require.NoError(t, err)

	// Seed drivers
	_, err = db.Exec(`INSERT OR IGNORE INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, license_number, license_expiry, status) VALUES
		('drv-vid-1', 'tenant-vid-a', 'DRV-V01', 'Rajesh', 'Sharma', '+919876543210', 'DL-01-2015-001', date('now','+2 years'), 'available'),
		('drv-vid-2', 'tenant-vid-a', 'DRV-V02', 'Sunil', 'Yadav', '+919876543211', 'DL-01-2015-002', date('now','+2 years'), 'available'),
		('drv-vid-b', 'tenant-vid-b', 'DRV-VB1', 'Ramesh', 'Kumar', '+919876543212', 'MH-01-2015-003', date('now','+2 years'), 'available')`)
	require.NoError(t, err)

	// Seed vehicles
	_, err = db.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		('veh-cam-1', 'tenant-vid-a', 'DL-01-CAM-1001', 'CAM-001', 'truck', 15.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 24000.0),
		('veh-cam-2', 'tenant-vid-a', 'DL-01-CAM-1002', 'CAM-002', 'truck', 12.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 18000.0),
		('veh-cam-b', 'tenant-vid-b', 'MH-02-CAM-9999', 'CAM-B01', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 5000.0)`)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestVideoTelematics_ProfileCRUD(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	// 1. Configure Dashcam Profile
	prof, err := svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-1", videotelematics.ConfigureDashcamProfileInput{
		CameraMakeModel:     "FleetX AI Pro Dual-200",
		CameraSerial:        "FX-CAM-99018",
		DualFacing:          true,
		ADASEnabled:         true,
		DMSEnabled:          true,
		LiveStreamSupported: true,
		FirmwareVersion:     "v3.1.2-edge",
		Status:              videotelematics.DashcamOnline,
	})
	require.NoError(t, err)
	require.NotNil(t, prof)
	assert.Equal(t, "veh-cam-1", prof.VehicleID)
	assert.Equal(t, "FX-CAM-99018", prof.CameraSerial)
	assert.True(t, prof.DualFacing)
	assert.True(t, prof.ADASEnabled)
	assert.True(t, prof.DMSEnabled)

	// 2. Fetch Profile
	fetched, err := svc.GetProfile(ctx, "tenant-vid-a", "veh-cam-1")
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, "FleetX AI Pro Dual-200", fetched.CameraMakeModel)

	// 3. Validation: serial required
	_, err = svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-1", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "",
	})
	assert.Error(t, err)
}

func TestVideoTelematics_NewCameraNotOnline(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	// Registering a camera that has no attested/connected device must not
	// advertise ONLINE (P3.12 honest connectivity, schema default deferred).
	prof, err := svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-1", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "CAM-NEW-01",
	})
	require.NoError(t, err)
	require.NotNil(t, prof)
	assert.NotEqual(t, videotelematics.DashcamOnline, prof.Status,
		"newly registered camera must not default to ONLINE")
	assert.Equal(t, videotelematics.DashcamStandby, prof.Status)
}

func TestVideoTelematics_SafetyEventIngestAndFilter(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	driverID := "drv-vid-1"
	lat := 28.6139
	lng := 77.2090

	// Ingest DMS drowsiness event
	event1, err := svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID:       "veh-cam-1",
		DriverID:        &driverID,
		Category:        videotelematics.CategoryDMS,
		EventType:       videotelematics.EventTypeDrowsiness,
		Severity:        videotelematics.SeverityCritical,
		SpeedKMH:        65.5,
		Latitude:        &lat,
		Longitude:       &lng,
		LocationName:    "NH-44 Highway Mile 142",
		DurationSeconds: 20,
	})
	require.NoError(t, err)
	require.NotNil(t, event1)
	assert.Equal(t, videotelematics.EventTypeDrowsiness, event1.EventType)
	assert.Equal(t, videotelematics.SeverityCritical, event1.Severity)
	assert.Equal(t, videotelematics.ReviewPending, event1.ReviewStatus)
	// No fabricated evidence: when the device sends no clip, none is stored (P3.12).
	assert.Empty(t, event1.MediaClipURL)
	assert.Empty(t, event1.ThumbnailURL)

	// Device-supplied evidence passes through untouched.
	uploadEvent, err := svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID:    "veh-cam-1",
		Category:     videotelematics.CategoryDMS,
		EventType:    videotelematics.EventTypeYawning,
		MediaClipURL: "https://media.internal.example/clips/dev-0001.mp4",
		ThumbnailURL: "https://media.internal.example/thumbs/dev-0001.jpg",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://media.internal.example/clips/dev-0001.mp4", uploadEvent.MediaClipURL)
	assert.Equal(t, "https://media.internal.example/thumbs/dev-0001.jpg", uploadEvent.ThumbnailURL)

	// Ingest ADAS tailgating event
	event2, err := svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID:       "veh-cam-1",
		DriverID:        &driverID,
		Category:        videotelematics.CategoryADAS,
		EventType:       videotelematics.EventTypeTailgating,
		Severity:        videotelematics.SeverityHigh,
		SpeedKMH:        82.0,
		DurationSeconds: 15,
	})
	require.NoError(t, err)
	require.NotNil(t, event2)

	// List events with filter
	events, total, err := svc.ListSafetyEvents(ctx, "tenant-vid-a", videotelematics.EventFilter{
		VehicleID: "veh-cam-1",
		Category:  videotelematics.CategoryDMS,
		EventType: videotelematics.EventTypeDrowsiness,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, events, 1)
	assert.Equal(t, videotelematics.EventTypeDrowsiness, events[0].EventType)
}

func TestVideoTelematics_ReviewAndCoachingWorkflow(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	driverID := "drv-vid-1"
	event, err := svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID: "veh-cam-1",
		DriverID:  &driverID,
		Category:  videotelematics.CategoryDMS,
		EventType: videotelematics.EventTypeCellphone,
		Severity:  videotelematics.SeverityHigh,
	})
	require.NoError(t, err)

	// Review with coaching note
	reviewed, err := svc.ReviewSafetyEvent(ctx, "tenant-vid-a", event.ID, videotelematics.ReviewSafetyEventInput{
		ReviewStatus: videotelematics.ReviewCoached,
		CoachingNote: "Discussed mandatory hands-free policy during safety debrief.",
		ReviewedBy:   "Safety Manager Kapoor",
	})
	require.NoError(t, err)
	require.NotNil(t, reviewed)
	assert.Equal(t, videotelematics.ReviewCoached, reviewed.ReviewStatus)
	assert.Equal(t, "Discussed mandatory hands-free policy during safety debrief.", reviewed.CoachingNote)
	assert.Equal(t, "Safety Manager Kapoor", reviewed.ReviewedBy)
	assert.NotNil(t, reviewed.ReviewedAt)

	// Validation: coaching note required for COACHED
	_, err = svc.ReviewSafetyEvent(ctx, "tenant-vid-a", event.ID, videotelematics.ReviewSafetyEventInput{
		ReviewStatus: videotelematics.ReviewCoached,
		CoachingNote: "   ",
	})
	assert.Error(t, err)
}

func TestVideoTelematics_LivestreamLifecycle(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	// Unconfigured media server must fail closed — no fabricated URLs
	// (P0 "stop fake data", P3.12), and no session row.
	session, err := svc.StartLivestream(ctx, "tenant-vid-a", videotelematics.StartLivestreamInput{
		VehicleID:      "veh-cam-1",
		Channel:        videotelematics.ChannelRoadFacing,
		StreamProtocol: videotelematics.ProtocolWebRTC,
		RequestedBy:    "Dispatcher Verma",
	})
	assert.Error(t, err)
	assert.Nil(t, session)

	var persisted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM video_livestream_sessions`).Scan(&persisted); err != nil {
		t.Fatalf("count livestream sessions: %v", err)
	}
	assert.Equal(t, 0, persisted)

	// Configured media server: URL built from the configured host and the
	// session stays INITIATED until a device/confirm call proves activity.
	svcCfg := videotelematics.NewService(repo, "https://media.internal.example:8443")
	session, err = svcCfg.StartLivestream(ctx, "tenant-vid-a", videotelematics.StartLivestreamInput{
		VehicleID:      "veh-cam-1",
		Channel:        videotelematics.ChannelRoadFacing,
		StreamProtocol: videotelematics.ProtocolWebRTC,
		RequestedBy:    "Dispatcher Verma",
	})
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Equal(t, videotelematics.StreamInitiated, session.Status)
	assert.Contains(t, session.StreamURL, "https://media.internal.example:8443/streams/")
	assert.NotContains(t, session.StreamURL, "fleetx.io")
	assert.NotEmpty(t, session.SessionToken)

	// Second start on the same vehicle ends the previous pending session (HLS path)
	session2, err := svcCfg.StartLivestream(ctx, "tenant-vid-a", videotelematics.StartLivestreamInput{
		VehicleID:      "veh-cam-1",
		Channel:        videotelematics.ChannelCabinFacing,
		StreamProtocol: videotelematics.ProtocolHLS,
		RequestedBy:    "Safety Lead",
	})
	require.NoError(t, err)
	require.NotNil(t, session2)
	assert.Equal(t, videotelematics.StreamInitiated, session2.Status)
	assert.Contains(t, session2.StreamURL, "https://media.internal.example:8443/hls/")

	oldSession, err := repo.GetLivestreamSession(ctx, "tenant-vid-a", session.ID)
	require.NoError(t, err)
	require.NotNil(t, oldSession)
	assert.Equal(t, videotelematics.StreamEnded, oldSession.Status)

	// End stream
	err = svcCfg.EndLivestream(ctx, "tenant-vid-a", session2.ID)
	require.NoError(t, err)

	endedSession, err := repo.GetLivestreamSession(ctx, "tenant-vid-a", session2.ID)
	require.NoError(t, err)
	assert.Equal(t, videotelematics.StreamEnded, endedSession.Status)
}

func TestVideoTelematics_ScorecardAndLeaderboard(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	driverID := "drv-vid-1"
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	// Ingest 1 critical and 2 medium events
	_, _ = svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID: "veh-cam-1",
		DriverID:  &driverID,
		Category:  videotelematics.CategoryDMS,
		EventType: videotelematics.EventTypeDrowsiness,
		Severity:  videotelematics.SeverityCritical,
		EventTime: now.Add(-2 * time.Hour),
	})
	_, _ = svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID: "veh-cam-1",
		DriverID:  &driverID,
		Category:  videotelematics.CategoryADAS,
		EventType: videotelematics.EventTypeLDW,
		Severity:  videotelematics.SeverityMedium,
		EventTime: now.Add(-1 * time.Hour),
	})

	card, err := svc.CalculateAndSaveDriverScorecard(ctx, "tenant-vid-a", driverID, start, now, 800.0)
	require.NoError(t, err)
	require.NotNil(t, card)
	assert.Equal(t, driverID, card.DriverID)
	assert.Equal(t, 800.0, card.DistanceKM)
	assert.True(t, card.SafetyScore > 0 && card.SafetyScore <= 100)
	assert.Equal(t, 1, card.CriticalEventCount)
	assert.Equal(t, 1, card.DMSEventCount)
	assert.Equal(t, 1, card.ADASEventCount)

	// Verify leaderboard
	cards, total, err := svc.ListDriverScorecards(ctx, "tenant-vid-a", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, cards, 1)
	assert.Equal(t, "Rajesh Sharma", *cards[0].DriverName)
}

func TestVideoTelematics_LivestreamConfirm(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	svcCfg := videotelematics.NewService(repo, "https://media.internal.example:8443")
	session, err := svcCfg.StartLivestream(ctx, "tenant-vid-a", videotelematics.StartLivestreamInput{
		VehicleID:      "veh-cam-1",
		Channel:        videotelematics.ChannelRoadFacing,
		StreamProtocol: videotelematics.ProtocolWebRTC,
		RequestedBy:    "Dispatcher Verma",
	})
	require.NoError(t, err)
	require.NotNil(t, session)

	// Fail-closed: the only path to ACTIVE is a real device/partner confirm,
	// and it is refused outright when no media server is configured (P0/P3.12).
	confirmed, err := svc.ConfirmLivestream(ctx, "tenant-vid-a", session.ID)
	require.Error(t, err, "confirm must fail closed without configured media server")
	assert.ErrorContains(t, err, "not configured")
	assert.Nil(t, confirmed)

	stored, err := repo.GetLivestreamSession(ctx, "tenant-vid-a", session.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, videotelematics.StreamInitiated, stored.Status, "no state change on refused confirm")

	// Configured media server: a real confirm flips INITIATED → ACTIVE.
	confirmed, err = svcCfg.ConfirmLivestream(ctx, "tenant-vid-a", session.ID)
	require.NoError(t, err)
	require.NotNil(t, confirmed)
	assert.Equal(t, videotelematics.StreamActive, confirmed.Status)

	// END still ends, and an ENDED session cannot be re-confirmed to ACTIVE.
	require.NoError(t, svcCfg.EndLivestream(ctx, "tenant-vid-a", session.ID))
	reconfirmed, err := svcCfg.ConfirmLivestream(ctx, "tenant-vid-a", session.ID)
	require.Error(t, err, "ENDED sessions must not be re-activated")
	assert.Nil(t, reconfirmed)
}

func TestVideoTelematics_ScorecardRefusesUnmeasuredDistance(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	driverID := "drv-vid-1"
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)

	// A driver with events but no reported mileage must NOT get a scorecard
	// normalized against an invented baseline (P0 "stop fake data").
	card, err := svc.CalculateAndSaveDriverScorecard(ctx, "tenant-vid-a", driverID, start, now, 0)
	require.Error(t, err, "refuse to score without real reported distance")
	require.ErrorIs(t, err, videotelematics.ErrInsufficientMileage)
	assert.Nil(t, card)

	var count int
	var distances float64
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(distance_km), 0) FROM driver_safety_scorecards`).Scan(&count, &distances))
	assert.Equal(t, 0, count, "no scorecard row may exist without measured mileage")
	assert.NotEqual(t, 1200.0, distances, "no fabricated 1200 km baseline may be stored")

	// A real distance keeps scoring honest: reuse of the driver's previously
	// reported distance while explicit mileage is absent is allowed (real data),
	// but a fresh driver with nothing reported is not scoreable.
	cardReal, err := svc.CalculateAndSaveDriverScorecard(ctx, "tenant-vid-a", driverID, start, now, 640.0)
	require.NoError(t, err)
	require.NotNil(t, cardReal)
	assert.Equal(t, 640.0, cardReal.DistanceKM)
}

func TestVideoTelematics_FleetMetrics(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	// Configure cameras
	_, _ = svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-1", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "CAM-001-A",
		Status:       videotelematics.DashcamOnline,
	})
	_, _ = svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-2", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "CAM-002-A",
		Status:       videotelematics.DashcamOffline,
	})

	// Add event
	_, _ = svc.IngestSafetyEvent(ctx, "tenant-vid-a", videotelematics.IngestSafetyEventInput{
		VehicleID: "veh-cam-1",
		Category:  videotelematics.CategoryDMS,
		EventType: videotelematics.EventTypeDrowsiness,
		Severity:  videotelematics.SeverityCritical,
	})

	metrics, err := svc.GetFleetMetrics(ctx, "tenant-vid-a")
	require.NoError(t, err)
	require.NotNil(t, metrics)
	assert.Equal(t, 2, metrics.TotalCameras)
	assert.Equal(t, 1, metrics.OnlineCameras)
	assert.Equal(t, 1, metrics.TodayEventsCount)
	assert.Equal(t, 1, metrics.CriticalEventsToday)
	assert.Equal(t, 1, metrics.PendingCoachingCount)
	// Honest fleet metrics: unmeasured fleet with no driver scorecards yields 0.0, never fabricated 100.0
	assert.Equal(t, 0.0, metrics.FleetAvgSafetyScore)

	// When a real driver scorecard is created with real mileage, fleet avg reflects the real score
	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	card, err := svc.CalculateAndSaveDriverScorecard(ctx, "tenant-vid-a", "drv-metrics-1", start, now, 500.0)
	require.NoError(t, err)
	require.NotNil(t, card)

	metricsWithScore, err := svc.GetFleetMetrics(ctx, "tenant-vid-a")
	require.NoError(t, err)
	assert.Equal(t, card.SafetyScore, metricsWithScore.FleetAvgSafetyScore)
}

func TestVideoTelematics_TenantIsolation(t *testing.T) {
	db := newTestVideoDB(t)
	repo := videotelematics.NewSQLRepository(db)
	svc := videotelematics.NewService(repo, "")
	ctx := context.Background()

	// Configure camera for Tenant A
	_, err := svc.ConfigureProfile(ctx, "tenant-vid-a", "veh-cam-1", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "CAM-A-01",
	})
	require.NoError(t, err)

	// Configure camera for Tenant B
	_, err = svc.ConfigureProfile(ctx, "tenant-vid-b", "veh-cam-b", videotelematics.ConfigureDashcamProfileInput{
		CameraSerial: "CAM-B-01",
	})
	require.NoError(t, err)

	// Tenant A should only see veh-cam-1
	listA, err := svc.ListDashcamVehicles(ctx, "tenant-vid-a")
	require.NoError(t, err)
	assert.Len(t, listA, 1)
	assert.Equal(t, "veh-cam-1", listA[0].VehicleID)

	// Tenant B should only see veh-cam-b
	listB, err := svc.ListDashcamVehicles(ctx, "tenant-vid-b")
	require.NoError(t, err)
	assert.Len(t, listB, 1)
	assert.Equal(t, "veh-cam-b", listB[0].VehicleID)
}
