package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/eta"
	"transport-app/internal/shared"
)

// ShareHandlers powers trip share link generation, public viewing, PIN validation,
// and administrative revocation (Spec 04 §4, §7).
type ShareHandlers struct {
	*App
	db         *sql.DB
	EtaService *eta.EtaService
}

// NewShareHandlers creates a new ShareHandlers instance.
func NewShareHandlers(app *App, db *sql.DB) *ShareHandlers {
	return &ShareHandlers{
		App: app,
		db:  db,
	}
}

type CreateShareRequest struct {
	Pin      string `json:"pin"`
	TTLHours int    `json:"ttl_hours"`
}

type ShareLinkItem struct {
	ID                string
	TripID            string
	TripNumber        string
	TokenHash         string
	HasPIN            bool
	CreatedBy         string
	CreatorName       string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	LastViewedAt      *time.Time
	ViewCount         int
	FailedPinAttempts int
	LockedUntil       *time.Time
	RevokedAt         *time.Time
	Status            string
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hashPIN(pin, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	h := sha256.Sum256(append([]byte(pin), salt...))
	return hex.EncodeToString(h[:])
}

func (h *ShareHandlers) getCookieSecret() string {
	if h.Config != nil && h.Config.CookieSecret != "" {
		return h.Config.CookieSecret
	}
	return "dev-secret-key-change-in-production-32b!"
}

func signPINCookie(tokenHash, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("pin-verified:" + tokenHash))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyPINCookie(tokenHash, cookieVal, secret string) bool {
	if cookieVal == "" {
		return false
	}
	expected := signPINCookie(tokenHash, secret)
	return hmac.Equal([]byte(cookieVal), []byte(expected))
}

func clampTTL(requestedHours, defaultHours, maxHours int) time.Duration {
	if requestedHours <= 0 {
		requestedHours = defaultHours
	}
	if requestedHours > maxHours {
		requestedHours = maxHours
	}
	if requestedHours <= 0 {
		requestedHours = 24
	}
	return time.Duration(requestedHours) * time.Hour
}

// CreateShare generates a cryptographically random token, stores its SHA-256 hash,
// and returns the raw URL/token once (Spec 04 §4).
func (h *ShareHandlers) CreateShare(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		tripID = r.FormValue("trip_id")
	}

	user, _ := h.getUserFromContext(r)
	tenantID := string(shared.MustTenantID(r.Context()))

	// 1. Validate trip exists and belongs to tenant
	var tripTenantID string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT tenant_id FROM trips WHERE id = $1`, tripID).Scan(&tripTenantID)
	if err != nil || tripTenantID != tenantID {
		http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
		return
	}

	// 2. Check SHARE_LINK_MAX_ACTIVE cap per trip
	maxActive := 20
	defaultTTL := 24
	maxTTL := 168
	if h.Config != nil {
		if h.Config.LiveMap.ShareLinkMaxActive > 0 {
			maxActive = h.Config.LiveMap.ShareLinkMaxActive
		}
		if h.Config.LiveMap.ShareLinkTTLHours > 0 {
			defaultTTL = h.Config.LiveMap.ShareLinkTTLHours
		}
		if h.Config.LiveMap.ShareLinkMaxTTLHours > 0 {
			maxTTL = h.Config.LiveMap.ShareLinkMaxTTLHours
		}
	}

	var activeCount int
	err = h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM share_links
		 WHERE trip_id = $1 AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP`,
		tripID).Scan(&activeCount)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	if activeCount >= maxActive {
		http.Error(w, `{"error":"maximum active share links reached for this trip"}`, http.StatusConflict)
		return
	}

	// Parse request (JSON or Form)
	var req CreateShareRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.Pin = r.FormValue("pin")
		fmt.Sscanf(r.FormValue("ttl_hours"), "%d", &req.TTLHours)
	}

	// 3. Optional PIN validation & hashing
	var pinHash, pinSalt *string
	if req.Pin != "" {
		if len(req.Pin) < 4 || len(req.Pin) > 6 {
			http.Error(w, `{"error":"PIN must be 4 to 6 digits"}`, http.StatusBadRequest)
			return
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			http.Error(w, `{"error":"failed to generate salt"}`, http.StatusInternalServerError)
			return
		}
		saltHex := hex.EncodeToString(salt)
		pHash := hashPIN(req.Pin, saltHex)
		pinHash = &pHash
		pinSalt = &saltHex
	}

	// 4. Generate token: crypto/rand 32 bytes → base64.RawURLEncoding (43 chars)
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256Hex(token)

	// 5. Expiry calculation
	ttl := clampTTL(req.TTLHours, defaultTTL, maxTTL)
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	linkID := uuid.NewString()
	createdBy := "system"
	if user != nil {
		createdBy = user.UserID
	}

	// 6. INSERT share_links
	_, err = h.db.ExecContext(r.Context(), `
		INSERT INTO share_links (id, trip_id, token_hash, pin_hash, pin_salt, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		linkID, tripID, tokenHash, pinHash, pinSalt, createdBy, now, expiresAt,
	)
	if err != nil {
		http.Error(w, `{"error":"failed to create share link"}`, http.StatusInternalServerError)
		return
	}

	// 7. Construct share URL
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || (h.Config != nil && h.Config.CookieSecure) {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	shareURL := fmt.Sprintf("%s/share/%s", baseURL, token)

	res := map[string]interface{}{
		"id":         linkID,
		"trip_id":    tripID,
		"token":      token,
		"url":        shareURL,
		"expires_at": expiresAt.Format(time.RFC3339),
		"has_pin":    pinHash != nil,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

// HandleShareIndex handles GET /share (without a specific token).
func (h *ShareHandlers) HandleShareIndex(w http.ResponseWriter, r *http.Request) {
	user, ok := h.getUserFromContext(r)
	if ok && user != nil {
		http.Redirect(w, r, "/shares", http.StatusSeeOther)
		return
	}
	h.renderErrorInfo(w, r, ErrorInfo{
		StatusCode: http.StatusNotFound,
		Title:      "Share Link Required",
		Message:    "Live trip tracking requires a valid share token (e.g., /share/{token}). If you are looking for your shares list, please log in first.",
		ErrorCode:  "ERR_SHARE_TOKEN_MISSING",
		Model:      "ShareLink",
		Path:       r.URL.Path,
	})
}

// ViewShare renders the public tracking map or the PIN verification form (Spec 04 §4).
func (h *ShareHandlers) ViewShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tokenHash := sha256Hex(token)

	// 1. Lookup share_links
	var id, tripID string
	var pinHash, pinSalt sql.NullString
	var createdAt, expiresAt time.Time
	var lastViewedAt, lockedUntil, revokedAt sql.NullTime
	var viewCount, failedAttempts int
	var tripNumber, status string
	var vehicleID, arrivalTime sql.NullString

	err := h.db.QueryRowContext(r.Context(), `
		SELECT s.id, s.trip_id, s.pin_hash, s.pin_salt, s.created_at, s.expires_at,
		       s.last_viewed_at, s.view_count, s.failed_pin_attempts, s.locked_until, s.revoked_at,
		       t.trip_number, t.status, t.vehicle_id, t.arrival_time
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&id, &tripID, &pinHash, &pinSalt, &createdAt, &expiresAt,
		&lastViewedAt, &viewCount, &failedAttempts, &lockedUntil, &revokedAt,
		&tripNumber, &status, &vehicleID, &arrivalTime,
	)

	// 404 for unknown token (uniform, no existence oracle)
	if err == sql.ErrNoRows {
		h.renderErrorInfo(w, r, ErrorInfo{
			StatusCode: http.StatusNotFound,
			Title:      "Share Link Not Found",
			Message:    "The requested live tracking link was not found or is invalid.",
			ErrorCode:  "ERR_SHARE_LINK_NOT_FOUND",
			Model:      "ShareLink",
			Path:       r.URL.Path,
		})
		return
	}
	if err != nil {
		h.renderErrorInfo(w, r, ErrorInfo{
			StatusCode: http.StatusInternalServerError,
			Title:      "Internal Server Error",
			Message:    "Failed to retrieve the live tracking link details.",
			ErrorCode:  "ERR_INTERNAL_SERVER",
			Model:      "ShareLink",
			Path:       r.URL.Path,
		})
		return
	}

	now := time.Now().UTC()

	// 410 for expired or revoked link
	if revokedAt.Valid || expiresAt.Before(now) {
		h.renderErrorInfo(w, r, ErrorInfo{
			StatusCode: http.StatusGone,
			Title:      "Share Link Expired",
			Message:    "This live tracking link has expired or has been revoked.",
			ErrorCode:  "ERR_SHARE_LINK_EXPIRED",
			Model:      "ShareLink",
			Path:       r.URL.Path,
		})
		return
	}

	// 2. Check PIN requirement
	if pinHash.Valid && pinHash.String != "" {
		cookie, err := r.Cookie("share_pin_" + tokenHash)
		secret := h.getCookieSecret()
		if err != nil || !verifyPINCookie(tokenHash, cookie.Value, secret) {
			isLocked := false
			var lockSec int
			if lockedUntil.Valid && now.Before(lockedUntil.Time) {
				isLocked = true
				lockSec = int(lockedUntil.Time.Sub(now).Seconds())
			}
			h.renderStandalone(w, "share_pin_form.html", map[string]interface{}{
				"Token":       token,
				"TripNumber":  tripNumber,
				"IsLocked":    isLocked,
				"LockSeconds": lockSec,
				"Error":       r.URL.Query().Get("error"),
			})
			return
		}
	}

	// 3. Sliding expiry: refresh expires_at on valid page view
	defaultTTL := 24
	maxTTL := 168
	if h.Config != nil {
		if h.Config.LiveMap.ShareLinkTTLHours > 0 {
			defaultTTL = h.Config.LiveMap.ShareLinkTTLHours
		}
		if h.Config.LiveMap.ShareLinkMaxTTLHours > 0 {
			maxTTL = h.Config.LiveMap.ShareLinkMaxTTLHours
		}
	}
	ttl := time.Duration(defaultTTL) * time.Hour
	maxExpiry := createdAt.Add(time.Duration(maxTTL) * time.Hour)
	newExpiry := now.Add(ttl)
	if newExpiry.After(maxExpiry) {
		newExpiry = maxExpiry
	}

	_, _ = h.db.ExecContext(r.Context(), `
		UPDATE share_links
		SET expires_at = $1, view_count = view_count + 1, last_viewed_at = $2
		WHERE id = $3`, newExpiry, now, id)

	// 4. Render share_public.html
	cfg := h.Config
	mapProvider := "auto"
	mapGoogleStyle := "m"
	mapGL := "IN"
	mapOSM := "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
	if cfg != nil {
		if cfg.LiveMap.MapTileProvider != "" {
			mapProvider = cfg.LiveMap.MapTileProvider
		}
		if cfg.LiveMap.MapGoogleStyle != "" {
			mapGoogleStyle = cfg.LiveMap.MapGoogleStyle
		}
		if cfg.LiveMap.MapGL != "" {
			mapGL = cfg.LiveMap.MapGL
		}
		if cfg.LiveMap.MapOSMURL != "" {
			mapOSM = cfg.LiveMap.MapOSMURL
		}
	}

	h.renderStandalone(w, "share_public.html", map[string]interface{}{
		"Token":        token,
		"TripNumber":   tripNumber,
		"TripStatus":   status,
		"DataEndpoint": fmt.Sprintf("/share/%s/data", token),
		"MapConfig": map[string]interface{}{
			"Provider":    mapProvider,
			"GoogleStyle": mapGoogleStyle,
			"GL":          mapGL,
			"OSMUrl":      mapOSM,
			"PollSec":     30,
		},
	})
}

// VerifyPIN handles PIN verification form submission (Spec 04 §4).
func (h *ShareHandlers) VerifyPIN(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tokenHash := sha256Hex(token)

	var id string
	var pinHash, pinSalt sql.NullString
	var createdAt, expiresAt time.Time
	var lockedUntil, revokedAt sql.NullTime
	var failedAttempts int

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, pin_hash, pin_salt, created_at, expires_at,
		       failed_pin_attempts, locked_until, revoked_at
		FROM share_links
		WHERE token_hash = $1`, tokenHash).Scan(
		&id, &pinHash, &pinSalt, &createdAt, &expiresAt,
		&failedAttempts, &lockedUntil, &revokedAt,
	)

	if err == sql.ErrNoRows {
		http.Error(w, "Share link not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	if revokedAt.Valid || expiresAt.Before(now) {
		http.Error(w, "Share link has expired or been revoked", http.StatusGone)
		return
	}

	if !pinHash.Valid || pinHash.String == "" {
		http.Redirect(w, r, "/share/"+token, http.StatusSeeOther)
		return
	}

	// Check lock status
	if lockedUntil.Valid && now.Before(lockedUntil.Time) {
		retrySec := int(lockedUntil.Time.Sub(now).Seconds())
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySec))
		http.Error(w, "Too many failed attempts. Locked for 15 minutes.", http.StatusForbidden)
		return
	}

	submittedPin := r.FormValue("pin")
	if submittedPin == "" && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Pin string `json:"pin"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		submittedPin = body.Pin
	}

	computedHash := hashPIN(submittedPin, pinSalt.String)
	if computedHash == pinHash.String {
		// SUCCESS
		secret := h.getCookieSecret()
		cookieVal := signPINCookie(tokenHash, secret)

		defaultTTL := 24
		maxTTL := 168
		if h.Config != nil {
			if h.Config.LiveMap.ShareLinkTTLHours > 0 {
				defaultTTL = h.Config.LiveMap.ShareLinkTTLHours
			}
			if h.Config.LiveMap.ShareLinkMaxTTLHours > 0 {
				maxTTL = h.Config.LiveMap.ShareLinkMaxTTLHours
			}
		}
		ttl := time.Duration(defaultTTL) * time.Hour
		maxExpiry := createdAt.Add(time.Duration(maxTTL) * time.Hour)
		newExpiry := now.Add(ttl)
		if newExpiry.After(maxExpiry) {
			newExpiry = maxExpiry
		}

		_, _ = h.db.ExecContext(r.Context(), `
			UPDATE share_links
			SET failed_pin_attempts = 0, locked_until = NULL, expires_at = $1
			WHERE id = $2`, newExpiry, id)

		isSecure := false
		if h.Config != nil && h.Config.CookieSecure {
			isSecure = true
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "share_pin_" + tokenHash,
			Value:    cookieVal,
			Path:     "/share/" + token,
			HttpOnly: true,
			Secure:   isSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(ttl.Seconds()),
		})

		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}
		http.Redirect(w, r, "/share/"+token, http.StatusSeeOther)
		return
	}

	// FAILURE
	newAttempts := failedAttempts + 1
	var newLock *time.Time
	if newAttempts >= 5 {
		lockTime := now.Add(15 * time.Minute)
		newLock = &lockTime
	}

	_, _ = h.db.ExecContext(r.Context(), `
		UPDATE share_links
		SET failed_pin_attempts = $1, locked_until = $2
		WHERE id = $3`, newAttempts, newLock, id)

	if newAttempts >= 5 {
		w.Header().Set("Retry-After", "900")
		http.Error(w, "Invalid PIN. Locked for 15 minutes.", http.StatusForbidden)
		return
	}

	http.Error(w, fmt.Sprintf("Invalid PIN. %d attempt(s) remaining.", 5-newAttempts), http.StatusForbidden)
}

// ShareData returns JSON formatted telemetry for the shared trip (Spec 04 §4, §7).
// Does NOT extend expires_at (anti-thrash protection).
func (h *ShareHandlers) ShareData(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tokenHash := sha256Hex(token)

	var id, tripID string
	var pinHash sql.NullString
	var expiresAt time.Time
	var revokedAt sql.NullTime
	var tripNumber, status string
	var vehicleID, arrivalTime sql.NullString
	var regNumber, vehNumber sql.NullString

	err := h.db.QueryRowContext(r.Context(), `
		SELECT s.id, s.trip_id, s.pin_hash, s.expires_at, s.revoked_at,
		       t.trip_number, t.status, t.vehicle_id, t.arrival_time,
		       v.registration_number, v.vehicle_number
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&id, &tripID, &pinHash, &expiresAt, &revokedAt,
		&tripNumber, &status, &vehicleID, &arrivalTime,
		&regNumber, &vehNumber,
	)

	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"share link not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	if revokedAt.Valid || expiresAt.Before(now) {
		http.Error(w, `{"error":"share link expired or revoked"}`, http.StatusGone)
		return
	}

	// PIN requirement check
	if pinHash.Valid && pinHash.String != "" {
		cookie, err := r.Cookie("share_pin_" + tokenHash)
		secret := h.getCookieSecret()
		if err != nil || !verifyPINCookie(tokenHash, cookie.Value, secret) {
			http.Error(w, `{"error":"PIN verification required"}`, http.StatusForbidden)
			return
		}
	}

	// Fetch latest telemetry snapshot for vehicle
	var lat, lng, speed, fuel, odo *float64
	var lastSeen *time.Time
	var snapFound bool

	if vehicleID.Valid && vehicleID.String != "" {
		var sLat, sLng, sSpeed, sFuel, sOdo sql.NullFloat64
		var sTs sql.NullTime
		rowErr := h.db.QueryRowContext(r.Context(), `
			SELECT latitude, longitude, speed, fuel_level, odometer, timestamp
			FROM telemetry_snapshots
			WHERE vehicle_id = $1 AND latitude IS NOT NULL AND longitude IS NOT NULL
			ORDER BY timestamp DESC LIMIT 1`, vehicleID.String).Scan(
			&sLat, &sLng, &sSpeed, &sFuel, &sOdo, &sTs,
		)
		if rowErr == nil && sLat.Valid && sLng.Valid {
			snapFound = true
			lat = &sLat.Float64
			lng = &sLng.Float64
			if sSpeed.Valid {
				speed = &sSpeed.Float64
			}
			if sFuel.Valid {
				fuel = &sFuel.Float64
			}
			if sOdo.Valid {
				odo = &sOdo.Float64
			}
			if sTs.Valid {
				tUTC := sTs.Time.UTC()
				lastSeen = &tUTC
			}
		}
	}

	// Check maintenance_due
	maintDue := false
	if vehicleID.Valid && vehicleID.String != "" {
		var md sql.NullBool
		_ = h.db.QueryRowContext(r.Context(),
			`SELECT maintenance_due FROM vehicles WHERE id = $1`, vehicleID.String).Scan(&md)
		if md.Valid && md.Bool {
			maintDue = true
		}
	}

	// ETA Hybrid Calculation (Spec 04 §5, 3D)
	var etaMin, etaMax *string
	etaMethod := "scheduled"
	if h.EtaService != nil {
		if res, err := h.EtaService.Calculate(r.Context(), tripID); err == nil {
			minT := res.EtaMin.UTC().Format(time.RFC3339)
			maxT := res.EtaMax.UTC().Format(time.RFC3339)
			etaMin = &minT
			etaMax = &maxT
			etaMethod = res.Method
		}
	} else {
		// Fallback to scheduled arrival time ±15 min if EtaService is not attached
		isLiveStatus := status == "started" || status == "reached_pickup" || status == "in_transit" || status == "delivered"
		staleMin := 15 * time.Minute
		if h.Config != nil && h.Config.LiveMap.TelemetryStaleMin > 0 {
			staleMin = time.Duration(h.Config.LiveMap.TelemetryStaleMin) * time.Minute
		}
		isFresh := snapFound && lastSeen != nil && now.Sub(*lastSeen) <= staleMin
		if isLiveStatus && isFresh && arrivalTime.Valid && arrivalTime.String != "" {
			if arrT, err := time.Parse("2006-01-02 15:04:05", arrivalTime.String); err == nil {
				minT := arrT.Add(-15 * time.Minute).UTC().Format(time.RFC3339)
				maxT := arrT.Add(15 * time.Minute).UTC().Format(time.RFC3339)
				etaMin = &minT
				etaMax = &maxT
			} else if arrT, err := time.Parse(time.RFC3339, arrivalTime.String); err == nil {
				minT := arrT.Add(-15 * time.Minute).UTC().Format(time.RFC3339)
				maxT := arrT.Add(15 * time.Minute).UTC().Format(time.RFC3339)
				etaMin = &minT
				etaMax = &maxT
			}
		}
	}

	vehLabel := "—"
	if regNumber.Valid && regNumber.String != "" {
		vehLabel = regNumber.String
	} else if vehNumber.Valid && vehNumber.String != "" {
		vehLabel = vehNumber.String
	}

	var lastSeenStr *string
	if lastSeen != nil {
		s := lastSeen.Format(time.RFC3339)
		lastSeenStr = &s
	}

	resp := map[string]interface{}{
		"trip_number":     tripNumber,
		"vehicle_label":   vehLabel,
		"status":          status,
		"lat":             lat,
		"lng":             lng,
		"speed":           speed,
		"fuel_level":      fuel,
		"odometer":        odo,
		"last_seen":       lastSeenStr,
		"eta_min":         etaMin,
		"eta_max":         etaMax,
		"eta_method":      etaMethod,
		"maintenance_due": maintDue,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_ = json.NewEncoder(w).Encode(resp)
}

// ListShares renders the administrative share link management page (Spec 04 §4).
func (h *ShareHandlers) ListShares(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	tenantID := string(shared.MustTenantID(r.Context()))

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT s.id, s.trip_id, t.trip_number, s.created_by, COALESCE(u.name, s.created_by),
		       s.created_at, s.expires_at, s.last_viewed_at, s.view_count,
		       s.pin_hash IS NOT NULL, s.revoked_at
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		LEFT JOIN users u ON u.id = s.created_by
		WHERE t.tenant_id = $1
		ORDER BY s.created_at DESC`, tenantID)
	if err != nil {
		http.Error(w, "failed to load share links", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	var shares []ShareLinkItem
	for rows.Next() {
		var item ShareLinkItem
		var lastViewed, revoked sql.NullTime
		var hasPIN int
		var createdAtVal, expiresAtVal interface{}
		if err := rows.Scan(
			&item.ID, &item.TripID, &item.TripNumber, &item.CreatedBy, &item.CreatorName,
			&createdAtVal, &expiresAtVal, &lastViewed, &item.ViewCount,
			&hasPIN, &revoked,
		); err != nil {
			continue
		}
		item.HasPIN = (hasPIN != 0)
		item.CreatedAt = parseAnyTime(createdAtVal)
		item.ExpiresAt = parseAnyTime(expiresAtVal)
		if lastViewed.Valid {
			item.LastViewedAt = &lastViewed.Time
		}
		if revoked.Valid {
			item.RevokedAt = &revoked.Time
			item.Status = "revoked"
		} else if item.ExpiresAt.Before(now) {
			item.Status = "expired"
		} else {
			item.Status = "active"
		}
		shares = append(shares, item)
	}

	h.renderPage(w, r, "shares_list.html", PageData{
		Title: "Trip Share Links",
		User:  session,
		Extra: map[string]interface{}{
			"Shares": shares,
		},
	})
}

// RevokeShare revokes a share link, rendering it immediately unusable (Spec 04 §4).
func (h *ShareHandlers) RevokeShare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tenantID := string(shared.MustTenantID(r.Context()))

	_, err := h.db.ExecContext(r.Context(), `
		UPDATE share_links
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND trip_id IN (SELECT id FROM trips WHERE tenant_id = $2)`,
		id, tenantID)
	if err != nil {
		http.Error(w, `{"error":"failed to revoke share link"}`, http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	http.Redirect(w, r, "/shares", http.StatusSeeOther)
}

func (h *ShareHandlers) renderStandalone(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	tmpl := h.Templates.Lookup(name)
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

func parseAnyTime(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch val := v.(type) {
	case time.Time:
		return val
	case string:
		return parseTimeStr(val)
	case []byte:
		return parseTimeStr(string(val))
	}
	return time.Time{}
}

func parseTimeStr(s string) time.Time {
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
