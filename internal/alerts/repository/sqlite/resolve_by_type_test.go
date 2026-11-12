package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOpenBySourceType_SystemRecovery(t *testing.T) {
	db := newInboxTestDB(t)
	repo := NewAlertRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	insert := func(id, tenant, source, alertType, status string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO alerts (
			id, source, alert_type, severity, status, dedup_key,
			tenant_id, ack_status, severity_rank, money_at_risk,
			title, message, occurrences, first_seen_at, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, 'critical', ?, ?, ?, 'open', 1, 0, 't', 'm', 1, ?, ?, ?, ?)`,
			id, source, alertType, status, "dk-"+id, tenant, now, now, now, now)
		require.NoError(t, err)
	}
	insert("a-open", "tenant-a", "availability", "sla_breach", "open")
	insert("a-escalated", "tenant-b", "availability", "sla_breach", "escalated")
	insert("a-done", "tenant-a", "availability", "sla_breach", "resolved")
	insert("t-open", "tenant-a", "telemetry", "speeding", "open")

	n, err := repo.ResolveOpenBySourceType(ctx, "availability", "sla_breach", "system:test", now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "only open/escalated SLA alerts are resolved")

	for _, id := range []string{"a-open", "a-escalated"} {
		var status, resolvedBy, ackStatus string
		var resolvedAt *time.Time
		require.NoError(t, db.QueryRow(
			`SELECT status, resolved_by, ack_status, resolved_at FROM alerts WHERE id = ?`, id).
			Scan(&status, &resolvedBy, &ackStatus, &resolvedAt))
		assert.Equal(t, "resolved", status)
		assert.Equal(t, "system:test", resolvedBy)
		assert.Equal(t, "resolved", ackStatus)
		assert.NotNil(t, resolvedAt)
	}

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM alerts WHERE id = 't-open'`).Scan(&status))
	assert.Equal(t, "open", status, "unrelated alert types must be untouched")

	// Idempotent: nothing left to resolve.
	n, err = repo.ResolveOpenBySourceType(ctx, "availability", "sla_breach", "system:test", now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}
