package notifications

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"transport-app/internal/events"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

var (
	ErrEmptyToken       = errors.New("push token cannot be empty")
	ErrTokenInvalid     = errors.New("push token is invalid or unregistered")
	ErrDispatchFailed   = errors.New("failed to dispatch FCM push notification")
	ErrFCMNotConfigured = errors.New("fcm credentials not configured")
)

// PushMessage defines the push notification payload sent to devices.
type PushMessage struct {
	Token    string            `json:"token"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Priority string            `json:"priority,omitempty"`
	Sound    string            `json:"sound,omitempty"`
}

// SentPushRecord records dispatched push notifications (in-memory for mock/testing).
type SentPushRecord struct {
	Token    string            `json:"token"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	SentAt   time.Time         `json:"sent_at"`
	MockMode bool              `json:"mock_mode"`
}

// FCMConfig holds Firebase Cloud Messaging settings.
type FCMConfig struct {
	ProjectID          string // FCM_PROJECT_ID (for HTTP v1)
	ServerKey          string // FCM_SERVER_KEY (for Legacy API)
	ServiceAccountJSON string // FCM_SERVICE_ACCOUNT (raw JSON or file path)
	Endpoint           string // Optional custom endpoint / base URL
	MockMode           bool   // Explicit mock mode override
	HTTPClient         *http.Client
}

// FCMService provides push notification dispatching via FCM HTTP v1 / Legacy APIs
// and manages driver device push tokens with safe mock mode fallback.
type FCMService struct {
	db         *sql.DB
	cfg        FCMConfig
	logger     *slog.Logger
	httpClient *http.Client
	baseURL    string

	tokenMu          sync.Mutex
	cachedOAuthToken string
	tokenExpiry      time.Time

	mu           sync.RWMutex
	sentMessages []SentPushRecord
}

// NewFCMService creates a new FCMService instance.
func NewFCMService(db *sql.DB, cfg FCMConfig, logger *slog.Logger) *FCMService {
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	baseURL := strings.TrimRight(cfg.Endpoint, "/")
	if baseURL == "" {
		baseURL = "https://fcm.googleapis.com"
	}

	return &FCMService{
		db:           db,
		cfg:          cfg,
		logger:       logger,
		httpClient:   httpClient,
		baseURL:      baseURL,
		sentMessages: make([]SentPushRecord, 0),
	}
}

// IsMockMode reports whether the service operates in mock/log mode.
func (s *FCMService) IsMockMode() bool {
	if s.cfg.MockMode {
		return true
	}
	return s.cfg.ProjectID == "" && s.cfg.ServerKey == "" && s.cfg.ServiceAccountJSON == ""
}

// SendToToken sends a push notification to a single FCM device token.
func (s *FCMService) SendToToken(ctx context.Context, token, title, body string, data map[string]string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrEmptyToken
	}

	if data == nil {
		data = make(map[string]string)
	}

	if s.IsMockMode() {
		record := SentPushRecord{
			Token:    token,
			Title:    title,
			Body:     body,
			Data:     data,
			SentAt:   time.Now().UTC(),
			MockMode: true,
		}

		s.mu.Lock()
		s.sentMessages = append(s.sentMessages, record)
		s.mu.Unlock()

		s.logger.Info("FCM push dispatched (mock mode)",
			"token", token,
			"title", title,
			"body", body,
			"data_count", len(data),
		)
		return nil
	}

	// Dispatch via HTTP v1 or Legacy FCM
	if s.cfg.ProjectID != "" {
		return s.sendHTTPv1(ctx, token, title, body, data)
	}

	if s.cfg.ServerKey != "" {
		return s.sendLegacyHTTP(ctx, token, title, body, data)
	}

	// Fallback to recording in mock mode if no direct keys
	s.logger.Info("FCM unconfigured; recording push in log mode",
		"token", token,
		"title", title,
	)
	return nil
}

// sendLegacyHTTP dispatches to https://fcm.googleapis.com/fcm/send using the Server Key.
func (s *FCMService) sendLegacyHTTP(ctx context.Context, token, title, body string, data map[string]string) error {
	payload := map[string]interface{}{
		"to": token,
		"notification": map[string]interface{}{
			"title": title,
			"body":  body,
			"sound": "default",
		},
		"data":     data,
		"priority": "high",
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal FCM legacy payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s/fcm/send", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build FCM request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("key=%s", s.cfg.ServerKey))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fcm legacy request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("FCM legacy push responded with non-200 status",
			"status_code", resp.StatusCode,
			"response", string(respBytes),
		)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return ErrTokenInvalid
		}
		return fmt.Errorf("%w: status %d (%s)", ErrDispatchFailed, resp.StatusCode, string(respBytes))
	}

	// Check for token-level failure in response
	var legacyResp struct {
		Failure int `json:"failure"`
		Results []struct {
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBytes, &legacyResp); err == nil {
		if legacyResp.Failure > 0 && len(legacyResp.Results) > 0 {
			errStr := legacyResp.Results[0].Error
			if isTokenInvalidReason(errStr) {
				return ErrTokenInvalid
			}
			return fmt.Errorf("%w: %s", ErrDispatchFailed, errStr)
		}
	}

	s.recordSent(token, title, body, data, false)
	return nil
}

// sendHTTPv1 dispatches to https://fcm.googleapis.com/v1/projects/{project_id}/messages:send.
func (s *FCMService) sendHTTPv1(ctx context.Context, token, title, body string, data map[string]string) error {
	bearerToken := s.cfg.ServerKey
	if bearerToken == "" && s.cfg.ServiceAccountJSON != "" {
		var err error
		bearerToken, err = s.getOAuth2AccessToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to obtain FCM OAuth2 token: %w", err)
		}
	}

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": token,
			"notification": map[string]interface{}{
				"title": title,
				"body":  body,
			},
			"data": data,
			"android": map[string]interface{}{
				"priority": "high",
				"notification": map[string]interface{}{
					"sound":      "default",
					"channel_id": "dispatches",
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal FCM v1 payload: %w", err)
	}

	projectID := s.cfg.ProjectID
	if projectID == "" {
		projectID = "message-97140"
	}

	reqURL := fmt.Sprintf("%s/v1/projects/%s/messages:send", s.baseURL, projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build FCM request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fcm v1 request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("FCM v1 push responded with error",
			"status_code", resp.StatusCode,
			"response", string(respBytes),
		)
		if resp.StatusCode == http.StatusNotFound || strings.Contains(string(respBytes), "UNREGISTERED") {
			return ErrTokenInvalid
		}
		return fmt.Errorf("%w: status %d (%s)", ErrDispatchFailed, resp.StatusCode, string(respBytes))
	}

	s.recordSent(token, title, body, data, false)
	return nil
}

type googleServiceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
}

func (s *FCMService) getOAuth2AccessToken(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	if s.cachedOAuthToken != "" && time.Now().Before(s.tokenExpiry) {
		return s.cachedOAuthToken, nil
	}

	if s.cfg.ServiceAccountJSON == "" {
		return "", errors.New("no service account JSON configured")
	}

	rawJSON := s.cfg.ServiceAccountJSON
	if !strings.HasPrefix(strings.TrimSpace(rawJSON), "{") {
		content, err := os.ReadFile(rawJSON)
		if err != nil {
			return "", fmt.Errorf("failed to read service account file: %w", err)
		}
		rawJSON = string(content)
	}

	var sa googleServiceAccount
	if err := json.Unmarshal([]byte(rawJSON), &sa); err != nil {
		return "", fmt.Errorf("failed to parse service account JSON: %w", err)
	}

	if s.cfg.ProjectID == "" && sa.ProjectID != "" {
		s.cfg.ProjectID = sa.ProjectID
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", errors.New("failed to decode private key PEM from service account")
	}

	var privKey *rsa.PrivateKey
	if parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		var ok bool
		privKey, ok = parsedKey.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("service account private key is not an RSA key")
		}
	} else if pkcs1Key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		privKey = pkcs1Key
	} else {
		return "", fmt.Errorf("failed to parse RSA private key: %w", err)
	}

	now := time.Now().UTC()
	headerBytes := []byte(`{"alg":"RS256","typ":"JWT"}`)
	claims := map[string]interface{}{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   "https://oauth2.googleapis.com/token",
		"exp":   now.Add(1 * time.Hour).Unix(),
		"iat":   now.Unix(),
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JWT claims: %w", err)
	}

	headerEncoded := base64.RawURLEncoding.EncodeToString(headerBytes)
	claimsEncoded := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := headerEncoded + "." + claimsEncoded

	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	signedJWT := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	formData := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signedJWT},
	}

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create oauth2 token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := s.httpClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("oauth2 token request failed: %w", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	respBody, _ := io.ReadAll(tokenResp.Body)
	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth2 token request rejected (status %d): %s", tokenResp.StatusCode, string(respBody))
	}

	var tokenResult struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResult); err != nil {
		return "", fmt.Errorf("failed to parse oauth2 token response: %w", err)
	}

	s.cachedOAuthToken = tokenResult.AccessToken
	s.tokenExpiry = now.Add(50 * time.Minute)
	return s.cachedOAuthToken, nil
}

func (s *FCMService) recordSent(token, title, body string, data map[string]string, mock bool) {
	record := SentPushRecord{
		Token:    token,
		Title:    title,
		Body:     body,
		Data:     data,
		SentAt:   time.Now().UTC(),
		MockMode: mock,
	}

	s.mu.Lock()
	s.sentMessages = append(s.sentMessages, record)
	s.mu.Unlock()
}

func isTokenInvalidReason(reason string) bool {
	reason = strings.ToLower(reason)
	return reason == "notregistered" ||
		reason == "invalidregistration" ||
		reason == "mismatchsenderid" ||
		reason == "unregistered" ||
		strings.Contains(reason, "invalid token")
}

// SendToTokens sends push notifications to multiple device tokens.
func (s *FCMService) SendToTokens(ctx context.Context, tokens []string, title, body string, data map[string]string) (int, error) {
	if len(tokens) == 0 {
		return 0, nil
	}

	successCount := 0
	var lastErr error

	for _, token := range tokens {
		err := s.SendToToken(ctx, token, title, body, data)
		if err != nil {
			lastErr = err
			if errors.Is(err, ErrTokenInvalid) {
				_ = s.DeactivateToken(ctx, token)
			}
		} else {
			successCount++
		}
	}

	if successCount == 0 && lastErr != nil {
		return 0, lastErr
	}
	return successCount, nil
}

// GetActiveTokens retrieves all active push tokens for a driver in a given tenant.
func (s *FCMService) GetActiveTokens(ctx context.Context, tenantID, driverID string) ([]string, error) {
	if s.db == nil || tenantID == "" || driverID == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT push_token
		FROM driver_push_tokens
		WHERE tenant_id = ? AND (driver_id = ? OR user_id = ?) AND is_active = 1
		ORDER BY updated_at DESC
	`, tenantID, driverID, driverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query driver push tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []string
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err == nil && tok != "" {
			tokens = append(tokens, tok)
		}
	}
	return tokens, rows.Err()
}

// DeactivateToken deactivates an invalid or expired push token in the database.
func (s *FCMService) DeactivateToken(ctx context.Context, token string) error {
	if s.db == nil || token == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE driver_push_tokens
		SET is_active = 0, updated_at = CURRENT_TIMESTAMP
		WHERE push_token = ?
	`, token)
	if err != nil {
		s.logger.Warn("failed to deactivate invalid push token", "token", token, "error", err)
		return err
	}
	s.logger.Info("deactivated invalid push token", "token", token)
	return nil
}

// SendToDriver queries all active push tokens for a driver and dispatches push notifications.
func (s *FCMService) SendToDriver(ctx context.Context, tenantID, driverID, title, body string, data map[string]string) (int, error) {
	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	tokens, err := s.GetActiveTokens(ctx, tenantID, driverID)
	if err != nil {
		s.logger.Warn("could not query driver push tokens", "tenant_id", tenantID, "driver_id", driverID, "error", err)
	}

	if len(tokens) == 0 {
		s.logger.Debug("no active push tokens registered for driver",
			"tenant_id", tenantID,
			"driver_id", driverID,
			"title", title,
		)
		// If in mock mode, also log mock dispatch so tests and dev logs capture driver notifications
		if s.IsMockMode() {
			s.recordSent(fmt.Sprintf("mock-driver-token-%s", driverID), title, body, data, true)
		}
		return 0, nil
	}

	return s.SendToTokens(ctx, tokens, title, body, data)
}

// SendPush implements the ports.NotificationService push provider interface.
func (s *FCMService) SendPush(ctx context.Context, msg ports.NotificationMessage) error {
	tenantID := msg.TenantID
	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	data := make(map[string]string)
	for k, v := range msg.Metadata {
		data[k] = fmt.Sprintf("%v", v)
	}

	// If recipient looks like an FCM token directly
	if strings.HasPrefix(msg.Recipient, "ExponentPushToken") || len(msg.Recipient) > 40 {
		return s.SendToToken(ctx, msg.Recipient, msg.Subject, msg.Body, data)
	}

	// Otherwise treat recipient as driver/user ID
	driverID := msg.Recipient
	if driverID == "" {
		driverID = msg.UserID
	}

	_, err := s.SendToDriver(ctx, tenantID, driverID, msg.Subject, msg.Body, data)
	return err
}

// GetSentMessages returns a copy of all sent push notifications.
func (s *FCMService) GetSentMessages() []SentPushRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]SentPushRecord, len(s.sentMessages))
	copy(res, s.sentMessages)
	return res
}

// ClearSentMessages resets the recorded sent push notifications.
func (s *FCMService) ClearSentMessages() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentMessages = make([]SentPushRecord, 0)
}

// SubscribeEvents wires domain event listeners on the event bus for push notifications.
func (s *FCMService) SubscribeEvents(bus events.EventBus) {
	if bus == nil {
		return
	}

	// 1. Trip Assigned / Trip Created
	tripAssignedHandler := func(ctx context.Context, e events.Event) error {
		return s.handleTripAssigned(ctx, e)
	}

	bus.Subscribe(events.TripAssigned, tripAssignedHandler)
	bus.Subscribe("TripAssignedEvent", tripAssignedHandler)
	bus.Subscribe(events.TripCreated, tripAssignedHandler)
	bus.Subscribe("TripCreatedEvent", tripAssignedHandler)

	// 2. Trip Cancelled
	tripCancelledHandler := func(ctx context.Context, e events.Event) error {
		return s.handleTripCancelled(ctx, e)
	}

	bus.Subscribe(events.TripCancelled, tripCancelledHandler)
	bus.Subscribe("TripCancelledEvent", tripCancelledHandler)

	// 3. Emergency SOS Alerts
	sosHandler := func(ctx context.Context, e events.Event) error {
		return s.handleSOSAlert(ctx, e)
	}

	bus.Subscribe(events.SOSEvent, sosHandler)
	bus.Subscribe("alert.sos", sosHandler)
	bus.Subscribe("SOSEvent", sosHandler)
	bus.Subscribe("telemetry.sos", sosHandler)
}

func (s *FCMService) handleTripAssigned(ctx context.Context, e events.Event) error {
	m := parseEventPayload(e.Payload)

	tripID := extractString(m, "trip_id", "TripID", "tripId", "id")
	driverID := extractString(m, "driver_id", "DriverID", "driverId")
	tenantID := extractString(m, "tenant_id", "TenantID", "tenantId")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	var destination, tripNumber string

	// Lookup trip details from database if available
	if s.db != nil && tripID != "" {
		var dbDriverID, dbTripNum, dbDest sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT t.driver_id, t.trip_number, r.destination
			FROM trips t
			LEFT JOIN routes r ON t.route_id = r.id
			WHERE t.id = ? OR t.trip_number = ?
			LIMIT 1
		`, tripID, tripID).Scan(&dbDriverID, &dbTripNum, &dbDest)
		if err == nil {
			if driverID == "" && dbDriverID.Valid {
				driverID = dbDriverID.String
			}
			if dbTripNum.Valid {
				tripNumber = dbTripNum.String
			}
			if dbDest.Valid {
				destination = dbDest.String
			}
		}
	}

	if driverID == "" {
		// No driver assigned yet
		return nil
	}

	if tripNumber == "" {
		tripNumber = extractString(m, "trip_number", "TripNumber", "tripNumber")
		if tripNumber == "" {
			tripNumber = tripID
		}
	}

	if destination == "" {
		destination = extractString(m, "destination", "Destination", "dest")
		if destination == "" {
			destination = "Scheduled Destination"
		}
	}

	title := "New Trip Assigned"
	if tripNumber != "" {
		title = fmt.Sprintf("New Trip Assigned: #%s", tripNumber)
	}

	body := fmt.Sprintf("New Trip Assigned: Destination %s", destination)

	data := map[string]string{
		"type":        "trip_assigned",
		"trip_id":     tripID,
		"trip_number": tripNumber,
		"destination": destination,
		"status":      "assigned",
	}

	_, err := s.SendToDriver(ctx, tenantID, driverID, title, body, data)
	return err
}

func (s *FCMService) handleTripCancelled(ctx context.Context, e events.Event) error {
	m := parseEventPayload(e.Payload)

	tripID := extractString(m, "trip_id", "TripID", "tripId", "id")
	driverID := extractString(m, "driver_id", "DriverID", "driverId")
	tenantID := extractString(m, "tenant_id", "TenantID", "tenantId")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	var tripNumber string

	// Lookup trip details from database if available
	if s.db != nil && tripID != "" {
		var dbDriverID, dbTripNum sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT driver_id, trip_number
			FROM trips
			WHERE id = ? OR trip_number = ?
			LIMIT 1
		`, tripID, tripID).Scan(&dbDriverID, &dbTripNum)
		if err == nil {
			if driverID == "" && dbDriverID.Valid {
				driverID = dbDriverID.String
			}
			if dbTripNum.Valid {
				tripNumber = dbTripNum.String
			}
		}
	}

	if driverID == "" {
		return nil
	}

	if tripNumber == "" {
		tripNumber = extractString(m, "trip_number", "TripNumber", "tripNumber")
		if tripNumber == "" {
			tripNumber = tripID
		}
	}

	title := "Trip Cancelled"
	body := fmt.Sprintf("Trip %s has been cancelled", tripNumber)

	data := map[string]string{
		"type":        "trip_cancelled",
		"trip_id":     tripID,
		"trip_number": tripNumber,
		"status":      "cancelled",
	}

	_, err := s.SendToDriver(ctx, tenantID, driverID, title, body, data)
	return err
}

func (s *FCMService) handleSOSAlert(ctx context.Context, e events.Event) error {
	m := parseEventPayload(e.Payload)

	driverID := extractString(m, "driver_id", "DriverID", "driverId")
	tenantID := extractString(m, "tenant_id", "TenantID", "tenantId")
	vehicleID := extractString(m, "vehicle_id", "VehicleID", "vehicleId")
	sosID := extractString(m, "sos_id", "SOSID", "sosId", "id")
	reason := extractString(m, "reason", "Reason")

	if tenantID == "" {
		tenantID = string(shared.TenantIDFromContext(ctx))
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	if reason == "" {
		reason = "Emergency Safety Alert Triggered"
	}

	title := "EMERGENCY SOS ALERT"
	body := fmt.Sprintf("Emergency SOS alert triggered for vehicle %s: %s", vehicleID, reason)

	data := map[string]string{
		"type":       "sos_alert",
		"sos_id":     sosID,
		"vehicle_id": vehicleID,
		"driver_id":  driverID,
		"priority":   "high",
		"reason":     reason,
	}

	if driverID != "" {
		_, err := s.SendToDriver(ctx, tenantID, driverID, title, body, data)
		return err
	}
	return nil
}

func parseEventPayload(payload any) map[string]interface{} {
	if payload == nil {
		return make(map[string]interface{})
	}

	if m, ok := payload.(map[string]interface{}); ok {
		return m
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return make(map[string]interface{})
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return make(map[string]interface{})
	}
	return m
}

func extractString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok && val != nil {
			switch v := val.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			case fmt.Stringer:
				s := strings.TrimSpace(v.String())
				if s != "" {
					return s
				}
			case map[string]interface{}:
				if s, ok := v["String"].(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}
