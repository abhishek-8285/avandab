package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// UserHandlers handles user management (admin only).
type UserHandlers struct {
	*App
}

func (h *UserHandlers) Routes(r chi.Router) {
	r.Get("/me/preferences", h.GetMyPreferences)
	r.Patch("/me/preferences", h.UpdateMyPreferences)
	r.Post("/me/preferences", h.UpdateMyPreferences)

	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "users", "update")).Post("/{id}/reset-password", h.ResetPassword)
}

func (h *UserHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	list, total, err := h.Services.Users.ListUsersDateRange(r.Context(), pp.Query, pp.Status, pp.DateFrom, pp.DateTo, pp.Limit, pp.Offset)
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, total, "/users")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "user_list_table.html", map[string]interface{}{
			"Users":        list,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
		})
		return
	}

	h.renderPage(w, r, "user_list.html", PageData{
		Title: "Users",
		User:  session,
		Extra: map[string]interface{}{"Users": list, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo},
	})
}

// isActingAdmin reports whether the request comes from a global admin session.
func (h *UserHandlers) isActingAdmin(r *http.Request) bool {
	session, ok := h.getUserFromContext(r)
	return ok && session != nil && session.Role == string(domain.RoleAdmin)
}

// visibleRoles hides the global admin role from non-admin actors in the
// New/Edit dropdowns. The backend guard ensureRoleAssignable stays the
// source of truth; this only avoids showing an option that 403s on submit.
func (h *UserHandlers) visibleRoles(r *http.Request, roles []domain.Role) []domain.Role {
	if h.isActingAdmin(r) {
		return roles
	}
	out := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		if role.Name == domain.RoleAdmin {
			continue
		}
		out = append(out, role)
	}
	return out
}

// ensureRoleAssignable blocks privilege escalation: the global admin role
// (tenants:manage + cross-tenant access) can only be granted by an acting
// administrator. Without this, any org_admin (a paying customer's own admin)
// could mint a global admin inside their tenant via the role_id form field.
func (h *UserHandlers) ensureRoleAssignable(w http.ResponseWriter, r *http.Request, roleID int64) bool {
	if roleID != domain.DefaultRoleID(domain.RoleAdmin) {
		return true
	}
	if h.isActingAdmin(r) {
		return true
	}
	http.Error(w, "Only administrators can assign the admin role", http.StatusForbidden)
	return false
}

func (h *UserHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	roles, _ := h.Services.Users.ListRoles(r.Context())
	roles = h.visibleRoles(r, roles)
	h.renderForm(w, r, "user_edit.html", PageData{Title: "New User", User: session, Roles: roles})
}

func (h *UserHandlers) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
		return
	}

	roleID, _ := strconv.ParseInt(r.PostFormValue("role_id"), 10, 64)

	pwd := r.PostFormValue("password")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		// Fail closed: never drop a new user into tenant "1" when the
		// request carries no resolved tenant.
		h.failPage(w, r, errors.New("tenant not set in request context"), http.StatusBadRequest, "Invalid Request Context")
		return
	}
	if !h.ensureRoleAssignable(w, r, roleID) {
		return
	}
	var created domain.User
	var err error

	if pwd != "" {
		created, err = h.Services.Users.CreateUserWithPassword(
			r.Context(),
			r.PostFormValue("email"),
			r.PostFormValue("name"),
			r.PostFormValue("phone"),
			pwd,
			roleID,
			domain.UserStatus(r.PostFormValue("status")),
			tenantID,
		)
	} else {
		created, err = h.Services.Users.CreateUser(
			r.Context(),
			r.PostFormValue("email"),
			r.PostFormValue("name"),
			r.PostFormValue("phone"),
			roleID,
			domain.UserStatus(r.PostFormValue("status")),
			tenantID,
		)
	}

	if err != nil {
		roles, _ := h.Services.Users.ListRoles(r.Context())
		roles = h.visibleRoles(r, roles)
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "user_edit.html", PageData{Title: "New User", User: session, FlashError: err.Error(), Roles: roles})
		return
	}

	// Update Casbin policy for RBAC
	roleName := h.getRoleNameByID(r.Context(), roleID)
	_ = h.AuthSrv.AddRoleForUser(created.ID.String(), roleName)

	if isDatastarRequest(r) {
		w.Header().Set("Location", "/users")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// tenantOf normalizes a user's tenant for comparison, treating legacy rows
// with an empty column as the bootstrap tenant.
func tenantOf(tenantID string) string {
	if tenantID == "" {
		return string(shared.DefaultTenant)
	}
	return tenantID
}

// ensureTenantUser enforces same-tenant user management: when the target user
// belongs to another organization the handler answers 404 (existence not
// disclosed). Spec 24 §Business logic — no cross-tenant user detail access.
func (h *UserHandlers) ensureTenantUser(w http.ResponseWriter, r *http.Request, u domain.User) bool {
	ctxTenant := string(shared.TenantIDFromContext(r.Context()))
	if ctxTenant == "" {
		ctxTenant = string(shared.DefaultTenant)
	}
	if tenantOf(u.TenantID) != ctxTenant {
		http.Error(w, "User not found", http.StatusNotFound)
		return false
	}
	return true
}

func (h *UserHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	id := domain.UserID(chi.URLParam(r, "id"))
	session, _ := h.getUserFromContext(r)

	user, err := h.Services.Users.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if !h.ensureTenantUser(w, r, user) {
		return
	}

	roles, _ := h.Services.Users.ListRoles(r.Context())
	roles = h.visibleRoles(r, roles)

	h.renderForm(w, r, "user_edit.html", PageData{Title: "Edit User", User: session, UserDetail: user, Roles: roles})
}

func (h *UserHandlers) Update(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Invalid Form Submission")
		return
	}

	id := domain.UserID(chi.URLParam(r, "id"))
	roleID, _ := strconv.ParseInt(r.PostFormValue("role_id"), 10, 64)

	if !h.ensureRoleAssignable(w, r, roleID) {
		return
	}

	target, err := h.Services.Users.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if !h.ensureTenantUser(w, r, target) {
		return
	}

	updated, err := h.Services.Users.UpdateUser(
		r.Context(), id,
		r.PostFormValue("email"),
		r.PostFormValue("name"),
		r.PostFormValue("phone"),
		roleID,
		domain.UserStatus(r.PostFormValue("status")),
	)
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Could Not Save User")
		return
	}

	// Update RBAC role in Casbin
	roleName := h.getRoleNameByID(r.Context(), roleID)
	_ = h.AuthSrv.DeleteRolesForUser(updated.ID.String())
	_ = h.AuthSrv.AddRoleForUser(updated.ID.String(), roleName)

	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *UserHandlers) getRoleNameByID(ctx context.Context, roleID int64) string {
	roles, err := h.Services.Users.ListRoles(ctx)
	if err != nil {
		return "viewer"
	}
	for _, r := range roles {
		if r.ID == roleID {
			return string(r.Name)
		}
	}
	return "viewer"
}

func (h *UserHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.UserID(chi.URLParam(r, "id"))
	target, err := h.Services.Users.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if !h.ensureTenantUser(w, r, target) {
		return
	}
	if err := h.Services.Users.DeleteUser(r.Context(), id); err != nil {
		h.failPage(w, r, err, http.StatusInternalServerError, "Could Not Delete User")
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *UserHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := domain.UserID(chi.URLParam(r, "id"))
	target, err := h.Services.Users.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if !h.ensureTenantUser(w, r, target) {
		return
	}

	if err := h.Services.Users.ResetPassword(r.Context(), id); err != nil {
		h.failPage(w, r, err, http.StatusInternalServerError, "Could Not Reset Password")
		return
	}

	if isDatastarRequest(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// GetMyPreferences handles GET /api/v1/users/me/preferences and GET /users/me/preferences
func (h *UserHandlers) GetMyPreferences(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	u, err := h.Services.Users.GetUser(r.Context(), domain.UserID(session.UserID))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	theme := u.ThemePreference
	if theme == "" {
		theme = "system"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":          session.UserID,
		"theme_preference": theme,
	})
}

// UpdateMyPreferences handles PATCH /api/v1/users/me/preferences and POST /api/v1/users/me/preferences
func (h *UserHandlers) UpdateMyPreferences(w http.ResponseWriter, r *http.Request) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Theme != "light" && req.Theme != "dark" && req.Theme != "system" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid theme: must be 'light', 'dark', or 'system'"})
		return
	}

	u, err := h.Services.Users.UpdateThemePreference(r.Context(), domain.UserID(session.UserID), req.Theme)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "updated",
		"theme_preference": u.ThemePreference,
	})
}
