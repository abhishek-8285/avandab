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

// Gap-3 guard: every synthetic ID minted in demo mode must carry a MOCK-
// marker so mock tolls are never mistaken for real NETC transactions.
func TestStub_MockMode_MarksSyntheticIDs(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: true}, nil)

	txs, err := c.ListTransactions(context.Background(), "MH01AB1111", 3)
	require.NoError(t, err)
	require.Len(t, txs, 3)
	for _, tx := range txs {
		assert.Contains(t, tx.TransactionID, "MOCK-", "mock txn ID %q must be marked", tx.TransactionID)
	}

	txn, err := c.DeductToll(context.Background(), DeductTollRequest{
		VehicleNumber: "MH01AB1111", Amount: 95, PlazaName: "Demo Plaza",
	})
	require.NoError(t, err, "demo mode without a ledger still mints a marked txn")
	assert.Contains(t, txn.TransactionID, "MOCK-", "mock deduct txn ID %q must be marked", txn.TransactionID)
}

// Gap-3 guard: a disabled integration must error honestly on every method,
// never fabricate balances, tolls, or history.
func TestStub_Disabled_ErrorsHonestly(t *testing.T) {
	c := NewClient(Config{Enabled: false, UseMock: true}, nil)
	ctx := context.Background()

	_, err := c.GetBalance(ctx, "MH01AB1111", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")

	_, err = c.DeductToll(ctx, DeductTollRequest{VehicleNumber: "MH01AB1111", Amount: 95})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")

	_, err = c.ListTransactions(ctx, "MH01AB1111", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")

	_, err = c.Reconcile(ctx, "MH01AB1111", "2026-08-01", "2026-08-02")
	require.Error(t, err)
}

// Gap-3 guard: with no local ledger and no demo mode, DeductToll must refuse
// instead of fabricating a SUCCESS toll out of thin air.
func TestStub_NoMock_NoLedger_DeductRefusesFabrication(t *testing.T) {
	c := NewClient(Config{Enabled: true, UseMock: false}, nil)
	_, err := c.DeductToll(context.Background(), DeductTollRequest{
		VehicleNumber: "MH01AB1111", Amount: 95, PlazaName: "Nowhere Plaza",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no local ledger")
}
