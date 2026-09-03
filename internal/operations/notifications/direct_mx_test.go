package notifications

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSMTPServer simulates an MX mail server for unit testing.
type mockSMTPServer struct {
	t        *testing.T
	listener net.Listener
	addr     string
	messages chan string
	fail     bool
	closed   chan struct{}
	mu       sync.Mutex
}

func newMockSMTPServer(t *testing.T, fail bool) *mockSMTPServer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &mockSMTPServer{
		t:        t,
		listener: l,
		addr:     l.Addr().String(),
		messages: make(chan string, 10),
		fail:     fail,
		closed:   make(chan struct{}),
	}
	go s.serve()
	return s
}

func (s *mockSMTPServer) close() {
	_ = s.listener.Close()
	select {
	case <-s.closed:
	case <-time.After(500 * time.Millisecond):
	}
}

func (s *mockSMTPServer) serve() {
	defer close(s.closed)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockSMTPServer) handleConn(conn net.Conn) {
	defer conn.Close()
	s.mu.Lock()
	shouldFail := s.fail
	s.mu.Unlock()

	if shouldFail {
		// Reject immediately or drop connection
		return
	}

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}

	writeLine("220 mx.test.example ESMTP Avandab-Test")
	inData := false
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(trimmed)

		switch {
		case inData:
			if trimmed == "." {
				inData = false
				writeLine("250 2.0.0 OK: queued as test-id-12345")
				s.messages <- data.String()
			} else {
				data.WriteString(line)
			}
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			writeLine("250-mx.test.example")
			writeLine("250 8BITMIME")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			writeLine("250 2.1.0 Sender OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			writeLine("250 2.1.5 Recipient OK")
		case strings.HasPrefix(upper, "DATA"):
			inData = true
			data.Reset()
			writeLine("354 Start mail input; end with <CRLF>.<CRLF>")
		case strings.HasPrefix(upper, "QUIT"):
			writeLine("221 2.0.0 Service closing transmission channel")
			return
		case strings.HasPrefix(upper, "RSET"):
			writeLine("250 2.0.0 OK")
		default:
			writeLine("250 OK")
		}
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLocal  string
		wantDomain string
		wantErr    bool
	}{
		{
			name:       "simple address",
			input:      "user@targetdomain.com",
			wantLocal:  "user",
			wantDomain: "targetdomain.com",
			wantErr:    false,
		},
		{
			name:       "name with angle brackets",
			input:      "Billing Team <billing@fleet.example.com>",
			wantLocal:  "billing",
			wantDomain: "fleet.example.com",
			wantErr:    false,
		},
		{
			name:       "quoted name with comma",
			input:      `"Operations, South" <ops@mvtms.org>`,
			wantLocal:  "ops",
			wantDomain: "mvtms.org",
			wantErr:    false,
		},
		{
			name:       "uppercase domain is normalized",
			input:      "ALICE@COMPANY.CO.IN",
			wantLocal:  "ALICE",
			wantDomain: "company.co.in",
			wantErr:    false,
		},
		{
			name:       "trailing dot in domain is normalized",
			input:      "user@example.com.",
			wantLocal:  "user",
			wantDomain: "example.com",
			wantErr:    false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no @ symbol",
			input:   "plainusername",
			wantErr: true,
		},
		{
			name:    "empty domain",
			input:   "user@",
			wantErr: true,
		},
		{
			name:    "empty user",
			input:   "@domain.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, domain, err := extractDomain(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLocal, local)
				assert.Equal(t, tt.wantDomain, domain)
			}
		})
	}
}

func TestParseEnvelopeAddress(t *testing.T) {
	assert.Equal(t, "user@domain.com", parseEnvelopeAddress("user@domain.com"))
	assert.Equal(t, "user@domain.com", parseEnvelopeAddress("User Name <user@domain.com>"))
	assert.Equal(t, "billing@avandab.com", parseEnvelopeAddress(`"Avandab Billing" <billing@avandab.com>`))
}

func TestDirectMXEmailSender_DirectDelivery(t *testing.T) {
	srv := newMockSMTPServer(t, false)
	defer srv.close()

	sender := NewDirectMXEmailSender(DirectMXConfig{
		From: "billing@avandab.com",
		LookupMX: func(ctx context.Context, domain string) ([]*net.MX, error) {
			assert.Equal(t, "targetdomain.com", domain)
			return []*net.MX{
				{Host: "mx1.targetdomain.com", Pref: 10},
			}, nil
		},
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			assert.Equal(t, "mx1.targetdomain.com:25", addr)
			// Route to local test server
			var d net.Dialer
			return d.DialContext(ctx, "tcp", srv.addr)
		},
	})

	require.True(t, sender.Configured())

	err := sender.SendHTML(context.Background(), "customer@targetdomain.com", "Invoice #INV-2026-999",
		"Plain text invoice note", "<h1>Invoice</h1><p>Amount: INR 12,000</p>")
	require.NoError(t, err)

	select {
	case msg := <-srv.messages:
		assert.Contains(t, msg, "From: billing@avandab.com")
		assert.Contains(t, msg, "To: customer@targetdomain.com")
		assert.Contains(t, msg, "Subject: Invoice #INV-2026-999")
		assert.Contains(t, msg, "Amount: INR 12,000")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message delivery")
	}
}

func TestDirectMXEmailSender_FallbackAcrossMXRecords(t *testing.T) {
	// Primary MX server is dead / refusing connections
	primarySrv := newMockSMTPServer(t, true)
	defer primarySrv.close()

	// Secondary MX server is healthy
	secondarySrv := newMockSMTPServer(t, false)
	defer secondarySrv.close()

	sender := NewDirectMXEmailSender(DirectMXConfig{
		From: "ops@avandab.com",
		LookupMX: func(ctx context.Context, domain string) ([]*net.MX, error) {
			// Unsorted return: preference 20 and 5
			return []*net.MX{
				{Host: "mx-backup.target.test", Pref: 20},
				{Host: "mx-primary.target.test", Pref: 5},
			}, nil
		},
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			if strings.HasPrefix(addr, "mx-primary.target.test") {
				// Connect to failing server
				return d.DialContext(ctx, "tcp", primarySrv.addr)
			}
			if strings.HasPrefix(addr, "mx-backup.target.test") {
				// Connect to healthy backup server
				return d.DialContext(ctx, "tcp", secondarySrv.addr)
			}
			return nil, fmt.Errorf("unknown host %s", addr)
		},
	})

	err := sender.Send(context.Background(), "user@target.test", "System Alert", "Vehicle reached destination.")
	require.NoError(t, err)

	select {
	case msg := <-secondarySrv.messages:
		assert.Contains(t, msg, "From: ops@avandab.com")
		assert.Contains(t, msg, "To: user@target.test")
		assert.Contains(t, msg, "Vehicle reached destination.")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fallback delivery to secondary MX")
	}
}

func TestDirectMXEmailSender_AllMXFailedReturnsError(t *testing.T) {
	sender := NewDirectMXEmailSender(DirectMXConfig{
		From: "ops@avandab.com",
		LookupMX: func(ctx context.Context, domain string) ([]*net.MX, error) {
			return []*net.MX{
				{Host: "mx1.unreachable.test", Pref: 10},
				{Host: "mx2.unreachable.test", Pref: 20},
			}, nil
		},
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, fmt.Errorf("connection refused")
		},
	})

	err := sender.Send(context.Background(), "user@unreachable.test", "Alert", "Body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to deliver")
	assert.Contains(t, err.Error(), "mx1.unreachable.test")
	assert.Contains(t, err.Error(), "mx2.unreachable.test")
}

func TestDirectMXEmailSender_LocalRelayMode(t *testing.T) {
	srv := newMockSMTPServer(t, false)
	defer srv.close()

	sender := NewDirectMXEmailSender(DirectMXConfig{
		From:       "billing@avandab.com",
		LocalRelay: srv.addr, // local postfix relay
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", srv.addr)
		},
	})

	require.True(t, sender.Configured())

	err := sender.SendWithAttachments(context.Background(), "client@remote.com", "Monthly Invoice",
		"Please find invoice attached", "<p>Please find invoice attached</p>", []Attachment{
			{Filename: "invoice.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 test")},
		})
	require.NoError(t, err)

	select {
	case msg := <-srv.messages:
		assert.Contains(t, msg, "From: billing@avandab.com")
		assert.Contains(t, msg, "To: client@remote.com")
		assert.Contains(t, msg, "Content-Type: multipart/mixed")
		assert.Contains(t, msg, "invoice.pdf")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message delivery via local relay")
	}
}

func TestDirectMXEmailSender_DefaultFromAddress(t *testing.T) {
	srv := newMockSMTPServer(t, false)
	defer srv.close()

	sender := NewDirectMXEmailSender(DirectMXConfig{
		From:       "", // should default to billing@avandab.com
		LocalRelay: srv.addr,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", srv.addr)
		},
	})

	err := sender.Send(context.Background(), "user@corp.com", "Test Subject", "Test Body")
	require.NoError(t, err)

	select {
	case msg := <-srv.messages:
		assert.Contains(t, msg, "From: billing@avandab.com")
		assert.Contains(t, msg, "To: user@corp.com")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message delivery")
	}
}
