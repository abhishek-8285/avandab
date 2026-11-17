package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	userdomain "transport-app/internal/domain/user"
	"transport-app/internal/repository"
)

// AuthService handles user authentication and session management.
type AuthService struct {
	baseService
}

// tenantActive rejects authentication for users whose tenant organization is
// not 'active'. A user whose tenant row is missing fails closed (ErrTenantMissing):
// a deleted or half-provisioned org must never authenticate.
func (s *AuthService) tenantActive(ctx context.Context, userID string) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return nil
	}
	row := getter.DB().QueryRowContext(ctx, `
		SELECT t.status
		FROM tenants t
		JOIN users u ON u.tenant_id = t.id
		WHERE u.id = $1`, userID)
	var status string
	if err := row.Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			// Fail closed: a user whose tenant row is absent must not log in.
			// The previous nil return treated a deleted/suspended-without-row
			// organization as legacy and allowed authentication.
			return auth.ErrTenantMissing
		}
		return err
	}
	if status != "active" {
		return auth.ErrTenantSuspended
	}
	return nil
}

const (
	loginMaxFailures = 5
	loginLockout     = 15 * time.Minute
	loginAttemptTTL  = 24 * time.Hour
)

// loginThrottleKey hashes the normalized email so raw addresses are not
// stored in the throttle table.
func loginThrottleKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// sessionTTL is the configured session lifetime for BOTH the cookie Max-Age
// (auth.SessionStore.SetMaxAge) and the DB session row. Defaults to 24h when
// the service has no config (tests/fakes) or SESSION_MAX_AGE was not set.
func (s *AuthService) sessionTTL() time.Duration {
	if s.cfg != nil && s.cfg.SessionMaxAge > 0 {
		return s.cfg.SessionMaxAge
	}
	return 24 * time.Hour
}

func (s *AuthService) loginThrottleDB() *sql.DB {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return nil
	}
	return getter.DB()
}

// loginLocked reports whether the account is inside a lockout window. A
// throttle storage error is logged and treated as unlocked: a database
// hiccup must not lock every operator out, and per-IP limits still apply.
func (s *AuthService) loginLocked(ctx context.Context, email string) bool {
	db := s.loginThrottleDB()
	if db == nil {
		return false
	}
	var lockedUntil sql.NullTime
	err := db.QueryRowContext(ctx,
		"SELECT locked_until FROM login_throttle WHERE email_key = $1",
		loginThrottleKey(email)).Scan(&lockedUntil)
	if err != nil {
		if err != sql.ErrNoRows {
			s.log.Warn("login throttle lookup failed", "error", err)
		}
		return false
	}
	return lockedUntil.Valid && time.Now().Before(lockedUntil.Time)
}

// recordLoginFailure atomically increments the failure counter, locks after
// loginMaxFailures, and expires stale counters after loginAttemptTTL.
//
// The increment is a single upsert with RETURNING: the previous
// select-then-write lost increments under concurrent attempts, so a parallel
// credential-stuffing burst (the exact M3 threat model) could exceed the
// failure budget without ever tripping the lock.
func (s *AuthService) recordLoginFailure(ctx context.Context, email string) {
	db := s.loginThrottleDB()
	if db == nil {
		return
	}
	key := loginThrottleKey(email)
	now := time.Now()

	var failures int
	err := db.QueryRowContext(ctx, `
		INSERT INTO login_throttle (email_key, failures, locked_until, updated_at)
		VALUES ($1, 1, NULL, $2)
		ON CONFLICT (email_key) DO UPDATE SET
		  failures = CASE
		    WHEN login_throttle.updated_at < $3 THEN 1
		    ELSE login_throttle.failures + 1
		  END,
		  updated_at = $2
		RETURNING failures`, key, now, now.Add(-loginAttemptTTL)).Scan(&failures)
	if err != nil {
		s.log.Warn("login throttle write failed", "error", err)
		return
	}
	if failures >= loginMaxFailures {
		if _, err := db.ExecContext(ctx,
			"UPDATE login_throttle SET locked_until = $1 WHERE email_key = $2", now.Add(loginLockout), key); err != nil {
			s.log.Warn("login throttle lock failed", "error", err)
		}
	}

	// Opportunistic cleanup of stale counters (indexed by updated_at). A
	// failure here must not break login, but it must not vanish silently.
	if _, err := db.ExecContext(ctx, "DELETE FROM login_throttle WHERE updated_at < $1", now.Add(-loginAttemptTTL)); err != nil {
		s.log.Warn("login throttle cleanup failed", "error", err)
	}
}

func (s *AuthService) clearLoginFailures(ctx context.Context, email string) {
	db := s.loginThrottleDB()
	if db == nil {
		return
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM login_throttle WHERE email_key = $1", loginThrottleKey(email)); err != nil {
		s.log.Warn("login throttle clear failed", "error", err)
	}
}

// LoginRequest contains the credentials for login.
type LoginRequest struct {
	Email    string
	Password string
	Remember bool
}

// LoginResult contains the result of a successful login.
type LoginResult struct {
	User         domain.User
	SessionToken string
}

// Login authenticates a user by email and password, creating a session.
func (s *AuthService) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	if s.loginLocked(ctx, req.Email) {
		return nil, auth.ErrAccountLocked
	}

	user, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		s.recordLoginFailure(ctx, req.Email)
		s.log.Warn("login failed: invalid credentials")
		return nil, domain.ErrInvalidCredentials
	}

	if err := auth.CheckPassword(req.Password, user.PasswordHash); err != nil {
		s.recordLoginFailure(ctx, req.Email)
		s.log.Warn("login failed: invalid credentials", "user_id", user.ID)
		return nil, domain.ErrInvalidCredentials
	}

	if user.Status != domain.UserStatusActive {
		return nil, domain.ErrUnauthorized
	}

	if err := s.tenantActive(ctx, string(user.ID)); err != nil {
		return nil, err
	}

	// DPDP §6(4): withdrawn consent ceases processing — refuse login while
	// set. No ledger row (legacy/OAuth/admin users) stays allowed.
	if withdrawn, err := consentWithdrawn(ctx, s.consentDB(), user.TenantID, string(user.ID)); err != nil {
		return nil, err
	} else if withdrawn {
		return nil, auth.ErrConsentWithdrawn
	}

	s.clearLoginFailures(ctx, req.Email)

	// Update last login
	if _, err := s.store.UpdateUserLastLogin(ctx, user.ID); err != nil {
		s.log.Warn("failed to update last login", "user_id", user.ID, "error", err)
	}

	// Create session in database
	token, err := auth.GenerateSecureToken()
	if err != nil {
		return nil, err
	}
	tokenHash := auth.HashToken(token)

	session := domain.Session{
		ID:        domain.SessionID(generateID()),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(s.sessionTTL()),
	}
	if _, err := s.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	s.log.Info("user logged in", "user_id", user.ID, "email", user.Email)

	s.logAudit(ctx, &user.ID, "login", "users", string(user.ID), nil, nil)

	return &LoginResult{
		User:         user,
		SessionToken: token,
	}, nil
}

// CreateSessionForUser generates a new server-side session for a user.
func (s *AuthService) CreateSessionForUser(ctx context.Context, userID domain.UserID) (*LoginResult, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status != domain.UserStatusActive {
		return nil, domain.ErrUnauthorized
	}

	if err := s.tenantActive(ctx, string(user.ID)); err != nil {
		return nil, err
	}

	// Same DPDP gate as Login: withdrawn consent blocks session minting on
	// every path (password, OAuth, registration auto-login).
	if withdrawn, err := consentWithdrawn(ctx, s.consentDB(), user.TenantID, string(user.ID)); err != nil {
		return nil, err
	} else if withdrawn {
		return nil, auth.ErrConsentWithdrawn
	}

	token, err := auth.GenerateSecureToken()
	if err != nil {
		return nil, err
	}
	tokenHash := auth.HashToken(token)

	maxAge := s.sessionTTL()

	session := domain.Session{
		ID:        domain.SessionID(generateID()),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(maxAge),
	}
	if _, err := s.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	return &LoginResult{
		User:         user,
		SessionToken: token,
	}, nil
}

// ValidateSessionToken checks that the session token exists in DB, is unexpired, and belongs to an active user.
func (s *AuthService) ValidateSessionToken(ctx context.Context, token string) (*auth.SessionData, error) {
	tokenHash := auth.HashToken(token)
	sess, err := s.store.GetSessionByToken(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, domain.ErrUnauthorized
	}
	if sess.UserStatus != string(domain.UserStatusActive) {
		return nil, domain.ErrUnauthorized
	}
	return &auth.SessionData{
		UserID:  string(sess.UserID),
		Role:    sess.RoleName,
		Name:    sess.UserName,
		Expires: sess.ExpiresAt.Unix(),
		Token:   token,
	}, nil
}

// RevokeSessionToken deletes the session from the DB.
func (s *AuthService) RevokeSessionToken(ctx context.Context, token string) error {
	return s.Logout(ctx, token)
}

// ValidateAPITokenUser verifies that the API token's UserID exists and is active, returning the user's current live role.
func (s *AuthService) ValidateAPITokenUser(ctx context.Context, userID string) (string, bool, error) {
	user, err := s.store.GetUserByID(ctx, domain.UserID(userID))
	if err != nil {
		return "", false, err
	}
	if user.Status != domain.UserStatusActive {
		return "", false, nil
	}
	return string(user.Role.Name), true, nil
}

// Logout deletes a user's session.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	tokenHash := auth.HashToken(token)
	return s.store.DeleteSession(ctx, tokenHash)
}

// ChangePassword changes the user's password after verifying the old one.
func (s *AuthService) ChangePassword(ctx context.Context, userID domain.UserID, oldPassword, newPassword string) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.Status != domain.UserStatusActive {
		return domain.ErrUnauthorized
	}

	if err := auth.CheckPassword(oldPassword, user.PasswordHash); err != nil {
		return domain.ErrInvalidCredentials
	}

	if len(newPassword) < userdomain.MinPasswordLength {
		return domain.ErrWeakPassword
	}

	hashed, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}

	_, err = s.store.UpdateUserPassword(ctx, userID, hashed)
	return err
}

// GetProfile returns a user's profile.
func (s *AuthService) GetProfile(ctx context.Context, userID domain.UserID) (domain.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

// UpdateProfile updates a user's profile information.
func (s *AuthService) UpdateProfile(ctx context.Context, userID domain.UserID, name, phone, timezone string) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if user.Status != domain.UserStatusActive {
		return domain.User{}, domain.ErrUnauthorized
	}

	user.Name = sanitizeName(name)
	if phone != "" {
		user.Phone = &phone
	} else {
		user.Phone = nil
	}
	if timezone != "" {
		user.Timezone = timezone
	}

	return s.store.UpdateUser(ctx, user)
}

// VerifySession verifies a session token and returns the associated user.
func (s *AuthService) VerifySession(ctx context.Context, token string) (*domain.User, error) {
	tokenHash := auth.HashToken(token)
	session, err := s.store.GetSessionByToken(ctx, tokenHash)
	if err != nil {
		return nil, domain.ErrSessionExpired
	}

	if time.Now().After(session.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, tokenHash)
		return nil, domain.ErrSessionExpired
	}

	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}

	return &user, nil
}
