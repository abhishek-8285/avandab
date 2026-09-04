package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

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
	r.With(gate("manage")).Get("/{id}/purge", h.PurgePreview)
	r.With(gate("manage")).Post("/{id}/purge", h.Purge)
	r.With(gate("manage")).Post("/{id}/plan", h.UpdatePlan)
	r.With(gate("manage")).Post("/{id}/extend-trial", h.ExtendTrial)
	r.With(gate("manage")).Post("/{id}/override", h.SetOverride)
}

// TenantCommercialItem enriches a tenant with subscription and quota metrics.
type TenantCommercialItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	UserCount     int64   `json:"user_count"`
	PlanID        string  `json:"plan_id"`
	SubStatus     string  `json:"sub_status"`
	MonthlyPrice  float64 `json:"monthly_price"`
	PeriodEnd     string  `json:"period_end"`
	TrialEnd      string  `json:"trial_end"`
	TripsUsed     int     `json:"trips_used"`
	TripsMax      int     `json:"trips_max"`
	QuotaUsagePct float64 `json:"quota_usage_pct"`
	IsNearQuota   bool    `json:"is_near_quota"`
}

// List renders the tenants page with Commercial Intelligence; Datastar requests get only the table fragment.
func (h *TenantsHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	list, err := h.Services.Users.ListTenants(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "tenant list failed", slog.Any("error", err))
		http.Error(w, "Failed to list tenants", http.StatusInternalServerError)
		return
	}
	h.applyUserCounts(r, list)

	var activeCount, suspendedCount, trialCount, paidCount, pastDueCount, nearQuotaCount int64
	var totalMRR float64

	commercialList := make([]TenantCommercialItem, 0, len(list))

	for _, t := range list {
		item := TenantCommercialItem{
			ID:        t.ID,
			Name:      t.Name,
			Slug:      t.Slug,
			Status:    t.Status,
			CreatedAt: t.CreatedAt.Format("2006-01-02"),
			UserCount: t.UserCount,
			PlanID:    "STARTER",
			SubStatus: "TRIAL",
		}

		if t.Status == "active" {
			activeCount++
		} else if t.Status == "suspended" {
			suspendedCount++
		}

		// Query commercial subscription details if available
		if h.DB != nil {
			var planID, subStatus string
			var price float64
			var periodEndStr sql.NullString
			var trialEndStr sql.NullString
			subErr := h.DB.QueryRowContext(r.Context(), `
				SELECT ts.plan_id, ts.status, sp.monthly_price_inr, ts.current_period_end, ts.trial_end
				FROM tenant_subscriptions ts
				JOIN subscription_plans sp ON sp.id = ts.plan_id
				WHERE ts.tenant_id = ?
			`, t.ID).Scan(&planID, &subStatus, &price, &periodEndStr, &trialEndStr)

			if subErr == nil {
				item.PlanID = planID
				item.SubStatus = subStatus
				item.MonthlyPrice = price
				if periodEndStr.Valid {
					item.PeriodEnd = periodEndStr.String
				}
				if trialEndStr.Valid {
					item.TrialEnd = trialEndStr.String
				}

				if subStatus == "TRIAL" {
					trialCount++
				} else if subStatus == "ACTIVE" {
					paidCount++
					totalMRR += price
				} else if subStatus == "PAST_DUE" {
					pastDueCount++
				}
			} else {
				trialCount++
			}

			// Query trip quota meter
			var usedQty, maxQty int
			mErr := h.DB.QueryRowContext(r.Context(), `
				SELECT used_quantity, max_quantity
				FROM tenant_usage_meters
				WHERE tenant_id = ? AND quota_key = 'max_trips_per_month'
				ORDER BY updated_at DESC LIMIT 1
			`, t.ID).Scan(&usedQty, &maxQty)

			if mErr == nil && maxQty > 0 {
				item.TripsUsed = usedQty
				item.TripsMax = maxQty
				item.QuotaUsagePct = float64(usedQty) / float64(maxQty) * 100.0
				if item.QuotaUsagePct >= 80.0 {
					item.IsNearQuota = true
					nearQuotaCount++
				}
			}
		}

		commercialList = append(commercialList, item)
	}

	data := PageData{
		Title: "Platform Commercial Control Center",
		User:  session,
		Extra: map[string]interface{}{
			"Tenants":          commercialList,
			"Total":            len(commercialList),
			"Active":           activeCount,
			"Suspended":        suspendedCount,
			"Trials":           trialCount,
			"Paid":             paidCount,
			"TotalMRR":         totalMRR,
			"PastDue":          pastDueCount,
			"NearQuotaCount":   nearQuotaCount,
			"CommercialAlerts": pastDueCount + nearQuotaCount,
		},
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "tenants_list_table.html", data)
		return
	}
	h.renderPage(w, r, "tenants_list.html", data)
}

// UpdatePlan updates a tenant's subscription plan directly from Platform Admin.
// provider_subscription_id (optional) links the org to its Razorpay
// subscription so billing webhooks can find it — without this link,
// paid events return ErrSubscriptionNotFound and upgrades never apply.
func (h *TenantsHandlers) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	planID := r.PostFormValue("plan_id")
	if planID == "" {
		planID = "GROWTH"
	}
	providerSubID := r.PostFormValue("provider_subscription_id")
	if h.DB != nil {
		now := time.Now().UTC()
		end := now.Add(30 * 24 * time.Hour)
		_, _ = h.DB.ExecContext(r.Context(), `
			INSERT INTO tenant_subscriptions (id, tenant_id, plan_id, status, current_period_start, current_period_end, provider_subscription_id, created_at, updated_at)
			VALUES (?, ?, ?, 'ACTIVE', ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id) DO UPDATE SET
				plan_id = excluded.plan_id,
				status = 'ACTIVE',
				current_period_start = excluded.current_period_start,
				current_period_end = excluded.current_period_end,
				provider_subscription_id = COALESCE(NULLIF(excluded.provider_subscription_id, ''), provider_subscription_id),
				updated_at = CURRENT_TIMESTAMP
		`, "sub_"+id, id, planID, now.Format(time.RFC3339), end.Format(time.RFC3339), providerSubID, now.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	h.auditTenant(r, "tenant.plan_update", id, map[string]string{"plan_id": planID})
	http.Redirect(w, r, "/tenants", http.StatusSeeOther)
}

// ExtendTrial grants an additional 14 days to a trial tenant.
func (h *TenantsHandlers) ExtendTrial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.DB != nil {
		now := time.Now().UTC()
		newTrial := now.Add(14 * 24 * time.Hour)
		_, _ = h.DB.ExecContext(r.Context(), `
			UPDATE tenant_subscriptions
			SET trial_end = ?, status = 'TRIAL', updated_at = CURRENT_TIMESTAMP
			WHERE tenant_id = ?
		`, newTrial.Format(time.RFC3339), id)
	}
	h.auditTenant(r, "tenant.extend_trial", id, map[string]string{"extended_days": "14"})
	http.Redirect(w, r, "/tenants", http.StatusSeeOther)
}

// SetOverride configures a privileged feature or quota override.
func (h *TenantsHandlers) SetOverride(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	entType := r.PostFormValue("entitlement_type")
	keyName := r.PostFormValue("key_name")
	val := r.PostFormValue("override_value")
	reason := r.PostFormValue("reason")
	if entType == "" {
		entType = "FEATURE"
	}
	if h.DB != nil {
		recID := "ovr_" + id + "_" + keyName
		_, _ = h.DB.ExecContext(r.Context(), `
			INSERT INTO tenant_entitlement_overrides (id, tenant_id, entitlement_type, key_name, override_value, reason)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, entitlement_type, key_name) DO UPDATE SET
				override_value = excluded.override_value,
				reason = excluded.reason
		`, recID, id, entType, keyName, val, reason)
	}
	h.auditTenant(r, "tenant.override", id, map[string]string{"key": keyName, "val": val})
	http.Redirect(w, r, "/tenants", http.StatusSeeOther)
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
