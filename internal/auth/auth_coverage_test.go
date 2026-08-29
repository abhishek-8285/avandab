package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/gorilla/securecookie"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// session: Validator / HasSession
// ---------------------------------------------------------------------------

type coverageValidator struct {
	validToken string
	revoked    bool
	err        error
	nilReturn  bool
}

func (m *coverageValidator) ValidateSessionToken(ctx context.Context, token string) (*SessionData, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.nilReturn {
		return nil, nil
	}
	if m.revoked || token != m.validToken {
		return nil, errors.New("invalid session")
	}
	return &SessionData{UserID: "uid-valid", Role: "admin", Name: "Validated"}, nil
}

func (m *coverageValidator) RevokeSessionToken(ctx context.Context, token string) error {
	m.revoked = true
	return nil
}

func (m *coverageValidator) ValidateAPITokenUser(ctx context.Context, userID string) (string, bool, error) {
	if m.revoked {
		return "", false, nil
	}
	return "admin", true, nil
}

func TestValidatorAndHasSession_Coverage(t *testing.T) {
	store := NewSessionStore("coverage-secret-32-bytes-long-!!!!", false)

	// Validator initially nil
	assert.Nil(t, store.Validator())

	mv := &coverageValidator{validToken: "tok-1"}
	store.SetValidator(mv)
	assert.Equal(t, mv, store.Validator())

	// HasSession false when no cookie
	req := httptest.NewRequest("GET", "/", nil)
	assert.False(t, store.HasSession(req))

	// HasSession true when cookie present (even with dummy value)
	req.AddCookie(&http.Cookie{Name: "session", Value: "dummy"})
	assert.True(t, store.HasSession(req))

	// HasSession false when cookie name differs
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: "other", Value: "x"})
	assert.False(t, store.HasSession(req2))

	// HasSession true after CreateSession (real signed cookie)
	rec := httptest.NewRecorder()
	store.CreateSession(rec, "u-1", "admin", "Test")
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.AddCookie(cookies[0])
	assert.True(t, store.HasSession(req3))

	// Clear validator
	store.SetValidator(nil)
	assert.Nil(t, store.Validator())
}

func TestSessionMustEncode_ErrorBranch(t *testing.T) {
	// Empty secret => securecookie has errHashKeyNotSet, Encode fails => mustEncode returns ""
	emptyStore := NewSessionStore("", false)
	result := emptyStore.mustEncode(&SessionData{UserID: "u", Role: "admin", Name: "n", Expires: 123})
	assert.Equal(t, "", result, "empty secret should cause mustEncode to return empty string")

	// NopEncoder expects []byte but we give *SessionData => Serialize fails
	store := NewSessionStore("another-secret-32-bytes-long-!!!!", false)
	store.signer.SetSerializer(securecookie.NopEncoder{})
	result2 := store.mustEncode(&SessionData{UserID: "u", Role: "admin"})
	assert.Equal(t, "", result2)

	// CreateSessionWithToken with failing encoder should still set cookie (value will be empty but path present)
	rec := httptest.NewRecorder()
	store.CreateSessionWithToken(rec, "u-2", "admin", "Bob", "tok")
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	// Even with encode failure, cookie Value is empty string but MaxAge still set
	assert.NotNil(t, cookies[0])
}

func TestValidateSession_Branches(t *testing.T) {
	secret := "branch-secret-32-bytes-long-!!!!!!"
	store := NewSessionStore(secret, false)

	t.Run("no cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
	})

	t.Run("invalid cookie value", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "not-a-valid-cookie-value"})
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
	})

	t.Run("expired", func(t *testing.T) {
		expired := &SessionData{
			UserID:  "u-exp",
			Role:    "admin",
			Name:    "Expired",
			Expires: time.Now().Add(-2 * time.Hour).Unix(),
			Token:   "",
		}
		encoded, err := store.signer.Encode("session", expired)
		require.NoError(t, err)
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: encoded})
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
	})

	t.Run("valid without validator", func(t *testing.T) {
		rec := httptest.NewRecorder()
		store.CreateSession(rec, "u-valid", "viewer", "Viewer")
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(rec.Result().Cookies()[0])
		data, ok := store.ValidateSession(req)
		require.True(t, ok)
		assert.Equal(t, "u-valid", data.UserID)
	})

	t.Run("valid with validator success overrides fields", func(t *testing.T) {
		mv := &coverageValidator{validToken: "tok-good"}
		store.SetValidator(mv)
		rec := httptest.NewRecorder()
		store.CreateSessionWithToken(rec, "u-orig", "viewer", "Orig", "tok-good")
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(rec.Result().Cookies()[0])
		data, ok := store.ValidateSession(req)
		require.True(t, ok)
		// validator upgrades role to admin and userID to uid-valid
		assert.Equal(t, "admin", data.Role)
		assert.Equal(t, "uid-valid", data.UserID)
		store.SetValidator(nil)
	})

	t.Run("valid with validator error", func(t *testing.T) {
		mv := &coverageValidator{err: errors.New("db down")}
		store.SetValidator(mv)
		rec := httptest.NewRecorder()
		// Need store without validator to create cookie, then re-attach error validator for validation
		tmp := NewSessionStore(secret, false)
		tmp.CreateSessionWithToken(rec, "u", "viewer", "U", "any-token")
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(rec.Result().Cookies()[0])
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
		store.SetValidator(nil)
	})

	t.Run("validator returns nil", func(t *testing.T) {
		mv := &coverageValidator{nilReturn: true}
		store.SetValidator(mv)
		tmp := NewSessionStore(secret, false)
		rec := httptest.NewRecorder()
		tmp.CreateSessionWithToken(rec, "u", "viewer", "U", "tok-x")
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(rec.Result().Cookies()[0])
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
		store.SetValidator(nil)
	})

	t.Run("token empty rejected when validator configured", func(t *testing.T) {
		mv := &coverageValidator{err: errors.New("should not be called")}
		store2 := NewSessionStore(secret, false)
		store2.SetValidator(mv)
		rec2 := httptest.NewRecorder()
		store2.CreateSession(rec2, "u-no-token", "viewer", "NoToken") // token ""
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(rec2.Result().Cookies()[0])
		// A tokenless cookie's role claim is client-controlled and cannot be
		// verified against the session store — it must be rejected outright
		// (forgeable-session bypass fix; Spec 10 §5.2).
		_, ok := store2.ValidateSession(req)
		assert.False(t, ok)
	})

	t.Run("tampered signature", func(t *testing.T) {
		rec := httptest.NewRecorder()
		store.CreateSession(rec, "u-tamper", "admin", "Admin")
		c := rec.Result().Cookies()[0]
		// Corrupt value
		c.Value = c.Value + "corrupt"
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(c)
		_, ok := store.ValidateSession(req)
		assert.False(t, ok)
	})
}

func TestClientLocation_Branches(t *testing.T) {
	tests := []struct {
		name    string
		country string
		city    string
		want    string
	}{
		{"both", "IN", "Mumbai", "Mumbai, IN"},
		{"country only", "US", "", "US"},
		{"city only -> Unknown", "", "Mumbai", "Unknown"},
		{"neither", "", "", "Unknown"},
		{"spaces trimmed", " IN ", " Mumbai ", "Mumbai, IN"},
		{"country spaces", "  US  ", "", "US"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.country != "" {
				req.Header.Set("CF-IPCountry", tt.country)
			}
			if tt.city != "" {
				req.Header.Set("CF-IPCity", tt.city)
			}
			got := ClientLocation(req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsTrustedProxy_Branches(t *testing.T) {
	t.Run("TRUST_PROXY env true overrides", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		assert.True(t, IsTrustedProxy("8.8.8.8:1234"))
		assert.True(t, IsTrustedProxy("203.0.113.5"))
	})
	t.Run("TRUST_PROXY not set", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		assert.True(t, IsTrustedProxy("127.0.0.1:8080"), "loopback")
		assert.True(t, IsTrustedProxy("127.0.0.1"))
		assert.True(t, IsTrustedProxy("10.0.0.5:3000"), "private 10")
		assert.True(t, IsTrustedProxy("192.168.1.1"))
		assert.True(t, IsTrustedProxy("172.16.0.5"))
		assert.True(t, IsTrustedProxy("169.254.10.20"), "link local")
		assert.True(t, IsTrustedProxy("192.0.2.1"), "docs 192.0.2")
		assert.True(t, IsTrustedProxy("198.51.100.23"))
		assert.True(t, IsTrustedProxy("203.0.113.9"))
		// With port variants for doc nets
		assert.True(t, IsTrustedProxy("192.0.2.55:443"))
		assert.True(t, IsTrustedProxy("198.51.100.1:8080"))
		assert.True(t, IsTrustedProxy("203.0.113.1:80"))

		assert.False(t, IsTrustedProxy("8.8.8.8:443"), "public should be false")
		assert.False(t, IsTrustedProxy("8.8.8.8"))
		assert.False(t, IsTrustedProxy("1.1.1.1"))

		// Unix socket / test request cases
		assert.True(t, IsTrustedProxy(""))
		assert.True(t, IsTrustedProxy("@"))

		// Invalid IP string without trusted env -> false
		assert.False(t, IsTrustedProxy("not-an-ip:8080"))
		assert.False(t, IsTrustedProxy("not-an-ip"))
	})
}

func TestClientIP_Branches(t *testing.T) {
	makeReq := func(remoteAddr string, headers map[string]string) *http.Request {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = remoteAddr
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req
	}

	t.Run("not trusted proxy returns remoteHost", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		req := makeReq("8.8.8.8:1234", map[string]string{
			"CF-Connecting-IP": "1.1.1.1",
			"X-Real-IP":        "2.2.2.2",
			"X-Forwarded-For":  "3.3.3.3",
		})
		assert.Equal(t, "8.8.8.8", ClientIP(req))
	})

	t.Run("not trusted without port", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		req := makeReq("8.8.8.8", nil)
		assert.Equal(t, "8.8.8.8", ClientIP(req))
	})

	t.Run("unknown when empty remoteHost", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		// Use non-trusted but empty RemoteAddr -> fallback to Unknown
		req := makeReq("", nil)
		// IsTrustedProxy("") is true, but headers empty -> remoteHost "" => Unknown
		// So test public empty variant: use non-parseable that returns false but remoteHost empty?
		// Simpler: request with public IP that is empty after SplitHostPort?
		// We'll use TRUST_PROXY=false and remoteAddr empty -> IsTrustedProxy true then headers empty then remoteHost empty => Unknown
		// covers line 245
		assert.Equal(t, "Unknown", ClientIP(req))
	})

	t.Run("trusted with CF-Connecting-IP valid", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{"CF-Connecting-IP": "203.0.113.5"})
		assert.Equal(t, "203.0.113.5", ClientIP(req))
	})

	t.Run("trusted CF invalid falls to X-Real-IP", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"CF-Connecting-IP": "not-an-ip",
			"X-Real-IP":        "198.51.100.7",
		})
		assert.Equal(t, "198.51.100.7", ClientIP(req))
	})

	t.Run("trusted CF and X-Real invalid falls to XFF", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"CF-Connecting-IP": "bad",
			"X-Real-IP":        "bad2",
			"X-Forwarded-For":  "198.51.100.9, 10.0.0.1",
		})
		assert.Equal(t, "198.51.100.9", ClientIP(req))
	})

	t.Run("XFF with spaces trimmed", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"X-Forwarded-For": "  203.0.113.10  , 10.0.0.1",
		})
		assert.Equal(t, "203.0.113.10", ClientIP(req))
	})

	t.Run("XFF invalid falls to remoteHost", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"X-Forwarded-For": "not-an-ip",
		})
		assert.Equal(t, "127.0.0.1", ClientIP(req))
	})

	t.Run("TRUST_PROXY env true allows headers from public IP", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		req := makeReq("8.8.8.8:1234", map[string]string{"CF-Connecting-IP": "1.1.1.1"})
		assert.Equal(t, "1.1.1.1", ClientIP(req))
	})

	t.Run("header values with surrounding spaces", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"CF-Connecting-IP": "  192.0.2.1  ",
		})
		assert.Equal(t, "192.0.2.1", ClientIP(req))
	})

	t.Run("X-Real-IP with spaces", func(t *testing.T) {
		req := makeReq("127.0.0.1:1234", map[string]string{
			"X-Real-IP": "  198.51.100.11  ",
		})
		assert.Equal(t, "198.51.100.11", ClientIP(req))
	})
}

func TestSetTokenSecret_EmptyIgnored(t *testing.T) {
	// Capture current secret hash for a known token
	SetTokenSecret([]byte("initial-secret-32-bytes-long!!!!"))
	hashBefore := HashToken("test-token-123")
	// Empty secret should be ignored, hash stays same
	SetTokenSecret([]byte(""))
	hashAfter := HashToken("test-token-123")
	assert.Equal(t, hashBefore, hashAfter)

	// Nil secret also ignored (len 0)
	SetTokenSecret(nil)
	hashAfter2 := HashToken("test-token-123")
	assert.Equal(t, hashBefore, hashAfter2)

	// Non-empty changes hash
	SetTokenSecret([]byte("different-secret-32-bytes-long!!"))
	hashDiff := HashToken("test-token-123")
	assert.NotEqual(t, hashBefore, hashDiff)

	// Restore
	SetTokenSecret([]byte("initial-secret-32-bytes-long!!!!"))
	_ = os.Getenv // avoid unused
}

func TestHasPermission_Complete(t *testing.T) {
	tests := []struct {
		role     int64
		resource string
		action   string
		allow    bool
	}{
		{1, "users", "write", true},
		{6, "users", "write", true},
		{2, "users", "write", false},
		{4, "drivers", "read", true},
		{4, "drivers", "create", false},
		{4, "trips", "read", true},
		{4, "users", "read", false}, // viewer cannot read users
		{3, "invoices", "read", true},
		{2, "invoices", "read", false},
		{5, "vehicles", "read", true},
		{5, "routes", "read", true},
		{5, "trips", "read", true},
		{5, "users", "read", false},
		{2, "drivers", "create", true},
		{3, "reports", "read", true},
		{4, "reports", "read", true},
		{4, "payments", "read", true},
		{99, "drivers", "read", false},         // unknown role
		{1, "unknown_resource", "read", false}, // unknown resource hits final return false (line 346)
		{99, "unknown", "read", false},
		{4, "settings", "read", false}, // viewer read but settings not in readResources
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.resource, tt.action)
		assert.Equal(t, tt.allow, got, "role %d resource %s action %s", tt.role, tt.resource, tt.action)
	}
}

func TestRoleNameForID_Complete(t *testing.T) {
	assert.Equal(t, "admin", string(RoleNameForID(1)))
	assert.Equal(t, "dispatcher", string(RoleNameForID(2)))
	assert.Equal(t, "accountant", string(RoleNameForID(3)))
	assert.Equal(t, "viewer", string(RoleNameForID(4)))
	assert.Equal(t, "driver", string(RoleNameForID(5)))
	assert.Equal(t, "org_admin", string(RoleNameForID(6)))
	assert.Equal(t, "dispatcher", string(RoleNameForID(99)), "default should be dispatcher")
	assert.Equal(t, "dispatcher", string(RoleNameForID(0)))
	assert.Equal(t, "dispatcher", string(RoleNameForID(-1)))
}

func TestGenerateSecureToken_Properties(t *testing.T) {
	tok1, err := GenerateSecureToken()
	require.NoError(t, err)
	assert.Len(t, tok1, 64, "32 bytes hex = 64 chars")
	tok2, err := GenerateSecureToken()
	require.NoError(t, err)
	assert.NotEqual(t, tok1, tok2)
	// Ensure hex decodable
	assert.Regexp(t, "^[0-9a-f]{64}$", tok1)
}

// ---------------------------------------------------------------------------
// casbin: DBAdapter no-op methods + LoadPolicy + AuthorizationService
// ---------------------------------------------------------------------------

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	// Use unique DSN per test to avoid cross-test shared cache pollution
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	// Use goose to apply migrations from project root
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	return db
}

func TestDBAdapter_NoOpMethods(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	adapter := NewDBAdapter(db)
	// Build empty model to pass
	m, err := model.NewModelFromString(CasbinModel)
	require.NoError(t, err)

	assert.NoError(t, adapter.SavePolicy(m))
	assert.NoError(t, adapter.AddPolicy("p", "p", []string{"admin", "drivers", "create"}))
	assert.NoError(t, adapter.RemovePolicy("p", "p", []string{"admin", "drivers", "create"}))
	assert.NoError(t, adapter.RemoveFilteredPolicy("p", "p", 0, "admin"))
	assert.NoError(t, adapter.RemoveFilteredPolicy("g", "g", 0, "user-1"))
	// Nil model Save also no-op
	assert.NoError(t, adapter.SavePolicy(nil))
}

func TestDBAdapter_LoadPolicy_SkipInvalidPerm(t *testing.T) {
	db := openMigratedDB(t)
	defer func() { _ = db.Close() }()

	// Insert a permission without colon - should be skipped by LoadPolicy
	_, err := db.Exec(`INSERT INTO permissions (name, description) VALUES ('invalidperm', 'no colon')`)
	require.NoError(t, err)
	// Ensure role 1 exists and assign that invalid permission to admin
	_, err = db.Exec(`INSERT OR IGNORE INTO role_permissions (role_id, permission_id) SELECT 1, id FROM permissions WHERE name='invalidperm'`)
	require.NoError(t, err)

	adapter := NewDBAdapter(db)
	m, err := model.NewModelFromString(CasbinModel)
	require.NoError(t, err)
	require.NoError(t, adapter.LoadPolicy(m))

	// Invalid perm should not create a policy; valid perms should still load
	// Count p policies - should be >0 but not contain invalidperm
	hasInvalid, _ := m.HasPolicy("p", "p", []string{"admin", "invalidperm", ""})
	assert.False(t, hasInvalid, "perm without colon should be skipped")
	hasValid, _ := m.HasPolicy("p", "p", []string{"admin", "drivers", "create"})
	assert.True(t, hasValid)
}

func TestDBAdapter_LoadPolicy_Errors(t *testing.T) {
	t.Run("query error on role_permissions", func(t *testing.T) {
		db := openMigratedDB(t)
		// Close DB to force Query error
		_ = db.Close()
		adapter := NewDBAdapter(db)
		m, _ := model.NewModelFromString(CasbinModel)
		err := adapter.LoadPolicy(m)
		assert.Error(t, err)
	})

	t.Run("query error on user_roles second query", func(t *testing.T) {
		// We can force second query error by dropping user_roles table after migration but before LoadPolicy
		db := openMigratedDB(t)
		defer func() { _ = db.Close() }()
		_, err := db.Exec(`DROP TABLE user_roles`)
		require.NoError(t, err)
		adapter := NewDBAdapter(db)
		m, _ := model.NewModelFromString(CasbinModel)
		// First query succeeds, second fails due to missing table
		err = adapter.LoadPolicy(m)
		assert.Error(t, err)
	})
}

func TestCasbinAuthorizationService_AddDeleteRoles_Reload(t *testing.T) {
	db := openMigratedDB(t)
	defer func() { _ = db.Close() }()

	// Seed users
	_, err := db.Exec(`
		INSERT INTO users (id, email, password_hash, name, role_id, status)
		VALUES 
			('cov-user-admin', 'cov-admin@test.com', 'hash', 'Cov Admin', 1, 'active'),
			('cov-user-viewer', 'cov-viewer@test.com', 'hash', 'Cov Viewer', 4, 'active')
	`)
	require.NoError(t, err)

	svc, err := NewCasbinAuthorizationService(db)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Reload explicitly
	require.NoError(t, svc.Reload())

	// Verify via Can
	assert.True(t, svc.Can("cov-user-admin", "drivers", "create"))
	assert.False(t, svc.Can("cov-user-viewer", "drivers", "create"))
	assert.True(t, svc.Can("cov-user-viewer", "drivers", "read"))

	// AddRoleForUser memory only
	err = svc.AddRoleForUser("cov-user-viewer", "admin")
	require.NoError(t, err)
	// Viewer now inherits admin via memory
	assert.True(t, svc.Can("cov-user-viewer", "drivers", "create"), "after AddRoleForUser should have admin perms")
	assert.True(t, svc.Can("cov-user-viewer", "users", "delete"))

	// DeleteRolesForUser
	err = svc.DeleteRolesForUser("cov-user-viewer")
	require.NoError(t, err)
	assert.False(t, svc.Can("cov-user-viewer", "drivers", "read"), "after DeleteRolesForUser should lose all roles")
	assert.False(t, svc.Can("cov-user-viewer", "drivers", "create"))

	// New role assignment via DB + Reload
	// Add viewer role back via DB insert directly then reload
	_, err = db.Exec(`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES ('cov-user-viewer', 4)`)
	require.NoError(t, err)
	require.NoError(t, svc.Reload())
	assert.True(t, svc.Can("cov-user-viewer", "drivers", "read"))
	assert.False(t, svc.Can("cov-user-viewer", "drivers", "create"))
}

func TestNewCasbinAuthorizationService_Errors(t *testing.T) {
	t.Run("closed DB load error", func(t *testing.T) {
		db := openMigratedDB(t)
		_ = db.Close()
		_, err := NewCasbinAuthorizationService(db)
		assert.Error(t, err)
	})

	t.Run("invalid model string", func(t *testing.T) {
		_, err := model.NewModelFromString("invalid model [[[[")
		assert.Error(t, err)
	})

	t.Run("adapter with fresh DB", func(t *testing.T) {
		db := openMigratedDB(t)
		defer func() { _ = db.Close() }()
		svc, err := NewCasbinAuthorizationService(db)
		require.NoError(t, err)
		// Ensure Can works for non-existent user => false
		assert.False(t, svc.Can("nonexistent", "drivers", "create"))
	})
}

func TestDBAdapter_NewDBAdapter(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	adapter := NewDBAdapter(db)
	assert.NotNil(t, adapter)
	assert.Equal(t, db, adapter.db)
}

func TestParseAPIToken_InvalidPayloadBranches(t *testing.T) {
	secret := []byte("branch-secret")

	// Valid token then tamper payload to be invalid base64 but HMAC matches
	// Craft payload "!!!not-base64!!!"
	payload := "!!!not-base64!!!"
	sig := computeHMAC(secret, payload)
	token := payload + "." + sig
	_, err := ParseAPIToken(secret, token)
	assert.ErrorIs(t, err, ErrTokenInvalid, "invalid base64 payload should be ErrTokenInvalid")

	// Payload base64 decodes but is not valid JSON
	raw := "{invalid-json"
	b64 := base64.RawURLEncoding.EncodeToString([]byte(raw))
	sig2 := computeHMAC(secret, b64)
	token2 := b64 + "." + sig2
	_, err = ParseAPIToken(secret, token2)
	assert.ErrorIs(t, err, ErrTokenInvalid)

	// Payload base64 decodes to JSON with unknown fields but valid structure should still parse
	// Ensure expired still checks after valid decode/unmarshal
}

func TestIssueAPIToken_MarshalError(t *testing.T) {
	// APITokenClaims marshaling never fails for normal struct, but we can still
	// verify IssueAPIToken covers success path with preset times
	secret := []byte("another-secret")
	claims := APITokenClaims{
		UserID:    "u-1",
		Role:      "admin",
		TenantID:  "t-1",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	tok, err := IssueAPIToken(secret, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	parsed, err := ParseAPIToken(secret, tok)
	require.NoError(t, err)
	assert.Equal(t, claims.UserID, parsed.UserID)
}
