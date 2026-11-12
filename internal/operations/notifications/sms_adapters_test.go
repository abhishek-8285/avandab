package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type twilioCapture struct {
	authHeader  string
	contentType string
	formValues  url.Values
}

func newTwilioSink(t *testing.T, capture *twilioCapture, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.authHeader = r.Header.Get("Authorization")
		capture.contentType = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		capture.formValues = r.PostForm
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTwilioSender_DeliversExpectedPayload(t *testing.T) {
	var cap twilioCapture
	srv := newTwilioSink(t, &cap, http.StatusCreated, `{"sid":"SM123","status":"queued"}`)

	sender := NewTwilioSMSSender(TwilioConfig{
		AccountSID: "AC12345",
		AuthToken:  "authsecret",
		FromNumber: "+15005550006",
		Endpoint:   srv.URL,
	})
	require.True(t, sender.Configured())

	err := sender.Send(context.Background(), "+919900112233", "Your verification code is 654321")
	require.NoError(t, err)

	assert.Contains(t, cap.contentType, "application/x-www-form-urlencoded")
	assert.NotEmpty(t, cap.authHeader)
	assert.Equal(t, "+15005550006", cap.formValues.Get("From"))
	assert.Equal(t, "+919900112233", cap.formValues.Get("To"))
	assert.Contains(t, cap.formValues.Get("Body"), "654321")
}

func TestTwilioSender_Non2xxIsError(t *testing.T) {
	var cap twilioCapture
	srv := newTwilioSink(t, &cap, http.StatusBadRequest, `{"code":21211,"message":"The 'To' number is not a valid phone number"}`)

	sender := NewTwilioSMSSender(TwilioConfig{
		AccountSID: "AC12345",
		AuthToken:  "authsecret",
		FromNumber: "+15005550006",
		Endpoint:   srv.URL,
	})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
	assert.Contains(t, err.Error(), "not a valid phone number")
}

func TestTwilioSender_RejectsBadPhoneWithoutSending(t *testing.T) {
	var cap twilioCapture
	srv := newTwilioSink(t, &cap, http.StatusCreated, `{"sid":"SM123"}`)

	sender := NewTwilioSMSSender(TwilioConfig{
		AccountSID: "AC12345",
		AuthToken:  "authsecret",
		FromNumber: "+15005550006",
		Endpoint:   srv.URL,
	})
	err := sender.Send(context.Background(), "invalid-phone", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "phone")
	assert.Empty(t, cap.formValues)
}

func TestTwilioConfiguredRequiresCredentials(t *testing.T) {
	assert.False(t, NewTwilioSMSSender(TwilioConfig{}).Configured())
	assert.False(t, NewTwilioSMSSender(TwilioConfig{AccountSID: "AC123"}).Configured())
	assert.False(t, NewTwilioSMSSender(TwilioConfig{AccountSID: "AC123", AuthToken: "tok"}).Configured())

	sender := NewTwilioSMSSender(TwilioConfig{AccountSID: "AC123", AuthToken: "tok", FromNumber: "+15005550006"})
	require.True(t, sender.Configured())

	partial := NewTwilioSMSSender(TwilioConfig{AccountSID: "AC123"})
	require.False(t, partial.Configured())
	err := partial.Send(context.Background(), "+919900112233", "hi")
	require.ErrorIs(t, err, ErrSMSNotConfigured)
}

type fast2smsCapture struct {
	authHeader  string
	contentType string
	payload     struct {
		Route    string `json:"route"`
		SenderID string `json:"sender_id"`
		Message  string `json:"message"`
		Numbers  string `json:"numbers"`
	}
}

func newFast2SMSSink(t *testing.T, capture *fast2smsCapture, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.authHeader = r.Header.Get("authorization")
		capture.contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capture.payload)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFast2SMSSender_DeliversExpectedPayload(t *testing.T) {
	var cap fast2smsCapture
	srv := newFast2SMSSink(t, &cap, http.StatusOK, `{"return":true,"request_id":"req-99","message":["SMS sent successfully"]}`)

	sender := NewFast2SMSSender(Fast2SMSConfig{
		APIKey:   "fstkey123",
		SenderID: "AVNDAB",
		Route:    "v3",
		Endpoint: srv.URL,
	})
	require.True(t, sender.Configured())

	err := sender.Send(context.Background(), "+919900112233", "Your OTP is 987654")
	require.NoError(t, err)

	assert.Equal(t, "fstkey123", cap.authHeader)
	assert.Equal(t, "application/json", cap.contentType)
	assert.Equal(t, "v3", cap.payload.Route)
	assert.Equal(t, "AVNDAB", cap.payload.SenderID)
	assert.Equal(t, "919900112233", cap.payload.Numbers)
	assert.Contains(t, cap.payload.Message, "987654")
}

func TestFast2SMSSender_Non2xxIsError(t *testing.T) {
	var cap fast2smsCapture
	srv := newFast2SMSSink(t, &cap, http.StatusUnauthorized, `{"return":false,"message":"Invalid authorization key"}`)

	sender := NewFast2SMSSender(Fast2SMSConfig{
		APIKey:   "badkey",
		Endpoint: srv.URL,
	})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
	assert.Contains(t, err.Error(), "Invalid authorization key")
}

func TestFast2SMSSender_ReturnFalseIsError(t *testing.T) {
	var cap fast2smsCapture
	srv := newFast2SMSSink(t, &cap, http.StatusOK, `{"return":false,"message":"Account balance insufficient"}`)

	sender := NewFast2SMSSender(Fast2SMSConfig{
		APIKey:   "fstkey123",
		Endpoint: srv.URL,
	})
	err := sender.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Account balance insufficient")
}

func TestFast2SMSSender_RejectsBadPhoneWithoutSending(t *testing.T) {
	var cap fast2smsCapture
	srv := newFast2SMSSink(t, &cap, http.StatusOK, `{"return":true}`)

	sender := NewFast2SMSSender(Fast2SMSConfig{
		APIKey:   "fstkey123",
		Endpoint: srv.URL,
	})
	err := sender.Send(context.Background(), "+abc1234", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "phone")
	assert.Empty(t, cap.payload.Numbers)
}

func TestFast2SMSConfiguredRequiresAPIKey(t *testing.T) {
	assert.False(t, NewFast2SMSSender(Fast2SMSConfig{}).Configured())

	sender := NewFast2SMSSender(Fast2SMSConfig{APIKey: "k3y"})
	require.True(t, sender.Configured())

	unconfigured := NewFast2SMSSender(Fast2SMSConfig{})
	err := unconfigured.Send(context.Background(), "+919900112233", "hi")
	require.ErrorIs(t, err, ErrSMSNotConfigured)
}

func TestNewSMSSenderFromConfig_SelectsTwilioAndFast2SMS(t *testing.T) {
	// Twilio selection
	sms := NewSMSSenderFromConfig(SMSConfig{
		Provider:         "twilio",
		TwilioAccountSID: "AC123",
		TwilioAuthToken:  "secret",
		TwilioFromNumber: "+15005550006",
	})
	tw, isTwilio := sms.(*TwilioSMSSender)
	require.True(t, isTwilio, "twilio provider must select TwilioSMSSender")
	assert.True(t, tw.Configured())

	// Fast2SMS selection
	sms = NewSMSSenderFromConfig(SMSConfig{
		Provider:       "FAST2SMS",
		Fast2SMSAPIKey: "fstkey",
	})
	f2s, isFast2SMS := sms.(*Fast2SMSSender)
	require.True(t, isFast2SMS, "fast2sms provider must select Fast2SMSSender")
	assert.True(t, f2s.Configured())

	// Truly unknown provider fails closed
	sms = NewSMSSenderFromConfig(SMSConfig{Provider: "some_unsupported_gateway"})
	unk, isUnknown := sms.(*unknownSMSSender)
	require.True(t, isUnknown)
	err := unk.Send(context.Background(), "+919900112233", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "some_unsupported_gateway")
}
