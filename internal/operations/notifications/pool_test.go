package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newPoolTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:pool_%d?mode=memory&cache=shared&_pragma=busy_timeout=5000", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS email_providers (
		provider TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 1, priority INTEGER NOT NULL DEFAULT 100,
		daily_quota INTEGER NOT NULL DEFAULT 0, monthly_quota INTEGER NOT NULL DEFAULT 0, cost_per_1k REAL NOT NULL DEFAULT 0,
		host TEXT, port TEXT, from_addr TEXT, created_at DATETIME DEFAULT (datetime('now')), updated_at DATETIME DEFAULT (datetime('now')) )`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS email_send_log (
		id TEXT PRIMARY KEY, provider TEXT NOT NULL, tenant_id TEXT DEFAULT '1', recipient TEXT NOT NULL,
		template TEXT DEFAULT 'generic', subject TEXT, status TEXT CHECK (status IN ('sent','failed')), error TEXT, created_at DATETIME DEFAULT (datetime('now')) )`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS email_provider_counters (
		provider TEXT PRIMARY KEY, daily_used INTEGER DEFAULT 0, monthly_used INTEGER DEFAULT 0,
		current_day TEXT DEFAULT (date('now')), current_month TEXT DEFAULT (strftime('%Y-%m','now')), updated_at DATETIME DEFAULT (datetime('now')) )`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_email_send_log_provider_day ON email_send_log(provider, status, created_at)`)
	require.NoError(t, err)
	return db
}

// fakePoolSender tracks calls and can be made to fail N times
type fakePoolSender struct {
	name      string
	failTimes int
	calls     int
	lastMsg   EmailMessage
}

func (f *fakePoolSender) Configured() bool { return true }
func (f *fakePoolSender) Send(ctx context.Context, to, subject, body string) error {
	return f.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: body})
}
func (f *fakePoolSender) SendHTML(ctx context.Context, to, subject, textBody, htmlBody string) error {
	return f.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody})
}
func (f *fakePoolSender) SendWithAttachments(ctx context.Context, to, subject, textBody, htmlBody string, atts []Attachment) error {
	return f.SendRich(ctx, EmailMessage{To: to, Subject: subject, TextBody: textBody, HTMLBody: htmlBody, Attachments: atts})
}
func (f *fakePoolSender) SendRich(_ context.Context, msg EmailMessage) error {
	f.calls++
	f.lastMsg = msg
	if f.calls <= f.failTimes {
		return fmt.Errorf("fake %s failure %d", f.name, f.calls)
	}
	return nil
}

func TestEmailPool_QuotaAwareSelection(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 2, MonthlyQuota: 9000, Priority: 1},
			{Name: "resend", Host: "smtp.resend.com", Port: "587", From: "billing@avandab.com", DailyQuota: 100, MonthlyQuota: 3000, Priority: 2},
			{Name: "direct", Host: "direct", Direct: true, From: "billing@avandab.com", DailyQuota: 0, MonthlyQuota: 0, Priority: 90},
		},
	}, db, nil)
	require.Equal(t, 3, len(pool.ListProviders()))

	// Inject fake senders that always succeed
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}

	ctx := context.Background()
	// First 2 sends should go to brevo (quota 2/day)
	require.NoError(t, pool.Send(ctx, "a@b.c", "s1", "b1"))
	require.NoError(t, pool.Send(ctx, "a@b.c", "s2", "b2"))
	usage := pool.GetUsage()
	var brevoUsage ProviderUsage
	for _, u := range usage {
		if u.Name == "brevo" {
			brevoUsage = u
		}
	}
	assert.Equal(t, 2, brevoUsage.DailyUsed)
	assert.True(t, brevoUsage.Exhausted, "brevo should be exhausted after 2 sends (daily quota 2)")

	// Third send must fall through to resend (brevo exhausted)
	require.NoError(t, pool.Send(ctx, "a@b.c", "s3", "b3"))
	usage = pool.GetUsage()
	for _, u := range usage {
		if u.Name == "resend" {
			assert.Equal(t, 1, u.DailyUsed, "resend should have 1 after brevo exhausted")
		}
	}
	// Verify resend sender was called once, brevo twice
	for _, e := range pool.providers {
		f := e.sender.(*fakePoolSender)
		if e.spec.Name == "brevo" {
			assert.Equal(t, 2, f.calls)
		}
		if e.spec.Name == "resend" {
			assert.Equal(t, 1, f.calls)
		}
		if e.spec.Name == "direct" {
			assert.Equal(t, 0, f.calls, "direct should not be used while resend has quota")
		}
	}
}

func TestEmailPool_FailoverOnError(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 100, MonthlyQuota: 9000, Priority: 1},
			{Name: "resend", Host: "smtp.resend.com", Port: "587", From: "billing@avandab.com", DailyQuota: 100, MonthlyQuota: 3000, Priority: 2},
		},
	}, db, nil)
	// brevo fails once, resend succeeds
	for _, e := range pool.providers {
		if e.spec.Name == "brevo" {
			e.sender = &fakePoolSender{name: "brevo", failTimes: 1}
		} else {
			e.sender = &fakePoolSender{name: "resend"}
		}
	}
	err := pool.Send(context.Background(), "a@b.c", "failover", "body")
	require.NoError(t, err, "pool should fail over from brevo to resend")

	// brevo should have 1 call (failed), resend 1 call (succeeded)
	for _, e := range pool.providers {
		f := e.sender.(*fakePoolSender)
		if e.spec.Name == "brevo" {
			assert.Equal(t, 1, f.calls)
		}
		if e.spec.Name == "resend" {
			assert.Equal(t, 1, f.calls)
		}
	}
	// usage should count only successful provider (resend)
	usage := pool.GetUsage()
	for _, u := range usage {
		if u.Name == "resend" {
			assert.Equal(t, 1, u.DailyUsed)
		}
		if u.Name == "brevo" {
			assert.Equal(t, 0, u.DailyUsed, "failed send should not increment quota")
		}
	}
}

func TestEmailPool_DynamicSwitchAndEnable(t *testing.T) {
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 300, MonthlyQuota: 9000, Priority: 1},
			{Name: "resend", Host: "smtp.resend.com", Port: "587", From: "billing@avandab.com", DailyQuota: 100, MonthlyQuota: 3000, Priority: 2},
			{Name: "direct", Host: "direct", Direct: true, From: "billing@avandab.com", Priority: 90},
		},
	}, nil, nil)
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}
	// Initially brevo is primary
	assert.Equal(t, "brevo", pool.ListProviders()[0].Name)

	// Switch primary to resend
	require.NoError(t, pool.SetPrimary("resend"))
	assert.Equal(t, "resend", pool.ListProviders()[0].Name)
	assert.Equal(t, 1, pool.ListProviders()[0].Priority)

	// Disable brevo -> it should be skipped
	require.NoError(t, pool.SetProviderEnabled("brevo", false))
	ctx := context.Background()
	require.NoError(t, pool.Send(ctx, "a@b.c", "s", "b"))
	// brevo disabled, so resend should handle it
	for _, e := range pool.providers {
		f := e.sender.(*fakePoolSender)
		if e.spec.Name == "brevo" {
			assert.Equal(t, 0, f.calls)
		}
		if e.spec.Name == "resend" {
			assert.Equal(t, 1, f.calls)
		}
	}

	// Re-enable brevo and set priority higher than resend again
	require.NoError(t, pool.SetProviderEnabled("brevo", true))
	require.NoError(t, pool.SetProviderPriority("brevo", 0))
	assert.Equal(t, "brevo", pool.ListProviders()[0].Name)
}

func TestEmailPool_GetUsageCountsAndRemaining(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 300, MonthlyQuota: 9000, Priority: 1},
			{Name: "resend", Host: "smtp.resend.com", Port: "587", From: "billing@avandab.com", DailyQuota: 100, MonthlyQuota: 3000, Priority: 2},
		},
	}, db, nil)
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, pool.Send(ctx, "a@b.c", fmt.Sprintf("s%d", i), "b"))
	}
	usage := pool.GetUsage()
	for _, u := range usage {
		if u.Name == "brevo" {
			assert.Equal(t, 5, u.DailyUsed)
			assert.Equal(t, 295, u.DailyRemaining)
			assert.Equal(t, 8995, u.MonthlyRemaining)
			assert.False(t, u.Exhausted)
		}
		if u.Name == "resend" {
			assert.Equal(t, 0, u.DailyUsed, "resend should be untouched while brevo has quota")
		}
	}
}

func TestEmailPool_AllExhaustedReturnsError(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 1, MonthlyQuota: 1, Priority: 1},
		},
	}, db, nil)
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}
	require.NoError(t, pool.Send(context.Background(), "a@b.c", "s1", "b"))
	err := pool.Send(context.Background(), "a@b.c", "s2", "b")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "no provider")
}

func TestLoadPoolConfigFromEnv_DefaultProviders(t *testing.T) {
	t.Setenv("EMAIL_PROVIDERS_JSON", "")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("BREVO_SMTP_HOST", "")
	t.Setenv("RESEND_SMTP_HOST", "")
	cfg := LoadPoolConfigFromEnv()
	// Default should still seed brevo+resend+direct
	names := make(map[string]bool)
	for _, p := range cfg.Providers {
		names[p.Name] = true
	}
	assert.True(t, names["brevo"], "default brevo seeded")
	assert.True(t, names["resend"], "default resend seeded")
	assert.True(t, names["direct"], "direct always present")
}

func TestEmailPool_SendRichHTMLAndAttachments(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 0, MonthlyQuota: 0, Priority: 1},
		},
	}, db, nil)
	fake := &fakePoolSender{name: "brevo"}
	pool.providers[0].sender = fake

	require.NoError(t, pool.SendHTML(context.Background(), "to@test.com", "subj", "text", "<b>html</b>"))
	assert.Equal(t, "<b>html</b>", fake.lastMsg.HTMLBody)

	require.NoError(t, pool.SendWithAttachments(context.Background(), "to@test.com", "subj2", "text2", "<p>hi</p>", []Attachment{{Filename: "a.pdf", Data: []byte("pdf")}}))
	assert.Equal(t, 1, len(fake.lastMsg.Attachments))
}

func TestEmailPool_ResetUsage(t *testing.T) {
	db := newPoolTestDB(t)
	pool := NewEmailPool(PoolConfig{
		Strategy: "priority",
		Providers: []ProviderSpec{
			{Name: "brevo", Host: "smtp-relay.brevo.com", Port: "587", From: "billing@avandab.com", DailyQuota: 10, MonthlyQuota: 100, Priority: 1},
		},
	}, db, nil)
	for _, e := range pool.providers {
		e.sender = &fakePoolSender{name: e.spec.Name}
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, pool.Send(context.Background(), "a@b.c", "s", "b"))
	}
	assert.Equal(t, 3, pool.GetUsage()[0].DailyUsed)
	require.NoError(t, pool.ResetUsage("brevo"))
	assert.Equal(t, 0, pool.GetUsage()[0].DailyUsed)
}
