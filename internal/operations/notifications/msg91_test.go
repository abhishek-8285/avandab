package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// msg91Capture records what the fake MSG91 gateway saw: the authkey header,
// content type, and the decoded /api/v5/sms/ request body.
type msg91Capture struct {
	authKey     string
	contentType string
	payload     struct {
		Sender string `json:"sender"`
		Route  string `json:"route"`
		SMS    []struct {
			Message string   `json:"message"`
			To      []string `json:"to"`
		} `json:"sms"`
	}
}

// newMSG91Sink stands up a fake MSG91 endpoint returning the given status and
// body, capturing every request into capture.
func newMSG91Sink(t *testing.T, capture *msg91Capture, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.authKey = r.Header.Get("authkey")
		capture.contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capture.payload)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMSG91Sender_DeliversExpectedPayload(t *testing.T) {
	var cap msg91Capture
	srv := newMSG91Sink(t, &cap, http.StatusOK, `{"type":"success"}`)

	sender := NewMSG91SMSSender(MSG91Config{
		AuthKey:  "k3y",
		SenderID: "AVNDAB",
		Endpoint: srv.URL,
	})
	require.True(t, sender.Configured())

	err := sender.Send(context.Background(), "+919900112233", "Your Avandab verification code is 123456")
	require.NoError(t, err)
	assert.Equal(t, "k3y", cap.authKey)
	assert.Equal(t, "application/json", cap.contentType)
	assert.Equal(t, "AVNDAB", cap.payload.Sender)
	assert.Equal(t, "4", cap.payload.Route)
	require.Len(t, cap.payload.SMS, 1)
	require.Len(t, cap.payload.SMS[0].To, 1)
	assert.Equal(t, "919900112233", cap.payload.SMS[0].To[0])
	assert.Contains(t, cap.payload.SMS[0].Message, "123456")
}

func TestMSG91Sender_Non2xxIsError(t *testing.T) {
	var cap msg91Capture
	srv := newMSG91Sink(t, &cap, http.StatusForbidden, `{"message":"quota exhausted"}`)

	sender := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y", SenderID: "AVNDAB", Endpoint: srv.URL})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 403")
	assert.Contains(t, err.Error(), "quota exhausted")
}

// MSG91 sometimes reports failure with HTTP 200 and `type: error` in the JSON
// body. A silent success would hide a real non-delivery, so the adapter must
// surface it.
func TestMSG91Sender_HTTP200TypeErrorIsError(t *testing.T) {
	var cap msg91Capture
	srv := newMSG91Sink(t, &cap, http.StatusOK, `{"type":"error","message":"DLT template not approved"}`)

	sender := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y", SenderID: "AVNDAB", Endpoint: srv.URL})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DLT template not approved")
}

// An HTTP 200 with an unparseable/empty body cannot be trusted as a delivery
// confirmation — fail loudly instead.
func TestMSG91Sender_HTTP200UnrecognizedBodyIsError(t *testing.T) {
	var cap msg91Capture
	srv := newMSG91Sink(t, &cap, http.StatusOK, `gateway ok, whatever`)

	sender := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y", SenderID: "AVNDAB", Endpoint: srv.URL})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 200")
}

func TestMSG91Sender_RejectsBadPhoneWithoutSending(t *testing.T) {
	var cap msg91Capture
	srv := newMSG91Sink(t, &cap, http.StatusOK, `{"type":"success"}`)

	sender := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y", SenderID: "AVNDAB", Endpoint: srv.URL})
	err := sender.Send(context.Background(), "+ 919900|112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "phone")
	assert.Empty(t, cap.payload.SMS, "gateway must not receive a request for a malformed number")
}

func TestMSG91ConfiguredRequiresAuthKeyAndSenderID(t *testing.T) {
	assert.False(t, NewMSG91SMSSender(MSG91Config{}).Configured())
	assert.False(t, NewMSG91SMSSender(MSG91Config{AuthKey: "k3y"}).Configured())
	assert.False(t, NewMSG91SMSSender(MSG91Config{SenderID: "AVNDAB"}).Configured())

	sender := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y", SenderID: "AVNDAB"})
	require.True(t, sender.Configured())

	// Partial credentials (auth key without sender ID) are untrusted: stay
	// unconfigured and refuse the send rather than guessing defaults.
	partial := NewMSG91SMSSender(MSG91Config{AuthKey: "k3y"})
	require.False(t, partial.Configured())
	err := partial.Send(context.Background(), "+919900112233", "hi")
	require.ErrorIs(t, err, ErrSMSNotConfigured)
}

func TestNewSMSSenderFromConfig_SelectsProvider(t *testing.T) {
	// Default (no provider) stays on the provider-agnostic webhook.
	sms := NewSMSSenderFromConfig(SMSConfig{WebhookURL: "http://x"})
	_, isWebhook := sms.(*WebhookSMSSender)
	require.True(t, isWebhook, "empty provider must select webhook sender")

	// msg91 (case-insensitive) selects the real gateway, configured when the
	// credentials are complete.
	sms = NewSMSSenderFromConfig(SMSConfig{Provider: "MSG91", MSG91AuthKey: "k3y", MSG91SenderID: "AVNDAB"})
	m91, isMSG91 := sms.(*MSG91SMSSender)
	require.True(t, isMSG91, "msg91 provider must select the MSG91 sender")
	assert.True(t, m91.Configured())

	// Unknown provider: fail closed — nothing configured, sends stay errors.
	sms = NewSMSSenderFromConfig(SMSConfig{Provider: "unsupported_mock"})
	unk, isUnknown := sms.(*unknownSMSSender)
	require.True(t, isUnknown, "unknown provider must be fail-closed, not a fake success")
	err := unk.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported_mock")
}
