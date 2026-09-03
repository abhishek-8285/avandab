package handlers

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	ID           string
	TicketNumber string
	Name         string
	Email        string
	Phone        string
	CompanyName  string
	Subject      string
	Category     string
	Message      string
	Status       string
	CreatedAt    string
	UpdatedAt    string
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
		ticket, searchErr = h.fetchTicketByNumber(ticketNo, email)
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

	_, err := h.DB.Exec(`
		INSERT INTO contact_submissions (id, ticket_number, name, email, phone, company_name, subject, category, message, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')
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

func (h *ContactHandlers) fetchTicketByNumber(ticketNo, email string) (*ContactTicket, string) {
	if ticketNo == "" || email == "" {
		return nil, "Enter both your ticket number and the email address you used to submit it."
	}

	var t ContactTicket

	err := h.DB.QueryRow(`
		SELECT id, ticket_number, name, email, COALESCE(phone, ''), COALESCE(company_name, ''), subject, category, message, status, created_at, updated_at
		FROM contact_submissions
		WHERE ticket_number = ? AND email = ?
		ORDER BY created_at DESC LIMIT 1
	`, ticketNo, email).Scan(
		&t.ID, &t.TicketNumber, &t.Name, &t.Email, &t.Phone, &t.CompanyName, &t.Subject, &t.Category, &t.Message, &t.Status, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return nil, "No support ticket found for the ticket number and email provided."
	}

	return &t, ""
}
