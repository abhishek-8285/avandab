package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"transport-app/internal/entitlement/application"
)

// WebhookHandler handles incoming Razorpay subscription webhooks.
type WebhookHandler struct {
	svc           application.Service
	webhookSecret string
}

// NewWebhookHandler creates a new WebhookHandler instance.
func NewWebhookHandler(svc application.Service, webhookSecret string) *WebhookHandler {
	return &WebhookHandler{
		svc:           svc,
		webhookSecret: webhookSecret,
	}
}

// ServeHTTP processes POST /api/v1/billing/webhooks/razorpay
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Verify HMAC-SHA256 signature if secret configured
	if h.webhookSecret != "" {
		sig := r.Header.Get("X-Razorpay-Signature")
		if sig == "" || !verifySignature(body, sig, h.webhookSecret) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
			return
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	eventID, _ := raw["event_id"].(string)
	eventType, _ := raw["event"].(string)
	createdAtNum, _ := raw["created_at"].(float64)
	eventTS := time.Now().UTC()
	if createdAtNum > 0 {
		eventTS = time.Unix(int64(createdAtNum), 0).UTC()
	}
	if eventID == "" {
		if idVal, ok := raw["id"].(string); ok {
			eventID = idVal
		}
	}

	// Extract subscription entity
	var providerSubID string
	var periodStart, periodEnd time.Time
	if payloadMap, ok := raw["payload"].(map[string]interface{}); ok {
		if subWrapper, ok := payloadMap["subscription"].(map[string]interface{}); ok {
			if entity, ok := subWrapper["entity"].(map[string]interface{}); ok {
				if id, ok := entity["id"].(string); ok {
					providerSubID = id
				}
				if st, ok := entity["current_start"].(float64); ok && st > 0 {
					periodStart = time.Unix(int64(st), 0).UTC()
				}
				if end, ok := entity["current_end"].(float64); ok && end > 0 {
					periodEnd = time.Unix(int64(end), 0).UTC()
				}
			}
		}
		if providerSubID == "" {
			if payWrapper, ok := payloadMap["payment"].(map[string]interface{}); ok {
				if entity, ok := payWrapper["entity"].(map[string]interface{}); ok {
					if id, ok := entity["subscription_id"].(string); ok {
						providerSubID = id
					}
				}
			}
		}
	}

	if providerSubID == "" {
		// Event does not contain subscription ID
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored_no_subscription"}`))
		return
	}

	if eventID == "" {
		eventID = providerSubID + "_" + eventType + "_" + eventTS.Format(time.RFC3339)
	}

	payload := application.WebhookEventPayload{
		EventID:                eventID,
		EventType:              eventType,
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PayloadJSON:            string(body),
		EventTimestamp:         eventTS,
		PeriodStart:            periodStart,
		PeriodEnd:              periodEnd,
	}

	if err := h.svc.ProcessSubscriptionWebhook(r.Context(), payload); err != nil {
		http.Error(w, `{"error":"failed to process webhook"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func verifySignature(body []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
