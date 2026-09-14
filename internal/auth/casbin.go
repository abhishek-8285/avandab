package auth

import (
	"context"
	"database/sql"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// CasbinModel is the standard RBAC configuration model.
const CasbinModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act
`

// DBAdapter implements the Casbin persist.Adapter interface using the database.
type DBAdapter struct {
	db *sql.DB
}

// NewDBAdapter creates a new DBAdapter.
func NewDBAdapter(db *sql.DB) *DBAdapter {
	return &DBAdapter{db: db}
}

// LoadPolicy loads all policies from role_permissions and user_roles tables.
// Its signature is fixed by casbin's persist.Adapter interface, so no caller
// context can be threaded in; the adapter is used at enforcer construction and
// on explicit Reload(), both outside any request scope.
func (a *DBAdapter) LoadPolicy(m model.Model) error {
	// 1. Load role permissions
	rows, err := a.db.QueryContext(context.Background(), `
		SELECT r.name, p.name
		FROM role_permissions rp
		JOIN roles r ON rp.role_id = r.id
		JOIN permissions p ON rp.permission_id = p.id
	`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var roleName, permName string
		if err := rows.Scan(&roleName, &permName); err != nil {
			return err
		}
		parts := strings.SplitN(permName, ":", 2)
		if len(parts) == 2 {
			resource := parts[0]
			action := parts[1]
			_ = m.AddPolicy("p", "p", []string{roleName, resource, action})
		}
	}

	// 2. Load user roles
	gRows, err := a.db.QueryContext(context.Background(), `
		SELECT u.id, r.name
		FROM user_roles ur
		JOIN users u ON ur.user_id = u.id
		JOIN roles r ON ur.role_id = r.id
	`)
	if err != nil {
		return err
	}
	defer func() { _ = gRows.Close() }()

	for gRows.Next() {
		var userID, roleName string
		if err := gRows.Scan(&userID, &roleName); err != nil {
			return err
		}
		_ = m.AddPolicy("g", "g", []string{userID, roleName})
	}

	return nil
}

// SavePolicy is read-only for Casbin; database tables are updated directly.
func (a *DBAdapter) SavePolicy(m model.Model) error {
	return nil
}

func (a *DBAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	return nil
}

func (a *DBAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return nil
}

func (a *DBAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	return nil
}

// AuthorizationService defines authorization capabilities.
type AuthorizationService interface {
	Can(userID string, resource string, action string) bool
	Reload() error
	AddRoleForUser(userID string, role string) error
	DeleteRolesForUser(userID string) error
}

// CasbinAuthorizationService wraps Casbin with SyncedEnforcer.
type CasbinAuthorizationService struct {
	enforcer *casbin.SyncedEnforcer
}

// NewCasbinAuthorizationService initializes the Casbin Enforcer with the DB adapter.
func NewCasbinAuthorizationService(db *sql.DB) (*CasbinAuthorizationService, error) {
	m, err := model.NewModelFromString(CasbinModel)
	if err != nil {
		return nil, err
	}

	adapter := NewDBAdapter(db)
	enforcer, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, err
	}

	if err := enforcer.LoadPolicy(); err != nil {
		return nil, err
	}

	return &CasbinAuthorizationService{enforcer: enforcer}, nil
}

// Can checks if a user has permission to perform an action on a resource.
func (s *CasbinAuthorizationService) Can(userID string, resource string, action string) bool {
	allowed, _ := s.enforcer.Enforce(userID, resource, action)
	return allowed
}

// Reload reloads the policies from the database.
func (s *CasbinAuthorizationService) Reload() error {
	return s.enforcer.LoadPolicy()
}

// AddRoleForUser assigns a role to a user in Casbin memory.
func (s *CasbinAuthorizationService) AddRoleForUser(userID string, role string) error {
	_, err := s.enforcer.AddRoleForUser(userID, role)
	return err
}

// DeleteRolesForUser removes all roles for a user in Casbin memory.
func (s *CasbinAuthorizationService) DeleteRolesForUser(userID string) error {
	_, err := s.enforcer.DeleteRolesForUser(userID)
	return err
}
