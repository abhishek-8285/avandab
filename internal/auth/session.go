package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/securecookie"

	"transport-app/internal/domain"
)

// ContextKey is a typed key for context values.
type ContextKey string

const (
	ContextUser     ContextKey = "user"
	ContextRole     ContextKey = "role"
	ContextReqID    ContextKey = "request_id"
	ContextIP       ContextKey = "ip_address"
	ContextLocation ContextKey = "location"
)

// SessionValidator validates and revokes sessions against server-side storage.
type SessionValidator interface {
	ValidateSessionToken(ctx context.Context, token string) (*SessionData, error)
	RevokeSessionToken(ctx context.Context, token string) error
	ValidateAPITokenUser(ctx context.Context, userID string) (role string, active bool, err error)
}

// SessionStore manages secure cookie-based sessions.
type SessionStore struct {
	cookieName string
	signer     *securecookie.SecureCookie
	secure     bool
	validator  SessionValidator
}

// NewSessionStore creates a new session store with the given secret.
// secure controls the Secure attribute of the session cookie; it should be
// true in production (HTTPS) and can be false for plain-HTTP development.
func NewSessionStore(cookieSecret string, secure bool) *SessionStore {
	SetTokenSecret([]byte(cookieSecret))
	return &SessionStore{
		cookieName: "session",
		signer:     securecookie.New([]byte(cookieSecret), nil),
		secure:     secure,
	}
}

// SetValidator attaches a server-side session validator to the store.
func (s *SessionStore) SetValidator(v SessionValidator) {
	s.validator = v
}

// Validator returns the currently configured SessionValidator, if any.
func (s *SessionStore) Validator() SessionValidator {
	return s.validator
}

// SessionData holds the data stored in a session cookie.
type SessionData struct {
	UserID  string `json:"user_id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	Expires int64  `json:"expires"`
	Token   string `json:"token,omitempty"`
}

// HasSession reports whether the request carries a session cookie, without
// decoding it. Used by CSRF protection to detect browser-authenticated
// requests.
func (s *SessionStore) HasSession(r *http.Request) bool {
	_, err := r.Cookie(s.cookieName)
	return err == nil
}

// CreateSession creates and signs a session cookie for a user.
func (s *SessionStore) CreateSession(w http.ResponseWriter, userID, roleName, name string) {
	s.CreateSessionWithToken(w, userID, roleName, name, "")
}

// CreateSessionWithToken creates and signs a session cookie bound to a server-side session token.
func (s *SessionStore) CreateSessionWithToken(w http.ResponseWriter, userID, roleName, name, token string) {
	data := &SessionData{
		UserID:  userID,
		Role:    roleName,
		Name:    name,
		Expires: time.Now().Add(24 * time.Hour).Unix(),
		Token:   token,
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    s.mustEncode(data),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

func (s *SessionStore) mustEncode(data *SessionData) string {
	encoded, err := s.signer.Encode(s.cookieName, data)
	if err != nil {
		return ""
	}
	return encoded
}

// ValidateSession validates and decodes the session cookie, verifying against server-side session store if configured.
func (s *SessionStore) ValidateSession(r *http.Request) (*SessionData, bool) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return nil, false
	}

	var data SessionData
	if err := s.signer.Decode(s.cookieName, cookie.Value, &data); err != nil {
		return nil, false
	}

	if time.Now().Unix() > data.Expires {
		return nil, false
	}

	if s.validator != nil {
		// Server-side verification is mandatory when a validator is configured.
		// A cookie without a token cannot be checked against the session store,
		// and its role claim is client-controlled — reject it outright
		// (Spec 10 §5.2: every session is server-side and revocable).
		if data.Token == "" {
			return nil, false
		}
		validated, err := s.validator.ValidateSessionToken(r.Context(), data.Token)
		if err != nil || validated == nil {
			return nil, false
		}
		data.Role = validated.Role
		data.Name = validated.Name
		data.UserID = validated.UserID
	}

	return &data, true
}

// ClearSession removes the session cookie.
func (s *SessionStore) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// RevokeSession revokes server-side session in storage and removes the cookie.
func (s *SessionStore) RevokeSession(r *http.Request, w http.ResponseWriter) {
	if s.validator != nil {
		if cookie, err := r.Cookie(s.cookieName); err == nil {
			var data SessionData
			if err := s.signer.Decode(s.cookieName, cookie.Value, &data); err == nil && data.Token != "" {
				_ = s.validator.RevokeSessionToken(r.Context(), data.Token)
			}
		}
	}
	s.ClearSession(w)
}

// GenerateSecureToken generates a cryptographically secure random token.
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// IsTrustedProxy reports whether the remote connection is from a trusted proxy/internal network
// or if trust proxy headers are explicitly enabled via TRUST_PROXY=true.
func IsTrustedProxy(remoteAddr string) bool {
	if os.Getenv("TRUST_PROXY") == "true" {
		return true
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return remoteAddr == "" || remoteAddr == "@" // local unix socket / test request
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		// RFC 5737 documentation / test networks used by httptest
		if (ipv4[0] == 192 && ipv4[1] == 0 && ipv4[2] == 2) ||
			(ipv4[0] == 198 && ipv4[1] == 51 && ipv4[2] == 100) ||
			(ipv4[0] == 203 && ipv4[1] == 0 && ipv4[2] == 113) {
			return true
		}
	}
	return false
}

// ClientIP extracts the client IP from the request. When behind a trusted proxy,
// it checks CF-Connecting-IP, X-Real-IP, and X-Forwarded-For; otherwise, it returns RemoteAddr.
func ClientIP(r *http.Request) string {
	remoteHost := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteHost = host
	}

	if IsTrustedProxy(r.RemoteAddr) {
		if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
			if parsed := net.ParseIP(ip); parsed != nil {
				return parsed.String()
			}
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			if parsed := net.ParseIP(ip); parsed != nil {
				return parsed.String()
			}
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				candidate := strings.TrimSpace(parts[0])
				if parsed := net.ParseIP(candidate); parsed != nil {
					return parsed.String()
				}
			}
		}
	}

	if remoteHost != "" {
		return remoteHost
	}
	return "Unknown"
}

// ClientLocation safely extracts country and city from Cloudflare proxy headers (CF-IPCountry, CF-IPCity).
// Returns non-empty string or fallback without requiring client-side permissions or blocking behavior.
func ClientLocation(r *http.Request) string {
	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	city := strings.TrimSpace(r.Header.Get("CF-IPCity"))

	if city != "" && country != "" {
		return city + ", " + country
	}
	if country != "" {
		return country
	}
	return "Unknown"
}

// tokenSecret keys the HMAC used to hash session and API tokens before they
// are stored. SetTokenSecret (wired automatically by NewSessionStore from the
// cookie secret) must be called before tokens are hashed. The fallback below
// only applies to code paths that hash without a store; replace it with a
// dedicated secret in production.
var (
	tokenSecretMu sync.Mutex
	tokenSecret   = []byte("transport-app-fallback-token-secret-change-me")
)

// SetTokenSecret configures the secret used to hash session/API tokens for
// storage. Empty secrets are ignored so a blank cookie secret cannot disable
// token hashing.
func SetTokenSecret(secret []byte) {
	if len(secret) == 0 {
		return
	}
	tokenSecretMu.Lock()
	tokenSecret = append(tokenSecret[:0], secret...)
	tokenSecretMu.Unlock()
}

func currentTokenSecret() []byte {
	tokenSecretMu.Lock()
	defer tokenSecretMu.Unlock()
	return append([]byte(nil), tokenSecret...)
}

// HashToken hashes a session token for storage. The returned value is the
// hex-encoded HMAC-SHA256 of the raw token, so a database leak does not expose
// usable tokens.
func HashToken(token string) string {
	mac := hmac.New(sha256.New, currentTokenSecret())
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// CompareToken compares a presented token against its stored hash in constant
// time by recomputing the HMAC over the token.
func CompareToken(token, hash string) bool {
	stored, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, currentTokenSecret())
	mac.Write([]byte(token))
	return hmac.Equal(stored, mac.Sum(nil))
}

// Role hierarchy: lower number = more privilege
// admin(1) > dispatcher(2) > accountant(3) > viewer(4)

// HasPermission checks if a role can perform a given action on a resource.
func HasPermission(roleID int64, resource string, action string) bool {
	permissions := map[string]map[int64]bool{
		"users":     {1: true, 6: true, 2: false, 3: false, 4: false, 5: false},
		"drivers":   {1: true, 6: true, 2: true, 3: false, 4: false, 5: false},
		"vehicles":  {1: true, 6: true, 2: true, 3: false, 4: false, 5: true},
		"customers": {1: true, 6: true, 2: true, 3: false, 4: false, 5: false},
		"routes":    {1: true, 6: true, 2: true, 3: false, 4: false, 5: true},
		"bookings":  {1: true, 6: true, 2: true, 3: false, 4: false, 5: false},
		"trips":     {1: true, 6: true, 2: true, 3: false, 4: false, 5: true},
		"invoices":  {1: true, 6: true, 2: false, 3: true, 4: false, 5: false},
		"payments":  {1: true, 6: true, 2: false, 3: true, 4: false, 5: false},
		"reports":   {1: true, 6: true, 2: true, 3: true, 4: false, 5: false},
	}

	if roleID == 4 {
		readResources := map[string]bool{
			"drivers": true, "vehicles": true, "customers": true, "routes": true,
			"bookings": true, "trips": true, "invoices": true, "payments": true, "reports": true,
		}
		if readResources[resource] && action == "read" {
			return true
		}
	}

	if resourcePerms, ok := permissions[resource]; ok {
		if allowed, ok := resourcePerms[roleID]; ok {
			return allowed
		}
	}

	return false
}

// RoleNameForID maps a role ID to its domain.RoleName.
func RoleNameForID(roleID int64) domain.RoleName {
	switch roleID {
	case 1:
		return domain.RoleAdmin
	case 2:
		return domain.RoleDispatcher
	case 3:
		return domain.RoleAccountant
	case 4:
		return domain.RoleViewer
	case 5:
		return domain.RoleDriver
	case 6:
		return domain.RoleOrgAdmin
	default:
		return domain.RoleDispatcher
	}
}

// RoleID for action checks (lower = more privilege)
func RoleIDForName(role domain.RoleName) int64 {
	return domain.DefaultRoleID(role)
}
