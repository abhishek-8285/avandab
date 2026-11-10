package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/eta"
	"transport-app/internal/shared"
	sharing "transport-app/internal/sharing"
	"transport-app/internal/trip/application"
)

// ShareHandlers powers trip share link generation, public viewing, PIN validation,
// and administrative revocation (Spec 04 §4, §7, Spec 20 §1).
type ShareHandlers struct {
	*App
	db              *sql.DB
	EtaService      *eta.EtaService
	TimelineUseCase *application.TimelineUseCase
}

// NewShareHandlers creates a new ShareHandlers instance.
func NewShareHandlers(app *App, db *sql.DB) *ShareHandlers {
	return &ShareHandlers{
		App:             app,
		db:              db,
		TimelineUseCase: application.NewTimelineUseCase(db, nil),
	}
}

type CreateShareRequest struct {
	Pin      string `json:"pin"`
	TTLHours int    `json:"ttl_hours"`
}

type ShareLinkItem = sharing.LinkItem

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

// requireTenant resolves the acting org. Fail closed with 401: share routes
// sit behind auth middleware which sets tenant; a missing tenant means a
// bad/expired session, never a 500 panic.
func (h *ShareHandlers) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid, err := shared.TenantRequired(r.Context())
	if err != nil {
		http.Error(w, `{"error":"tenant required"}`, http.StatusUnauthorized)
		return "", false
	}
	return string(tid), true
}

// ShareMyTrip backs the driver app's SHARE LIVE button.
//
// A share link hands an unauthenticated customer live tracking of one trip, so
// the caller must be the driver assigned to that exact trip: minting against
// arbitrary trip ids would let any signed-in driver read any trip's movement.
// Mounted at /api/v1/drivers/me/trips/{id}/share behind RequirePermission
// ("driver","write-self"); CreateShare's tenant check, TTL clamp and
// SHARE_LINK_MAX_ACTIVE cap still apply afterwards.
func (h *ShareHandlers) ShareMyTrip(w http.ResponseWriter, r *http.Request) {
	user, ok := h.getUserFromContext(r)
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	// Same identity rule as GET /api/v1/drivers/me: trips.driver_id may carry
	// the driver row id, the human driver code, or the auth user id directly.
	onTrip, err := sharing.New(h.db).IsDriverOnTrip(r.Context(), tenantID, chi.URLParam(r, "id"), user.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "share trip ownership lookup failed",
			slog.String("trip_id", chi.URLParam(r, "id")), slog.Any("error", err))
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	if !onTrip {
		http.Error(w, `{"error":"not your trip"}`, http.StatusForbidden)
		return
	}

	h.CreateShare(w, r)
}

// CreateShare generates a cryptographically random token, stores its SHA-256 hash,
// and returns the raw URL/token once (Spec 04 §4).
func (h *ShareHandlers) CreateShare(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		tripID = r.FormValue("trip_id")
	}

	user, _ := h.getUserFromContext(r)
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	// 1. Resolve share-link policy: per-trip active cap and TTL clamp.
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

	// 2. Parse request (JSON or Form)
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

	// 6. Persist: trip-tenant check + active-link cap + INSERT.
	if _, err := sharing.New(h.db).Create(r.Context(), tenantID, tripID, linkID, tokenHash,
		pinHash, pinSalt, createdBy, now, expiresAt, maxActive); err != nil {
		switch {
		case errors.Is(err, sharing.ErrTripNotFound):
			http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
		case errors.Is(err, sharing.ErrMaxActive):
			http.Error(w, `{"error":"maximum active share links reached for this trip"}`, http.StatusConflict)
		case errors.Is(err, sharing.ErrLinkCreate):
			http.Error(w, `{"error":"failed to create share link"}`, http.StatusInternalServerError)
		default:
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		}
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

	link, err := sharing.New(h.db).ByToken(r.Context(), tokenHash)
	var id string
	var pinHash sql.NullString
	var createdAt, expiresAt time.Time
	var lockedUntil, revokedAt sql.NullTime
	var tripNumber, status string
	if link != nil {
		id = link.ID
		pinHash = link.PinHash
		createdAt, expiresAt = link.CreatedAt, link.ExpiresAt
		lockedUntil, revokedAt = link.LockedUntil, link.RevokedAt
		tripNumber, status = link.TripNumber, link.Status
	}

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
			h.renderStandalone(w, r, "share_pin_form.html", map[string]interface{}{
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

	if sErr := sharing.New(h.db).RecordView(r.Context(), id, newExpiry, now); sErr != nil {
		slog.WarnContext(r.Context(), "share view counter update failed", slog.Any("error", sErr))
	}

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

	w.Header().Set("Cache-Control", "no-cache, s-maxage=10, stale-while-revalidate=30")
	h.renderStandalone(w, r, "share_public.html", map[string]interface{}{
		"Token":            token,
		"TripNumber":       tripNumber,
		"TripStatus":       status,
		"DataEndpoint":     fmt.Sprintf("/share/%s/data", token),
		"TimelineEndpoint": fmt.Sprintf("/api/v1/share/%s/timeline", token),
		"MapConfig": map[string]interface{}{
			"Provider":    mapProvider,
			"GoogleStyle": mapGoogleStyle,
			"GL":          mapGL,
			"OSMUrl":      mapOSM,
			"PollSec":     5,
		},
	})
}

// VerifyPIN handles PIN verification form submission (Spec 04 §4).
func (h *ShareHandlers) VerifyPIN(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tokenHash := sha256Hex(token)

	plink, err := sharing.New(h.db).PINByToken(r.Context(), tokenHash)
	var id string
	var pinHash, pinSalt sql.NullString
	var createdAt, expiresAt time.Time
	var lockedUntil, revokedAt sql.NullTime
	var failedAttempts int
	if plink != nil {
		id = plink.ID
		pinHash, pinSalt = plink.PinHash, plink.PinSalt
		createdAt, expiresAt = plink.CreatedAt, plink.ExpiresAt
		failedAttempts, lockedUntil, revokedAt = plink.FailedPinAttempts, plink.LockedUntil, plink.RevokedAt
	}

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

		if sErr := sharing.New(h.db).RecordPINSuccess(r.Context(), id, newExpiry); sErr != nil {
			slog.WarnContext(r.Context(), "share PIN unlock counter reset failed", slog.Any("error", sErr))
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "share_pin_" + tokenHash,
			Value:    cookieVal,
			Path:     "/share/" + token,
			HttpOnly: true,
			Secure:   true,
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

	if sErr := sharing.New(h.db).RecordPINFailure(r.Context(), id, newAttempts, newLock); sErr != nil {
		// Security-relevant counter: a lost write means brute-force lockout
		// tracking silently resets.
		slog.WarnContext(r.Context(), "share PIN failure counter update failed", slog.Any("error", sErr))
	}

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

	dlink, err := sharing.New(h.db).DataByToken(r.Context(), tokenHash)
	var tripID string
	var pinHash sql.NullString
	var expiresAt time.Time
	var revokedAt sql.NullTime
	var tripNumber, status string
	var vehicleID, arrivalTime sql.NullString
	var regNumber, vehNumber sql.NullString
	if dlink != nil {
		tripID = dlink.TripID
		pinHash = dlink.PinHash
		expiresAt, revokedAt = dlink.ExpiresAt, dlink.RevokedAt
		tripNumber, status = dlink.TripNumber, dlink.Status
		vehicleID, arrivalTime = dlink.VehicleID, dlink.ArrivalTime
		regNumber, vehNumber = dlink.RegNumber, dlink.VehicleNumber
	}

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

	var lat, lng, speed, fuel, odo *float64
	var lastSeen *time.Time
	var snapFound bool
	maintDue := false

	// Latest telemetry snapshot from the read model.
	if vehicleID.Valid && vehicleID.String != "" {
		snap := sharing.New(h.db).Snapshot(r.Context(), vehicleID.String)
		snapFound = snap.Found
		lat, lng = snap.Lat, snap.Lng
		speed, fuel, odo = snap.Speed, snap.Fuel, snap.Odometer
		lastSeen = snap.LastSeen
	}

	// Maintenance flag from the read model.
	maintDue = sharing.New(h.db).MaintenanceDue(r.Context(), vehicleID.String)

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

	var polyline [][]float64
	// Polyline from the read model; fall back to the latest fix.
	polyline = sharing.New(h.db).Polyline(r.Context(), tripID)
	if len(polyline) == 0 && lat != nil && lng != nil {
		polyline = append(polyline, []float64{*lat, *lng})
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
		"polyline":        polyline,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, s-maxage=5, stale-while-revalidate=10")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *ShareHandlers) getTimelineUseCase() *application.TimelineUseCase {
	if h.TimelineUseCase == nil {
		h.TimelineUseCase = application.NewTimelineUseCase(h.db, h.EtaService)
	}
	return h.TimelineUseCase
}

// ShareTimeline returns the CX tracking timeline for the shared trip (Spec 20 §1, §3).
func (h *ShareHandlers) ShareTimeline(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tokenHash := sha256Hex(token)

	tlink, err := sharing.New(h.db).TimelineByToken(r.Context(), tokenHash)
	var tripID, tripTenantID string
	var pinHash sql.NullString
	var expiresAt time.Time
	var revokedAt sql.NullTime
	if tlink != nil {
		tripID, tripTenantID = tlink.TripID, tlink.TenantID
		pinHash = tlink.PinHash
		expiresAt, revokedAt = tlink.ExpiresAt, tlink.RevokedAt
	}

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

	uc := h.getTimelineUseCase()
	timeline, err := uc.BuildTimeline(r.Context(), tripID, tripTenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to build CX timeline", slog.String("trip_id", tripID), slog.Any("error", err))
		http.Error(w, `{"error":"failed to generate timeline"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, s-maxage=2, stale-while-revalidate=5")
	_ = json.NewEncoder(w).Encode(timeline)
}

// ListShares renders the administrative share link management page (Spec 04 §4).
func (h *ShareHandlers) ListShares(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	items, err := sharing.New(h.db).List(r.Context(), tenantID)
	if err != nil {
		http.Error(w, "failed to load share links", http.StatusInternalServerError)
		return
	}
	shares := items

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
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	if _, err := sharing.New(h.db).Revoke(r.Context(), tenantID, id); err != nil {
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

func (h *ShareHandlers) renderStandalone(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if m, ok := data.(map[string]interface{}); ok {
		m["Lang"] = langOf(r)
	}
	tmpl := h.templatesFor(r).Lookup(name)
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
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
