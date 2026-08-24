package user

import (
	"time"

	"transport-app/internal/domain/types"
)

// User represents an application user with authentication credentials.
type User struct {
	ID              types.UserID
	Email           string
	PasswordHash    string
	Name            string
	Phone           *string
	Timezone        string
	ThemePreference string
	Role            Role
	Status          UserStatus
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Role represents a user role with associated permissions.
type Role struct {
	ID          int64
	Name        RoleName
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UserStatus is the status of a user account.
type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusInactive  UserStatus = "inactive"
	UserStatusSuspended UserStatus = "suspended"
)

// RoleName is the name of a user role.
type RoleName string

const (
	RoleAdmin      RoleName = "admin"
	RoleOrgAdmin   RoleName = "org_admin"
	RoleDispatcher RoleName = "dispatcher"
	RoleAccountant RoleName = "accountant"
	RoleViewer     RoleName = "viewer"
	RoleDriver     RoleName = "driver"
	RoleCustomer   RoleName = "customer"
)

// DefaultRoleID returns the role ID for a given role name.
func DefaultRoleID(name RoleName) int64 {
	switch name {
	case RoleAdmin:
		return 1
	case RoleDispatcher:
		return 2
	case RoleAccountant:
		return 3
	case RoleViewer:
		return 4
	case RoleDriver:
		return 5
	case RoleOrgAdmin:
		return 6
	default:
		return 2
	}
}
