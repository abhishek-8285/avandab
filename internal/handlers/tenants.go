package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/httpx"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// tenantSlugPattern keeps slugs URL/DNS-label safe: lowercase alphanumerics
// separated by single hyphens, no leading/trailing hyphen.
var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// maxTenantSlugLen caps slug (and therefore provisioned tenant id) length.
const maxTenantSlugLen = 40

// bootstrapTenantID is the seeded single-org tenant; suspending it would lock
// every legacy account out once the resolver is on.
const bootstrapTenantID = string(shared.DefaultTenant)

// TenantsHandlers powers /tenants — super-admin provisioning of customer
// organizations and their first org admins (Spec 24 §Tenants management).
type TenantsHandlers struct {
	*App
}

// NewTenantsHandlers wires a tenants handler group onto the shared app deps.
func NewTenantsHandlers(app *App) *TenantsHandlers {
	return &TenantsHandlers{App: app}
}

// TenantForm carries sticky form values back into tenants_edit.html after a
// validation failure so the operator does not retype everything.
type TenantForm struct {
	Name      string
	Slug      string
	Email     string
	AdminName string
}

func (h *TenantsHandlers) Routes(r chi.Router) {
	gate := func(action string) func(http.Handler) http.Handler {
		return middleware.ResourcePermission(h.AuthSrv, "tenants", action)
	}

	r.With(gate("manage")).Get("/", h.List)
	r.With(gate("manage")).Get("/new", h.New)
	r.With(gate("manage")).Post("/new", h.Create)
	r.With(gate("manage")).Post("/{id}/suspend", h.Suspend)
	r.With(gate("manage")).Post("/{id}/activate", h.Activate)
}

// List renders the tenants page; Datastar requests get only the table fragment.
func (h *TenantsHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	list, err := h.Services.Users.ListTenants(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "tenant list failed", slog.Any("error", err))
		http.Error(w, "Failed to list tenants", http.StatusInternalServerError)
		return
	}
	h.applyUserCounts(r, list)

	var activeCount, suspendedCount int64
	for _, t := range list {
		switch t.Status {
		case "active":
			activeCount++
		case "suspended":
			suspendedCount++
		}
	}

	data := PageData{
		Title: "Tenants",
		User:  session,
		Extra: map[string]interface{}{
			"Tenants":   list,
			"Total":     len(list),
			"Active":    activeCount,
			"Suspended": suspendedCount,
		},
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "tenants_list_table.html", data)
		return
	}
	h.renderPage(w, r, "tenants_list.html", data)
}

// applyUserCounts enriches the list with per-tenant user totals from one
// grouped query. Counts are cosmetic — failures degrade to zeros, loudly.
func (h *TenantsHandlers) applyUserCounts(r *http.Request, list []service.TenantSummary) {
	if h.DB == nil || len(list) == 0 {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT tenant_id, COUNT(*) FROM users GROUP BY tenant_id`)
	if err != nil {
		slog.WarnContext(r.Context(), "tenant user counts unavailable", slog.Any("error", err))
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tenantID string
		var n int64
		if err := rows.Scan(&tenantID, &n); err != nil {
			slog.WarnContext(r.Context(), "tenant user count row skipped", slog.Any("error", err))
			continue
		}
		for i := range list {
			if list[i].ID == tenantID {
				list[i].UserCount = n
			}
		}
	}
	if err := rows.Err(); err != nil {
		slog.WarnContext(r.Context(), "tenant user counts incomplete", slog.Any("error", err))
	}
}

// New renders the blank provisioning form with a server-suggested slug.
func (h *TenantsHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderEdit(w, r, session, "New Tenant", TenantForm{}, "")
}

func (h *TenantsHandlers) renderEdit(w http.ResponseWriter, r *http.Request, session *auth.SessionData, title string, form TenantForm, flashErr string) {
	h.renderForm(w, r, "tenants_edit.html", PageData{
		Title:      title,
		User:       session,
		FlashError: flashErr,
		Extra: map[string]interface{}{
			"Form":          form,
			"MultiTenantOn": h.Config != nil && h.Config.MultiTenant.Enabled,
		},
	})
}

// Create provisions a tenant plus its org admin in one transaction. The whole
// feature is refused while MULTI_TENANT_ENABLED=false because the resolver
// could never authenticate the new org's users.
func (h *TenantsHandlers) Create(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	formFrom := func() TenantForm {
		return TenantForm{
			Name:      strings.TrimSpace(r.PostFormValue("name")),
			Slug:      suggestTenantSlug(r.PostFormValue("slug")),
			Email:     strings.TrimSpace(r.PostFormValue("email")),
			AdminName: strings.TrimSpace(r.PostFormValue("admin_name")),
		}
	}
	fail := func(msg string) {
		h.renderEdit(w, r, session, "New Tenant", formFrom(), msg)
	}

	if h.Config == nil || !h.Config.MultiTenant.Enabled {
		fail("Set MULTI_TENANT_ENABLED=true to provision tenants (resolver is off; new orgs could not log in usefully)")
		return
	}

	if err := r.ParseForm(); err != nil {
		fail("Invalid Form Submission")
		return
	}

	form := formFrom()
	password := r.PostFormValue("password")

	switch {
	case form.Name == "":
		fail("Organization Name is required")
		return
	case form.Email == "":
		fail("Admin Email is required")
		return
	case form.AdminName == "":
		fail("Admin Name is required")
		return
	case len(password) < 12:
		fail("Admin Password must be at least 12 characters")
		return
	case form.Slug == "" || len(form.Slug) > maxTenantSlugLen || !tenantSlugPattern.MatchString(form.Slug):
		fail("Slug must be lowercase letters/digits separated by single hyphens (max 40 characters)")
		return
	}

	// Slug doubles as the tenant id for provisioned orgs, so one probe covers
	// both the primary key and the UNIQUE(slug) index.
	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(1) FROM tenants WHERE id = ? OR slug = ?`, form.Slug, form.Slug,
	).Scan(&existing); err != nil {
		slog.ErrorContext(r.Context(), "tenant conflict probe failed", slog.Any("error", err))
		http.Error(w, "Failed to create tenant", http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		fail(`A tenant with slug "` + form.Slug + `" already exists. Choose another.`)
		return
	}

	admin, err := h.Services.Users.CreateTenantWithAdmin(r.Context(), form.Slug, form.Name, form.Slug, form.Email, form.AdminName, password)
	if err != nil {
		slog.ErrorContext(r.Context(), "tenant provisioning failed",
			slog.String("slug", form.Slug), slog.Any("error", err))
		fail("Could not create tenant: " + err.Error())
		return
	}

	// Casbin grant for the new org admin. Logged on failure — never swallowed.
	if err := h.AuthSrv.AddRoleForUser(admin.ID.String(), string(domain.RoleOrgAdmin)); err != nil {
		slog.ErrorContext(r.Context(), "org_admin RBAC role grant failed",
			slog.String("user_id", admin.ID.String()), slog.Any("error", err))
	}

	h.auditTenant(r, "tenant.create", admin.ID.String(), map[string]string{
		"name": form.Name, "slug": form.Slug, "admin_email": form.Email,
	})

	http.SetCookie(w, flashCookie("flash_success",
		`Tenant `+form.Name+` provisioned - org admin `+form.Email+` can sign in`))
	if isDatastarRequest(r) {
		w.Header().Set("Location", "/tenants")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/tenants", http.StatusSeeOther)
}

// Suspend flips a tenant to 'suspended' and purges its users' sessions.
func (h *TenantsHandlers) Suspend(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, "suspended")
}

// Activate re-enables a suspended tenant.
func (h *TenantsHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, "active")
}

func (h *TenantsHandlers) setStatus(w http.ResponseWriter, r *http.Request, status string) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "missing tenant id"})
		return
	}
	if id == bootstrapTenantID && status == "suspended" {
		httpx.JSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "message": "bootstrap tenant cannot be suspended"})
		return
	}

	if err := h.Services.Users.SetTenantStatus(r.Context(), id, status); err != nil {
		slog.ErrorContext(r.Context(), "tenant status update failed",
			slog.String("tenant_id", id), slog.String("status", status), slog.Any("error", err))
		httpx.JSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "message": "Could not update tenant status"})
		return
	}

	if status == "suspended" && h.DB != nil {
		res, err := h.DB.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE tenant_id = ?)`, id)
		if err != nil {
			slog.WarnContext(r.Context(), "tenant session purge failed",
				slog.String("tenant_id", id), slog.Any("error", err))
		} else if n, nerr := res.RowsAffected(); nerr == nil && n > 0 {
			slog.InfoContext(r.Context(), "suspended tenant sessions purged",
				slog.String("tenant_id", id), slog.Int64("sessions", n))
		}
	}

	action := "tenant.suspend"
	if status == "active" {
		action = "tenant.activate"
	}
	h.auditTenant(r, action, id, map[string]string{"status": status})
	slog.InfoContext(r.Context(), "tenant status changed",
		slog.String("tenant_id", id), slog.String("status", status))

	httpx.JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": status, "tenant_id": id})
}

func (h *TenantsHandlers) auditTenant(r *http.Request, action, recordID string, kv map[string]string) {
	if h.Services == nil || h.Services.Audit == nil {
		return
	}
	var uid domain.UserID
	if session, ok := h.getUserFromContext(r); ok && session != nil {
		uid = domain.UserID(session.UserID)
	}
	blob, err := json.Marshal(kv)
	if err != nil {
		slog.WarnContext(r.Context(), "tenant audit payload marshal failed", slog.Any("error", err))
		return
	}
	payload := string(blob)
	if err := h.Services.Audit.LogAction(r.Context(), &uid, action, "tenants", recordID, nil, &payload); err != nil {
		slog.WarnContext(r.Context(), "tenant audit log failed",
			slog.String("action", action), slog.String("record_id", recordID), slog.Any("error", err))
	}
}

// suggestTenantSlug normalizes free text into a slug candidate: lowercase,
// [a-z0-9-], collapsed separators, trimmed, max 40 chars.
func suggestTenantSlug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, rn := range lower {
		if b.Len() >= maxTenantSlugLen {
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
			// drop anything else (apostrophes, unicode punctuation, …)
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxTenantSlugLen {
		out = out[:maxTenantSlugLen]
	}
	return strings.Trim(out, "-")
}
