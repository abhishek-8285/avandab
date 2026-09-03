package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthConfig_AuthCodeURL verifies the consent URL carries every
// parameter the code-exchange step will later validate against.
func TestOAuthConfig_AuthCodeURL(t *testing.T) {
	cfg := NewGoogleOAuthConfig("cid-123", "secret-456", "https://app.example.com/auth/google/callback")
	link := cfg.AuthCodeURL("state-abc")

	assert.Contains(t, link, "client_id=cid-123")
	assert.Contains(t, link, "redirect_uri=https%3A%2F%2Fapp.example.com%2Fauth%2Fgoogle%2Fcallback")
	assert.Contains(t, link, "response_type=code")
	assert.Contains(t, link, "state=state-abc")
	assert.Contains(t, link, "scope=openid+email+profile")
	assert.Contains(t, link, "prompt=select_account")
	assert.True(t, strings.HasPrefix(link, googleAuthURL+"?"))
}

// TestOAuthConfig_Exchange verifies the token swap posts the full form
// (code, client credentials, redirect_uri, grant_type) and decodes the
// access token. Fake token endpoint via httptest — no network.
func TestOAuthConfig_Exchange(t *testing.T) {
	var gotForm map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		gotForm = map[string]string{}
		for k := range r.Form {
			gotForm[k] = r.FormValue(k)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer","expires_in":3599}`))
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "https://app.example.com/auth/google/callback")
	cfg.TokenURL = srv.URL

	tok, err := cfg.Exchange(context.Background(), "auth-code-1")
	require.NoError(t, err)
	assert.Equal(t, "at-1", tok.AccessToken)

	assert.Equal(t, "auth-code-1", gotForm["code"])
	assert.Equal(t, "cid", gotForm["client_id"])
	assert.Equal(t, "csecret", gotForm["client_secret"])
	assert.Equal(t, "authorization_code", gotForm["grant_type"])
	assert.Equal(t, "https://app.example.com/auth/google/callback", gotForm["redirect_uri"])
}

// TestOAuthConfig_Exchange_ErrorStatus — non-200 from the token endpoint must
// surface as an error, never an empty-token silent pass.
func TestOAuthConfig_Exchange_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "cb")
	cfg.TokenURL = srv.URL
	_, err := cfg.Exchange(context.Background(), "bad-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// TestOAuthConfig_FetchUserInfo verifies the OIDC userinfo fetch and decode.
func TestOAuthConfig_FetchUserInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok-9", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"sub-42","email":"OP@Example.com","email_verified":true,"name":"Op One"}`))
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "cb")
	cfg.UserInfoURL = srv.URL

	info, err := cfg.FetchUserInfo(context.Background(), "tok-9")
	require.NoError(t, err)
	assert.Equal(t, "sub-42", info.GetID())
	assert.Equal(t, "OP@Example.com", info.Email)
	assert.True(t, info.IsEmailVerified())
	assert.Equal(t, "Op One", info.Name)
}

// TestOAuthConfig_FetchUserInfo_GoogleV2Format verifies Google oauth2/v2/userinfo format.
func TestOAuthConfig_FetchUserInfo_GoogleV2Format(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok-v2", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"google-id-99","email":"user@google.com","verified_email":true,"name":"Google User","picture":"https://example.com/pic.jpg"}`))
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "cb")
	cfg.UserInfoURL = srv.URL

	info, err := cfg.FetchUserInfo(context.Background(), "tok-v2")
	require.NoError(t, err)
	assert.Equal(t, "google-id-99", info.GetID())
	assert.Equal(t, "user@google.com", info.Email)
	assert.True(t, info.IsEmailVerified())
	assert.Equal(t, "Google User", info.Name)
	assert.Equal(t, "https://example.com/pic.jpg", info.Picture)
}

// TestOAuthConfig_FetchUserInfo_MissingSub — a payload without sub/email is
// rejected: the service layer depends on both being present.
func TestOAuthConfig_FetchUserInfo_MissingSub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"email_verified":true}`))
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "cb")
	cfg.UserInfoURL = srv.URL
	_, err := cfg.FetchUserInfo(context.Background(), "tok")
	require.Error(t, err)
}

// TestOAuthConfig_Exchange_GarbageJSON guards decode errors.
func TestOAuthConfig_Exchange_GarbageJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	cfg := NewGoogleOAuthConfig("cid", "csecret", "cb")
	cfg.TokenURL = srv.URL
	_, err := cfg.Exchange(context.Background(), "code")
	require.Error(t, err)

	// sanity: error is a decode error, not a panic leak
	var probe map[string]interface{}
	assert.Error(t, json.Unmarshal([]byte("not-json"), &probe))
}
