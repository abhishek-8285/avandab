package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthConfig is a minimal, stdlib-only OAuth 2.0 authorization-code client
// for "Sign in with Google". Deliberately no golang.org/x/oauth2 dependency —
// the flow is three HTTP calls and injecting endpoint URLs makes it fully
// testable with httptest. Zero-cost tier: the provider (Google) is free and
// never needs replacing.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	HTTPClient   *http.Client
}

const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
)

// NewGoogleOAuthConfig returns a config pointed at Google's production
// endpoints with OIDC scopes (openid email profile).
func NewGoogleOAuthConfig(clientID, clientSecret, redirectURL string) *OAuthConfig {
	return &OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      googleAuthURL,
		TokenURL:     googleTokenURL,
		UserInfoURL:  googleUserInfoURL,
		Scopes:       []string{"openid", "email", "profile"},
		HTTPClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *OAuthConfig) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// AuthCodeURL builds the Google consent-screen URL carrying the CSRF state.
func (c *OAuthConfig) AuthCodeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("state", state)
	// select_account forces the chooser instead of silently reusing a stale
	// Google session — one wrong account is the #1 support ticket.
	q.Set("prompt", "select_account")
	return c.AuthURL + "?" + q.Encode()
}

// Token is the OAuth2 token exchange response (subset we consume).
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Exchange swaps the authorization code for an access token.
func (c *OAuthConfig) Exchange(ctx context.Context, code string) (*Token, error) {
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", c.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth token read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth token exchange failed: status %d", resp.StatusCode)
	}
	var tok Token
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("oauth token decode: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("oauth token exchange returned no access token")
	}
	return &tok, nil
}

// UserInfo is the Google OAuth2 / OIDC userinfo payload.
type UserInfo struct {
	ID            string `json:"id"`
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	VerifiedEmail *bool  `json:"verified_email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// GetID returns the unique Google user identifier (id or sub).
func (u *UserInfo) GetID() string {
	if u.ID != "" {
		return u.ID
	}
	return u.Sub
}

// IsEmailVerified returns true if Google confirmed the email address.
func (u *UserInfo) IsEmailVerified() bool {
	if u.VerifiedEmail != nil {
		return *u.VerifiedEmail
	}
	if u.EmailVerified != nil {
		return *u.EmailVerified
	}
	return false
}

// FetchUserInfo retrieves the verified identity behind the access token.
func (c *OAuthConfig) FetchUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oauth userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth userinfo fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth userinfo read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth userinfo fetch failed: status %d", resp.StatusCode)
	}
	var info UserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("oauth userinfo decode: %w", err)
	}
	if info.GetID() == "" || info.Email == "" {
		return nil, fmt.Errorf("oauth userinfo missing id/sub or email")
	}
	return &info, nil
}
