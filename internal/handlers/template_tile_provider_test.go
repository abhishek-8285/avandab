package handlers

// Spec 04 §2: tile provider is config-driven; OSM-only unless Google tiles are
// explicitly configured. share_public.html and trip_playback.html previously
// hardcoded unofficial Google `mt1.google.com` tile URLs — a spec violation
// with zero test coverage (only tracking.html was guarded).

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
)

// renderTemplateViaApp builds a full App (which parses all templates) and
// executes the named template, mirroring how pages render in production.
func renderTemplateViaApp(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	dbName := fmt.Sprintf("test_tiles_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+dbName+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()
	_ = goose.SetDialect("sqlite")
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Up(db, "db/migrations"))

	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		CookieSecure: false,
		UploadDir:    t.TempDir(),
	}
	repo := repoSQLite.NewRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svcs := service.NewServices(repo, cfg, logger, events.NewInMemoryBus())
	app := NewApp(svcs, cfg, nil, db, nil, nil)

	var buf bytes.Buffer
	require.NoError(t, app.Templates.ExecuteTemplate(&buf, name, data))
	return buf.String()
}

func osmMapConfig() map[string]any {
	return map[string]any{
		"Provider":    "auto",
		"GoogleStyle": "m",
		"GL":          "IN",
		"OSMUrl":      "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
		"PollSec":     30,
	}
}

func TestSharePublicTemplate_AutoProvider_ServesOSMNotGoogleScrape(t *testing.T) {
	body := renderTemplateViaApp(t, "share_public.html", map[string]any{
		"Token":        "tok",
		"TripNumber":   "TRIP-001",
		"TripStatus":   "transit",
		"DataEndpoint": "/share/tok/data",
		"MapConfig":    osmMapConfig(),
	})
	assert.NotContains(t, body, "mt1.google.com",
		"auto provider must not emit Google tile scraping (Spec 04 §2)")
	assert.Contains(t, body, "tile.openstreetmap.org", "auto provider must serve the configured OSMUrl")
}

func TestSharePublicTemplate_ExplicitGoogleConfig_KeepsGoogleTiles(t *testing.T) {
	cfg := osmMapConfig()
	cfg["Provider"] = "google"
	body := renderTemplateViaApp(t, "share_public.html", map[string]any{
		"Token":        "tok",
		"TripNumber":   "TRIP-001",
		"TripStatus":   "transit",
		"DataEndpoint": "/share/tok/data",
		"MapConfig":    cfg,
	})
	assert.Contains(t, body, "mt1.google.com",
		"explicitly configured google provider keeps Google tiles")
}

func TestTripPlaybackTemplate_AutoProvider_ServesOSMNotGoogleScrape(t *testing.T) {
	trip := map[string]any{
		"ID":               "t1",
		"TripNumber":       "TRIP-001",
		"Status":           "completed",
		"RouteSource":      "Bengaluru",
		"RouteDestination": "Mysuru",
		"DepartureTime":    time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC),
		"ArrivalTime":      nil,
	}
	body := renderTemplateViaApp(t, "trip_playback.html", map[string]any{
		"Title":   "Trip Playback",
		"Version": "test",
		"Trip":    trip,
		"PlaybackConfig": map[string]any{
			"TripID":      "t1",
			"VehicleID":   "",
			"HistoryAPI":  "/api/v1/telemetry/history",
			"PlaybackAPI": "/api/v1/trips/t1/playback",
		},
		"MapConfig": osmMapConfig(),
	})
	assert.NotContains(t, body, "mt1.google.com",
		"auto provider must not emit Google tile scraping (Spec 04 §2)")
	assert.Contains(t, body, "tile.openstreetmap.org", "auto provider must serve the configured OSMUrl")
}
