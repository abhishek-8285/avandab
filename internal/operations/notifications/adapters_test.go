package notifications

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared/ports"
)

// smtpSink is a minimal SMTP server: greets, 250s everything until DATA,
// captures the message payload, then 250s and quits.
type smtpSink struct {
	t        *testing.T
	listener net.Listener
	messages chan string
}

func newSMTPSink(t *testing.T) *smtpSink {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &smtpSink{t: t, listener: l, messages: make(chan string, 4)}
	go s.serve()
	return s
}

func (s *smtpSink) addr() string { return s.listener.Addr().String() }

func (s *smtpSink) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(line string) {
		w.WriteString(line + "\r\n")
		w.Flush()
	}
	writeLine("220 sink ESMTP")
	inData := false
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case inData:
			if trimmed == "." {
				inData = false
				writeLine("250 OK")
				s.messages <- data.String()
			} else {
				data.WriteString(line)
			}
		case strings.HasPrefix(strings.ToUpper(trimmed), "DATA"):
			inData = true
			data.Reset()
			writeLine("354 go ahead")
		case strings.HasPrefix(strings.ToUpper(trimmed), "QUIT"):
			writeLine("221 bye")
			return
		default:
			writeLine("250 OK")
		}
	}
}

func TestSMTPEmailSenderDelivers(t *testing.T) {
	sink := newSMTPSink(t)
	host, port, _ := net.SplitHostPort(sink.addr())
	sender := NewSMTPEmailSender(SMTPConfig{Host: host, Port: port, From: "ops@fleet.test"})
	require.True(t, sender.Configured())

	err := sender.Send(context.Background(), "to@consignee.test", "Reset your password", "Use the link.")
	require.NoError(t, err)

	msg := <-sink.messages
	assert.Contains(t, msg, "From: ops@fleet.test")
	assert.Contains(t, msg, "To: to@consignee.test")
	assert.Contains(t, msg, "Subject: Reset your password")
	assert.Contains(t, msg, "Use the link.")
}

func TestSendEmailUnconfiguredFailsHonestly(t *testing.T) {
	svc := NewServiceWithChannels(NewSMTPEmailSender(SMTPConfig{}), nil)
	require.False(t, svc.EmailConfigured())
	err := svc.SendEmail(context.Background(), ports.NotificationMessage{Recipient: "a@b.c", Subject: "x", Body: "y"})
	require.ErrorIs(t, err, ErrEmailNotConfigured)
}

func TestSendSMSWebhookDeliversJSON(t *testing.T) {
	var gotTo, gotMsg, gotSecret, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotSecret = r.Header.Get("X-SMS-Secret")
		var payload struct {
			To      string `json:"to"`
			Message string `json:"message"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		gotTo, gotMsg = payload.To, payload.Message
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewWebhookSMSSender(srv.URL, "shhh")
	require.True(t, sender.Configured())
	err := sender.Send(context.Background(), "+919900112233", "OTP 123456")
	require.NoError(t, err)
	assert.Contains(t, gotCT, "application/json")
	assert.Equal(t, "shhh", gotSecret)
	assert.Equal(t, "+919900112233", gotTo)
	assert.Contains(t, gotMsg, "123456")
}

func TestSendSMSNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gateway exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sender := NewWebhookSMSSender(srv.URL, "")
	err := sender.Send(context.Background(), "+91", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestServiceChannelsHonestWhenUnconfigured(t *testing.T) {
	svc := NewService()
	assert.False(t, svc.SMSConfigured())
	err := svc.SendSMS(context.Background(), ports.NotificationMessage{Recipient: "+91", Body: "x"})
	require.ErrorIs(t, err, ErrSMSNotConfigured)

	err = svc.SendEmail(context.Background(), ports.NotificationMessage{Recipient: "a@b.c", Body: "x"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, nil))
}
