package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// APITokenClaims are the claims embedded in a signed API token.
type APITokenClaims struct {
	UserID    string `json:"uid"`
	Role      string `json:"role"`
	TenantID  string `json:"tid"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var (
	ErrTokenInvalid    = errors.New("api token invalid")
	ErrTokenExpired    = errors.New("api token expired")
	ErrTokenRevoked    = errors.New("api token revoked or user inactive")
	ErrTenantSuspended = errors.New("organization account is suspended")
)

// IssueAPIToken creates a signed, base64url-encoded token.
// Format: base64url(json(claims)) + "." + base64url(hmac-sha256(payload, secret))
func IssueAPIToken(secret []byte, claims APITokenClaims) (string, error) {
	if claims.IssuedAt == 0 {
		claims.IssuedAt = time.Now().Unix()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	b64payload := base64.RawURLEncoding.EncodeToString(payload)
	sig := computeHMAC(secret, b64payload)
	return b64payload + "." + sig, nil
}

// ParseAPIToken validates the signature and expiry, then returns the claims.
func ParseAPIToken(secret []byte, token string) (APITokenClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return APITokenClaims{}, ErrTokenInvalid
	}

	b64payload, sig := parts[0], parts[1]

	expectedSig := computeHMAC(secret, b64payload)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return APITokenClaims{}, ErrTokenInvalid
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(b64payload)
	if err != nil {
		return APITokenClaims{}, ErrTokenInvalid
	}

	var claims APITokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return APITokenClaims{}, ErrTokenInvalid
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return APITokenClaims{}, ErrTokenExpired
	}

	return claims, nil
}

func computeHMAC(secret []byte, data string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
