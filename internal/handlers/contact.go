package handlers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/apperr"
	"transport-app/internal/httpx"
	"transport-app/internal/middleware"
)

// ContactHandlers handles public contact inquiries and ticket status tracking.
type ContactHandlers struct {
	*App
}

func (h *ContactHandlers) Routes(r chi.Router) {
	r.Get("/", h.Page)
	// Distributed limiter so the per-IP budget holds across replicas
	// (in-memory counters multiply with replica count).
	r.With(middleware.RateLimitDistributed(h.Cache, 10)).Post("/submit", h.Submit)
	r.Get("/status", h.StatusCheck)
}

// Ticket struct for data rendering
type ContactTicket struct {
	ID             string
	TicketNumber   string
	Name           string
	Email          string
	Phone          string
	CompanyName    string
	Subject        string
	Category       string
	Message        string
	Status         string
	AcknowledgedAt string
	CreatedAt      string
	UpdatedAt      string
}

// Consumer grievance SLAs (E-Commerce Amendment 2026, eff. 1 Jan 2027):
// acknowledge within 48h, redress within 1 month. Acknowledgement is proven
// by acknowledged_at (00156, stamped on first admin touch); the status proxy
// (non-pending = touched) covers only pre-migration rows.
// ponytail: date math only, no timers/queues until complaint volume justifies.
func contactSLA(createdAt, acknowledgedAt, status string, now time.Time) (ackDue, redressDue time.Time, state string) {
	created, err := time.ParseInLocation("2006-01-02 15:04:05", createdAt, time.UTC)
	if err != nil {
		if created, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return time.Time{}, time.Time{}, "unknown"
		}
	}
	ackDue = created.Add(48 * time.Hour)
	redressDue = created.AddDate(0, 1, 0)
	acked := acknowledgedAt != "" || status != "pending"
	switch {
	case status == "resolved" || status == "closed":
		return ackDue, redressDue, "redressed"
	case now.After(redressDue):
		return ackDue, redressDue, "redress-overdue"
	case acked:
		return ackDue, redressDue, "on-track"
	case now.After(ackDue):
		return ackDue, redressDue, "ack-overdue"
	default:
		return ackDue, redressDue, "on-track"
	}
}

func generateTicketNumber() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		return fmt.Sprintf("AVN-%X", n.Int64())
	}
	return fmt.Sprintf("AVN-%X", b)
}

// Page renders the contact-us & ticket status page.
func (h *ContactHandlers) Page(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	ticketNo := r.URL.Query().Get("ticket")
	email := r.URL.Query().Get("email")
	ref := r.URL.Query().Get("ref")
	about := r.URL.Query().Get("about")

	var ticket *ContactTicket
	var searchErr string

	if ticketNo != "" {
		ticket, searchErr = h.fetchTicketByNumber(r.Context(), ticketNo, email)
	}

	var slaAckDue, slaRedressDue, slaState, slaAckAt string
	if ticket != nil {
		ackDue, redressDue, state := contactSLA(ticket.CreatedAt, ticket.AcknowledgedAt, ticket.Status, time.Now().UTC())
		if !ackDue.IsZero() {
			slaAckDue = ackDue.Format("02 Jan 2006, 15:04 UTC")
			slaRedressDue = redressDue.Format("02 Jan 2006")
		}
		slaState = state
		if ticket.AcknowledgedAt != "" {
			if at, err := time.ParseInLocation("2006-01-02 15:04:05", ticket.AcknowledgedAt, time.UTC); err == nil {
				slaAckAt = at.Format("02 Jan 2006, 15:04 UTC")
			} else {
				slaAckAt = ticket.AcknowledgedAt
			}
		}
	}

	pd := PageData{
		Title:          "Contact Us & Support Status",
		SEODescription: "Contact Avandab support — fleet onboarding, billing, tracking help and ticket status lookup.",
		CanonicalPath:  "/contact-us",
		NoIndex:        false,
		User:           session,
		Extra: map[string]interface{}{
			"Ticket":         ticket,
			"SearchErr":      searchErr,
			"SearchQuery":    ticketNo,
			"SearchEmail":    email,
			"SubmittedNum":   r.URL.Query().Get("submitted"),
			"SubmittedEmail": email,
			"SLAAckDue":      slaAckDue,
			"SLARedressDue":  slaRedressDue,
			"SLAState":       slaState,
			"SLAAckAt":       slaAckAt,
			"ErrorRef":       ref,
			"ErrorAbout":     about,
			"PrefillSubject": func() string {
				if about == "error-page" && ref != "" {
					return "Support request — error ref " + ref
				}
				return ""
			}(),
			"PrefillMessage": func() string {
				if about == "error-page" && ref != "" {
					return "I encountered an error (Ref: " + ref + "). Please help.\n\nDetails:\n"
				}
				return ""
			}(),
		},
	}

	// If user is unauthenticated, render standalone page without dashboard sidebar layout
	if session == nil {
		h.renderAuthPage(w, "contact.html", pd)
		return
	}

	h.renderPage(w, r, "contact.html", pd)
}

// Submit handles submission of a new inquiry.
func (h *ContactHandlers) Submit(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	email := r.PostFormValue("email")
	phone := r.PostFormValue("phone")
	company := r.PostFormValue("company_name")
	subject := r.PostFormValue("subject")
	category := r.PostFormValue("category")
	message := r.PostFormValue("message")
	if ref := r.PostFormValue("error_ref"); ref != "" {
		message = "[Error Ref: " + ref + "]\n" + message
		if subject == "" {
			subject = "Support request — error ref " + ref
		}
		if category == "" {
			category = "support"
		}
	}

	if name == "" || email == "" || subject == "" || message == "" {
		http.Redirect(w, r, "/contact-us?error=Missing+required+fields", http.StatusSeeOther)
		return
	}

	id := uuid.New().String()
	ticketNo := generateTicketNumber()

	_, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO contact_submissions (id, ticket_number, name, email, phone, company_name, subject, category, message, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
	`, id, ticketNo, name, email, phone, company, subject, category, message)

	if err != nil {
		http.Error(w, "Failed to submit inquiry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	query := url.Values{}
	query.Set("ticket", ticketNo)
	query.Set("email", email)
	query.Set("submitted", ticketNo)
	http.Redirect(w, r, "/contact-us?"+query.Encode(), http.StatusSeeOther)
}

// StatusCheck JSON/fragment endpoint for tracking ticket status.
func (h *ContactHandlers) StatusCheck(w http.ResponseWriter, r *http.Request) {
	ticketNo := r.URL.Query().Get("ticket")
	if ticketNo == "" {
		http.Redirect(w, r, "/contact-us", http.StatusSeeOther)
		return
	}
	query := url.Values{}
	query.Set("ticket", ticketNo)
	if email := r.URL.Query().Get("email"); email != "" {
		query.Set("email", email)
	}
	http.Redirect(w, r, "/contact-us?"+query.Encode(), http.StatusSeeOther)
}

func (h *ContactHandlers) fetchTicketByNumber(ctx context.Context, ticketNo, email string) (*ContactTicket, string) {
	if ticketNo == "" || email == "" {
		return nil, "Enter both your ticket number and the email address you used to submit it."
	}

	var t ContactTicket

	err := h.DB.QueryRowContext(ctx, `
		SELECT id, ticket_number, name, email, COALESCE(phone, ''), COALESCE(company_name, ''), subject, category, message, status, COALESCE(acknowledged_at, ''), created_at, updated_at
		FROM contact_submissions
		WHERE ticket_number = $1 AND email = $2
		ORDER BY created_at DESC LIMIT 1
	`, ticketNo, email).Scan(
		&t.ID, &t.TicketNumber, &t.Name, &t.Email, &t.Phone, &t.CompanyName, &t.Subject, &t.Category, &t.Message, &t.Status, &t.AcknowledgedAt, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return nil, "No support ticket found for the ticket number and email provided."
	}

	return &t, ""
}

// UpdateStatus transitions a grievance ticket. Mounted behind users:manage
// (same admin gate as plans price updates). First touch stamps
// acknowledged_at — the 48h-ack proof. Whitelisted statuses only.
// NOTE: contact_submissions is a company-global inbox (no tenant_id).
// Triage is for the designated grievance team; in multi-tenant use, treat
// tickets as cross-tenant data and restrict users:manage accordingly.
func (h *ContactHandlers) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	ticketNo := chi.URLParam(r, "ticket")
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httpx.Error(w, r, apperr.New(apperr.CodeMalformedJSON).WithCause(err))
		return
	}
	switch req.Status {
	case "pending", "in_progress", "resolved", "closed":
	default:
		httpx.Error(w, r, apperr.New(apperr.CodeValidation).
			WithDetail("status must be one of pending, in_progress, resolved, closed"))
		return
	}
	res, err := h.DB.ExecContext(r.Context(), `
		UPDATE contact_submissions
		SET status = $1, updated_at = CURRENT_TIMESTAMP,
		    acknowledged_at = COALESCE(acknowledged_at, CURRENT_TIMESTAMP)
		WHERE ticket_number = $2
	`, req.Status, ticketNo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
		return
	}
	slog.InfoContext(r.Context(), "grievance ticket updated",
		slog.String("ticket", ticketNo),
		slog.String("status", req.Status),
	)
	httpx.JSON(w, http.StatusOK, map[string]interface{}{
		"ticket": ticketNo, "status": req.Status, "acknowledged": true,
	})
}

// ListTickets returns grievance tickets, newest first, cap 100, optionally
// ?status=. Same users:manage gate as UpdateStatus (see note above).
func (h *ContactHandlers) ListTickets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	query := `SELECT ticket_number, name, email, subject, category, status,
		COALESCE(acknowledged_at, ''), created_at, updated_at
		FROM contact_submissions`
	var args []interface{}
	if status != "" {
		switch status {
		case "pending", "in_progress", "resolved", "closed":
		default:
			httpx.Error(w, r, apperr.New(apperr.CodeValidation).
				WithDetail("status must be one of pending, in_progress, resolved, closed"))
			return
		}
		query += ` WHERE status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()
	type ticketRow struct {
		TicketNumber   string `json:"ticket"`
		Name           string `json:"name"`
		Email          string `json:"email"`
		Subject        string `json:"subject"`
		Category       string `json:"category"`
		Status         string `json:"status"`
		AcknowledgedAt string `json:"acknowledged_at"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
	}
	out := make([]ticketRow, 0)
	for rows.Next() {
		var t ticketRow
		if err := rows.Scan(&t.TicketNumber, &t.Name, &t.Email, &t.Subject,
			&t.Category, &t.Status, &t.AcknowledgedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			httpx.Error(w, r, err)
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"tickets": out})
}
