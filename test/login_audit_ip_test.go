package test

// Regression: LOGIN rows in /audit-logs showed "-" (NULL ip_address) because
// the public login routes never seeded auth.ContextIP — the session middleware
// that sets it only runs after authentication.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiauth "transport-app/internal/auth/presentation/api/handlers"
)

func postToken(t *testing.T, h http.Handler, email, password, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestLoginAudit_StoresClientIP(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	_, password := createTestAdmin(t, svc)

	h := apiauth.NewAPIAuthHandler(svc.Auth, svc.Users, []byte("test-api-secret-32bytes-long!!"), db)
	r := chi.NewRouter()
	h.Register(r)

	rr := postToken(t, r, "admin@transport.local", password, "203.0.113.9:4123", nil)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	var ip string
	err := db.QueryRow(`SELECT ip_address FROM audit_logs WHERE action = 'login' ORDER BY created_at DESC LIMIT 1`).Scan(&ip)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.9", ip, "login audit must store the TCP peer IP when no proxy headers exist")
}

func TestLoginAudit_HonorsProxyHeaders(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	_, password := createTestAdmin(t, svc)

	h := apiauth.NewAPIAuthHandler(svc.Auth, svc.Users, []byte("test-api-secret-32bytes-long!!"), db)
	r := chi.NewRouter()
	h.Register(r)

	// Loopback is a trusted proxy: CF-Connecting-IP must win over RemoteAddr.
	rr := postToken(t, r, "admin@transport.local", password, "127.0.0.1:4123",
		map[string]string{
			"CF-Connecting-IP": "203.0.113.7",
			"CF-IPCountry":     "IN",
			"CF-IPCity":        "Pune",
		})
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	var ip string
	var location *string
	err := db.QueryRow(`SELECT ip_address, location FROM audit_logs WHERE action = 'login' ORDER BY created_at DESC LIMIT 1`).Scan(&ip, &location)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.7", ip, "login audit must store CF-Connecting-IP when behind a trusted proxy")
	require.NotNil(t, location, "login audit must store coarse location from Cloudflare headers")
	assert.Equal(t, "Pune, IN", *location)
}
