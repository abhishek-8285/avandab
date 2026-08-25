package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/service"
)

// CreditNoteHandlers serves GST credit/debit note issuance and listing.
// Post-issuance corrections only: issued invoices are immutable, so value
// reductions go through POST /invoices/{id}/credit-note and value increases
// through POST /invoices/{id}/debit-note. Routes are mounted from
// InvoiceHandlers.Routes with the same permission middleware as invoice
// mutations (update for issuance, read for listing).
type CreditNoteHandlers struct {
	*App
}

// noteGuardError writes the HTTP response for a service-layer note guard or
// validation failure and reports whether it handled the error
// (mirrors invoiceGuardError in invoices.go).
func noteGuardError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, domain.ErrInvoiceNotFound):
		http.Error(w, "Invoice not found", http.StatusNotFound)
	case errors.Is(err, service.ErrNoteExceedsInvoiceTotal):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, service.ErrNoteReasonRequired), errors.Is(err, service.ErrNoteInvalidAmount):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		return false
	}
	return true
}

// parseFormAmount parses a decimal form field; an empty field means 0
// (matching the DDL defaults). Non-numeric or NaN/Inf input is rejected so
// strconv's lenient parsing can't smuggle bad amounts through.
func parseFormAmount(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// CreateCreditNote issues a credit note against an invoice (form POST like
// the line-item editors; JSON body accepted → 201 with the created note).
func (h *CreditNoteHandlers) CreateCreditNote(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, "credit")
}

// CreateDebitNote issues a debit note against an invoice.
func (h *CreditNoteHandlers) CreateDebitNote(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, "debit")
}

func (h *CreditNoteHandlers) create(w http.ResponseWriter, r *http.Request, noteType string) {
	invoiceID := chi.URLParam(r, "id")

	taxable, ok := parseFormAmount(r.FormValue("taxable_value"))
	if !ok {
		http.Error(w, "taxable_value must be a number", http.StatusBadRequest)
		return
	}
	igst, okI := parseFormAmount(r.FormValue("igst"))
	cgst, okC := parseFormAmount(r.FormValue("cgst"))
	sgst, okS := parseFormAmount(r.FormValue("sgst"))
	if !okI || !okC || !okS {
		http.Error(w, "igst/cgst/sgst must be numbers", http.StatusBadRequest)
		return
	}

	createdBy := ""
	if session, ok := h.getUserFromContext(r); ok && session != nil {
		createdBy = session.UserID
	}

	req := service.NoteRequest{
		InvoiceID:     invoiceID,
		Reason:        r.FormValue("reason"),
		PlaceOfSupply: r.FormValue("place_of_supply"),
		TaxableValue:  taxable,
		IGST:          igst,
		CGST:          cgst,
		SGST:          sgst,
		CreatedBy:     createdBy,
	}

	var (
		note *service.CreditNoteRecord
		err  error
	)
	if noteType == "credit" {
		note, err = h.Services.Notes.CreateCreditNote(r.Context(), req)
	} else {
		note, err = h.Services.Notes.CreateDebitNote(r.Context(), req)
	}
	if err != nil {
		if noteGuardError(w, err) {
			return
		}
		http.Error(w, fmt.Sprintf("Failed to create %s note", noteType), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(note)
		return
	}
	http.Redirect(w, r, "/invoices/"+invoiceID, http.StatusSeeOther)
}

// ListForInvoice returns the notes issued against an invoice as JSON
// (matches the pure-JSON style of SearchHSNSAC — no template needed).
func (h *CreditNoteHandlers) ListForInvoice(w http.ResponseWriter, r *http.Request) {
	invoiceID := chi.URLParam(r, "id")
	notes, err := h.Services.Notes.GetNotesForInvoice(r.Context(), invoiceID)
	if err != nil {
		if errors.Is(err, domain.ErrInvoiceNotFound) {
			http.Error(w, "Invoice not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to list notes", http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []*service.CreditNoteRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(notes)
}
