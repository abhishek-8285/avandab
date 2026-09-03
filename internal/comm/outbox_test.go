package comm

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/operations/notifications"
)

// newOutboxTestDB creates an in-memory SQLite with the comm_outbox schema
// exactly as migration 00118 defines it.
func newOutboxTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:comm_%d?mode=memory&cache=shared&_pragma=busy_timeout=5000", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS comm_outbox (
		id              TEXT PRIMARY KEY,
		tenant_id       TEXT NOT NULL,
		channel         TEXT NOT NULL CHECK (channel IN ('email', 'whatsapp')),
		recipient       TEXT NOT NULL,
		template        TEXT NOT NULL,
		payload_json    TEXT NOT NULL DEFAULT '{}',
		status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'dead')),
		attempts        INTEGER NOT NULL DEFAULT 0,
		next_attempt_at DATETIME NOT NULL DEFAULT (datetime('now')),
		last_error      TEXT,
		created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
		sent_at         DATETIME
	)`)
	require.NoError(t, err)
	return db
}

// fakeEmailSender records calls and returns a configurable error.
type fakeEmailSender struct {
	mu    sync.Mutex
	calls []string // "to|subject|body"
	err   error
}

func (f *fakeEmailSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, to+"|"+subject+"|"+body)
	return f.err
}

func (f *fakeEmailSender) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeWhatsAppSender records calls and returns a configurable error.
type fakeWhatsAppSender struct {
	mu    sync.Mutex
	calls []string // "phone|text"
	err   error
}

func (f *fakeWhatsAppSender) SendWhatsApp(_ context.Context, phone, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, phone+"|"+text)
	return f.err
}

func (f *fakeWhatsAppSender) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func outboxRow(t *testing.T, db *sql.DB, id string) (status string, attempts int, nextAt string, lastErr sql.NullString) {
	t.Helper()
	err := db.QueryRow(`SELECT status, attempts, next_attempt_at, last_error FROM comm_outbox WHERE id = ?`, id).
		Scan(&status, &attempts, &nextAt, &lastErr)
	require.NoError(t, err)
	return
}

func TestEnqueueEmail_Validation(t *testing.T) {
	db := newOutboxTestDB(t)
	ctx := context.Background()

	_, err := EnqueueEmail(ctx, db, "", "a@b.c", "generic", "s", "b")
	require.ErrorContains(t, err, "tenantID required")

	_, err = EnqueueEmail(ctx, db, "t1", "", "generic", "s", "b")
	require.ErrorContains(t, err, "recipient required")

	id, err := EnqueueEmail(ctx, db, "t1", "a@b.c", "welcome", "Hello", "Body")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	var channel, recipient, payload string
	require.NoError(t, db.QueryRow(`SELECT channel, recipient, payload_json FROM comm_outbox WHERE id = ?`, id).
		Scan(&channel, &recipient, &payload))
	require.Equal(t, "email", channel)
	require.Equal(t, "a@b.c", recipient)
	require.Contains(t, payload, `"subject":"Hello"`)
}

func TestEnqueueWhatsApp_Validation(t *testing.T) {
	db := newOutboxTestDB(t)
	ctx := context.Background()

	_, err := EnqueueWhatsApp(ctx, db, "", "+919876543210", "trip_dispatched", "Msg")
	require.ErrorContains(t, err, "tenantID required")

	_, err = EnqueueWhatsApp(ctx, db, "t1", "", "trip_dispatched", "Msg")
	require.ErrorContains(t, err, "recipient phone required")

	id, err := EnqueueWhatsApp(ctx, db, "t1", "+919876543210", "trip_dispatched", "Trip Dispatched: Mumbai -> Pune")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	var channel, recipient, payload string
	require.NoError(t, db.QueryRow(`SELECT channel, recipient, payload_json FROM comm_outbox WHERE id = ?`, id).
		Scan(&channel, &recipient, &payload))
	require.Equal(t, "whatsapp", channel)
	require.Equal(t, "+919876543210", recipient)
	require.Contains(t, payload, "Trip Dispatched: Mumbai")
}

func TestWorker_HappyPath_EmailAndWhatsApp(t *testing.T) {
	db := newOutboxTestDB(t)
	emailSender := &fakeEmailSender{}
	waSender := &fakeWhatsAppSender{}
	w := NewWorker(db, emailSender, waSender, nil)
	w.interval = time.Hour
	w.SetJitter(0, 0) // zero jitter for instantaneous unit test

	id1, err := EnqueueEmail(context.Background(), db, "t1", "a@b.c", "welcome", "Hello", "Body text")
	require.NoError(t, err)

	id2, err := EnqueueWhatsApp(context.Background(), db, "t1", "+919876543210", "trip_dispatched", "Dispatched to Pune")
	require.NoError(t, err)

	w.process(context.Background())

	status1, attempts1, _, lastErr1 := outboxRow(t, db, id1)
	require.Equal(t, "sent", status1)
	require.Equal(t, 1, attempts1)
	require.False(t, lastErr1.Valid)

	status2, attempts2, _, lastErr2 := outboxRow(t, db, id2)
	require.Equal(t, "sent", status2)
	require.Equal(t, 1, attempts2)
	require.False(t, lastErr2.Valid)

	require.Len(t, emailSender.sent(), 1)
	require.Equal(t, "a@b.c|Hello|Body text", emailSender.sent()[0])

	require.Len(t, waSender.sent(), 1)
	require.Equal(t, "+919876543210|Dispatched to Pune", waSender.sent()[0])
}

func TestWorker_WhatsApp_AntiBanRateLimiting_JitterDelay(t *testing.T) {
	db := newOutboxTestDB(t)
	waSender := &fakeWhatsAppSender{}
	w := NewWorker(db, nil, waSender, nil)
	w.interval = time.Hour
	w.SetJitter(3*time.Second, 5*time.Second)

	var sleepCount int64
	var sleptTotal time.Duration
	var mu sync.Mutex
	w.SetSleeper(func(ctx context.Context, d time.Duration) error {
		atomic.AddInt64(&sleepCount, 1)
		mu.Lock()
		sleptTotal += d
		mu.Unlock()
		assert.GreaterOrEqual(t, d, 3*time.Second, "jitter must be at least 3s")
		assert.LessOrEqual(t, d, 5*time.Second, "jitter must be at most 5s")
		return nil
	})

	// Enqueue 3 WhatsApp messages
	_, err := EnqueueWhatsApp(context.Background(), db, "t1", "+919876543210", "trip_1", "Msg 1")
	require.NoError(t, err)
	_, err = EnqueueWhatsApp(context.Background(), db, "t1", "+919876543211", "trip_2", "Msg 2")
	require.NoError(t, err)
	_, err = EnqueueWhatsApp(context.Background(), db, "t1", "+919876543212", "trip_3", "Msg 3")
	require.NoError(t, err)

	w.process(context.Background())

	// Consecutive sends between 3 messages means 2 jitter delays in worker process loop
	assert.Equal(t, int64(2), atomic.LoadInt64(&sleepCount))
	assert.Len(t, waSender.sent(), 3)

	var sentCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM comm_outbox WHERE status = 'sent' AND channel = 'whatsapp'`).Scan(&sentCount))
	assert.Equal(t, 3, sentCount)
}

func TestWorker_FailureThenRetry(t *testing.T) {
	db := newOutboxTestDB(t)
	sender := &fakeEmailSender{err: fmt.Errorf("smtp relay down")}
	w := NewWorker(db, sender, nil, nil)
	w.interval = time.Hour

	id, err := EnqueueEmail(context.Background(), db, "t1", "a@b.c", "welcome", "s", "b")
	require.NoError(t, err)

	w.process(context.Background())

	status, attempts, nextAt, lastErr := outboxRow(t, db, id)
	require.Equal(t, "failed", status)
	require.Equal(t, 1, attempts)
	require.Contains(t, lastErr.String, "smtp relay down")
	require.NotEmpty(t, nextAt)

	// Not yet due: backoff keeps it queued.
	w.process(context.Background())
	_, _, nextAt2, _ := outboxRow(t, db, id)
	require.Equal(t, nextAt, nextAt2, "row must not be retried before next_attempt_at")

	// Time passes, sender recovers → retry succeeds.
	_, err = db.Exec(`UPDATE comm_outbox SET next_attempt_at = datetime('now','-1 second') WHERE id = ?`, id)
	require.NoError(t, err)
	sender.mu.Lock()
	sender.err = nil
	sender.mu.Unlock()

	w.process(context.Background())
	status, attempts, _, _ = outboxRow(t, db, id)
	require.Equal(t, "sent", status)
	require.Equal(t, 2, attempts)
	require.Len(t, sender.sent(), 2)
}

func TestWorker_DeadLetterAfterMaxAttempts(t *testing.T) {
	db := newOutboxTestDB(t)
	sender := &fakeEmailSender{err: fmt.Errorf("permanent smtp failure")}
	w := NewWorker(db, sender, nil, nil)
	w.interval = time.Hour

	id, err := EnqueueEmail(context.Background(), db, "t1", "a@b.c", "welcome", "s", "b")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE comm_outbox SET attempts = ? WHERE id = ?`, w.maxAttempts-1, id)
	require.NoError(t, err)

	w.process(context.Background())

	status, attempts, _, lastErr := outboxRow(t, db, id)
	require.Equal(t, "dead", status)
	require.Equal(t, w.maxAttempts, attempts)
	require.Contains(t, lastErr.String, "permanent smtp failure")

	// Dead rows are never retried.
	w.process(context.Background())
	require.Len(t, sender.sent(), 1)
}

func TestWorker_StaleSendingReaped(t *testing.T) {
	db := newOutboxTestDB(t)
	sender := &fakeEmailSender{}
	w := NewWorker(db, sender, nil, nil)
	w.interval = time.Hour
	w.staleAfter = time.Minute

	// Row claimed by a worker that died mid-send long ago.
	id, err := EnqueueEmail(context.Background(), db, "t1", "a@b.c", "welcome", "s", "b")
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE comm_outbox SET status='sending', attempts=1,
	                  next_attempt_at = datetime('now','-10 minutes') WHERE id = ?`, id)
	require.NoError(t, err)

	w.process(context.Background())

	status, attempts, _, lastErr := outboxRow(t, db, id)
	require.Equal(t, "sent", status, "reaped row must be retried and delivered in the same tick")
	require.Equal(t, 2, attempts)
	require.False(t, lastErr.Valid, "success clears the stale-reap note in last_error")
}

func TestWorker_NilSenderFailsHonestly(t *testing.T) {
	db := newOutboxTestDB(t)
	w := NewWorker(db, nil, nil, nil) // unconfigured senders
	w.interval = time.Hour

	id1, err := EnqueueEmail(context.Background(), db, "t1", "a@b.c", "welcome", "s", "b")
	require.NoError(t, err)

	id2, err := EnqueueWhatsApp(context.Background(), db, "t1", "+919876543210", "trip", "b")
	require.NoError(t, err)

	w.process(context.Background())

	status1, _, _, lastErr1 := outboxRow(t, db, id1)
	require.Equal(t, "failed", status1)
	require.Contains(t, lastErr1.String, "email sender not wired")

	status2, _, _, lastErr2 := outboxRow(t, db, id2)
	require.Equal(t, "failed", status2)
	require.Contains(t, lastErr2.String, "whatsapp sender not wired")
}

func TestWorker_MockWhatsAppLoggingMode(t *testing.T) {
	db := newOutboxTestDB(t)
	mockWASender := NewLogWhatsAppSender(nil)
	w := NewWorker(db, nil, mockWASender, nil)
	w.interval = time.Hour
	w.SetJitter(0, 0)

	id, err := EnqueueWhatsApp(context.Background(), db, "t1", "+919876543210", "trip_dispatched", "Hello mock")
	require.NoError(t, err)

	w.process(context.Background())

	status, attempts, _, lastErr := outboxRow(t, db, id)
	require.Equal(t, "sent", status)
	require.Equal(t, 1, attempts)
	require.False(t, lastErr.Valid)
}

// fakeRichSender records rich email deliveries
type fakeRichSender struct {
	mu       sync.Mutex
	messages []notifications.EmailMessage
	err      error
}

func (f *fakeRichSender) Configured() bool {
	return true
}

func (f *fakeRichSender) Send(_ context.Context, to, subject, body string) error {
	return f.SendRich(context.Background(), notifications.EmailMessage{To: to, Subject: subject, TextBody: body})
}

func (f *fakeRichSender) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return f.SendRich(ctx, notifications.EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody})
}

func (f *fakeRichSender) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, attachments []notifications.Attachment) error {
	return f.SendRich(ctx, notifications.EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody, Attachments: attachments})
}

func (f *fakeRichSender) SendRich(_ context.Context, msg notifications.EmailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return f.err
}

func (f *fakeRichSender) delivered() []notifications.EmailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notifications.EmailMessage(nil), f.messages...)
}

func TestEnqueueInvoiceEmail_WithPDFAttachment(t *testing.T) {
	db := newOutboxTestDB(t)
	fakePDF := []byte("%PDF-1.4 test invoice")

	id, err := EnqueueInvoiceEmail(context.Background(), db, "tenant-100", "billing@client.com", "INV-2026-99", 12500.50, "2026-09-30", fakePDF)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	sender := &fakeRichSender{}
	w := NewWorker(db, sender, nil, nil)
	w.interval = time.Hour
	w.process(context.Background())

	status, attempts, _, lastErr := outboxRow(t, db, id)
	require.Equal(t, "sent", status)
	require.Equal(t, 1, attempts)
	require.False(t, lastErr.Valid)

	msgs := sender.delivered()
	require.Len(t, msgs, 1)
	assert.Equal(t, "billing@client.com", msgs[0].To)
	assert.Contains(t, msgs[0].Subject, "INV-2026-99")
	assert.Contains(t, msgs[0].HTMLBody, "12500.50")
	require.Len(t, msgs[0].Attachments, 1)
	assert.Equal(t, "invoice_INV-2026-99.pdf", msgs[0].Attachments[0].Filename)
	assert.Equal(t, "application/pdf", msgs[0].Attachments[0].ContentType)
	assert.Equal(t, fakePDF, msgs[0].Attachments[0].Bytes())
}

func TestEnqueuePODEmail_DeliveryReceipt(t *testing.T) {
	db := newOutboxTestDB(t)

	id, err := EnqueuePODEmail(context.Background(), db, "tenant-100", "consignee@dest.com", "TRP-8800", "https://vault.fleet.test/pod/trp-8800.jpg", nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	sender := &fakeRichSender{}
	w := NewWorker(db, sender, nil, nil)
	w.interval = time.Hour
	w.process(context.Background())

	status, attempts, _, _ := outboxRow(t, db, id)
	require.Equal(t, "sent", status)
	require.Equal(t, 1, attempts)

	msgs := sender.delivered()
	require.Len(t, msgs, 1)
	assert.Equal(t, "consignee@dest.com", msgs[0].To)
	assert.Contains(t, msgs[0].Subject, "TRP-8800")
	assert.Contains(t, msgs[0].HTMLBody, "TRP-8800")
	assert.Contains(t, msgs[0].HTMLBody, "https://vault.fleet.test/pod/trp-8800.jpg")
}

func TestWhatsAppTemplates_Formatting(t *testing.T) {
	// 1. Trip dispatch message
	dispatchMsg := FormatTripDispatchMessage("Mumbai JNPT", "Pune Chakan", "MH12AB1234")
	assert.Equal(t, "🚚 Avandab Trip Dispatched: Mumbai JNPT ➔ Pune Chakan | Live Tracking: https://avandab.com/tracking#v=MH12AB1234", dispatchMsg)

	// 2. Trip tracking message
	trackMsg := FormatTripTrackingMessage("TRP-9001", "Delhi", "Jaipur", "DL01XY9999")
	assert.Equal(t, "🚚 Avandab Shipment On The Way: Trip #TRP-9001 (Delhi ➔ Jaipur) has departed. Live Tracking: https://avandab.com/tracking#v=DL01XY9999", trackMsg)

	// 3. Booking confirmed message
	bookingMsg := FormatBookingConfirmedMessage("BK-500", "Chennai", "Bengaluru", "https://avandab.com/tracking#b=BK-500")
	assert.Equal(t, "📦 Avandab Booking Confirmed: #BK-500 (Chennai ➔ Bengaluru). Track your shipment live: https://avandab.com/tracking#b=BK-500", bookingMsg)

	// 4. POD receipt message
	podMsg := FormatPODReceiptMessage("TRP-9001", "https://epod.avandab.com/receipt/9001.pdf")
	assert.Equal(t, "✅ Avandab Delivery Completed: Trip #TRP-9001 has been delivered. View your digital e-POD receipt: https://epod.avandab.com/receipt/9001.pdf", podMsg)
}

func TestEventSubscriber_HandlesAllDomainEvents(t *testing.T) {
	db := newOutboxTestDB(t)

	// Seed relational tables
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS customers (id TEXT PRIMARY KEY, name TEXT, email TEXT, phone TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, email, phone) VALUES ('cust-1', 'Acme Corp', 'billing@acme.test', '+919811122233')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS drivers (id TEXT PRIMARY KEY, driver_id TEXT, first_name TEXT, phone TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, phone) VALUES ('drv-1', 'DRV-101', 'Rajesh', '+919988776655')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS routes (id TEXT PRIMARY KEY, source TEXT, destination TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, source, destination) VALUES ('rt-1', 'Mumbai', 'Pune')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS vehicles (id TEXT PRIMARY KEY, registration_number TEXT, vehicle_number TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number) VALUES ('veh-1', 'MH12AB1234', 'MH12AB1234')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS invoices (id TEXT PRIMARY KEY, tenant_id TEXT, customer_id TEXT, invoice_number TEXT, total REAL, due_date TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO invoices (id, tenant_id, customer_id, invoice_number, total, due_date) VALUES ('inv-1', 'tenant-1', 'cust-1', 'INV-001', 45000, '2026-09-15')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS bookings (id TEXT PRIMARY KEY, tenant_id TEXT, customer_id TEXT, route_id TEXT, booking_number TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, customer_id, route_id, booking_number) VALUES ('bk-1', 'tenant-1', 'cust-1', 'rt-1', 'BK-2026')`)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS trips (id TEXT PRIMARY KEY, tenant_id TEXT, booking_id TEXT, driver_id TEXT, vehicle_id TEXT, route_id TEXT, trip_number TEXT, pod_url TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, booking_id, driver_id, vehicle_id, route_id, trip_number, pod_url) VALUES ('trp-1', 'tenant-1', 'bk-1', 'drv-1', 'veh-1', 'rt-1', 'TRP-101', 'https://epod.test/1.jpg')`)
	require.NoError(t, err)

	sub := NewEventSubscriber(db, nil)
	ctx := context.Background()

	// 1. Test Invoice event (Email)
	err = sub.HandleInvoiceEvent(ctx, events.Event{
		Type: "invoice.issued",
		Payload: map[string]interface{}{
			"invoice_id": "inv-1",
		},
	})
	require.NoError(t, err)

	var invCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM comm_outbox WHERE channel = 'email' AND template = 'invoice_issued' AND recipient = 'billing@acme.test'`).Scan(&invCount))
	assert.Equal(t, 1, invCount)

	// 2. Test Trip Dispatched / Assigned event (WhatsApp to driver)
	err = sub.HandleTripDispatchEvent(ctx, events.Event{
		Type: "trip.assigned",
		Payload: map[string]interface{}{
			"trip_id": "trp-1",
		},
	})
	require.NoError(t, err)

	var dispatchRow struct {
		Recipient string
		Payload   string
	}
	require.NoError(t, db.QueryRow(`SELECT recipient, payload_json FROM comm_outbox WHERE channel = 'whatsapp' AND template = 'trip_dispatched'`).Scan(&dispatchRow.Recipient, &dispatchRow.Payload))
	assert.Equal(t, "+919988776655", dispatchRow.Recipient)
	assert.Contains(t, dispatchRow.Payload, "🚚 Avandab Trip Dispatched: Mumbai ➔ Pune | Live Tracking: https://avandab.com/tracking#v=MH12AB1234")

	// 3. Test Booking Confirmed event (WhatsApp to customer)
	err = sub.HandleTrackingEvent(ctx, events.Event{
		Type: "booking.confirmed",
		Payload: map[string]interface{}{
			"booking_id": "bk-1",
		},
	})
	require.NoError(t, err)

	var bookingRow struct {
		Recipient string
		Payload   string
	}
	require.NoError(t, db.QueryRow(`SELECT recipient, payload_json FROM comm_outbox WHERE channel = 'whatsapp' AND template = 'booking_confirmed'`).Scan(&bookingRow.Recipient, &bookingRow.Payload))
	assert.Equal(t, "+919811122233", bookingRow.Recipient)
	assert.Contains(t, bookingRow.Payload, "📦 Avandab Booking Confirmed: #BK-2026 (Mumbai ➔ Pune). Track your shipment live:")

	// 4. Test Trip Started event (WhatsApp to customer)
	err = sub.HandleTrackingEvent(ctx, events.Event{
		Type: "trip.started",
		Payload: map[string]interface{}{
			"trip_id": "trp-1",
		},
	})
	require.NoError(t, err)

	var tripTrackRow struct {
		Recipient string
		Payload   string
	}
	require.NoError(t, db.QueryRow(`SELECT recipient, payload_json FROM comm_outbox WHERE channel = 'whatsapp' AND template = 'trip_tracking'`).Scan(&tripTrackRow.Recipient, &tripTrackRow.Payload))
	assert.Equal(t, "+919811122233", tripTrackRow.Recipient)
	assert.Contains(t, tripTrackRow.Payload, "🚚 Avandab Shipment On The Way: Trip #TRP-101 (Mumbai ➔ Pune) has departed. Live Tracking: https://avandab.com/tracking#v=MH12AB1234")

	// 5. Test POD / Delivery completed event (Email + WhatsApp to customer)
	err = sub.HandlePODEvent(ctx, events.Event{
		Type: "trip.completed",
		Payload: map[string]interface{}{
			"trip_id": "trp-1",
		},
	})
	require.NoError(t, err)

	var podEmailCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM comm_outbox WHERE channel = 'email' AND template = 'trip_completed' AND recipient = 'billing@acme.test'`).Scan(&podEmailCount))
	assert.Equal(t, 1, podEmailCount)

	var podWARow struct {
		Recipient string
		Payload   string
	}
	require.NoError(t, db.QueryRow(`SELECT recipient, payload_json FROM comm_outbox WHERE channel = 'whatsapp' AND template = 'pod_receipt'`).Scan(&podWARow.Recipient, &podWARow.Payload))
	assert.Equal(t, "+919811122233", podWARow.Recipient)
	assert.Contains(t, podWARow.Payload, "✅ Avandab Delivery Completed: Trip #TRP-101 has been delivered. View your digital e-POD receipt: https://epod.test/1.jpg")

	// 6. Test Auth password reset & welcome events (Email)
	err = sub.HandleAuthEvent(ctx, events.Event{
		Type: "auth.password_reset",
		Payload: map[string]interface{}{
			"email":      "user@test.com",
			"reset_link": "https://fleet.test/auth/reset?token=xyz123",
		},
	})
	require.NoError(t, err)

	err = sub.HandleAuthEvent(ctx, events.Event{
		Type: "user.registered",
		Payload: map[string]interface{}{
			"email": "newuser@test.com",
			"name":  "Alice Sharma",
		},
	})
	require.NoError(t, err)

	// Deliver all through worker with mock loggers
	devEmailSender := notifications.NewLogEmailSender(nil)
	devWASender := NewLogWhatsAppSender(nil)
	w := NewWorker(db, devEmailSender, devWASender, nil)
	w.interval = time.Hour
	w.SetJitter(0, 0)
	w.process(ctx)

	var pendingCount int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM comm_outbox WHERE status != 'sent'`).Scan(&pendingCount))
	assert.Equal(t, 0, pendingCount, "all queued transactional communications (emails & WhatsApp) must be sent successfully")
}
