package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/service"
)

// APIAuthHandler handles REST authentication endpoints.
type APIAuthHandler struct {
	authSvc    *service.AuthService
	userSvc    *service.UserService
	authorizer auth.AuthorizationService
	secret     []byte
	db         *sql.DB
}

// NewAPIAuthHandler constructs an APIAuthHandler.
func NewAPIAuthHandler(authSvc *service.AuthService, userSvc *service.UserService, secret []byte, db ...*sql.DB) *APIAuthHandler {
	var dbConn *sql.DB
	if len(db) > 0 {
		dbConn = db[0]
	}
	return &APIAuthHandler{authSvc: authSvc, userSvc: userSvc, secret: secret, db: dbConn}
}

// WithAuthorizer attaches the Casbin authorizer so self-registration grants
// the role in-memory immediately (mirrors the web path). Without it, new
// users exist in user_roles but fail every permission check until restart.
func (h *APIAuthHandler) WithAuthorizer(a auth.AuthorizationService) *APIAuthHandler {
	h.authorizer = a
	return h
}

// Register mounts the auth endpoints onto a chi.Router.
func (h *APIAuthHandler) Register(r chi.Router) {
	r.Post("/api/v1/auth/token", h.IssueToken)
	r.Post("/api/v1/auth/register", h.RegisterUser)
}

// RegisterUser handles public REST user registration.
func (h *APIAuthHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		Email         string `json:"email"`
		Phone         string `json:"phone"`
		Password      string `json:"password"`
		CompanyName   string `json:"company_name"`   // Optional company / fleet name
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

	user, isAdmin, err := h.userSvc.RegisterSelfServiceAccount(r.Context(), req.Email, req.Name, req.Phone, req.Password, req.CompanyName)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	roleName := string(domain.RoleViewer)
	if isAdmin {
		// isAdmin is isNewOwner: self-registration always mints org_admin
		// (role 6), never platform admin (role 1). Matches the web path
		// (auth.go SaveRegister) — a token claiming "admin" would over-grant
		// wherever the live-role override has no validator attached.
		roleName = string(domain.RoleOrgAdmin)
	}
	// Grant the role in the live authorizer now: the DB row alone leaves
	// every permission check failing until the next restart (Casbin policy
	// loads once at boot). Best-effort like the web path.
	if h.authorizer != nil {
		_ = h.authorizer.AddRoleForUser(string(user.ID), roleName)
	}

	userTenantID := user.TenantID
	if userTenantID == "" {
		// Service invariant violated: a fresh registration must carry its
		// isolated tenant. Never fall back to DefaultTenant here — that
		// would write the new user's vehicle/driver/device rows into
		// another org.
		apiError(w, http.StatusInternalServerError, "registration failed: tenant provisioning error")
		return
	}

	// Link vehicle & driver profile if vehicle registration number provided or role is driver.
	// Best-effort and atomic (single tx): the user + tenant already committed
	// above, so a failure here must never fail registration — it logs loudly
	// and the driver completes provisioning via onboarding/wizard instead.
	if h.db != nil && (req.VehicleNumber != "" || req.Role == "driver") {
		if err := h.linkDriverProfile(r.Context(), user, req.Name, req.Phone, req.Email,
			strings.ToUpper(strings.TrimSpace(req.VehicleNumber)), userTenantID); err != nil {
			slog.Error("registration driver-profile provisioning failed",
				"user_id", string(user.ID), "tenant_id", userTenantID, "error", err)
		}
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
		// users.tenant_id is NOT NULL DEFAULT '1' plus backfill 00065, so an
		// empty tenant here means data corruption. Fail closed instead of
		// minting a token scoped to another org.
		apiError(w, http.StatusInternalServerError, "account misconfigured, contact support")
		return
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

// linkDriverProfile provisions the driver-side rows for a self-registered
// user in one transaction: vehicle (only when an explicit number was given —
// never a fabricated placeholder), driver row (license NULL per 00131), and
// the mobile_app device bound to the new tenant's own vehicle row.
//
// The vehicle/device lookups are tenant-scoped: registration_number is
// globally UNIQUE, so an unscoped SELECT or ON CONFLICT touch could latch
// onto another tenant's vehicle.
func (h *APIAuthHandler) linkDriverProfile(ctx context.Context, user domain.User, name, phone, email, vNum, tenantID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var vehicleID sql.NullString
	if vNum != "" {
		vID := uuid.New().String()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
			VALUES ($1, $2, $3, 'truck', 5000, 'diesel', NULL, NULL, NULL, 'available', $4)
			ON CONFLICT (registration_number) DO NOTHING`,
			vID, vNum, vNum, tenantID); err != nil {
			return err
		}
		var got string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM vehicles WHERE registration_number = $1 AND tenant_id = $2`, vNum, tenantID).Scan(&got); err != nil {
			return err
		}
		vehicleID = sql.NullString{String: got, Valid: true}
	}

	names := strings.SplitN(name, " ", 2)
	firstName := names[0]
	lastName := ""
	if len(names) > 1 {
		lastName = names[1]
	}
	// Unknown license stays NULL (00131) — same rule as RegisterDriver.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, notes, tenant_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, 'available', $7, $8)
		ON CONFLICT (driver_id) DO UPDATE SET notes = excluded.notes, updated_at = CURRENT_TIMESTAMP`,
		uuid.New().String(), string(user.ID), firstName, lastName, phone, email, vNum, tenantID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO telemetry_devices (id, tenant_id, imei, device_type, status, vehicle_id, activated_at)
		VALUES ($1, $2, $3, 'mobile_app', 'active', $4, CURRENT_TIMESTAMP)
		ON CONFLICT (imei) DO UPDATE SET vehicle_id = excluded.vehicle_id, status = 'active'`,
		uuid.New().String(), tenantID, string(user.ID), vehicleID); err != nil {
		return err
	}

	return tx.Commit()
}
