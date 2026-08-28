package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"
	"transport-app/internal/repository"

	db "transport-app/db/generated/sqlite"
)

// RoleRepository implementation

func (r *SQLRepository) GetRoleByID(ctx context.Context, id int64) (domain.Role, error) {
	rl, err := r.Q(ctx).GetRoleByID(ctx, id)
	if err != nil {
		return domain.Role{}, err
	}
	return toDomainRole(rl), nil
}

func (r *SQLRepository) GetRoleByName(ctx context.Context, name domain.RoleName) (domain.Role, error) {
	rl, err := r.Q(ctx).GetRoleByName(ctx, string(name))
	if err != nil {
		return domain.Role{}, err
	}
	return toDomainRole(rl), nil
}

func (r *SQLRepository) ListRoles(ctx context.Context) ([]domain.Role, error) {
	roles, err := r.Q(ctx).ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Role, len(roles))
	for i, rl := range roles {
		result[i] = toDomainRole(rl)
	}
	return result, nil
}

// UserRepository implementation

func (r *SQLRepository) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	theme := user.ThemePreference
	if theme == "" {
		theme = "system"
	}
	created, err := r.Q(ctx).CreateUser(ctx, db.CreateUserParams{
		ID:              string(user.ID),
		Email:           user.Email,
		PasswordHash:    user.PasswordHash,
		Name:            user.Name,
		Phone:           nullString(user.Phone),
		RoleID:          user.Role.ID,
		Status:          string(user.Status),
		TenantID:        user.TenantID,
		ThemePreference: theme,
	})
	if err != nil {
		return domain.User{}, err
	}

	role, _ := r.Q(ctx).GetRoleByID(ctx, user.Role.ID)
	return toCreateUserRowWithRole(created, toDomainRole(role)), nil
}

func (r *SQLRepository) GetUserByID(ctx context.Context, id domain.UserID) (domain.User, error) {
	u, err := r.Q(ctx).GetUserByID(ctx, string(id))
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, u.RoleID)
	return toGetUserByIDRowWithRole(u, toDomainRole(role)), nil
}

func (r *SQLRepository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	u, err := r.Q(ctx).GetUserByEmail(ctx, email)
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, u.RoleID)
	return toGetUserByEmailRowWithRole(u, toDomainRole(role)), nil
}

func (r *SQLRepository) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	updated, err := r.Q(ctx).UpdateUser(ctx, db.UpdateUserParams{
		Email:  user.Email,
		Name:   user.Name,
		Phone:  nullString(user.Phone),
		RoleID: user.Role.ID,
		Status: string(user.Status),
		ID:     string(user.ID),
	})
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, updated.RoleID)
	return toUpdateUserRowWithRole(updated, toDomainRole(role)), nil
}

func (r *SQLRepository) UpdateUserPassword(ctx context.Context, userID domain.UserID, passwordHash string) (domain.User, error) {
	updated, err := r.Q(ctx).UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		PasswordHash: passwordHash,
		ID:           string(userID),
	})
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, updated.RoleID)
	return toUpdateUserPasswordRowWithRole(updated, toDomainRole(role)), nil
}

func (r *SQLRepository) UpdateUserThemePreference(ctx context.Context, userID domain.UserID, theme string) (domain.User, error) {
	updated, err := r.Q(ctx).UpdateUserThemePreference(ctx, db.UpdateUserThemePreferenceParams{
		ThemePreference: theme,
		ID:              string(userID),
	})
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, updated.RoleID)
	return toUpdateUserThemePreferenceRowWithRole(updated, toDomainRole(role)), nil
}

func (r *SQLRepository) UpdateUserLastLogin(ctx context.Context, userID domain.UserID) (domain.User, error) {
	updated, err := r.Q(ctx).UpdateUserLastLogin(ctx, string(userID))
	if err != nil {
		return domain.User{}, err
	}
	role, _ := r.Q(ctx).GetRoleByID(ctx, updated.RoleID)
	return toUpdateUserLastLoginRowWithRole(updated, toDomainRole(role)), nil
}

func (r *SQLRepository) DeleteUser(ctx context.Context, userID domain.UserID) error {
	return r.Q(ctx).DeleteUser(ctx, db.DeleteUserParams{
		ID:       string(userID),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchUsers(ctx context.Context, query string, status string, limit, offset int, tenantID string) ([]repository.UserWithRole, error) {
	rows, err := r.Q(ctx).SearchUsers(ctx, db.SearchUsersParams{
		TenantID: tenantID,
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  status,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}

	result := make([]repository.UserWithRole, len(rows))
	for i, row := range rows {
		result[i] = repository.UserWithRole{
			ID:          domain.UserID(row.ID),
			Email:       row.Email,
			Name:        row.Name,
			Phone:       fromNullString(row.Phone),
			RoleID:      row.RoleID,
			RoleName:    row.RoleName,
			Status:      row.Status,
			LastLoginAt: fromNullTime(row.LastLoginAt),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
	}
	return result, nil
}

func (r *SQLRepository) CountUsers(ctx context.Context, query string, status string, tenantID string) (int64, error) {
	count, err := r.Q(ctx).CountUsers(ctx, db.CountUsersParams{
		TenantID: tenantID,
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  status,
		Status:   status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// SessionRepository implementation

func (r *SQLRepository) CreateSession(ctx context.Context, session domain.Session) (domain.Session, error) {
	created, err := r.Q(ctx).CreateSession(ctx, db.CreateSessionParams{
		ID:        string(session.ID),
		UserID:    string(session.UserID),
		TokenHash: session.TokenHash,
		ExpiresAt: session.ExpiresAt,
		UserAgent: nullString(session.UserAgent),
		IpAddress: nullString(session.IPAddress),
	})
	if err != nil {
		return domain.Session{}, err
	}
	return toDomainSession(created), nil
}

func (r *SQLRepository) GetSessionByToken(ctx context.Context, tokenHash string) (repository.SessionWithUser, error) {
	s, err := r.Q(ctx).GetSessionByToken(ctx, tokenHash)
	if err != nil {
		return repository.SessionWithUser{}, err
	}
	return repository.SessionWithUser{
		ID:         domain.SessionID(s.ID),
		UserID:     domain.UserID(s.UserID),
		TokenHash:  s.TokenHash,
		ExpiresAt:  s.ExpiresAt,
		UserAgent:  fromNullString(s.UserAgent),
		IPAddress:  fromNullString(s.IpAddress),
		CreatedAt:  s.CreatedAt,
		UserEmail:  s.UserEmail,
		UserName:   s.UserName,
		RoleID:     s.RoleID,
		RoleName:   s.RoleName,
		UserStatus: s.UserStatus,
	}, nil
}

func (r *SQLRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	return r.Q(ctx).DeleteSession(ctx, tokenHash)
}

func (r *SQLRepository) DeleteExpiredSessions(ctx context.Context) error {
	return r.Q(ctx).DeleteExpiredSessions(ctx)
}
