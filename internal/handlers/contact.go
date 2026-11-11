package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	contactapp "transport-app/internal/contact/application"
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

// ContactTicket is the contact_submissions row rendered on the status page.
// The type lives in internal/contact/application (handlers-end-state ADR,
// Phase 2); the alias keeps template references unchanged.
type ContactTicket = contactapp.Ticket

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
		h.renderAuthPage(w, r, "contact.html", pd)
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

	ticketNo, err := contactapp.NewService(h.DB).Submit(r.Context(), contactapp.SubmitInput{
		Name:        name,
		Email:       email,
		Phone:       phone,
		CompanyName: company,
		Subject:     subject,
		Category:    category,
		Message:     message,
	})
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

	ticket, err := contactapp.NewService(h.DB).FindForRequester(ctx, ticketNo, email)
	if err != nil {
		return nil, "No support ticket found for the ticket number and email provided."
	}
	return ticket, ""
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
	found, err := contactapp.NewService(h.DB).UpdateStatus(r.Context(), ticketNo, req.Status)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if !found {
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
	if status != "" {
		switch status {
		case "pending", "in_progress", "resolved", "closed":
		default:
			httpx.Error(w, r, apperr.New(apperr.CodeValidation).
				WithDetail("status must be one of pending, in_progress, resolved, closed"))
			return
		}
	}
	out, err := contactapp.NewService(h.DB).List(r.Context(), status)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]interface{}{"tickets": out})
}
