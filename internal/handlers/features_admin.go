package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"transport-app/internal/apperr"
	"transport-app/internal/domain"
	"transport-app/internal/features"
	"transport-app/internal/httpx"
	"transport-app/internal/shared"
)

// FeaturesAdmin powers /settings/features — the per-org feature toggle page.
// Every catalog feature is listed with its tier; flipping a toggle writes a
// feature_flags row for this org (audit-logged) and takes effect on the next
// request (routes) or worker tick (background jobs).
type FeaturesAdmin struct {
	*App
}

func (h *FeaturesAdmin) tenantID(r *http.Request) string {
	if id := shared.TenantIDFromContext(r.Context()); id != "" {
		return string(id)
	}
	return string(shared.DefaultTenant)
}

func (h *FeaturesAdmin) Page(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil || session.Role != "admin" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	tenantID := h.tenantID(r)

	categories := map[string][]features.SnapshotEntry{}
	var order []string
	for _, e := range h.Features.Snapshot(r.Context(), tenantID) {
		if _, seen := categories[e.Category]; !seen {
			order = append(order, e.Category)
		}
		categories[e.Category] = append(categories[e.Category], e)
	}

	h.renderPage(w, r, "features_admin.html", PageData{
		Title: "Features",
		User:  session,
		Extra: map[string]interface{}{
			"Categories": categories,
			"Order":      order,
		},
	})
}

func (h *FeaturesAdmin) Toggle(w http.ResponseWriter, r *http.Request) {
	if session, ok := h.getUserFromContext(r); !ok || session == nil || session.Role != "admin" {
		httpx.Error(w, r, apperr.New(apperr.CodeForbidden).WithDetail("only administrators can change feature flags"))
		return
	}
	var body struct {
		Feature string `json:"feature"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeMalformedJSON).WithCause(err))
		return
	}
	f, ok := features.ByKey(body.Feature)
	if !ok {
		httpx.Error(w, r, apperr.New(apperr.CodeValidation).
			WithDetail("unknown feature: "+body.Feature))
		return
	}

	userID := ""
	if session, oku := h.getUserFromContext(r); oku && session != nil {
		userID = session.UserID
	}
	tenantID := h.tenantID(r)
	if err := h.Features.Set(r.Context(), tenantID, f.Key, body.Enabled, userID); err != nil {
		httpx.Error(w, r, err)
		return
	}

	slog.InfoContext(r.Context(), "feature flag toggled",
		slog.String("feature", f.Key),
		slog.Bool("enabled", body.Enabled),
		slog.String("tenant_id", tenantID),
		slog.String("updated_by", userID),
	)
	if h.Services != nil && h.Services.Audit != nil {
		uid := domain.UserID(userID)
		detail := f.Key + "=" + boolLabel(body.Enabled)
		_ = h.Services.Audit.LogAction(r.Context(), &uid, "feature_flag.toggle", "feature_flags", f.Key, nil, &detail)
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{
		"feature": f.Key,
		"enabled": body.Enabled,
	})
}

func boolLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
