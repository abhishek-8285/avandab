package service

import (
	"context"
	"database/sql"
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
// not 'active'. A user with no tenant row is treated as legacy/default and
// allowed through.
func (s *AuthService) tenantActive(ctx context.Context, userID string) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return nil
	}
	row := getter.DB().QueryRowContext(ctx, `
		SELECT t.status
		FROM tenants t
		JOIN users u ON u.tenant_id = t.id
		WHERE u.id = ?`, userID)
	var status string
	if err := row.Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if status != "active" {
		return auth.ErrTenantSuspended
	}
	return nil
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
	user, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		s.log.Warn("login failed: invalid credentials")
		return nil, domain.ErrInvalidCredentials
	}

	if err := auth.CheckPassword(req.Password, user.PasswordHash); err != nil {
		s.log.Warn("login failed: invalid credentials", "user_id", user.ID)
		return nil, domain.ErrInvalidCredentials
	}

	if user.Status != domain.UserStatusActive {
		return nil, domain.ErrUnauthorized
	}

	if err := s.tenantActive(ctx, string(user.ID)); err != nil {
		return nil, err
	}

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
		ExpiresAt: time.Now().Add(24 * time.Hour),
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

	token, err := auth.GenerateSecureToken()
	if err != nil {
		return nil, err
	}
	tokenHash := auth.HashToken(token)

	maxAge := 24 * time.Hour
	if s.cfg != nil && s.cfg.SessionMaxAge > 0 {
		maxAge = s.cfg.SessionMaxAge
	}

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
