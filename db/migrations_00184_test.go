package db_test

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

func TestMigration00184_StuckPayoutAlertRule_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00184_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 183))

	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_payout_stuck'`).Scan(&count))
	assert.Zero(t, count, "rule_payout_stuck must not exist before v184")

	require.NoError(t, goose.UpTo(db, "migrations", 184))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_payout_stuck'`).Scan(&count))
	assert.Equal(t, 1, count, "rule_payout_stuck must exist after v184")

	var source, severity, schedule, cooldown string
	require.NoError(t, db.QueryRow(`
		SELECT source, severity, escalation_schedule, cooldown_seconds
		FROM alert_rules WHERE id = 'rule_payout_stuck'`).Scan(&source, &severity, &schedule, &cooldown))
	assert.Equal(t, "settlement", source)
	assert.Equal(t, "critical", severity)
	assert.Contains(t, schedule, "dispatcher")
	assert.Equal(t, "3600", cooldown)

	require.NoError(t, goose.DownTo(db, "migrations", 183))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_payout_stuck'`).Scan(&count))
	assert.Zero(t, count, "rule_payout_stuck must be removed on down migration")
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_sources WHERE name = 'settlement'`).Scan(&count))
	assert.Zero(t, count, "settlement alert source must be removed on down migration")
}
