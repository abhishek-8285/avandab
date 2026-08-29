package user

import (
	"context"

	"transport-app/internal/domain/types"
)

// UserService defines the interface for user management operations.
type UserService interface {
	CreateUser(ctx context.Context, email, name, phone string, roleID int64, status UserStatus, tenantID string) (User, error)
	CreateUserWithPassword(ctx context.Context, email, name, phone, password string, roleID int64, status UserStatus, tenantID string) (User, error)
	GetUser(ctx context.Context, id types.UserID) (User, error)
	ListUsers(ctx context.Context, query, status string, limit, offset int) ([]UserWithRole, int64, error)
	UpdateUser(ctx context.Context, id types.UserID, email, name, phone string, roleID int64, status UserStatus) (User, error)
	DeleteUser(ctx context.Context, id types.UserID) error
	ListRoles(ctx context.Context) ([]Role, error)
	ResetPassword(ctx context.Context, id types.UserID) error
}

// AuthService defines the interface for authentication operations.
type AuthService interface {
	Login(ctx context.Context, email, password string) (*LoginResult, error)
	Logout(ctx context.Context, token string) error
	ChangePassword(ctx context.Context, userID types.UserID, oldPassword, newPassword string) error
	GetProfile(ctx context.Context, userID types.UserID) (User, error)
	UpdateProfile(ctx context.Context, userID types.UserID, name, phone, timezone string) (User, error)
	VerifySession(ctx context.Context, token string) (*User, error)
}

// LoginResult is the result of a successful login attempt.
type LoginResult struct {
	Session *types.Session
	User    User
}

// LoginRequest contains fields for authentication.
type LoginRequest struct {
	Email    string
	Password string
}

// Can checks if a user has permission for a resource action.
type CanFunc func(userID string, resource, action string) bool
