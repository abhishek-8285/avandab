package application

// Characterization + extraction tests for the contact-ticket store moved out
// of internal/handlers/contact.go (handlers-end-state ADR, Phase 2):
// submission/find by number+email, first-touch acknowledgement stamping,
// list ordering/filter/cap and the frozen global-inbox scope.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const ticketsTestSchema = `
CREATE TABLE contact_submissions (
    id TEXT PRIMARY KEY,
    ticket_number TEXT NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    phone TEXT,
    company_name TEXT,
    subject TEXT NOT NULL,
    category TEXT,
    message TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    acknowledged_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newTicketsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("contact_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(ticketsTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecTickets(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestContactService_SubmitAndFindForRequester(t *testing.T) {
	db := newTicketsTestDB(t)
	ctx := context.Background()
	svc := NewService(db)

	ticketNo, err := svc.Submit(ctx, SubmitInput{
		Name: "Asha", Email: "asha@test.io", Phone: "999", CompanyName: "Asha Logistics",
		Subject: "Where is my bus?", Category: "support", Message: "Late by 2h",
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ticketNo, "AVN-"), "legacy public ticket reference")

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM contact_submissions WHERE ticket_number = ?`, ticketNo).Scan(&status))
	assert.Equal(t, "pending", status)

	ticket, err := svc.FindForRequester(ctx, ticketNo, "asha@test.io")
	require.NoError(t, err)
	require.NotNil(t, ticket)
	assert.Equal(t, ticketNo, ticket.TicketNumber)
	assert.Equal(t, "Asha", ticket.Name)
	assert.Equal(t, "support", ticket.Category)
	assert.Equal(t, "Late by 2h", ticket.Message)
	assert.Empty(t, ticket.AcknowledgedAt)

	// Requesters must present the email they submitted with.
	_, err = svc.FindForRequester(ctx, ticketNo, "someone-else@test.io")
	assert.ErrorIs(t, err, ErrTicketNotFound)
	_, err = svc.FindForRequester(ctx, "AVN-NOPE", "asha@test.io")
	assert.ErrorIs(t, err, ErrTicketNotFound)

	// FROZEN LEGACY SCOPE: contact_submissions is a company-global inbox
	// (no tenant_id); the store has no tenant filter by design.
	_, err = NewService(nil).Submit(ctx, SubmitInput{})
	require.Error(t, err)
}

func TestContactService_UpdateStatusStampsAckOnce(t *testing.T) {
	db := newTicketsTestDB(t)
	ctx := context.Background()
	mustExecTickets(t, db, `INSERT INTO contact_submissions (id, ticket_number, name, email, subject, category, message, status, created_at, updated_at)
		VALUES ('t-1', 'AVN-ACK1', 'Acker', 'acker@test.io', 'Late bus', 'support', 'Where?', 'pending', '2026-09-10 10:00:00', '2026-09-10 10:00:00')`)

	svc := NewService(db)
	found, err := svc.UpdateStatus(ctx, "AVN-ACK1", "in_progress")
	require.NoError(t, err)
	assert.True(t, found)

	var status string
	var ack sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status, CAST(acknowledged_at AS TEXT) FROM contact_submissions WHERE ticket_number = 'AVN-ACK1'`).Scan(&status, &ack))
	assert.Equal(t, "in_progress", status)
	assert.True(t, ack.Valid, "first touch stamps acknowledged_at")
	firstAck := ack.String

	found, err = svc.UpdateStatus(ctx, "AVN-ACK1", "resolved")
	require.NoError(t, err)
	assert.True(t, found)
	require.NoError(t, db.QueryRow(`SELECT CAST(acknowledged_at AS TEXT) FROM contact_submissions WHERE ticket_number = 'AVN-ACK1'`).Scan(&ack))
	assert.Equal(t, firstAck, ack.String, "second touch must keep the first acknowledgement timestamp")

	found, err = svc.UpdateStatus(ctx, "AVN-NOPE", "resolved")
	require.NoError(t, err)
	assert.False(t, found, "unknown ticket -> handler 404")
}

func TestContactService_ListOrderingFilterCap(t *testing.T) {
	db := newTicketsTestDB(t)
	ctx := context.Background()
	mustExecTickets(t, db, `INSERT INTO contact_submissions (id, ticket_number, name, email, subject, category, message, status, created_at)
		VALUES ('t-1', 'AVN-L1', 'One', 'l1@test.io', 'S1', 'support', 'M1', 'pending', '2026-09-14 10:00:00'),
		       ('t-2', 'AVN-L2', 'Two', 'l2@test.io', 'S2', 'billing', 'M2', 'resolved', '2026-09-13 10:00:00')`)

	svc := NewService(db)
	all, err := svc.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "AVN-L1", all[0].TicketNumber, "newest first")
	assert.Equal(t, "AVN-L2", all[1].TicketNumber)
	assert.Equal(t, "One", all[0].Name)
	assert.Equal(t, "support", all[0].Category)
	assert.Empty(t, all[0].AcknowledgedAt, "NULL acknowledged_at renders empty")

	pending, err := svc.List(ctx, "pending")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "AVN-L1", pending[0].TicketNumber)

	resolved, err := svc.List(ctx, "resolved")
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "AVN-L2", resolved[0].TicketNumber)

	// Legacy cap is 100 rows, newest first.
	mustExecTickets(t, db, `DELETE FROM contact_submissions`)
	for i := 0; i < 101; i++ {
		mustExecTickets(t, db, `INSERT INTO contact_submissions (id, ticket_number, name, email, subject, category, message, status, created_at)
			VALUES (?, ?, 'Bulk', 'bulk@test.io', 'S', 'support', 'M', 'pending', ?)`,
			fmt.Sprintf("tb-%03d", i), fmt.Sprintf("AVN-B%03d", i), fmt.Sprintf("2026-02-01 00:%02d:%02d", (i-1+60)/60, i%60))
	}
	capped, err := svc.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, capped, 100)
}
