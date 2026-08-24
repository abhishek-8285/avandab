package service

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00094_RoundTrip proves the verification columns apply and
// revert cleanly (Spec 22 §7 per-step gate).
func TestMigration00094_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("rt94_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	colExists := func(col string) bool {
		var n int
		db.QueryRow(`SELECT count(*) FROM pragma_table_info('driver_expenses') WHERE name=?`, col).Scan(&n)
		return n == 1
	}
	idxExists := func(idx string) bool {
		var n int
		db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n)
		return n == 1
	}

	for _, col := range []string{"verification_state", "flag_reason", "ocr_amount", "ocr_confidence"} {
		assert.Truef(t, colExists(col), "driver_expenses.%s must exist after up", col)
	}
	assert.True(t, idxExists("idx_driver_expenses_verify"))

	// Default state on existing rows is 'manual' (CHECK constraint holds).
	_, err = db.Exec(`INSERT INTO driver_expenses (id, expense_type, category, amount)
		VALUES ('de-rt', 'fuel', 'fuel', 100)`)
	require.NoError(t, err)
	var vs string
	require.NoError(t, db.QueryRow(`SELECT verification_state FROM driver_expenses WHERE id='de-rt'`).Scan(&vs))
	assert.Equal(t, VerifyManual, vs)

	_, err = db.Exec(`INSERT INTO driver_expenses (id, expense_type, category, amount, verification_state)
		VALUES ('de-bad', 'fuel', 'fuel', 1, 'bogus')`)
	assert.Error(t, err, "CHECK constraint must reject unknown states")

	require.NoError(t, goose.DownTo(db, "../../db/migrations", 93))
	for _, col := range []string{"verification_state", "flag_reason", "ocr_amount", "ocr_confidence"} {
		assert.Falsef(t, colExists(col), "%s must be dropped after down", col)
	}
	assert.False(t, idxExists("idx_driver_expenses_verify"))

	require.NoError(t, goose.Up(db, "../../db/migrations"))
	assert.True(t, colExists("verification_state"), "re-up restores columns")
}
