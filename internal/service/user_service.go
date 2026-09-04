package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/auth"
	"transport-app/internal/domain"
	userdomain "transport-app/internal/domain/user"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// UserService handles user management.
type UserService struct {
	baseService
}

// RegisterSelfServiceAccount provisions an account created through public
// self-registration with an isolated tenant organization. The registering
// user becomes the ORG admin (role_id = 6, org_admin) of their newly
// provisioned tenant — never the platform admin (role_id = 1). Platform
// powers (tenants:manage, suspend, MRR, cross-tenant access) stay with
// accounts provisioned as admin (bootstrap env, platform tenant creation).
// Returns isNewOwner=true so callers route the owner into onboarding and
// bind the org_admin session/Casbin role.
func (s *UserService) RegisterSelfServiceAccount(ctx context.Context, email, name, phone, password, companyName string) (domain.User, bool, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil || s.txManager == nil {
		return domain.User{}, false, fmt.Errorf("self-registration unavailable: storage does not support transactions")
	}
	rawDB := getter.DB()

	compName := strings.TrimSpace(companyName)
	if compName == "" {
		userName := strings.TrimSpace(name)
		if userName != "" {
			compName = userName + "'s Fleet"
		} else {
			compName = "My Fleet"
		}
	}

	var created domain.User
	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		// Reject duplicate email
		if _, err := s.store.GetUserByEmail(txCtx, email); err == nil {
			return domain.ErrUserEmailExists
		}

		baseSlug := suggestTenantSlug(compName)
		shortUID := generateID()[:8]
		tenantID := fmt.Sprintf("tenant_%s", shortUID)

		// Probe slug uniqueness
		var slugCount int
		var row *sql.Row
		if tx := repository.TxFromContext(txCtx); tx != nil {
			row = tx.QueryRowContext(txCtx, `SELECT COUNT(1) FROM tenants WHERE slug = ?`, baseSlug)
		} else {
			row = rawDB.QueryRowContext(txCtx, `SELECT COUNT(1) FROM tenants WHERE slug = ?`, baseSlug)
		}
		if err := row.Scan(&slugCount); err != nil {
			return err
		}

		slug := baseSlug
		if slugCount > 0 {
			slug = fmt.Sprintf("%s-%s", baseSlug, shortUID[:6])
		}

		// Provision isolated tenant organization
		if err := s.CreateTenant(txCtx, tenantID, compName, slug); err != nil {
			return fmt.Errorf("failed to create tenant: %w", err)
		}

		// Create user with RoleOrgAdmin (role_id = 6) in their new isolated tenant.
		// RoleAdmin (1) is the platform super-admin and must never be minted
		// by public self-registration — otherwise every tenant owner could
		// open /tenants, suspend other orgs, and mint global admins.
		roleID := domain.DefaultRoleID(domain.RoleOrgAdmin)
		u, err := s.CreateUserWithPassword(txCtx, email, name, phone, password, roleID, domain.UserStatusActive, tenantID)
		if err != nil {
			return err
		}
		created = u

		// Seed initial trial subscription if subscription table exists
		now := time.Now().UTC()
		trialEnd := now.Add(14 * 24 * time.Hour)
		subID := "sub_" + tenantID
		if tx := repository.TxFromContext(txCtx); tx != nil {
			_, _ = tx.ExecContext(txCtx, `
				INSERT OR IGNORE INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, trial_end, created_at, updated_at)
				VALUES (?, ?, 'STARTER', 'TRIAL', ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			`, subID, tenantID, now.Format(time.RFC3339), trialEnd.Format(time.RFC3339), trialEnd.Format(time.RFC3339))
		} else {
			_, _ = rawDB.ExecContext(txCtx, `
				INSERT OR IGNORE INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, trial_end, created_at, updated_at)
				VALUES (?, ?, 'STARTER', 'TRIAL', ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			`, subID, tenantID, now.Format(time.RFC3339), trialEnd.Format(time.RFC3339), trialEnd.Format(time.RFC3339))
		}

		return nil
	})
	if err != nil {
		return domain.User{}, false, err
	}
	if s.log != nil {
		s.log.Info("self-registered tenant and org admin created", "tenant_id", created.TenantID, "user_id", created.ID, "email", created.Email)
	}
	return created, true, nil
}

// suggestTenantSlug normalizes free text into a slug candidate: lowercase,
// [a-z0-9-], collapsed separators, trimmed, max 32 chars.
func suggestTenantSlug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, rn := range lower {
		if b.Len() >= 32 {
			break
		}
		switch {
		case rn >= 'a' && rn <= 'z' || rn >= '0' && rn <= '9':
			b.WriteRune(rn)
			prevDash = false
		case rn == ' ' || rn == '-' || rn == '_' || rn == '.' || rn == ',' || rn == '&' || rn == '/':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "fleet"
	}
	return out
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

// ResolveGoogleUser maps a verified Google identity (OIDC subject + verified
// email) onto an Avandab account:
//
//  1. google_sub already linked → return that account (no mutation).
//  2. Existing active password account with the same email → link the Google
//     identity (link-only UPDATE; an account already bound to a *different*
//     sub is rejected — no silent identity takeover).
//  3. No match → provision a new isolated tenant with the registrant as
//     org admin (same transactional path as password self-registration) and link.
//
// Returns (user, isNewTenantOwner, error). Suspended accounts are rejected
// with domain.ErrUnauthorized in every branch.
func (s *UserService) ResolveGoogleUser(ctx context.Context, googleSub, email, name string) (domain.User, bool, error) {
	googleSub = strings.TrimSpace(googleSub)
	email = strings.ToLower(strings.TrimSpace(email))
	if googleSub == "" || email == "" {
		return domain.User{}, false, fmt.Errorf("google identity requires sub and email")
	}

	// 1. Already-linked Google account.
	if u, found, err := s.getUserByGoogleSub(ctx, googleSub); err != nil {
		return domain.User{}, false, err
	} else if found {
		if u.Status != domain.UserStatusActive {
			return domain.User{}, false, domain.ErrUnauthorized
		}
		return u, false, nil
	}

	// 2. Same email, password account → link.
	existing, err := s.store.GetUserByEmail(ctx, email)
	if err == nil {
		if existing.Status != domain.UserStatusActive {
			return domain.User{}, false, domain.ErrUnauthorized
		}
		if err := s.linkGoogleSub(ctx, string(existing.ID), googleSub); err != nil {
			return domain.User{}, false, err
		}
		existing.Role.Name = auth.RoleNameForID(existing.Role.ID)
		s.log.Info("google identity linked to existing account", "user_id", existing.ID)
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrUserNotFound) {
		return domain.User{}, false, err
	}

	// 3. New operator → provision isolated tenant + org admin, then link.
	tempPassword, err := generateTemporaryPassword()
	if err != nil {
		return domain.User{}, false, err
	}
	u, isNewOwner, err := s.RegisterSelfServiceAccount(ctx, email, sanitizeName(name), "", tempPassword, "")
	if err != nil {
		return domain.User{}, false, err
	}
	if err := s.linkGoogleSub(ctx, string(u.ID), googleSub); err != nil {
		// Provisioning succeeded but the link failed — surface it; the user
		// can still recover via password reset since email is real.
		s.log.Warn("google_sub link after provisioning failed", "user_id", u.ID, "error", err)
	}
	return u, isNewOwner, nil
}

// getUserByGoogleSub looks up a Google-linked account. Raw SQL because the
// repository store interface is not google-aware (and must not become so for
// one column). Returns found=false on no match.
func (s *UserService) getUserByGoogleSub(ctx context.Context, googleSub string) (domain.User, bool, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return domain.User{}, false, fmt.Errorf("google sign-in unavailable: storage does not support raw DB access")
	}
	row := getter.DB().QueryRowContext(ctx,
		`SELECT id, email, name, role_id, tenant_id, status FROM users WHERE google_sub = ? LIMIT 1`, googleSub)
	var u domain.User
	var roleID int64
	var status string
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &roleID, &u.TenantID, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, false, nil
		}
		return domain.User{}, false, err
	}
	u.Role = domain.Role{ID: roleID, Name: auth.RoleNameForID(roleID)}
	u.Status = domain.UserStatus(status)
	return u, true, nil
}

// linkGoogleSub binds google_sub to a user. The guarded WHERE makes the link
// idempotent and refuses to overwrite an identity already bound to a
// different Google account.
func (s *UserService) linkGoogleSub(ctx context.Context, userID, googleSub string) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return fmt.Errorf("google link unavailable: storage does not support raw DB access")
	}
	res, err := getter.DB().ExecContext(ctx,
		`UPDATE users SET google_sub = ?, auth_provider = 'google', updated_at = datetime('now')
		 WHERE id = ? AND (google_sub IS NULL OR google_sub = ?)`,
		googleSub, userID, googleSub)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("account already linked to a different Google identity")
	}
	return nil
}

// CreateUser creates a new user with a randomly generated temporary password.
func (s *UserService) CreateUser(ctx context.Context, email, name, phone string, roleID int64, status domain.UserStatus, tenantID string) (domain.User, error) {
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
		TenantID:     tenantID,
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
func (s *UserService) CreateUserWithPassword(ctx context.Context, email, name, phone, password string, roleID int64, status domain.UserStatus, tenantID string) (domain.User, error) {
	if email == "" {
		return domain.User{}, domain.ErrUserEmailRequired
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
		TenantID:     tenantID,
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

// ListUsers retrieves users with search and pagination, scoped to the tenant
// in context (falling back to the bootstrap tenant when unset).
func (s *UserService) ListUsers(ctx context.Context, query, status string, limit, offset int) ([]repository.UserWithRole, int64, error) {
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	users, err := s.store.SearchUsers(ctx, query, status, limit, offset, tenantID)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountUsers(ctx, query, status, tenantID)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// dateRangeUserRepo is implemented by user repositories that support
// created_at window filtering. Asserted optionally so existing repository
// implementations/mocks keep compiling unchanged.
type dateRangeUserRepo interface {
	SearchUsersDateRange(ctx context.Context, query, status, from, to string, limit, offset int, tenantID string) ([]repository.UserWithRole, error)
	CountUsersDateRange(ctx context.Context, query, status, from, to string, tenantID string) (int64, error)
}

// ListUsersDateRange retrieves users with search, status and created_at
// window filtering. Falls back to ListUsers semantics when the store does
// not support the window.
func (s *UserService) ListUsersDateRange(ctx context.Context, query, status, from, to string, limit, offset int) ([]repository.UserWithRole, int64, error) {
	dateRepo, ok := s.store.(dateRangeUserRepo)
	if !ok || (from == "" && to == "") {
		return s.ListUsers(ctx, query, status, limit, offset)
	}
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	users, err := dateRepo.SearchUsersDateRange(ctx, query, status, from, to, limit, offset, tenantID)
	if err != nil {
		return nil, 0, err
	}
	total, err := dateRepo.CountUsersDateRange(ctx, query, status, from, to, tenantID)
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

// tenantQueries returns sqlc queries bound to the request's transaction when
// one is active (via the store's Q accessor), falling back to a fresh set of
// queries over the raw DB.
func (s *UserService) tenantQueries(ctx context.Context) (*db.Queries, error) {
	if q, ok := s.store.(interface {
		Q(context.Context) *db.Queries
	}); ok {
		return q.Q(ctx), nil
	}
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return nil, fmt.Errorf("storage does not support tenant operations")
	}
	return db.New(getter.DB()), nil
}

// CreateTenant provisions an active tenant organization.
func (s *UserService) CreateTenant(ctx context.Context, id, name, slug string) error {
	q, err := s.tenantQueries(ctx)
	if err != nil {
		return err
	}
	if _, err := q.InsertTenant(ctx, db.InsertTenantParams{ID: id, Name: name, Slug: toNullString(slug)}); err != nil {
		return err
	}
	s.log.Info("tenant created", "tenant_id", id, "name", name)
	return nil
}

// SetTenantStatus flips a tenant between 'active' and 'suspended'.
func (s *UserService) SetTenantStatus(ctx context.Context, tenantID, status string) error {
	if status != "active" && status != "suspended" {
		return fmt.Errorf("invalid tenant status: must be 'active' or 'suspended'")
	}
	q, err := s.tenantQueries(ctx)
	if err != nil {
		return err
	}
	if err := q.SetTenantStatus(ctx, db.SetTenantStatusParams{Status: status, ID: tenantID}); err != nil {
		return err
	}
	s.log.Info("tenant status changed", "tenant_id", tenantID, "status", status)
	return nil
}

func toNullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

// ErrTenantSlugTaken is returned when provisioning collides with an existing
// tenant id or slug.
var ErrTenantSlugTaken = errors.New("a tenant with this slug already exists")

// isPostgresDriver reports whether the sql.DB handle is backed by a Postgres
// driver. database/sql exposes no driver name at runtime, so this matches the
// concrete driver type registered in internal/database (pgx/v5/stdlib →
// "*stdlib.Driver"; also tolerates lib/pq "*pq.Driver").
func isPostgresDriver(handle *sql.DB) bool {
	t := fmt.Sprintf("%T", handle.Driver())
	return strings.Contains(t, "stdlib.Driver") ||
		strings.Contains(t, "pq.Driver") ||
		strings.Contains(t, "postgres")
}

// TenantSummary is one row of the super-admin tenants list (Spec 24 & Spec 25).
type TenantSummary struct {
	ID            string
	Name          string
	Slug          string
	Status        string
	CreatedAt     time.Time
	UserCount     int64
	PlanID        string
	SubStatus     string
	MonthlyPrice  float64
	PeriodEnd     string
	TrialEnd      string
	TripsUsed     int
	TripsMax      int
	QuotaUsagePct float64
	IsNearQuota   bool
}

// ListTenants returns every tenant organization, newest first. UserCount is
// left zero — callers enrich it from a single grouped users query when the
// UI needs it.
func (s *UserService) ListTenants(ctx context.Context) ([]TenantSummary, error) {
	q, err := s.tenantQueries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TenantSummary, 0, len(rows))
	for _, t := range rows {
		out = append(out, TenantSummary{
			ID:        t.ID,
			Name:      t.Name,
			Slug:      t.Slug.String,
			Status:    t.Status,
			CreatedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// CreateTenantWithAdmin provisions a tenant organization and its first
// org_admin account atomically: if either insert fails, neither lands. The
// tenant insert and the admin user creation must share one transaction —
// CreateTenant and CreateUserWithPassword each open their own transaction via
// the tx manager, so nesting them here would break; instead both raw steps run
// inside this single WithTransaction scope (mirror of
// RegisterSelfServiceAccount).
func (s *UserService) CreateTenantWithAdmin(ctx context.Context, tenantID, name, slug, adminEmail, adminName, adminPassword string) (domain.User, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil || s.txManager == nil {
		return domain.User{}, fmt.Errorf("tenant provisioning unavailable: storage does not support transactions")
	}
	rawDB := getter.DB()

	var created domain.User
	err := s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		created = domain.User{}

		// Provisioned orgs use the slug as their tenant id (seeded bootstrap
		// tenant '1' excepted), so probing both columns catches every collision.
		var row *sql.Row
		if tx := repository.TxFromContext(txCtx); tx != nil {
			row = tx.QueryRowContext(txCtx, `SELECT COUNT(1) FROM tenants WHERE id = ? OR slug = ?`, tenantID, slug)
		} else {
			row = rawDB.QueryRowContext(txCtx, `SELECT COUNT(1) FROM tenants WHERE id = ? OR slug = ?`, tenantID, slug)
		}
		var existing int
		if err := row.Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return ErrTenantSlugTaken
		}

		if err := s.CreateTenant(txCtx, tenantID, name, slug); err != nil {
			return err
		}
		u, err := s.CreateUserWithPassword(txCtx, adminEmail, adminName, "", adminPassword, domain.DefaultRoleID(domain.RoleOrgAdmin), domain.UserStatusActive, tenantID)
		if err != nil {
			return err
		}
		created = u
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}
	s.log.Info("tenant provisioned with org admin", "tenant_id", tenantID, "admin_user_id", created.ID, "email", created.Email)
	return created, nil
}
