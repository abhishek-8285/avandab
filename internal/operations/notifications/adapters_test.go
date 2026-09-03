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

func TestSMTPEmailSender_DeliversHTML(t *testing.T) {
	sink := newSMTPSink(t)
	host, port, _ := net.SplitHostPort(sink.addr())
	sender := NewSMTPEmailSender(SMTPConfig{Host: host, Port: port, From: "billing@fleet.test"})
	require.True(t, sender.Configured())

	err := sender.SendHTML(context.Background(), "customer@test.com", "Invoice Ready",
		"Your invoice is ready. Total: INR 5000",
		"<h1>Invoice Ready</h1><p>Total: <strong>INR 5000</strong></p>")
	require.NoError(t, err)

	msg := <-sink.messages
	assert.Contains(t, msg, "From: billing@fleet.test")
	assert.Contains(t, msg, "To: customer@test.com")
	assert.Contains(t, msg, "Subject: Invoice Ready")
	assert.Contains(t, msg, "Content-Type: multipart/alternative")
	assert.Contains(t, msg, "text/plain")
	assert.Contains(t, msg, "Your invoice is ready. Total: INR 5000")
	assert.Contains(t, msg, "text/html")
	assert.Contains(t, msg, "<h1>Invoice Ready</h1>")
}

func TestSMTPEmailSender_DeliversWithPDFAttachment(t *testing.T) {
	sink := newSMTPSink(t)
	host, port, _ := net.SplitHostPort(sink.addr())
	sender := NewSMTPEmailSender(SMTPConfig{Host: host, Port: port, From: "invoices@fleet.test"})
	require.True(t, sender.Configured())

	fakePDF := []byte("%PDF-1.4 test invoice content 12345")
	err := sender.SendWithAttachments(context.Background(), "accounts@client.test", "Invoice #INV-2026-001",
		"Please find attached invoice.",
		"<p>Please find attached invoice <b>#INV-2026-001</b>.</p>",
		[]Attachment{
			{
				Filename:    "invoice_INV-2026-001.pdf",
				ContentType: "application/pdf",
				Data:        fakePDF,
			},
		})
	require.NoError(t, err)

	msg := <-sink.messages
	assert.Contains(t, msg, "From: invoices@fleet.test")
	assert.Contains(t, msg, "To: accounts@client.test")
	assert.Contains(t, msg, "Content-Type: multipart/mixed")
	assert.Contains(t, msg, `Content-Type: application/pdf; name="invoice_INV-2026-001.pdf"`)
	assert.Contains(t, `Content-Disposition: attachment; filename="invoice_INV-2026-001.pdf"`, "invoice_INV-2026-001.pdf")
	assert.Contains(t, msg, "Content-Transfer-Encoding: base64")
	// Verify base64 encoding contains the encoded pdf string
	assert.Contains(t, msg, "JVBERi0xLjQgdGVzdCBpbnZvaWNlIGNvbnRlbnQgMTIzNDU=")
}

func TestLogEmailSender_CapturesCleanly(t *testing.T) {
	devSender := NewLogEmailSender(nil)
	require.True(t, devSender.Configured())

	err := devSender.Send(context.Background(), "user@test.com", "Subject", "Body")
	require.NoError(t, err)

	err = devSender.SendHTML(context.Background(), "user@test.com", "Subject", "Text", "<p>HTML</p>")
	require.NoError(t, err)

	err = devSender.SendWithAttachments(context.Background(), "user@test.com", "Subject", "Text", "<p>HTML</p>", []Attachment{
		{Filename: "doc.pdf", ContentType: "application/pdf", Data: []byte("sample")},
	})
	require.NoError(t, err)
}

func TestNewEmailSenderFromConfig(t *testing.T) {
	// 1. Direct host
	directSender := NewEmailSenderFromConfig(SMTPConfig{
		Host: "direct",
	})
	_, isDirect := directSender.(*DirectMXEmailSender)
	assert.True(t, isDirect, "Host='direct' must instantiate *DirectMXEmailSender")
	assert.True(t, directSender.Configured())

	// 2. Direct flag
	directFlagSender := NewEmailSenderFromConfig(SMTPConfig{
		Direct: true,
	})
	_, isDirectFlag := directFlagSender.(*DirectMXEmailSender)
	assert.True(t, isDirectFlag, "Direct=true must instantiate *DirectMXEmailSender")
	assert.True(t, directFlagSender.Configured())

	// 3. Localhost (Postfix mode)
	localSender := NewEmailSenderFromConfig(SMTPConfig{
		Host: "localhost",
		From: "billing@avandab.com",
	})
	smtpLocal, isSMTP := localSender.(*SMTPEmailSender)
	assert.True(t, isSMTP, "Host='localhost' must instantiate *SMTPEmailSender")
	assert.Equal(t, "25", smtpLocal.cfg.Port, "localhost default port must be 25")
	assert.Empty(t, smtpLocal.cfg.User, "localhost must not use auth")
	assert.True(t, localSender.Configured())

	// 4. 127.0.0.1
	ipSender := NewEmailSenderFromConfig(SMTPConfig{
		Host: "127.0.0.1",
		From: "billing@avandab.com",
	})
	smtpIP, isSMTPIP := ipSender.(*SMTPEmailSender)
	assert.True(t, isSMTPIP)
	assert.Equal(t, "25", smtpIP.cfg.Port)

	// 5. External relay (e.g. Gmail)
	gmailSender := NewEmailSenderFromConfig(SMTPConfig{
		Host:     "smtp.gmail.com",
		User:     "user@gmail.com",
		Password: "secret-password",
		From:     "user@gmail.com",
	})
	smtpGmail, isGmail := gmailSender.(*SMTPEmailSender)
	assert.True(t, isGmail)
	assert.Equal(t, "587", smtpGmail.cfg.Port)
	assert.Equal(t, "user@gmail.com", smtpGmail.cfg.User)
	assert.True(t, gmailSender.Configured())

	// 6. Unconfigured
	unconfiguredSender := NewEmailSenderFromConfig(SMTPConfig{})
	assert.False(t, unconfiguredSender.Configured())
}
