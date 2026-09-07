package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// stubDBGetter is an in-memory DBGetter fake — no migrations, no files.
type stubDBGetter struct{ db *sql.DB }

func (s stubDBGetter) DB() *sql.DB { return s.db }

func openProbeDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("file:repotx_%s_%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	conn, err := sql.Open("sqlite", name)
	require.NoError(t, err)
	_, err = conn.Exec(`CREATE TABLE tx_probe (id TEXT PRIMARY KEY)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close probe db: %v", err)
		}
	})
	return conn
}

func probeCount(t *testing.T, conn *sql.DB, id string) int {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM tx_probe WHERE id = ?`, id).Scan(&n))
	return n
}

// TestTxContextRoundTrip covers WithTxInContext/TxFromContext: absent tx
// yields nil, a stored tx comes back as the same pointer.
func TestTxContextRoundTrip(t *testing.T) {
	require.Nil(t, TxFromContext(context.Background()))

	conn := openProbeDB(t)
	tx, err := conn.Begin()
	require.NoError(t, err)
	require.Same(t, tx, TxFromContext(WithTxInContext(context.Background(), tx)))
	require.NoError(t, tx.Rollback())
}

// TestTxManagerCommit proves fn's writes survive when fn returns nil.
func TestTxManagerCommit(t *testing.T) {
	conn := openProbeDB(t)
	tm := NewTxManager(stubDBGetter{db: conn})
	require.NoError(t, tm.WithTransaction(context.Background(), func(ctx context.Context) error {
		tx := TxFromContext(ctx)
		require.NotNil(t, tx)
		_, err := tx.ExecContext(ctx, `INSERT INTO tx_probe (id) VALUES ('commit-1')`)
		return err
	}))
	require.Equal(t, 1, probeCount(t, conn, "commit-1"))
}

// TestTxManagerRollback proves fn's writes are discarded and fn's error is
// returned unwrapped when fn fails.
func TestTxManagerRollback(t *testing.T) {
	sentinel := errors.New("probe failure")
	conn := openProbeDB(t)
	tm := NewTxManager(stubDBGetter{db: conn})
	err := tm.WithTransaction(context.Background(), func(ctx context.Context) error {
		tx := TxFromContext(ctx)
		require.NotNil(t, tx)
		_, execErr := tx.ExecContext(ctx, `INSERT INTO tx_probe (id) VALUES ('rollback-1')`)
		require.NoError(t, execErr)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 0, probeCount(t, conn, "rollback-1"))
}
