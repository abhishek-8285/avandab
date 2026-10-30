package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"transport-app/internal/auth"
	"transport-app/internal/httpx"
	privacyapp "transport-app/internal/privacy/application"
	"transport-app/internal/shared"
)

// DSARHandlers serves Data Subject Access Requests (DSAR), erasure,
// retention schedules, and sub-processor registries under the DPDP Act 2023 §11–15.
type DSARHandlers struct {
	svc *privacyapp.DSARService
}

// NewDSARHandlers wires DSAR handlers.
func NewDSARHandlers(svc *privacyapp.DSARService) *DSARHandlers {
	return &DSARHandlers{svc: svc}
}

func getSessionUser(r *http.Request) (*auth.SessionData, string, bool) {
	sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || sess == nil || sess.UserID == "" {
		return nil, "", false
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		return nil, "", false
	}
	return sess, tenantID, true
}

// ExportDSARAPI handles DPDP §11 data principal access requests (POST /api/v1/privacy/dsar/export).
// Principals can export their own records; administrators (admin/org_admin) can export on behalf of any tenant user.
func (h *DSARHandlers) ExportDSARAPI(w http.ResponseWriter, r *http.Request) {
	sess, tenantID, ok := getSessionUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	targetUserID := sess.UserID
	var req struct {
		UserID string `json:"user_id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != "" && req.UserID != sess.UserID {
		role := strings.ToLower(sess.Role)
		if role != "admin" && role != "org_admin" {
			httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: cannot export data of another principal"})
			return
		}
		targetUserID = req.UserID
	}

	bundle, err := h.svc.ExportDataPrincipal(r.Context(), tenantID, targetUserID, sess.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "data principal not found"})
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "could not export personal data"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("format") == "download" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dsar_export_%s.json"`, targetUserID))
	}
	_ = json.NewEncoder(w).Encode(bundle)
}

// EraseDSARAPI handles DPDP §12 erasure / right-to-be-forgotten requests (POST /api/v1/privacy/dsar/erase).
// Principals can request erasure of their own records; administrators (admin/org_admin) can execute for any tenant user.
func (h *DSARHandlers) EraseDSARAPI(w http.ResponseWriter, r *http.Request) {
	sess, tenantID, ok := getSessionUser(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	targetUserID := sess.UserID
	var req struct {
		UserID string `json:"user_id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != "" && req.UserID != sess.UserID {
		role := strings.ToLower(sess.Role)
		if role != "admin" && role != "org_admin" {
			httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: cannot request erasure for another principal"})
			return
		}
		targetUserID = req.UserID
	}

	result, err := h.svc.EraseDataPrincipal(r.Context(), tenantID, targetUserID, sess.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "data principal not found"})
			return
		}
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "could not execute data erasure"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ListDSARRequestsAPI serves the tenant's DSAR audit ledger history (GET /api/v1/privacy/dsar/requests).
// Mounted under RequirePermission(privacy:manage).
func (h *DSARHandlers) ListDSARRequestsAPI(w http.ResponseWriter, r *http.Request) {
	tenantID := privacyTenant(r)
	if tenantID == "" {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	records, err := h.svc.ListDSARRequests(r.Context(), tenantID)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list dsar requests"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"requests": records})
}

// RetentionScheduleAPI returns the statutory retention schedule (GET /api/v1/privacy/retention-schedule).
func (h *DSARHandlers) RetentionScheduleAPI(w http.ResponseWriter, r *http.Request) {
	schedule := h.svc.DefaultRetentionSchedule()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"retention_schedule": schedule})
}

// SubProcessorsAPI returns the vetted sub-processor registry (GET /api/v1/privacy/sub-processors).
func (h *DSARHandlers) SubProcessorsAPI(w http.ResponseWriter, r *http.Request) {
	processors := h.svc.DefaultSubProcessors()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub_processors": processors})
}
