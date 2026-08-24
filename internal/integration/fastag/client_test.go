package fastag

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression guards for the fabrication fix: the stub client must never
// invent balances, transactions, or reconcile counts when demo mode is off.
// Fabricated tolls previously flowed into approved driver expenses.

func TestStub_NoMock_ListTransactionsNeverFabricates(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: false}, nil)
	txs, err := c.ListTransactions(context.Background(), "MH01AB1111", 10)
	require.NoError(t, err, "empty history is a valid state, not an error")
	assert.Empty(t, txs, "must not synthesize toll transactions")
}

func TestStub_NoMock_GetBalanceErrorsWithoutTagRecord(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: false}, nil)
	_, err := c.GetBalance(context.Background(), "MH01AB1111", "")
	require.Error(t, err, "invented balance forbidden outside demo mode")
}

func TestStub_NoMock_ReconcileRefusesFakeCounts(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: false}, nil)
	_, err := c.Reconcile(context.Background(), "MH01AB1111", "2026-08-01", "2026-08-02")
	require.Error(t, err)
}

func TestStub_MockMode_KeepsDemoBehaviour(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: true}, nil)

	txs, err := c.ListTransactions(context.Background(), "MH01AB1111", 3)
	require.NoError(t, err)
	assert.Len(t, txs, 3, "demo mode keeps synthesized sample data")

	bal, err := c.GetBalance(context.Background(), "", "TAGX")
	require.NoError(t, err)
	assert.Equal(t, 2475.50, bal.Balance)
}
