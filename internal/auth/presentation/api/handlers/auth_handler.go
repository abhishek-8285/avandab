package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// APIAuthHandler handles REST authentication endpoints.
type APIAuthHandler struct {
	authSvc *service.AuthService
	userSvc *service.UserService
	secret  []byte
	db      *sql.DB
}

// NewAPIAuthHandler constructs an APIAuthHandler.
func NewAPIAuthHandler(authSvc *service.AuthService, userSvc *service.UserService, secret []byte, db ...*sql.DB) *APIAuthHandler {
	var dbConn *sql.DB
	if len(db) > 0 {
		dbConn = db[0]
	}
	return &APIAuthHandler{authSvc: authSvc, userSvc: userSvc, secret: secret, db: dbConn}
}

// Register mounts the auth endpoints onto a chi.Router.
func (h *APIAuthHandler) Register(r chi.Router) {
	r.Post("/api/v1/auth/token", h.IssueToken)
	r.Post("/api/v1/auth/register", h.RegisterUser)
}

// RegisterUser handles public REST user registration. Only the least-privilege
// viewer role may be self-requested; privileged role requests are rejected.
func (h *APIAuthHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		Email         string `json:"email"`
		Phone         string `json:"phone"`
		Password      string `json:"password"`
		Role          string `json:"role"`           // "driver", "dispatcher", "admin"
		VehicleNumber string `json:"vehicle_number"` // Optional metadata
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		apiError(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}

	// First-run claim: the first account on the deployment becomes admin;
	// every later registration is least-privilege viewer regardless of any
	// requested role. Privileged role assignment otherwise stays admin-only.
	user, isAdmin, err := h.userSvc.RegisterSelfServiceAccount(r.Context(), req.Email, req.Name, req.Phone, req.Password)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	roleName := string(domain.RoleViewer)
	if isAdmin {
		roleName = string(domain.RoleAdmin)
	}

	userTenantID := user.TenantID
	if userTenantID == "" {
		userTenantID = string(shared.DefaultTenant)
	}

	// Link vehicle & driver profile if vehicle registration number provided or role is driver
	if h.db != nil && (req.VehicleNumber != "" || req.Role == "driver") {
		vNum := strings.ToUpper(strings.TrimSpace(req.VehicleNumber))
		if vNum == "" {
			vNum = "DL1LN9999"
		}
		vID := uuid.New().String()
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
			VALUES (?, ?, ?, 'truck', 5000, 'diesel', date('now', '+1 year'), date('now', '+1 year'), date('now', '+1 year'), 'available', ?)
			ON CONFLICT(registration_number) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`,
			vID, vNum, vNum, userTenantID)

		dID := uuid.New().String()
		names := strings.SplitN(req.Name, " ", 2)
		firstName := names[0]
		lastName := ""
		if len(names) > 1 {
			lastName = names[1]
		}
		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, notes, tenant_id)
			VALUES (?, ?, ?, ?, ?, ?, 'DL-PENDING', date('now', '+5 years'), 'available', ?, ?)
			ON CONFLICT(driver_id) DO UPDATE SET notes = excluded.notes, updated_at = CURRENT_TIMESTAMP`,
			dID, string(user.ID), firstName, lastName, req.Phone, req.Email, vNum, userTenantID)

		_, _ = h.db.ExecContext(r.Context(), `
			INSERT INTO telemetry_devices (id, tenant_id, imei, device_type, status, vehicle_id, activated_at)
			VALUES (?, ?, ?, 'mobile_app', 'active', (SELECT id FROM vehicles WHERE registration_number = ? LIMIT 1), CURRENT_TIMESTAMP)
			ON CONFLICT(imei) DO UPDATE SET vehicle_id = (SELECT id FROM vehicles WHERE registration_number = ? LIMIT 1), status = 'active'`,
			uuid.New().String(), userTenantID, string(user.ID), vNum, vNum)
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	token, err := auth.IssueAPIToken(h.secret, auth.APITokenClaims{
		UserID:    string(user.ID),
		Role:      roleName,
		TenantID:  userTenantID,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		apiError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"user": map[string]string{
			"id":    string(user.ID),
			"name":  user.Name,
			"email": user.Email,
			"role":  roleName,
		},
	})
}

// IssueToken godoc
//
//	POST /api/v1/auth/token
//	Body: {"email":"user@example.com","password":"secret"}
//	Returns: {"token":"<signed-token>","expires_at":"<RFC3339>"}
func (h *APIAuthHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		apiError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	result, err := h.authSvc.Login(r.Context(), service.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		// Don't distinguish "not found" from "wrong password" to prevent enumeration.
		apiError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	resultTenantID := result.User.TenantID
	if resultTenantID == "" {
		resultTenantID = string(shared.DefaultTenant)
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	token, err := auth.IssueAPIToken(h.secret, auth.APITokenClaims{
		UserID:    string(result.User.ID),
		Role:      string(result.User.Role.Name),
		TenantID:  resultTenantID,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		apiError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      token,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"user_id":    string(result.User.ID),
		"role":       string(result.User.Role.Name),
		"name":       result.User.Name,
		"email":      result.User.Email,
	})
}

// apiError writes a consistent JSON error response.
func apiError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
