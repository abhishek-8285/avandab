package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/alerts/domain"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
)

// allowAllAuthSvc permits all permissions in tests.
type allowAllAuthSvc struct{}

func (allowAllAuthSvc) Can(userID, resource, action string) bool { return true }
func (allowAllAuthSvc) Reload() error                            { return nil }
func (allowAllAuthSvc) AddRoleForUser(userID, role string) error { return nil }
func (allowAllAuthSvc) DeleteRolesForUser(userID string) error   { return nil }

func newAlertsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_alerts_handlers_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newAlertsTestApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	authSrv := allowAllAuthSvc{}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)

	repo := alertsqlite.NewAlertRepository(db)
	app := &App{
		DB:         db,
		Templates:  tmpl,
		AuthSrv:    authSrv,
		AlertsRepo: repo,
	}
	app.Alerts = NewAlertHandlers(app, repo)
	return app
}

func TestAlertHandlers_ListAndBadge(t *testing.T) {
	db := newAlertsTestDB(t)
	app := newAlertsTestApp(t, db)

	// Seed 2 alerts (1 open, 1 resolved)
	alert1 := &domain.Alert{
		ID:          "alert-test-1",
		TenantID:    "1",
		Source:      "telemetry",
		AlertType:   "speeding",
		Severity:    "warning",
		Status:      "open",
		DedupKey:    "telemetry:speeding:v-1",
		Title:       "Speeding 95 km/h",
		Message:     "Vehicle v-1 exceeded 80 km/h",
		Occurrences: 1,
		FirstSeenAt: time.Now(),
		LastSeenAt:  time.Now(),
	}
	alert2 := &domain.Alert{
		ID:          "alert-test-2",
		TenantID:    "1",
		Source:      "fuel",
		AlertType:   "theft_suspicion",
		Severity:    "critical",
		Status:      "resolved",
		DedupKey:    "fuel:theft_suspicion:v-2",
		Title:       "Fuel Theft Suspicion",
		Message:     "Fuel dropped 25L with ignition OFF",
		Occurrences: 2,
		FirstSeenAt: time.Now(),
		LastSeenAt:  time.Now(),
	}
	require.NoError(t, app.AlertsRepo.CreateAlert(t.Context(), alert1))
	require.NoError(t, app.AlertsRepo.CreateAlert(t.Context(), alert2))

	r := chi.NewRouter()
	r.Route("/alerts", app.Alerts.Routes)

	// 1. GET /alerts (default open)
	req := withSession(httptest.NewRequest(http.MethodGet, "/alerts", nil), "user-admin", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Operational Alerts")
	assert.Contains(t, w.Body.String(), "Speeding 95 km/h")

	// 2. GET /alerts/unread (badge fragment)
	reqUnread := withSession(httptest.NewRequest(http.MethodGet, "/alerts/unread", nil), "user-admin", "admin")
	wUnread := httptest.NewRecorder()
	r.ServeHTTP(wUnread, reqUnread)

	assert.Equal(t, http.StatusOK, wUnread.Code)
	assert.Contains(t, wUnread.Body.String(), "id=\"notif-badge\"")
	assert.Contains(t, wUnread.Body.String(), ">1<") // 1 open alert

	// 3. POST /alerts/{id}/ack
	reqAck := withSession(httptest.NewRequest(http.MethodPost, "/alerts/alert-test-1/ack", nil), "user-admin", "admin")
	wAck := httptest.NewRecorder()
	r.ServeHTTP(wAck, reqAck)
	assert.Equal(t, http.StatusSeeOther, wAck.Code)

	// Verify status updated to acknowledged
	a1, err := app.AlertsRepo.FindOpenByDedupKey(t.Context(), "telemetry:speeding:v-1")
	require.NoError(t, err)
	require.NotNil(t, a1)
	assert.Equal(t, "acknowledged", a1.Status)

	// 4. POST /alerts/{id}/resolve
	reqRes := withSession(httptest.NewRequest(http.MethodPost, "/alerts/alert-test-1/resolve", nil), "user-admin", "admin")
	wRes := httptest.NewRecorder()
	r.ServeHTTP(wRes, reqRes)
	assert.Equal(t, http.StatusSeeOther, wRes.Code)

	// Verify status updated to resolved
	a1Resolved, err := app.AlertsRepo.FindOpenByDedupKey(t.Context(), "telemetry:speeding:v-1")
	require.NoError(t, err)
	assert.Nil(t, a1Resolved, "resolved alert must no longer match FindOpenByDedupKey")

	// 5. Unread count after resolution is 0
	count, err := app.AlertsRepo.UnreadCount(t.Context(), "user-admin")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
