package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// AccessReviewHandlers serves the periodic access re-certification ledger
// (00155). All routes mount behind RequirePermission(privacy:manage) — org
// admins and platform admins only (no new permission: reviewing who holds
// access is the same governance surface as breach reporting, and
// users:manage is platform-only). Reads are tenant-scoped fail-closed in
// the service layer. privacyTenant (privacy.go) supplies the tenant.
type AccessReviewHandlers struct {
	*App
}

func accessReviewTenant(r *http.Request) string {
	return string(shared.TenantIDFromContext(r.Context()))
}

func accessReviewJSON(r *service.AccessReview) map[string]interface{} {
	out := map[string]interface{}{
		"id":        r.ID,
		"user_id":   r.UserID,
		"role_name": r.RoleName,
		"period":    r.Period,
		"status":    r.Status,
		"due_at":    r.DueAt.UTC().Format(time.RFC3339),
		"notes":     r.Notes,
	}
	if r.ReviewedBy.Valid {
		out["reviewed_by"] = r.ReviewedBy.String
	}
	if r.ReviewedAt.Valid {
		out["reviewed_at"] = r.ReviewedAt.Time.UTC().Format(time.RFC3339)
	}
	return out
}

func parseReviewDueAt(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}

// ListDueReviewsAPI lists the tenant's pending reviews past due — the
// re-certification watchlist.
func (h *AccessReviewHandlers) ListDueReviewsAPI(w http.ResponseWriter, r *http.Request) {
	tenantID := accessReviewTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	reviews, err := h.Services.AccessReviews.ListDueAccessReviews(r.Context(), tenantID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not list reviews")
		return
	}
	out := make([]map[string]interface{}, 0, len(reviews))
	for i := range reviews {
		out = append(out, accessReviewJSON(&reviews[i]))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"reviews": out})
}

// OpenReviewAPI files (or re-opens idempotently) the pending row for one
// user + period ({"user_id","period","due_at"}; due_at RFC3339).
func (h *AccessReviewHandlers) OpenReviewAPI(w http.ResponseWriter, r *http.Request) {
	tenantID := accessReviewTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		UserID   string `json:"user_id"`
		RoleName string `json:"role_name"`
		Period   string `json:"period"`
		DueAt    string `json:"due_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.Period == "" {
		writeJSONError(w, http.StatusBadRequest, "user_id and period are required")
		return
	}
	dueAt, err := parseReviewDueAt(req.DueAt)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "due_at is required (RFC3339)")
		return
	}
	id, err := h.Services.AccessReviews.OpenDueReview(r.Context(), tenantID, req.UserID, req.RoleName, req.Period, dueAt)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not open review")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})
}

func (h *AccessReviewHandlers) reviewByID(w http.ResponseWriter, r *http.Request) (*service.AccessReview, bool) {
	tenantID := accessReviewTenant(r)
	if tenantID == "" {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	rev, err := h.Services.AccessReviews.GetAccessReview(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "review not found")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "could not read review")
		}
		return nil, false
	}
	return rev, true
}

// GetReviewAPI reads one review inside the caller's tenant.
func (h *AccessReviewHandlers) GetReviewAPI(w http.ResponseWriter, r *http.Request) {
	rev, ok := h.reviewByID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accessReviewJSON(rev))
}

// CertifyReviewAPI records the reviewer keeping the grant ({"reviewed_by"}).
func (h *AccessReviewHandlers) CertifyReviewAPI(w http.ResponseWriter, r *http.Request) {
	rev, ok := h.reviewByID(w, r)
	if !ok {
		return
	}
	var req struct {
		ReviewedBy string `json:"reviewed_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReviewedBy == "" {
		writeJSONError(w, http.StatusBadRequest, "reviewed_by is required")
		return
	}
	tenantID := accessReviewTenant(r)
	if err := h.Services.AccessReviews.CertifyAccessReview(r.Context(), tenantID, rev.ID, req.ReviewedBy); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not certify review")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// RevokeReviewAPI records the reviewer withdrawing the grant
// ({"reviewed_by","note"}; empty note keeps existing notes).
func (h *AccessReviewHandlers) RevokeReviewAPI(w http.ResponseWriter, r *http.Request) {
	rev, ok := h.reviewByID(w, r)
	if !ok {
		return
	}
	var req struct {
		ReviewedBy string `json:"reviewed_by"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReviewedBy == "" {
		writeJSONError(w, http.StatusBadRequest, "reviewed_by is required")
		return
	}
	tenantID := accessReviewTenant(r)
	if err := h.Services.AccessReviews.RevokeAccessReview(r.Context(), tenantID, rev.ID, req.ReviewedBy, req.Note); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not revoke review")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
