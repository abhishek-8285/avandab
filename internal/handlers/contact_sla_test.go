package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ratchet: SLA derives from created_at/status only (no migration).
// Fails on pre-SLA code (no contactSLA helper).
func TestContactSLA_AckOverdue(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	ackDue, redressDue, state := contactSLA("2026-09-10 10:00:00", "", "pending", now)
	assert.Equal(t, "ack-overdue", state)
	assert.Equal(t, "12 Sep 2026, 10:00 UTC", ackDue.Format("02 Jan 2006, 15:04 UTC"))
	assert.Equal(t, "10 Oct 2026", redressDue.Format("02 Jan 2006"))
}

func TestContactSLA_OnTrackAndRedressed(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	_, _, fresh := contactSLA("2026-09-15 11:00:00", "", "pending", now)
	assert.Equal(t, "on-track", fresh)

	_, _, touched := contactSLA("2026-09-10 10:00:00", "", "in_progress", now)
	assert.Equal(t, "on-track", touched, "human touch counts as acknowledgement")

	_, _, done := contactSLA("2026-06-01 10:00:00", "", "resolved", now)
	assert.Equal(t, "redressed", done)

	_, _, stale := contactSLA("2026-06-01 10:00:00", "", "pending", now)
	assert.Equal(t, "redress-overdue", stale)
}

func TestContactSLA_BadTimestamp(t *testing.T) {
	_, _, state := contactSLA("not-a-date", "", "pending", time.Now().UTC())
	assert.Equal(t, "unknown", state)
	require.NotNil(t, contactSLA)
}

func TestContactSLA_AcknowledgedAtProvesAck(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	// Stale pending ticket with acknowledged_at set counts as acknowledged.
	_, _, state := contactSLA("2026-09-10 10:00:00", "2026-09-10 11:00:00", "pending", now)
	assert.Equal(t, "on-track", state)
}

func postTicketStatus(t *testing.T, app *App, ticket, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/support/tickets/"+ticket+"/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ticket", ticket)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	(&ContactHandlers{app}).UpdateStatus(rr, req)
	return rr
}

// Ratchet: status transitions stamp acknowledged_at once (first touch wins).
// Fails on pre-00156 code (no acknowledged_at column / handler).
func TestContactUpdateStatus_StampsAckOnce(t *testing.T) {
	app := newRegisterTestApp(t)
	_, err := app.DB.Exec(`INSERT INTO contact_submissions (id, ticket_number, name, email, subject, category, message, status, created_at)
		VALUES ('t-ack-1', 'AVN-ACK1', 'Acker', 'acker@test.io', 'Late bus', 'support', 'Where is my bus?', 'pending', '2026-09-10 10:00:00')`)
	require.NoError(t, err)

	rr := postTicketStatus(t, app, "AVN-ACK1", `{"status":"in_progress"}`)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"acknowledged":true`)

	var status, ack string
	require.NoError(t, app.DB.QueryRow(`SELECT status, COALESCE(acknowledged_at,'') FROM contact_submissions WHERE ticket_number='AVN-ACK1'`).Scan(&status, &ack))
	assert.Equal(t, "in_progress", status)
	require.NotEmpty(t, ack, "first touch must stamp acknowledged_at")

	rr = postTicketStatus(t, app, "AVN-ACK1", `{"status":"resolved"}`)
	assert.Equal(t, http.StatusOK, rr.Code)
	var ack2 string
	require.NoError(t, app.DB.QueryRow(`SELECT COALESCE(acknowledged_at,'') FROM contact_submissions WHERE ticket_number='AVN-ACK1'`).Scan(&ack2))
	assert.Equal(t, ack, ack2, "second touch must not overwrite first acknowledgement")
}

func TestContactUpdateStatus_RejectsBadInput(t *testing.T) {
	app := newRegisterTestApp(t)

	rr := postTicketStatus(t, app, "AVN-ACK1", `{"status":"vibing"}`)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	rr = postTicketStatus(t, app, "AVN-NOPE", `{"status":"resolved"}`)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestContactListTickets_FiltersByStatus(t *testing.T) {
	app := newRegisterTestApp(t)
	_, err := app.DB.Exec(`INSERT INTO contact_submissions (id, ticket_number, name, email, subject, category, message, status, created_at) VALUES
		('t-l1', 'AVN-L1', 'Lister One', 'l1@test.io', 'S1', 'support', 'M1', 'pending', '2026-09-14 10:00:00'),
		('t-l2', 'AVN-L2', 'Lister Two', 'l2@test.io', 'S2', 'billing', 'M2', 'resolved', '2026-09-13 10:00:00')`)
	require.NoError(t, err)

	h := &ContactHandlers{app}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/support/tickets?status=pending", nil)
	rr := httptest.NewRecorder()
	h.ListTickets(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "AVN-L1")
	assert.NotContains(t, rr.Body.String(), "AVN-L2")

	req = httptest.NewRequest(http.MethodGet, "/api/v1/support/tickets", nil)
	rr = httptest.NewRecorder()
	h.ListTickets(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "AVN-L1")
	assert.Contains(t, rr.Body.String(), "AVN-L2")

	req = httptest.NewRequest(http.MethodGet, "/api/v1/support/tickets?status=vibing", nil)
	rr = httptest.NewRecorder()
	h.ListTickets(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
