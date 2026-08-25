package sqlite

import (
	"context"
	"database/sql"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/domain"
	"transport-app/internal/repository"
)

// Date-range search variants for the users list page calendar (optional
// interface asserted by UserService). Keeps the core UserRepository interface
// and its mocks untouched.

const userDateClause = `
  AND (? = '' OR date(substr(u.created_at,1,10)) >= date(?))
  AND (? = '' OR date(substr(u.created_at,1,10)) <= date(?))`

// query runs a raw multi-row query, picking up the active transaction from
// context when present (mirrors exec/queryRow helpers).
func (r *SQLRepository) query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx.QueryContext(ctx, query, args...)
	}
	return r.db.QueryContext(ctx, query, args...)
}

// SearchUsersDateRange mirrors SearchUsers with a created_at window filter.
func (r *SQLRepository) SearchUsersDateRange(ctx context.Context, query string, status string, from string, to string, limit int, offset int) ([]repository.UserWithRole, error) {
	rows, err := r.query(ctx, `
SELECT u.id, u.email, u.password_hash, u.name, u.phone, u.role_id, u.status,
       u.last_login_at, u.theme_preference, u.created_at, u.updated_at,
       r.name AS role_name
FROM users u
JOIN roles r ON u.role_id = r.id
WHERE (? = '' OR u.name LIKE '%' || ? || '%' OR u.email LIKE '%' || ? || '%')
  AND (? = '' OR u.status = ?)`+userDateClause+`
ORDER BY u.created_at DESC
LIMIT ? OFFSET ?`,
		query, query, query,
		status, status,
		from, from, to, to,
		int64(limit), int64(offset),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.UserWithRole, 0)
	for rows.Next() {
		var row db.SearchUsersRow
		if err := rows.Scan(
			&row.ID, &row.Email, &row.PasswordHash, &row.Name, &row.Phone,
			&row.RoleID, &row.Status, &row.LastLoginAt, &row.ThemePreference,
			&row.CreatedAt, &row.UpdatedAt, &row.RoleName,
		); err != nil {
			return nil, err
		}
		result = append(result, searchUserRowToWithRole(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountUsersDateRange counts users matching the same filters as SearchUsersDateRange.
func (r *SQLRepository) CountUsersDateRange(ctx context.Context, query string, status string, from string, to string) (int64, error) {
	var count int64
	err := r.queryRow(ctx, `
SELECT COUNT(*)
FROM users u
WHERE (? = '' OR u.name LIKE '%' || ? || '%' OR u.email LIKE '%' || ? || '%')
  AND (? = '' OR u.status = ?)`+userDateClause,
		query, query, query,
		status, status,
		from, from, to, to,
	).Scan(&count)
	return count, err
}

// searchUserRowToWithRole converts a generated SearchUsersRow into the domain type.
func searchUserRowToWithRole(row db.SearchUsersRow) repository.UserWithRole {
	return repository.UserWithRole{
		ID:           domain.UserID(row.ID),
		Email:        row.Email,
		PasswordHash: row.PasswordHash,
		Name:         row.Name,
		Phone:        fromNullString(row.Phone),
		RoleID:       row.RoleID,
		RoleName:     row.RoleName,
		Status:       row.Status,
		LastLoginAt:  fromNullTime(row.LastLoginAt),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
