package user

import (
	"context"
	"time"

	"transport-app/internal/domain/types"
)

// RoleRepository defines operations for user roles.
type RoleRepository interface {
	GetRoleByID(ctx context.Context, id int64) (Role, error)
	GetRoleByName(ctx context.Context, name RoleName) (Role, error)
	ListRoles(ctx context.Context) ([]Role, error)
}

// UserRepository defines operations for user management.
type UserRepository interface {
	CreateUser(ctx context.Context, user User) (User, error)
	GetUserByID(ctx context.Context, id types.UserID) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	UpdateUser(ctx context.Context, user User) (User, error)
	UpdateUserPassword(ctx context.Context, userID types.UserID, passwordHash string) (User, error)
	UpdateUserThemePreference(ctx context.Context, userID types.UserID, theme string) (User, error)
	UpdateUserLastLogin(ctx context.Context, userID types.UserID) (User, error)
	DeleteUser(ctx context.Context, userID types.UserID) error
	SearchUsers(ctx context.Context, query string, status string, limit, offset int, tenantID string) ([]UserWithRole, error)
	CountUsers(ctx context.Context, query string, status string, tenantID string) (int64, error)
}

// UserWithRole is a user with their role name joined.
type UserWithRole struct {
	ID           types.UserID
	Email        string
	PasswordHash string
	Name         string
	Phone        *string
	RoleID       int64
	RoleName     string
	Status       string
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SessionRepository defines operations for session management.
type SessionRepository interface {
	CreateSession(ctx context.Context, session types.Session) (types.Session, error)
	GetSessionByToken(ctx context.Context, tokenHash string) (SessionWithUser, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) error
}

// SessionWithUser is a session with the associated user info joined.
type SessionWithUser struct {
	ID         types.SessionID
	UserID     types.UserID
	TokenHash  string
	ExpiresAt  time.Time
	UserAgent  *string
	IPAddress  *string
	CreatedAt  time.Time
	UserEmail  string
	UserName   string
	RoleID     int64
	RoleName   string
	UserStatus string
}
