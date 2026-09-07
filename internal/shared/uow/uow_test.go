package uow

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
	"transport-app/internal/repository"
	"transport-app/internal/shared/ports"
)

func openProbeDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("file:uow_%s_%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	conn, err := sql.Open("sqlite", name)
	require.NoError(t, err)
	_, err = conn.Exec(`CREATE TABLE uow_probe (id TEXT PRIMARY KEY)`)
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
	require.NoError(t, conn.QueryRow(`SELECT COUNT(*) FROM uow_probe WHERE id = ?`, id).Scan(&n))
	return n
}

// TestExecuteCommitsOnSuccess proves Execute injects a tx into the context,
// exposes all repository providers, and commits fn's writes on nil return.
func TestExecuteCommitsOnSuccess(t *testing.T) {
	conn := openProbeDB(t)
	u := NewSQLUnitOfWork(conn)
	require.NoError(t, u.Execute(context.Background(), func(txCtx ports.TxContext) error {
		p := txCtx.Repositories()
		require.NotNil(t, p)
		require.NotNil(t, p.Bookings())
		require.NotNil(t, p.Trips())
		require.NotNil(t, p.Drivers())
		require.NotNil(t, p.Vehicles())
		require.NotNil(t, p.Invoices())
		require.NotNil(t, p.Payments())
		require.NotNil(t, p.AuditLogs())
		require.NotNil(t, p.Maintenance())
		tx := repository.TxFromContext(txCtx)
		require.NotNil(t, tx)
		_, err := tx.ExecContext(txCtx, `INSERT INTO uow_probe (id) VALUES ('uow-commit')`)
		return err
	}))
	require.Equal(t, 1, probeCount(t, conn, "uow-commit"))
}

// TestExecuteRollsBackOnError proves fn's writes are discarded and fn's
// error propagates when fn fails.
func TestExecuteRollsBackOnError(t *testing.T) {
	sentinel := errors.New("probe failure")
	conn := openProbeDB(t)
	u := NewSQLUnitOfWork(conn)
	err := u.Execute(context.Background(), func(txCtx ports.TxContext) error {
		tx := repository.TxFromContext(txCtx)
		require.NotNil(t, tx)
		_, execErr := tx.ExecContext(txCtx, `INSERT INTO uow_probe (id) VALUES ('uow-rollback')`)
		require.NoError(t, execErr)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 0, probeCount(t, conn, "uow-rollback"))
}
