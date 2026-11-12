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

func TestMigration00182_AvailabilitySLAAlert_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00182_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 181))

	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_sla_breach'`).Scan(&count))
	assert.Equal(t, 0, count, "rule_sla_breach must not exist before v182")

	require.NoError(t, goose.UpTo(db, "migrations", 182))

	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_sla_breach'`).Scan(&count))
	assert.Equal(t, 1, count, "rule_sla_breach must exist after v182")

	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_sources WHERE name = 'availability'`).Scan(&count))
	assert.Equal(t, 1, count, "availability alert source must exist after v182")

	var raw string
	var cooldown int
	require.NoError(t, db.QueryRow(
		`SELECT escalation_schedule, cooldown_seconds FROM alert_rules WHERE id = 'rule_sla_breach'`).Scan(&raw, &cooldown))
	assert.Equal(t, 21600, cooldown, "SLA breach alerts must not spam (6h cooldown)")

	var steps []struct {
		AfterSeconds int    `json:"after_seconds"`
		TargetRole   string `json:"target_role"`
		Channel      string `json:"channel"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &steps))
	require.Len(t, steps, 3, "critical SLA rule gets 3 escalation steps")
	assert.Equal(t, "in_app", steps[0].Channel)

	require.NoError(t, goose.DownTo(db, "migrations", 181))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_rules WHERE id = 'rule_sla_breach'`).Scan(&count))
	assert.Equal(t, 0, count, "rule_sla_breach must be removed on down migration")
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM alert_sources WHERE name = 'availability'`).Scan(&count))
	assert.Equal(t, 0, count, "availability source must be removed on down migration")
}
