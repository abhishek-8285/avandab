package channels

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
	"transport-app/internal/alerts/domain"
)

func waLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// countingSender records sends so the gate can be asserted.
type countingSender struct {
	sends int
	err   error
}

func (c *countingSender) send(ctx context.Context, phone, text string) error {
	c.sends++
	return c.err
}

func TestWhatsApp_RankGate(t *testing.T) {
	sender := &countingSender{}
	p := &WhatsAppProvider{gate: true, sender: sender, logger: waLogger()}
	ctx := context.Background()

	for _, tc := range []struct {
		rank   int
		wantOK bool
	}{
		{domain.RankCritical, true},
		{domain.RankUrgent, true},
		{domain.RankMoney, true},
		{domain.RankWaste, false}, // Spec §10: rank 4/5 never WhatsApp
		{domain.RankInfo, false},
	} {
		sender.sends = 0
		msg := Message{AlertID: "a", Title: "t", Body: "b", SeverityRank: tc.rank}
		require.NoError(t, p.Send(ctx, msg), "rank %d", tc.rank)
		if tc.wantOK {
			assert.Equalf(t, 1, sender.sends, "rank %d must send", tc.rank)
		} else {
			assert.Equalf(t, 0, sender.sends, "rank %d must be suppressed", tc.rank)
		}
	}

	// No explicit rank: derived from severity string.
	sender.sends = 0
	_ = p.Send(ctx, Message{AlertID: "a", Title: "t", Body: "b", Severity: domain.SeverityCritical})
	assert.Equal(t, 1, sender.sends, "critical severity derives to rank ≤3")

	sender.sends = 0
	_ = p.Send(ctx, Message{AlertID: "a", Title: "t", Body: "b", Severity: domain.SeverityInfo})
	assert.Equal(t, 0, sender.sends, "info severity derives to rank 5 → suppressed")
}

func TestWhatsApp_TransportsFailHonestWithoutCreds(t *testing.T) {
	g := gupshupSender{}
	m := metaSender{}
	ctx := context.Background()
	if err := g.send(ctx, "+911234567890", "x"); err == nil {
		t.Error("gupshup without api key must fail")
	}
	if err := m.send(ctx, "+911234567890", "x"); err == nil {
		t.Error("meta without token must fail")
	}
}

func TestLoggingProvider_RecordsSendAndFailure(t *testing.T) {
	db := newNLogTestDB(t)

	ok := NewLoggingProvider(stubOK{}, db, waLogger())
	fail := NewLoggingProvider(stubErr{}, db, waLogger())
	ctx := context.Background()

	require.NoError(t, ok.Send(ctx, Message{AlertID: "al-1", Phone: "+911234567890"}))
	require.Error(t, fail.Send(ctx, Message{AlertID: "al-2", Email: "ops@x.com"}))

	var status1, status2 string
	var target2 string
	require.NoError(t, db.QueryRow(`SELECT status FROM notification_log WHERE alert_id='al-1'`).Scan(&status1))
	require.NoError(t, db.QueryRow(`SELECT status, COALESCE(error,'') FROM notification_log WHERE alert_id='al-2'`).Scan(&status2, &target2))
	assert.Equal(t, "sent", status1)
	assert.Equal(t, "failed", status2)

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notification_log`).Scan(&n))
	assert.Equal(t, 2, n)
}

type stubOK struct{}

func (stubOK) Name() string { return "stub-ok" }
func (stubOK) Send(ctx context.Context, msg Message) error {
	return ctx.Err()
}

type stubErr struct{}

func (stubErr) Name() string { return "stub-err" }
func (stubErr) Send(ctx context.Context, msg Message) error {
	return fmt.Errorf("transport down")
}

func newNLogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite",
		"file:test_nlog_"+fmt.Sprint(time.Now().UnixNano())+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_ = goose.SetDialect("sqlite")
	cwd, _ := os.Getwd()
	mig := filepath.Join(cwd, "../../../db/migrations")
	if filepath.Base(cwd) == "basic" {
		mig = "db/migrations"
	}
	require.NoError(t, goose.Up(db, mig))
	return db
}
