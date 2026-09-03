package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/operations/notifications"
)

// captureSMSSender records delivered SMS bodies (no network).
type captureSMSSender struct {
	mu    sync.Mutex
	calls []string // "to|body"
}

func (c *captureSMSSender) Send(_ context.Context, to, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, to+"|"+message)
	return nil
}

// Configured reports true so the notification service treats it as a real
// SMS channel (SMSConfigured() gate in SendOTP).
func (c *captureSMSSender) Configured() bool { return true }

func (c *captureSMSSender) sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

// postJSON posts a JSON body and decodes the JSON response.
func postJSON(t *testing.T, url, body string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSendOTP_DevReturnsCode(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()
	app.Config.AppEnv = "development"
	app.Notify = nil // no SMS sender → dev fallback

	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"+919876543210"}`)
	assert.Equal(t, 200, status)
	assert.Equal(t, true, out["ok"])
	code, _ := out["dev_code"].(string)
	require.Len(t, code, 6, "dev fallback must return the 6-digit code")
}

func TestSendOTP_SMSConfigured_DeliversViaNotify(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()
	capturer := &captureSMSSender{}
	app.Notify = notifications.NewServiceWithChannels(nil, capturer)

	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"+919876543210"}`)
	assert.Equal(t, 200, status)
	assert.Equal(t, true, out["ok"])
	_, hasDevCode := out["dev_code"]
	assert.False(t, hasDevCode, "real SMS must not leak the code in the response")

	calls := capturer.sent()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0], "+919876543210|")
	assert.Contains(t, calls[0], "verification code")
}

func TestSendOTP_InvalidPhoneRejected(t *testing.T) {
	srv, _, cleanup := setupAuthAPIEnv(t)
	defer cleanup()

	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"not-a-number"}`)
	assert.Equal(t, 400, status)
	assert.Contains(t, out["error"], "international format")

	// Missing field is also 400.
	status, _ = postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{}`)
	assert.Equal(t, 400, status)
}

func TestVerifyOTP_SuccessStampsPhoneVerifiedAt(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()
	app.Config.AppEnv = "development" // get the code back for testing
	app.Notify = nil

	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES ('t-otp', 'OtpCo', 'otpco', 'active')`)
	require.NoError(t, err)
	_, err = app.DB.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id, phone)
		VALUES ('u-otp-1', 'driver@otpco.test', 'hash', 'OTP Driver', 5, 'active', 't-otp', '+919876543210')`)
	require.NoError(t, err)

	_, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"+919876543210"}`)
	code, _ := out["dev_code"].(string)
	require.Len(t, code, 6)

	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/verify",
		fmt.Sprintf(`{"phone":"+919876543210","code":"%s"}`, code))
	assert.Equal(t, 200, status)
	assert.Equal(t, true, out["verified"])

	var verifiedAt sql.NullTime
	require.NoError(t, app.DB.QueryRow(`SELECT phone_verified_at FROM users WHERE id = 'u-otp-1'`).Scan(&verifiedAt))
	assert.True(t, verifiedAt.Valid, "phone_verified_at must be stamped on successful OTP")
}

func TestVerifyOTP_WrongCodeFails(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()
	app.Config.AppEnv = "development"
	app.Notify = nil

	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES ('t-otp2', 'Otp2', 'otp2', 'active')`)
	require.NoError(t, err)
	_, err = app.DB.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id, phone)
		VALUES ('u-otp-2', 'driver2@otpco.test', 'hash', 'OTP Driver 2', 5, 'active', 't-otp2', '+919876543211')`)
	require.NoError(t, err)

	_, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"+919876543211"}`)
	code, _ := out["dev_code"].(string)
	require.Len(t, code, 6)

	// Wrong code → verified:false, and no stamp.
	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/verify",
		fmt.Sprintf(`{"phone":"+919876543211","code":"%s"}`, "000000"))
	assert.Equal(t, 200, status)
	assert.Equal(t, false, out["verified"])

	// Correct code within budget still works.
	_, out = postJSON(t, srv.URL+"/api/v1/auth/otp/verify",
		fmt.Sprintf(`{"phone":"+919876543211","code":"%s"}`, code))
	assert.Equal(t, true, out["verified"])
}

func TestVerifyOTP_UnknownPhoneNoEnumeration(t *testing.T) {
	srv, app, cleanup := setupAuthAPIEnv(t)
	defer cleanup()
	app.Config.AppEnv = "development"
	app.Notify = nil

	_, out := postJSON(t, srv.URL+"/api/v1/auth/otp/send", `{"phone":"+919999999999"}`)
	code, _ := out["dev_code"].(string)
	require.Len(t, code, 6)

	status, out := postJSON(t, srv.URL+"/api/v1/auth/otp/verify",
		fmt.Sprintf(`{"phone":"+919999999999","code":"%s"}`, code))
	assert.Equal(t, 200, status)
	assert.Equal(t, true, out["verified"],
		"a valid code verifies even for an unknown phone — possession is proven, no account enumation")

	var n int
	_ = app.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE phone = '+919999999999'`).Scan(&n)
	assert.Zero(t, n, "no user rows are created by verification")
}
