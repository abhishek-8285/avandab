package maintenance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Due alerts reach each org's own admins with the right tenant on the event
// — never a dead 'system' recipient, never another org.
func TestWorker_DueNotifiesOwningOrg(t *testing.T) {
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','A','a'),('tenant-B','B','b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id) VALUES
		('u-a','a@x.in','x','Admin A',1,'active','tenant-A'),
		('u-b','b@x.in','x','Admin B',6,'active','tenant-B')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('va','RA','RA','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-A'),
		       ('vb','RB','RB','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-B')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, due_at, active)
		VALUES ('sched-a','va','oil_change',datetime('now','-1 day'),1),
		       ('sched-b','vb','brake',datetime('now','-1 day'),1)`)
	require.NoError(t, err)

	bus := newMockBus()
	worker := NewWorker(db, bus, nil, 15, "")
	worker.EvaluateSchedules(context.Background())

	// Each admin got exactly their own org's alert.
	var aCount, bCount, sysCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 'u-a'`).Scan(&aCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 'u-b'`).Scan(&bCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 'system'`).Scan(&sysCount))
	assert.Equal(t, 1, aCount)
	assert.Equal(t, 1, bCount)
	assert.Equal(t, 0, sysCount, "no dead-letter system notifications")

	// Events carry their org.
	events := bus.GetEvents("maintenance.due")
	require.Len(t, events, 2)
	tenants := map[string]bool{}
	for _, e := range events {
		m, ok := e.Payload.(map[string]interface{})
		require.True(t, ok)
		tenants[m["tenant_id"].(string)] = true
	}
	assert.True(t, tenants["tenant-A"] && tenants["tenant-B"])
}

// A service date 3 days out sends one advance reminder (not a due), and
// repeat sweeps do not spam: one row per window.
func TestWorker_DueSoonReminderOnce(t *testing.T) {
	db := newMaintTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-R','R','r')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id) VALUES
		('u-r','r@x.in','x','Admin R',1,'active','tenant-R')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES ('vr','RR','RR','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'),'tenant-R')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, due_at, active)
		VALUES ('sched-r','vr','oil_change',datetime('now','+3 days'),1)`)
	require.NoError(t, err)

	bus := newMockBus()
	worker := NewWorker(db, bus, nil, 15, "")
	worker.EvaluateSchedules(context.Background())
	worker.EvaluateSchedules(context.Background())

	var reminder, due int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 'u-r' AND title LIKE 'Maintenance Due Soon%'`).Scan(&reminder))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = 'u-r' AND title LIKE 'Maintenance Due:%'`).Scan(&due))
	assert.Equal(t, 1, reminder, "exactly one reminder across two sweeps")
	assert.Equal(t, 0, due, "not yet due")
	assert.Empty(t, bus.GetEvents("maintenance.due"), "reminders stay inbox-only")
}
