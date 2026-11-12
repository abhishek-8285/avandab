package db_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00181_AlertEscalationSchedules_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00181_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 180))

	var nullCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE escalation_schedule IS NULL OR escalation_schedule = ''`).Scan(&nullCount))
	assert.Equal(t, 16, nullCount, "all shipped rules must start without a schedule (pre-00181)")

	require.NoError(t, goose.UpTo(db, "migrations", 181))

	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE escalation_schedule IS NULL OR escalation_schedule = ''`).Scan(&nullCount))
	assert.Equal(t, 0, nullCount, "migration 00181 must backfill every shipped rule")

	// Severity tiers get different schedule depths.
	steps := func(ruleID string) int {
		var raw string
		require.NoError(t, db.QueryRow(`SELECT escalation_schedule FROM alert_rules WHERE id = ?`, ruleID).Scan(&raw))
		var parsed []struct {
			AfterSeconds int    `json:"after_seconds"`
			TargetRole   string `json:"target_role"`
			Channel      string `json:"channel"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &parsed))
		return len(parsed)
	}
	assert.Equal(t, 3, steps("rule_temp_breach"), "critical rule gets 3 escalation steps")
	assert.Equal(t, 2, steps("rule_speeding"), "warning rule gets 2 escalation steps")
	assert.Equal(t, 1, steps("rule_refill"), "info rule gets 1 escalation step")

	require.NoError(t, goose.DownTo(db, "migrations", 180))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE escalation_schedule IS NULL OR escalation_schedule = ''`).Scan(&nullCount))
	assert.Equal(t, 16, nullCount, "down migration must clear the backfilled schedules")
}
