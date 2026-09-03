package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/operations/notifications"
	"transport-app/internal/shared"
)

// setupAuthAPIEnv builds the mobile-API test env plus a real reset-token store
// and an httptest server exposing the two public JSON reset endpoints exactly
// as main.go mounts them (public, rate limiting omitted in tests).
func setupAuthAPIEnv(t *testing.T) (*httptest.Server, *App, func()) {
	t.Helper()
	dbConn, app, _ := setupMobileAPITestEnv(t)
	app.ResetTokens = auth.NewResetTokenStore(15 * time.Minute)
	app.OTPStore = auth.NewOTPStore(0)
	app.Auth = &AuthHandlers{App: app}
	app.OTP = &OTPHandlers{App: app}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/forgot-password", app.Auth.ForgotPasswordAPI)
	mux.HandleFunc("POST /api/v1/auth/reset-password", app.Auth.ResetPasswordAPI)
	mux.HandleFunc("POST /api/v1/auth/otp/send", app.OTP.SendOTP)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", app.OTP.VerifyOTP)
	srv := httptest.NewServer(mux)

	return srv, app, func() { srv.Close(); dbConn.Close() }
}

func TestForgotPasswordAPI_SMTPConfigured_EnqueuesOutbox(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	// When SMTP is configured the reset email must go through the durable
	// comm_outbox queue (Phase 2) instead of being dropped or dev-linked.
	app.Notify = notifications.NewServiceWithChannels(
		notifications.NewSMTPEmailSender(notifications.SMTPConfig{
			Host: "smtp.test", From: "noreply@test",
		}),
		nil,
	)

	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES ('t-acme', 'Acme', 'acme', 'active')`)
	require.NoError(t, err)
	_, err = app.DB.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES ('u-reset-2', 'reset2@example.com', 'hash', 'Reset User 2', 5, 'active', 't-acme')`)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"email": "reset2@example.com"})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/auth/forgot-password", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	// Testing env must never return the raw link — the durable queue takes over.
	_, hasLink := out["reset_link"]
	assert.False(t, hasLink)

	var channel, recipient, template, tenantID string
	err = app.DB.QueryRow(`SELECT channel, recipient, template, tenant_id FROM comm_outbox WHERE recipient = ?`, "reset2@example.com").
		Scan(&channel, &recipient, &template, &tenantID)
	require.NoError(t, err, "reset email must be queued in comm_outbox")
	assert.Equal(t, "email", channel)
	assert.Equal(t, "password_reset", template)
	assert.Equal(t, "t-acme", tenantID, "tenant must come from the user record, never hardcoded")
}

func TestForgotPasswordAPI_UnknownEmail_GenericResponse(t *testing.T) {
	srv, _, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	httpResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/forgot-password", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer httpResp.Body.Close()

	assert.Equal(t, 200, httpResp.StatusCode)
	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(httpResp.Body).Decode(&out))
	assert.Equal(t, true, out["ok"])
	assert.Contains(t, out["message"], "If an account exists")

	// Unknown account must never leak a reset link.
	_, hasLink := out["reset_link"]
	assert.False(t, hasLink)
}

func TestForgotPasswordAPI_ExistingActiveUser_NoLinkOutsideDev(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	// Test env uses AppEnv=testing (not development), so even a valid account
	// must not receive the raw reset link over the API.
	_, err := app.DB.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES ('u-reset-1', 'reset@example.com', 'hash', 'Reset User', 5, 'active')`)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"email": "reset@example.com"})
	httpResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/forgot-password", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer httpResp.Body.Close()

	assert.Equal(t, 200, httpResp.StatusCode)
	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(httpResp.Body).Decode(&out))
	_, hasLink := out["reset_link"]
	assert.False(t, hasLink, "reset_link must not be returned outside development")
}

func TestForgotPasswordAPI_MissingAndMalformedBody(t *testing.T) {
	srv, _, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	for name, payload := range map[string]string{
		"empty object": `{}`,
		"malformed":    `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			httpResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/forgot-password", "application/json", strings.NewReader(payload))
			require.NoError(t, err)
			defer httpResp.Body.Close()
			assert.Equal(t, 400, httpResp.StatusCode)
		})
	}
}

func TestResetPasswordAPI_EndToEnd(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	email := "flow@example.com"
	_, err := app.DB.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES ('u-flow-1', 'flow@example.com', 'hash', 'Flow User', 5, 'active')`)
	require.NoError(t, err)

	token, err := app.ResetTokens.Create(email)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	// Invalid token rejected first.
	badBody, _ := json.Marshal(map[string]string{"token": "bogus", "password": "BrandNew123!"})
	badResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(badBody))
	require.NoError(t, err)
	defer badResp.Body.Close()
	assert.Equal(t, 400, badResp.StatusCode)

	// Valid token sets the password.
	goodBody, _ := json.Marshal(map[string]string{"token": token, "password": "BrandNew123!"})
	goodResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(goodBody))
	require.NoError(t, err)
	defer goodResp.Body.Close()
	assert.Equal(t, 200, goodResp.StatusCode)
	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(goodResp.Body).Decode(&out))
	assert.Equal(t, true, out["ok"])

	// Token is single-use: second redemption fails.
	replayBody, _ := json.Marshal(map[string]string{"token": token, "password": "Another123!"})
	replayResp, err := srv.Client().Post(srv.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(replayBody))
	require.NoError(t, err)
	defer replayResp.Body.Close()
	assert.Equal(t, 400, replayResp.StatusCode)
}

func TestResetPasswordAPI_PasswordPolicyEnforced(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	token, err := app.ResetTokens.Create("policy@example.com")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"token": token, "password": "short"})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/auth/reset-password", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
	var out map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.NotEmpty(t, out["error"])
}

func TestDriverStatusAndIssues(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	_, err := dbConn.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES ('u-st-1', 'st@example.com', 'hash', 'St User', 5, 'active')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-st-1', 'DRV-ST', 'St', 'User', '+919900000002', 'st@example.com', 'DL-ST', '2030-01-01', 'available', '1')`)
	require.NoError(t, err)

	currentUser := &auth.SessionData{UserID: "u-st-1"}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, currentUser)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/v1/drivers/me/status", app.Drivers.UpdateMyStatus)
	r.Post("/api/v1/drivers/me/issues", app.Drivers.ReportIssue)
	r.Get("/api/v1/drivers/me/issues", app.Drivers.ListMyIssues)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Status toggle: valid value accepted.
	resp, err := http.Post(srv.URL+"/api/v1/drivers/me/status", "application/json",
		strings.NewReader(`{"status":"leave"}`))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()
	var st int
	require.NoError(t, dbConn.QueryRow(`SELECT count(*) FROM drivers WHERE id='d-st-1' AND status='leave'`).Scan(&st))
	require.Equal(t, 1, st)

	// on_trip is lifecycle-controlled — rejected.
	resp2, err2 := http.Post(srv.URL+"/api/v1/drivers/me/status", "application/json",
		strings.NewReader(`{"status":"on_trip"}`))
	require.NoError(t, err2)
	assert.Equal(t, 400, resp2.StatusCode)
	resp2.Body.Close()

	// Issue without message rejected (multipart form like the app sends).
	emptyForm := &bytes.Buffer{}
	resp3, err3 := http.Post(srv.URL+"/api/v1/drivers/me/issues", "application/json", emptyForm)
	require.NoError(t, err3)
	assert.Equal(t, 400, resp3.StatusCode)
	resp3.Body.Close()

	// Valid issue created + listed.
	formBuf := &bytes.Buffer{}
	mw := multipart.NewWriter(formBuf)
	_ = mw.WriteField("message", "Tyre wear front-left")
	_ = mw.WriteField("severity", "high")
	_ = mw.WriteField("category", "vehicle")
	mw.Close()
	resp4, err4 := http.Post(srv.URL+"/api/v1/drivers/me/issues", mw.FormDataContentType(), formBuf)
	require.NoError(t, err4)
	require.Equal(t, 201, resp4.StatusCode)
	var created map[string]interface{}
	require.NoError(t, json.NewDecoder(resp4.Body).Decode(&created))
	resp4.Body.Close()
	assert.Equal(t, "open", created["status"])

	resp5, err5 := http.Get(srv.URL + "/api/v1/drivers/me/issues")
	require.NoError(t, err5)
	require.Equal(t, 200, resp5.StatusCode)
	var list struct {
		Issues []map[string]interface{} `json:"issues"`
	}
	require.NoError(t, json.NewDecoder(resp5.Body).Decode(&list))
	require.Len(t, list.Issues, 1)
	assert.Equal(t, "Tyre wear front-left", list.Issues[0]["message"])
}

func TestDriverUpdateMe(t *testing.T) {
	dbConn, app, _ := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	_, err := dbConn.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES ('u-upd-1', 'upd@example.com', 'hash', 'Update Driver', 5, 'active')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-upd-1', 'DRV-UPD', 'Update', 'Driver', '+919900000003', 'upd@example.com', 'OLD-DL', '2025-01-01', 'inactive', '1')`)
	require.NoError(t, err)

	currentUser := &auth.SessionData{UserID: "u-upd-1"}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, currentUser)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/v1/drivers/me", app.Drivers.GetMe)
	r.Put("/api/v1/drivers/me", app.Drivers.UpdateMe)
	r.Post("/api/v1/drivers/me", app.Drivers.UpdateMe)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Update license and bank details
	payload := `{"license_number":"MH1420210088991","license_expiry":"2032-12-31","bank_details":"HDFC BANK · ****6587","status":"available"}`
	req, err := http.NewRequest("PUT", srv.URL+"/api/v1/drivers/me", strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	// Fetch via GET /api/v1/drivers/me and verify persistence
	resp2, err := http.Get(srv.URL + "/api/v1/drivers/me")
	require.NoError(t, err)
	require.Equal(t, 200, resp2.StatusCode)
	var me map[string]interface{}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&me))
	resp2.Body.Close()

	assert.Equal(t, "MH1420210088991", me["license_number"])
	assert.Equal(t, "2032-12-31", me["license_expiry"])
	assert.Equal(t, "HDFC BANK · ****6587", me["bank_details"])
	assert.Equal(t, "available", me["status"])
}
