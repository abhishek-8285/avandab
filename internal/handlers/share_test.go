package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/eta"
	"transport-app/internal/middleware"
)

type allowAuthSvc struct{}

func (allowAuthSvc) Can(userID, resource, action string) bool { return true }
func (allowAuthSvc) Reload() error                            { return nil }
func (allowAuthSvc) AddRoleForUser(userID, role string) error { return nil }
func (allowAuthSvc) DeleteRolesForUser(userID string) error   { return nil }

func newShareTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_share_%d", time.Now().UnixNano())
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

func newShareTestApp(t *testing.T, db *sql.DB, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)

	cfg := &config.Config{
		CookieSecret: "test-cookie-secret-32-chars-long!",
		LiveMap: config.LiveMapConfig{
			ShareLinkTTLHours:    24,
			ShareLinkMaxTTLHours: 168,
			ShareLinkMaxActive:   2, // Low cap for test assertions
			TelemetryStaleMin:    15,
			MapTileProvider:      "auto",
		},
	}

	app := &App{
		DB:        db,
		Templates: tmpl,
		AuthSrv:   authSrv,
		Config:    cfg,
	}
	app.Share = NewShareHandlers(app, db)
	app.Share.EtaService = eta.NewEtaService(db, 15, 30, 5)
	return app
}

func setupTestTrip(t *testing.T, db *sql.DB, tripID, vehicleID string) {
	t.Helper()
	_, _ = db.Exec(`INSERT OR IGNORE INTO users (id, name, email, password_hash, status, tenant_id)
		VALUES ('user-1', 'Admin User', 'user-1@test.com', 'hash', 'active', '1')`)

	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due)
		VALUES (?, ?, ?, 'truck', 10, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 0)`,
		vehicleID, "MH-12-AB-1234", "MH-12-AB-1234")
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r-1', 'Mumbai', 'Pune', 150.0, 3.5, 5000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips
		(id, trip_number, route_id, vehicle_id, departure_time, arrival_time, status, tenant_id)
		VALUES (?, 'TRP-001', 'r-1', ?, '2026-08-19 08:00:00', '2026-08-19 14:00:00', 'in_transit', '1')`,
		tripID, vehicleID)
	require.NoError(t, err)
}

func TestShare_Create_And_MaxActiveCap(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-1", "veh-1")

	r := chi.NewRouter()
	r.Post("/trips/{id}/share", app.Share.CreateShare)

	// 1. Create first share link (with PIN)
	body1 := `{"pin":"1234","ttl_hours":12}`
	req1 := withSession(httptest.NewRequest("POST", "/trips/trip-1/share", strings.NewReader(body1)), "user-1", "admin")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusCreated, w1.Code)
	var resp1 map[string]interface{}
	require.NoError(t, json.NewDecoder(w1.Body).Decode(&resp1))
	token1 := resp1["token"].(string)
	assert.NotEmpty(t, token1)
	assert.True(t, resp1["has_pin"].(bool))
	assert.Contains(t, resp1["url"].(string), "/share/"+token1)

	// Verify token hash is stored, not raw token
	var storedHash string
	err := db.QueryRow("SELECT token_hash FROM share_links WHERE id = ?", resp1["id"]).Scan(&storedHash)
	require.NoError(t, err)
	assert.Equal(t, sha256Hex(token1), storedHash)
	assert.NotEqual(t, token1, storedHash)

	// 2. Create second share link (no PIN)
	body2 := `{"ttl_hours":24}`
	req2 := withSession(httptest.NewRequest("POST", "/trips/trip-1/share", strings.NewReader(body2)), "user-1", "admin")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusCreated, w2.Code)

	// 3. Create third share link -> exceeds MaxActive (2) -> 409 Conflict
	body3 := `{"ttl_hours":24}`
	req3 := withSession(httptest.NewRequest("POST", "/trips/trip-1/share", strings.NewReader(body3)), "user-1", "admin")
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusConflict, w3.Code)
}

func TestShare_View_NoPIN_And_SlidingExpiry(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-2", "veh-2")

	// Insert share link without PIN
	token := "open-token-12345"
	tokenHash := sha256Hex(token)
	initialExpiry := time.Now().UTC().Add(2 * time.Hour)
	_, err := db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('link-open', 'trip-2', ?, 'user-1', CURRENT_TIMESTAMP, ?)`, tokenHash, initialExpiry)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/share/{token}", app.Share.ViewShare)

	// Public view without session
	req := httptest.NewRequest("GET", "/share/"+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "TRP-001")
	assert.Contains(t, w.Header().Get("Cache-Control"), "no-cache")

	// Check sliding expiry and view_count increment
	var newExpiresAt time.Time
	var viewCount int
	err = db.QueryRow("SELECT expires_at, view_count FROM share_links WHERE id = 'link-open'").Scan(&newExpiresAt, &viewCount)
	require.NoError(t, err)
	assert.Equal(t, 1, viewCount)
	assert.True(t, newExpiresAt.After(initialExpiry))
}

func TestShare_PIN_Verification_Flow_And_Lockout(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-3", "veh-3")

	token := "pin-token-12345"
	tokenHash := sha256Hex(token)
	saltHex := "0102030405060708090a0b0c0d0e0f10"
	pinHash := hashPIN("5678", saltHex)

	_, err := db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, pin_hash, pin_salt, created_by, created_at, expires_at)
		VALUES ('link-pin', 'trip-3', ?, ?, ?, 'user-1', CURRENT_TIMESTAMP, datetime('now', '+24 hours'))`,
		tokenHash, pinHash, saltHex)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/share/{token}", app.Share.ViewShare)
	r.Post("/share/{token}/verify", app.Share.VerifyPIN)

	// 1. Initial GET -> rendered PIN entry form
	req1 := httptest.NewRequest("GET", "/share/"+token, nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Contains(t, w1.Body.String(), "Enter 4–6 Digit PIN")

	// 2. Submit wrong PIN -> 403 Forbidden
	formWrong := url.Values{"pin": {"9999"}}
	reqWrong := httptest.NewRequest("POST", "/share/"+token+"/verify", strings.NewReader(formWrong.Encode()))
	reqWrong.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wWrong := httptest.NewRecorder()
	r.ServeHTTP(wWrong, reqWrong)
	assert.Equal(t, http.StatusForbidden, wWrong.Code)

	var failedAttempts int
	_ = db.QueryRow("SELECT failed_pin_attempts FROM share_links WHERE id = 'link-pin'").Scan(&failedAttempts)
	assert.Equal(t, 1, failedAttempts)

	// 3. Fail 4 more times -> 5 total -> Account Lockout
	for i := 2; i <= 5; i++ {
		reqFail := httptest.NewRequest("POST", "/share/"+token+"/verify", strings.NewReader(formWrong.Encode()))
		reqFail.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		wFail := httptest.NewRecorder()
		r.ServeHTTP(wFail, reqFail)
		assert.Equal(t, http.StatusForbidden, wFail.Code)
	}

	var lockedUntil sql.NullTime
	_ = db.QueryRow("SELECT locked_until FROM share_links WHERE id = 'link-pin'").Scan(&lockedUntil)
	assert.True(t, lockedUntil.Valid)
	assert.True(t, lockedUntil.Time.After(time.Now().UTC()))

	// Reset lock in DB to test correct PIN
	_, _ = db.Exec("UPDATE share_links SET locked_until = NULL, failed_pin_attempts = 0 WHERE id = 'link-pin'")

	// 4. Submit correct PIN -> Cookie set + 303 Redirect
	formCorrect := url.Values{"pin": {"5678"}}
	reqCorrect := httptest.NewRequest("POST", "/share/"+token+"/verify", strings.NewReader(formCorrect.Encode()))
	reqCorrect.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wCorrect := httptest.NewRecorder()
	r.ServeHTTP(wCorrect, reqCorrect)

	assert.Equal(t, http.StatusSeeOther, wCorrect.Code)
	cookies := wCorrect.Result().Cookies()
	var pinCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "share_pin_"+tokenHash {
			pinCookie = c
			break
		}
	}
	require.NotNil(t, pinCookie)
	assert.True(t, pinCookie.HttpOnly)

	// 5. Subsequent GET with Cookie -> Renders public map
	reqAuthed := httptest.NewRequest("GET", "/share/"+token, nil)
	reqAuthed.AddCookie(pinCookie)
	wAuthed := httptest.NewRecorder()
	r.ServeHTTP(wAuthed, reqAuthed)
	assert.Equal(t, http.StatusOK, wAuthed.Code)
	assert.Contains(t, wAuthed.Body.String(), "share-map")
}

func TestShare_Data_Endpoint_And_AntiThrash(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-4", "veh-4")

	// Insert telemetry snapshot
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO telemetry_snapshots
		(id, vehicle_id, latitude, longitude, speed, fuel_level, odometer, timestamp)
		VALUES ('snap-1', 'veh-4', 19.0760, 72.8777, 45.0, 75.0, 1500.0, ?)`, now)
	require.NoError(t, err)

	token := "data-token-12345"
	tokenHash := sha256Hex(token)
	initialExpiry := now.Add(5 * time.Hour)
	_, err = db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('link-data', 'trip-4', ?, 'user-1', ?, ?)`, tokenHash, now, initialExpiry)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/share/{token}/data", app.Share.ShareData)

	req := httptest.NewRequest("GET", "/share/"+token+"/data", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Cache-Control"), "no-cache")

	var data map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&data))
	assert.Equal(t, "TRP-001", data["trip_number"])
	assert.Equal(t, "in_transit", data["status"])
	assert.Equal(t, 19.0760, data["lat"])
	assert.Equal(t, 72.8777, data["lng"])
	assert.Equal(t, 45.0, data["speed"])
	assert.Equal(t, "scheduled", data["eta_method"])

	// Anti-thrash check: expires_at and view_count MUST NOT change
	var checkExpiry time.Time
	var checkViews int
	_ = db.QueryRow("SELECT expires_at, view_count FROM share_links WHERE id = 'link-data'").Scan(&checkExpiry, &checkViews)
	assert.Equal(t, initialExpiry.Unix(), checkExpiry.Unix())
	assert.Equal(t, 0, checkViews)
}

func TestShare_UniformResponses_Unknown_Expired_Revoked(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-5", "veh-5")

	// 1. Unknown token -> 404
	r := chi.NewRouter()
	r.Get("/share/{token}", app.Share.ViewShare)
	r.Get("/share/{token}/data", app.Share.ShareData)

	reqUnknown := httptest.NewRequest("GET", "/share/non-existent-token", nil)
	wUnknown := httptest.NewRecorder()
	r.ServeHTTP(wUnknown, reqUnknown)
	assert.Equal(t, http.StatusNotFound, wUnknown.Code)

	// 2. Expired token -> 410
	tokExp := "expired-token"
	tokExpHash := sha256Hex(tokExp)
	_, _ = db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('link-exp', 'trip-5', ?, 'user-1', datetime('now', '-2 days'), datetime('now', '-1 day'))`, tokExpHash)

	reqExp := httptest.NewRequest("GET", "/share/"+tokExp, nil)
	wExp := httptest.NewRecorder()
	r.ServeHTTP(wExp, reqExp)
	assert.Equal(t, http.StatusGone, wExp.Code)

	// 3. Revoked token -> 410
	tokRev := "revoked-token"
	tokRevHash := sha256Hex(tokRev)
	_, _ = db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at, revoked_at)
		VALUES ('link-rev', 'trip-5', ?, 'user-1', CURRENT_TIMESTAMP, datetime('now', '+1 day'), CURRENT_TIMESTAMP)`, tokRevHash)

	reqRev := httptest.NewRequest("GET", "/share/"+tokRev, nil)
	wRev := httptest.NewRecorder()
	r.ServeHTTP(wRev, reqRev)
	assert.Equal(t, http.StatusGone, wRev.Code)
}

func TestShare_ListShares_And_Revoke(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-6", "veh-6")

	_, err := db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('link-to-revoke', 'trip-6', 'hash-1', 'user-1', CURRENT_TIMESTAMP, datetime('now', '+1 day'))`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/shares", app.Share.ListShares)
	r.Post("/shares/{id}/revoke", app.Share.RevokeShare)

	// List
	reqList := withSession(httptest.NewRequest("GET", "/shares", nil), "user-1", "admin")
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	assert.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "TRP-001")

	// Revoke
	reqRevoke := withSession(httptest.NewRequest("POST", "/shares/link-to-revoke/revoke", nil), "user-1", "admin")
	wRevoke := httptest.NewRecorder()
	r.ServeHTTP(wRevoke, reqRevoke)
	assert.Equal(t, http.StatusSeeOther, wRevoke.Code)

	var revokedAt sql.NullTime
	_ = db.QueryRow("SELECT revoked_at FROM share_links WHERE id = 'link-to-revoke'").Scan(&revokedAt)
	assert.True(t, revokedAt.Valid)
}

func TestShare_RBAC_Permissions(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, denyAuthSvc{})

	r := chi.NewRouter()
	r.With(middleware.ResourcePermission(app.AuthSrv, "shares", "create")).Post("/trips/{id}/share", app.Share.CreateShare)
	r.With(middleware.ResourcePermission(app.AuthSrv, "shares", "read")).Get("/shares", app.Share.ListShares)
	r.With(middleware.ResourcePermission(app.AuthSrv, "shares", "revoke")).Post("/shares/{id}/revoke", app.Share.RevokeShare)

	// Create -> 403 Forbidden
	reqCreate := withSession(httptest.NewRequest("POST", "/trips/trip-1/share", nil), "viewer-1", "viewer")
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqCreate)
	assert.Equal(t, http.StatusForbidden, wCreate.Code)

	// List -> 403 Forbidden
	reqList := withSession(httptest.NewRequest("GET", "/shares", nil), "viewer-1", "viewer")
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	assert.Equal(t, http.StatusForbidden, wList.Code)

	// Revoke -> 403 Forbidden
	reqRevoke := withSession(httptest.NewRequest("POST", "/shares/link-1/revoke", nil), "viewer-1", "viewer")
	wRevoke := httptest.NewRecorder()
	r.ServeHTTP(wRevoke, reqRevoke)
	assert.Equal(t, http.StatusForbidden, wRevoke.Code)
}

func TestShare_Data_Endpoint_HybridEta(t *testing.T) {
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})
	setupTestTrip(t, db, "trip-hybrid-share", "veh-hybrid-share")

	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)

	// Seed 4 snapshots: 1 at start, 3 in last 10 minutes -> triggers hybrid calculation
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-hs1', 'trip-hybrid-share', 'veh-hybrid-share', 19.0, 72.8, 80.0, 1000.0, ?)`, started)
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-hs2', 'trip-hybrid-share', 'veh-hybrid-share', 19.0, 72.8, 80.0, 1020.0, ?)`, now.Add(-10*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-hs3', 'trip-hybrid-share', 'veh-hybrid-share', 19.0, 72.8, 80.0, 1040.0, ?)`, now.Add(-5*time.Minute))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots (id, trip_id, vehicle_id, latitude, longitude, speed, odometer, timestamp)
		VALUES ('snap-hs4', 'trip-hybrid-share', 'veh-hybrid-share', 19.0, 72.8, 80.0, 1060.0, ?)`, now.Add(-1*time.Minute))

	token := "hybrid-share-token"
	tokenHash := sha256Hex(token)
	_, err := db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES ('link-hybrid-share', 'trip-hybrid-share', ?, 'user-1', ?, ?)`, tokenHash, now, now.Add(24*time.Hour))
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/share/{token}/data", app.Share.ShareData)

	req := httptest.NewRequest("GET", "/share/"+token+"/data", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var data map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&data))
	assert.Equal(t, "hybrid", data["eta_method"])
	assert.NotEmpty(t, data["eta_min"])
	assert.NotEmpty(t, data["eta_max"])
}
