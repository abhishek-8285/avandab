package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"transport-app/internal/shared/ports"
)

// OTPHandlers power phone-number verification (Phase 4, zero-cost comms).
// Send issues a 6-digit code delivered via the configured SMS sender
// (SMS_WEBHOOK_URL / SMS_WEBHOOK_TOKEN — provider-agnostic webhook);
// Verify checks the code and stamps users.phone_verified_at. Both endpoints
// are public + rate-limited (mounted like forgot-password), respond with a
// generic envelope so a valid vs invalid phone is indistinguishable, and the
// raw code is only ever returned in development without a configured SMS
// sender (mirrors the password-reset dev-link pattern).
type OTPHandlers struct {
	*App
}

var phoneRe = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// SendOTP POST /api/v1/auth/otp/send — issues a code and delivers it.
func (h *OTPHandlers) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !phoneRe.MatchString(req.Phone) {
		writeJSONError(w, http.StatusBadRequest, "phone must be in international format, e.g. +919876543210")
		return
	}
	if h.App == nil || h.App.OTPStore == nil {
		writeJSONError(w, http.StatusInternalServerError, "OTP service not configured")
		return
	}

	// Throttle (30s) BEFORE burning an SMS — protects the free-tier quota.
	if blocked := h.App.OTPStore.ResendBlockedFor(req.Phone); blocked > 0 {
		// Same generic envelope: never reveal send state on a public endpoint.
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		return
	}

	code, err := h.App.OTPStore.Create(req.Phone)
	if err != nil {
		// Throttle still blocks (race): respond generically.
		slog.Warn("otp create failed", "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		return
	}
	slog.Info("OTP issued", "phone_prefix", phonePrefix(req.Phone))

	if h.App.Notify != nil && h.App.Notify.SMSConfigured() {
		msg := fmt.Sprintf("Your Avandab verification code is %s. It expires in 5 minutes. Do not share it.", code)
		if serr := h.App.Notify.SendSMS(r.Context(), ports.NotificationMessage{
			Recipient: req.Phone,
			Body:      msg,
		}); serr != nil {
			slog.Error("OTP SMS delivery failed", "phone_prefix", phonePrefix(req.Phone), "error", serr)
			// Don't leak the code on a public endpoint; the client can retry.
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
			return
		}
	} else if h.Config != nil && h.Config.IsDevelopment() {
		// No SMS sender: dev convenience echoes the code (reset-link pattern).
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "dev_code": code})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// VerifyOTP POST /api/v1/auth/otp/verify — checks the code and stamps
// users.phone_verified_at for any user holding that phone number. The
// response is identical whether or not an account exists (no enumeration);
// a verified-but-unregistered phone is fine — the driver app links it at
// registration.
func (h *OTPHandlers) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !phoneRe.MatchString(req.Phone) || len(req.Code) != 6 {
		writeJSONError(w, http.StatusBadRequest, "phone and 6-digit code are required")
		return
	}
	if h.App == nil || h.App.OTPStore == nil || h.App.DB == nil {
		writeJSONError(w, http.StatusInternalServerError, "OTP service not configured")
		return
	}

	if !h.App.OTPStore.Verify(req.Phone, req.Code) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"verified": false})
		return
	}

	// Possession proven: stamp for every user with this phone (typically one).
	if _, err := h.App.DB.ExecContext(r.Context(),
		`UPDATE users SET phone_verified_at = datetime('now') WHERE phone = ? AND phone_verified_at IS NULL`,
		req.Phone); err != nil {
		slog.Error("otp verify stamp failed", "phone_prefix", phonePrefix(req.Phone), "error", err)
	}

	slog.Info("OTP verified", "phone_prefix", phonePrefix(req.Phone))
	writeJSON(w, http.StatusOK, map[string]interface{}{"verified": true})
}

// phonePrefix redacts everything but the first 3 digits of an international
// phone for logs (never log full numbers).
func phonePrefix(phone string) string {
	if len(phone) >= 4 {
		return phone[:4] + "***"
	}
	return "***"
}
