package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	userdomain "transport-app/internal/domain/user"
	"transport-app/internal/repository"
)

// UserService handles user management.
type UserService struct {
	baseService
}

// RegisterSelfServiceAccount provisions an account created through public
// self-registration using the first-run claim model: the first account on a
// deployment becomes its admin, every later registration is least-privilege
// viewer. The admin check and the insert run in one transaction, so two
// simultaneous registrations can never both win the claim. Returns the user
// and whether this registration claimed the admin role.
func (s *UserService) RegisterSelfServiceAccount(ctx context.Context, email, name, phone, password string) (domain.User, bool, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil || s.txManager == nil {
		return domain.User{}, false, fmt.Errorf("self-registration unavailable: storage does not support transactions")
	}
	rawDB := getter.DB()

	var created domain.User
	claimed := false
	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		claimed = false
		var row *sql.Row
		if tx := repository.TxFromContext(txCtx); tx != nil {
			row = tx.QueryRowContext(txCtx, `SELECT COUNT(*) FROM users WHERE role_id = 1`)
		} else {
			row = rawDB.QueryRowContext(txCtx, `SELECT COUNT(*) FROM users WHERE role_id = 1`)
		}
		var admins int
		if err := row.Scan(&admins); err != nil {
			return err
		}

		roleID := domain.DefaultRoleID(domain.RoleViewer)
		if admins == 0 {
			roleID = domain.DefaultRoleID(domain.RoleAdmin)
			claimed = true
		}

		u, err := s.CreateUserWithPassword(txCtx, email, name, phone, password, roleID, domain.UserStatusActive)
		if err != nil {
			return err
		}
		created = u
		return nil
	})
	if err != nil {
		return domain.User{}, false, err
	}
	if claimed && s.log != nil {
		s.log.Info("first-run claim: self-registered account became admin", "user_id", created.ID, "email", created.Email)
	}
	return created, claimed, nil
}

// generateTemporaryPassword returns a cryptographically random 16-character
// temporary password used for newly created or reset accounts.
func generateTemporaryPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CreateUser creates a new user with a randomly generated temporary password.
func (s *UserService) CreateUser(ctx context.Context, email, name, phone string, roleID int64, status domain.UserStatus) (domain.User, error) {
	if email == "" {
		return domain.User{}, domain.ErrUserEmailRequired
	}
	if phone == "" {
		return domain.User{}, domain.ErrUserPhoneRequired
	}

	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return domain.User{}, domain.ErrUserEmailExists
	}

	if _, err := s.store.GetRoleByID(ctx, roleID); err != nil {
		return domain.User{}, fmt.Errorf("invalid role")
	}

	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return domain.User{}, err
	}

	hashed, err := auth.HashPassword(tempPassword)
	if err != nil {
		return domain.User{}, err
	}

	user := domain.User{
		ID:           domain.UserID(generateID()),
		Email:        email,
		PasswordHash: hashed,
		Name:         sanitizeName(name),
		Phone:        &phone,
		Role:         domain.Role{ID: roleID},
		Status:       status,
	}

	created, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return domain.User{}, err
	}

	s.log.Info("user created", "user_id", created.ID, "email", created.Email)
	return created, nil
}

// CreateUserWithPassword creates a user with a specific password.
func (s *UserService) CreateUserWithPassword(ctx context.Context, email, name, phone, password string, roleID int64, status domain.UserStatus) (domain.User, error) {
	if email == "" {
		return domain.User{}, domain.ErrUserEmailRequired
	}
	if phone == "" {
		return domain.User{}, domain.ErrUserPhoneRequired
	}
	if len(password) < userdomain.MinPasswordLength {
		return domain.User{}, domain.ErrWeakPassword
	}

	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return domain.User{}, domain.ErrUserEmailExists
	}

	if _, err := s.store.GetRoleByID(ctx, roleID); err != nil {
		return domain.User{}, fmt.Errorf("invalid role")
	}

	hashed, err := auth.HashPassword(password)
	if err != nil {
		return domain.User{}, err
	}

	user := domain.User{
		ID:           domain.UserID(generateID()),
		Email:        email,
		PasswordHash: hashed,
		Name:         sanitizeName(name),
		Role:         domain.Role{ID: roleID},
		Status:       status,
	}

	if phone != "" {
		user.Phone = &phone
	}

	created, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return domain.User{}, err
	}

	s.log.Info("user created", "user_id", created.ID, "email", created.Email)
	return created, nil
}

// GetUser retrieves a user by ID.
func (s *UserService) GetUser(ctx context.Context, id domain.UserID) (domain.User, error) {
	return s.store.GetUserByID(ctx, id)
}

// GetUserByEmail retrieves a user by email.
func (s *UserService) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return s.store.GetUserByEmail(ctx, email)
}

// ListUsers retrieves users with search and pagination.
func (s *UserService) ListUsers(ctx context.Context, query, status string, limit, offset int) ([]repository.UserWithRole, int64, error) {
	users, err := s.store.SearchUsers(ctx, query, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountUsers(ctx, query, status)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UpdateUser updates an existing user.
func (s *UserService) UpdateUser(ctx context.Context, id domain.UserID, email, name, phone string, roleID int64, status domain.UserStatus) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, domain.ErrUserNotFound
	}

	existing, _ := s.store.GetUserByEmail(ctx, email)
	if existing.ID != user.ID && existing.Email == email {
		return domain.User{}, domain.ErrUserEmailExists
	}

	user.Email = email
	user.Name = sanitizeName(name)
	user.Role.ID = roleID
	user.Status = status
	if phone != "" {
		user.Phone = &phone
	} else {
		user.Phone = nil
	}

	updated, err := s.store.UpdateUser(ctx, user)
	if err != nil {
		return domain.User{}, err
	}

	s.log.Info("user updated", "user_id", id)
	return updated, nil
}

// DeleteUser deletes a user by ID.
func (s *UserService) DeleteUser(ctx context.Context, id domain.UserID) error {
	if err := s.store.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.log.Info("user deleted", "user_id", id)
	return nil
}

// ListRoles returns all roles.
func (s *UserService) ListRoles(ctx context.Context) ([]domain.Role, error) {
	return s.store.ListRoles(ctx)
}

// SetPasswordByEmail sets a new password for the account matching the given
// email. Used by the password-reset (forgot password) flow after a valid reset
// token is redeemed. The new password is validated against the policy.
func (s *UserService) SetPasswordByEmail(ctx context.Context, email, newPassword string) error {
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return domain.ErrUserNotFound
	}
	if err := auth.ValidatePassword(newPassword); err != nil {
		return err
	}
	hashed, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if _, err := s.store.UpdateUserPassword(ctx, user.ID, hashed); err != nil {
		return err
	}
	s.log.Info("password reset via token", "user_id", user.ID, "email", user.Email)
	return nil
}

// ResetPassword resets a user's password to a randomly generated temporary value.
func (s *UserService) ResetPassword(ctx context.Context, id domain.UserID) error {
	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return err
	}
	hashed, err := auth.HashPassword(tempPassword)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateUserPassword(ctx, id, hashed)
	return err
}

// UpdateThemePreference updates a user's theme preference ('light', 'dark', 'system').
func (s *UserService) UpdateThemePreference(ctx context.Context, id domain.UserID, theme string) (domain.User, error) {
	if theme != "light" && theme != "dark" && theme != "system" {
		return domain.User{}, fmt.Errorf("invalid theme preference: must be 'light', 'dark', or 'system'")
	}
	updated, err := s.store.UpdateUserThemePreference(ctx, id, theme)
	if err != nil {
		return domain.User{}, err
	}
	s.log.Info("theme preference updated", "user_id", id, "theme", theme)
	return updated, nil
}
