package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/integration"
	gstn "transport-app/internal/integration/gstn"
	invoiceapp "transport-app/internal/invoice/application"
	invoiceagg "transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/middleware"
	pdfgen "transport-app/internal/pdf"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	"transport-app/internal/shared/gstin"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
	"transport-app/internal/validate"
)

// InvoiceHandlers handles invoice management.
type InvoiceHandlers struct {
	*App
	getUC      *invoiceapp.GetInvoiceUseCase
	listUC     *invoiceapp.ListInvoicesUseCase
	generateUC *invoiceapp.GenerateInvoiceUseCase
	docQ       *invoiceapp.InvoiceDocumentQueries
	lineItems  *invoiceapp.LineItemService
	irnUC      *invoiceapp.GenerateIRNUseCase
}

func (h *InvoiceHandlers) init() {
	if h.Services == nil {
		h.Services = service.NewServices(
			sqlite.NewRepository(h.DB), h.Config, slog.Default(), nil)
	}
	if h.getUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()

		h.getUC = invoiceapp.NewGetInvoiceUseCase(uowImpl)
		h.listUC = invoiceapp.NewListInvoicesUseCase(uowImpl)
		h.generateUC = invoiceapp.NewGenerateInvoiceUseCase(uowImpl, idGenImpl, clockImpl)
	}
	if h.docQ == nil {
		h.docQ = invoiceapp.NewInvoiceDocumentQueries(h.DB)
		h.lineItems = invoiceapp.NewLineItemService(h.DB)
		h.irnUC = invoiceapp.NewGenerateIRNUseCase(h.DB)
	}
}

func (h *InvoiceHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/{id}/pdf", h.DownloadPDF)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/number/{number}", h.ViewByNumber)

	// Line items editor & GST e-invoicing routes (Spec 07 §4.1)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/{id}/line-items", h.LineItemsEditor)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/line-items", h.AddLineItem)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/line-items/{lineId}/edit", h.EditLineItem)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/line-items/{lineId}/delete", h.DeleteLineItem)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/generate-irn", h.GenerateIRN)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/{id}/irn", h.GetIRNFragment)

	// GST credit/debit notes (migration 00098): post-issuance corrections.
	// Issued invoices are immutable — reductions via credit note, increases
	// via debit note. Same permission tier as invoice mutations.
	notes := &CreditNoteHandlers{App: h.App}
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/credit-note", notes.CreateCreditNote)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "update")).Post("/{id}/debit-note", notes.CreateDebitNote)
	r.With(middleware.ResourcePermission(h.AuthSrv, "invoices", "read")).Get("/{id}/notes", notes.ListForInvoice)
}

func (h *InvoiceHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	res, err := h.listUC.Execute(r.Context(), invoiceapp.ListInvoicesQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     pp.Page,
		Limit:    pp.Limit,
		Search:   pp.Query,
		Status:   pp.Status,
		DateFrom: pp.DateFrom,
		DateTo:   pp.DateTo,
	})
	if err != nil {
		http.Error(w, "Failed to list invoices", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, res.Total, "/invoices")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, r, "invoice_list_table.html", map[string]interface{}{
			"Invoices":     res.Invoices,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
		})
		return
	}

	h.renderPage(w, r, "invoice_list.html", PageData{
		Title: "Invoices",
		User:  session,
		Extra: map[string]interface{}{"Invoices": res.Invoices, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFilterError": pp.DateFilterError, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.invoiceKPIs(r.Context())},
	})
}

func (h *InvoiceHandlers) View(w http.ResponseWriter, r *http.Request) {
	h.init()
	idParam := chi.URLParam(r, "id")
	invoice, err := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(idParam),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		slog.Error("DBG view execute", "id", idParam, "err", err)
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}

	session, _ := h.getUserFromContext(r)

	h.renderPage(w, r, "invoice_view.html", PageData{
		Title: "View Invoice",
		User:  session,
		Extra: h.invoiceViewExtra(r, idParam, invoice),
	})
}

func (h *InvoiceHandlers) ViewByNumber(w http.ResponseWriter, r *http.Request) {
	h.init()
	// Fallback to legacy get since query by number is a read operation on db
	invoiceLegacy, err := h.Services.Invoices.GetInvoiceByNumber(r.Context(), chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}
	// The legacy InvoiceWithJoins read model carries no IRN/e-invoice fields,
	// so re-fetch the full DTO by the resolved ID — invoice_view.html (and its
	// irn_qr partial) need them.
	idParam := string(invoiceLegacy.ID)
	invoice, err := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(idParam),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}
	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "invoice_view.html", PageData{
		Title: "View Invoice",
		User:  session,
		Extra: h.invoiceViewExtra(r, idParam, invoice),
	})
}

// invoiceViewExtra assembles the invoice view template payload shared by
// View and ViewByNumber: payments, balance, issued credit/debit notes and
// the computed Locked flag. An invoice is locked once it has an IRN or any
// recorded payment (immutability guards reject delete/edit with 409), so the
// UI hides destructive affordances and surfaces notes as the only correction
// path.
func (h *InvoiceHandlers) invoiceViewExtra(r *http.Request, invoiceID string, invoice invoiceapp.InvoiceResponseDTO) map[string]interface{} {
	payments, errPay := h.Services.Invoices.GetPaymentsForInvoice(r.Context(), domain.InvoiceID(invoiceID))
	if errPay != nil {
		slog.Error("Failed to load payments for invoice", "invoice_id", invoiceID, "error", errPay)
		payments = nil
	}
	balance, errBal := h.Services.Invoices.GetBalance(r.Context(), domain.InvoiceID(invoiceID))
	if errBal != nil {
		slog.Error("Failed to calculate balance for invoice", "invoice_id", invoiceID, "error", errBal)
		balance = float64(0)
	}
	locked := invoice.IRN != "" || len(payments) > 0

	var notes []*service.CreditNoteRecord
	if h.Services.Notes != nil {
		var errNotes error
		notes, errNotes = h.Services.Notes.GetNotesForInvoice(r.Context(), invoiceID)
		if errNotes != nil {
			slog.Error("Failed to load credit/debit notes for invoice", "invoice_id", invoiceID, "error", errNotes)
			notes = nil
		}
	}

	custPhone := ""
	cust, errCust := h.Services.Customers.GetCustomer(r.Context(), domain.CustomerID(invoice.CustomerID))
	if errCust == nil {
		custPhone = invoiceapp.CleanPhoneForWhatsApp(cust.Phone)
	}

	dueDateStr := ""
	tenantID := shared.TenantIDFromContext(r.Context())
	if raw, err := h.docQ.DueDate(r.Context(), invoiceapp.InvoiceDocumentKey{InvoiceID: invoiceID, TenantID: tenantID}); err == nil && raw != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
			dueDateStr = t.Format("02 Jan 2006")
		} else if len(raw) >= 10 {
			dueDateStr = raw[:10]
		}
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	payURL := fmt.Sprintf("%s://%s/pay/%s", scheme, host, invoiceID)
	if host == "" {
		payURL = fmt.Sprintf("/pay/%s", invoiceID)
	}

	waText := fmt.Sprintf(
		"Dear %s, please find details for Invoice #%s.\n\n"+
			"• Total Amount: ₹%.2f\n"+
			"• Outstanding Balance: ₹%.2f\n",
		invoice.CustomerName, invoice.InvoiceNumber, invoice.Total, balance,
	)
	if dueDateStr != "" {
		waText += fmt.Sprintf("• Due Date: %s\n", dueDateStr)
	}
	waText += fmt.Sprintf("\nPay securely online: %s\nThank you for your business!", payURL)

	waShareURL := fmt.Sprintf("https://wa.me/%s?text=%s", custPhone, url.QueryEscape(waText))

	prof := h.loadCompanyProfile(r.Context())

	return map[string]interface{}{
		"Invoice":             invoice,
		"Payments":            payments,
		"Balance":             balance,
		"Locked":              locked,
		"Notes":               notes,
		"CustomerPhone":       custPhone,
		"PublicPayURL":        payURL,
		"PublicPayPath":       fmt.Sprintf("/pay/%s", invoiceID),
		"WhatsAppShareURL":    waShareURL,
		"WhatsAppInvoiceText": waText,
		"Company":             prof,
		"TaxTier":             prof.TaxTier(),
		"LegalTitle":          prof.LegalTitle(),
	}
}

// invoiceGuardError writes the HTTP response for a service-layer invoice
// immutability guard failure and reports whether it handled the error.
func invoiceGuardError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, domain.ErrInvoiceNotFound):
		http.Error(w, "Invoice not found", http.StatusNotFound)
	case errors.Is(err, domain.ErrInvoiceEInvoiced), errors.Is(err, domain.ErrInvoiceHasPayments):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		return false
	}
	return true
}

func (h *InvoiceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Services.Invoices.DeleteInvoice(r.Context(), domain.InvoiceID(chi.URLParam(r, "id"))); err != nil {
		if invoiceGuardError(w, err) {
			return
		}
		http.Error(w, "Failed to delete invoice", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/invoices", http.StatusSeeOther)
}

func (h *InvoiceHandlers) DownloadPDF(w http.ResponseWriter, r *http.Request) {
	h.init()
	idParam := chi.URLParam(r, "id")
	invDTO, err := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(idParam),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		slog.Error("DownloadPDF: invoice query failed", "invoice_id", idParam, "error", err)
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}

	balance := 0.0
	if h.Services != nil && h.Services.Invoices != nil {
		var errBal error
		balance, errBal = h.Services.Invoices.GetBalance(r.Context(), domain.InvoiceID(idParam))
		if errBal != nil {
			slog.Error("Failed to calculate balance for PDF", "invoice_id", idParam, "error", errBal)
			balance = 0
		}
	}
	paidAmount := invDTO.Total - balance
	if paidAmount < 0 {
		paidAmount = 0
	}

	data, err := h.buildInvoicePDFData(r.Context(), &invDTO, paidAmount, balance)
	if err != nil {
		slog.Error("Failed to compose PDF data", "invoice_id", idParam, "error", err)
		http.Error(w, "Failed to generate PDF", http.StatusInternalServerError)
		return
	}

	pdfBytes, err := pdfgen.GenerateInvoicePDF(*data)
	if err != nil {
		slog.Error("PDF generation failed", "invoice_id", idParam, "error", err)
		http.Error(w, "Failed to generate PDF", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	safeNum := sanitizeHeaderFilename(invDTO.InvoiceNumber)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, safeNum))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// buildInvoicePDFData composes the complete render input for the invoice
// PDF from tenant settings, customer record, line items and payments.
// Missing optional data degrades gracefully (blank labels), never faked.
func (h *InvoiceHandlers) buildInvoicePDFData(ctx context.Context,
	invDTO *invoiceapp.InvoiceResponseDTO, paidAmount, balance float64,
) (*pdfgen.InvoicePDFData, error) {
	prof := h.loadCompanyProfile(ctx)
	custName, custGSTIN, custAddr := h.loadCustomerBillTo(ctx, invDTO.CustomerID)
	items := h.loadLineItems(ctx, invDTO.ID)

	custState := ""
	if len(custGSTIN) >= 2 {
		custState = custGSTIN[:2]
	}
	intra := custState == "" || custState == prof.StateCode

	var sumTaxable, sumCGST, sumSGST, sumIGST float64
	gstData := false
	lines := make([]pdfgen.PDFLineItem, 0, len(items))
	for _, it := range items {
		sumTaxable += it.TaxableValue
		sumCGST += it.CGSTAmount
		sumSGST += it.SGSTAmount
		sumIGST += it.IGSTAmount
		if it.HSNSACCode != "" || it.CGSTRate > 0 || it.SGSTRate > 0 || it.IGSTRate > 0 {
			gstData = true
		}
		qty := strconv.FormatFloat(it.Quantity, 'f', -1, 64) + " " + it.Unit
		lines = append(lines, pdfgen.PDFLineItem{
			Description:  it.Description,
			HSNSAC:       it.HSNSACCode,
			Quantity:     qty,
			Rate:         it.Rate,
			TaxableValue: it.TaxableValue,
			CGST:         it.CGSTAmount,
			SGST:         it.SGSTAmount,
			IGST:         it.IGSTAmount,
			Total:        it.Total,
		})
	}

	cgst, sgst, igst := sumCGST, sumSGST, sumIGST
	taxableTotal := sumTaxable
	if !gstData {
		// No itemized tax data — fall back to header-level columns.
		cgst, sgst, igst = invDTO.CGST, invDTO.SGST, invDTO.IGST
		if cgst == 0 && sgst == 0 && igst == 0 {
			if intra {
				cgst, sgst = invDTO.Tax/2, invDTO.Tax/2
			} else {
				igst = invDTO.Tax
			}
		}
		taxableTotal = invDTO.Subtotal
	}

	placeOfSupply := custState
	if placeOfSupply == "" {
		placeOfSupply = prof.StateCode
	}
	if prof.GSTNumber != "" && !validGSTINFormat(prof.GSTNumber) {
		slog.Warn("supplier GSTIN failed format check; not rendering in PDF",
			"gstin", prof.GSTNumber, "tenant", shared.TenantIDFromContext(ctx))
		prof.GSTNumber = ""
	}
	if prof.PanNumber != "" && !validPANFormat(prof.PanNumber) {
		slog.Warn("supplier PAN failed format check; not rendering in PDF",
			"pan", prof.PanNumber, "tenant", shared.TenantIDFromContext(ctx))
		prof.PanNumber = ""
	}
	if custGSTIN != "" && !validGSTINFormat(custGSTIN) {
		slog.Warn("customer GSTIN failed format check; not rendering in PDF",
			"gstin", custGSTIN, "tenant", shared.TenantIDFromContext(ctx))
		custGSTIN = ""
	}

	var payRows []pdfgen.PDFPaymentRow
	if h.Services != nil && h.Services.Invoices != nil {
		payments, err := h.Services.Invoices.GetPaymentsForInvoice(ctx, domain.InvoiceID(invDTO.ID))
		if err != nil {
			slog.Warn("payments lookup failed for invoice PDF", "invoice_id", invDTO.ID, "error", err)
		}
		for _, p := range payments {
			ref := ""
			if p.Reference != nil {
				ref = *p.Reference
			}
			payRows = append(payRows, pdfgen.PDFPaymentRow{
				Date:   p.PaymentDate.Format("02 Jan 2006"),
				Method: string(p.Method),
				Ref:    ref,
				Amount: p.Amount,
			})
		}
	}

	dueDate, ewbNumber := h.loadInvoiceExtras(ctx, invDTO.ID)

	roundOff := 0.0
	if gstData {
		expected := invDTO.Subtotal - invDTO.Discount + cgst + sgst + igst
		roundOff = invDTO.Total - expected
		if roundOff > -0.005 && roundOff < 0.005 {
			roundOff = 0
		}
	}

	return &pdfgen.InvoicePDFData{
		Company: pdfgen.PDFParty{
			Name:      prof.Name,
			Address:   prof.Address,
			GSTIN:     prof.GSTNumber,
			PAN:       prof.PanNumber,
			StateCode: prof.StateCode,
			Phone:     prof.Phone,
			Email:     prof.Email,
		},
		LegalTitle: prof.LegalTitle(),
		Customer: pdfgen.PDFParty{
			Name:      custName,
			Address:   custAddr,
			GSTIN:     custGSTIN,
			StateCode: custState,
		},
		InvoiceNumber: invDTO.InvoiceNumber,
		PaymentStatus: invDTO.PaymentStatus,
		InvoiceDate:   invDTO.CreatedAt.Format("02 Jan 2006"),
		DueDate:       dueDate,
		PlaceOfSupply: placeOfSupply,
		Subtotal:      invDTO.Subtotal,
		Tax:           invDTO.Tax,
		Discount:      invDTO.Discount,
		Total:         invDTO.Total,
		Paid:          paidAmount,
		Balance:       balance,
		CGST:          cgst,
		SGST:          sgst,
		IGST:          igst,
		GSTBreakdown:  gstData,
		IntraState:    intra,
		Items:         lines,
		TaxableTotal:  taxableTotal,
		RoundOff:      roundOff,
		Payments:      payRows,
		IRN:           invDTO.IRN,
		IRNAckNo:      invDTO.IRNAckNo,
		IRNAckDate:    invDTO.IRNAckDate,
		SignedQR:      pdfgen.QRImageBytes(invDTO.SignedQR),
		EWBNumber:     ewbNumber,
	}, nil
}

// loadInvoiceExtras formats due_date + ewb_number for the invoice PDF.
func (h *InvoiceHandlers) loadInvoiceExtras(ctx context.Context, invoiceID string) (dueDate, ewb string) {
	tenantID := shared.TenantIDFromContext(ctx)
	dueRaw, ewbNumber, err := h.docQ.Extras(ctx, invoiceapp.InvoiceDocumentKey{InvoiceID: invoiceID, TenantID: tenantID})
	if err != nil {
		slog.Warn("invoice extras lookup failed", "invoice_id", invoiceID, "error", err)
		return "", ""
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", dueRaw); err == nil && dueRaw != "" {
		return t.Format("02 Jan 2006"), ewbNumber
	}
	if dueRaw != "" {
		return dueRaw[:10], ewbNumber // ISO date prefix fallback
	}
	return "", ewbNumber
}

// loadCompanyProfile reads the tenant's seller profile for the invoice
// document (tenant profile first, bootstrap singleton fallback).
func (h *InvoiceHandlers) loadCompanyProfile(ctx context.Context) invoiceapp.CompanyProfile {
	return h.docQ.CompanyProfile(ctx, shared.TenantIDFromContext(ctx))
}

// loadCustomerBillTo reads the buyer identity for the Bill To block.
func (h *InvoiceHandlers) loadCustomerBillTo(ctx context.Context, customerID string) (name, gstNumber, address string) {
	name, gstNumber, address, err := h.docQ.CustomerBillTo(ctx, shared.TenantIDFromContext(ctx), customerID)
	if err != nil {
		slog.Warn("customer lookup failed for invoice PDF", "customer_id", customerID, "error", err)
	}
	return name, gstNumber, address
}

// loadLineItems reads the itemized rows with per-line GST data.
func (h *InvoiceHandlers) loadLineItems(ctx context.Context, invoiceID string) []LineItemRecord {
	items, err := h.docQ.InvoiceLineItems(ctx, invoiceapp.InvoiceDocumentKey{InvoiceID: invoiceID, TenantID: shared.TenantIDFromContext(ctx)})
	if err != nil {
		slog.Warn("line items query failed for invoice PDF", "invoice_id", invoiceID, "error", err)
		return nil
	}
	var records []LineItemRecord
	for _, it := range items {
		records = append(records, LineItemRecord{
			ID: it.ID, InvoiceID: it.InvoiceID, HSNSACCode: it.HSNSACCode,
			Description: it.Description, Unit: it.Unit, Quantity: it.Quantity,
			Rate: it.Rate, TaxableValue: it.TaxableValue,
			CGSTRate: it.CGSTRate, SGSTRate: it.SGSTRate, IGSTRate: it.IGSTRate,
			CGSTAmount: it.CGSTAmount, SGSTAmount: it.SGSTAmount, IGSTAmount: it.IGSTAmount,
			Total: it.Total,
		})
	}
	return records
}

// loadHSNRecords lists the active HSN/SAC master rows for the editor datalist.
func (h *InvoiceHandlers) loadHSNRecords(ctx context.Context) []HSNSACRecord {
	records, err := h.docQ.ActiveHSNRecords(ctx)
	if err != nil {
		return nil
	}
	var out []HSNSACRecord
	for _, rec := range records {
		out = append(out, HSNSACRecord{Code: rec.Code, Description: rec.Description, Type: rec.Type, Rate: rec.Rate})
	}
	return out
}

func sanitizeHeaderFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if res == "" {
		return "invoice"
	}
	return res
}

// LineItemRecord represents an item row for rendering in the editor.
type LineItemRecord struct {
	ID           string  `json:"id"`
	InvoiceID    string  `json:"invoice_id"`
	HSNSACCode   string  `json:"hsn_sac_code"`
	Description  string  `json:"description"`
	Unit         string  `json:"unit"`
	Quantity     float64 `json:"quantity"`
	Rate         float64 `json:"rate"`
	TaxableValue float64 `json:"taxable_value"`
	CGSTRate     float64 `json:"cgst_rate"`
	SGSTRate     float64 `json:"sgst_rate"`
	IGSTRate     float64 `json:"igst_rate"`
	CGSTAmount   float64 `json:"cgst_amount"`
	SGSTAmount   float64 `json:"sgst_amount"`
	IGSTAmount   float64 `json:"igst_amount"`
	Total        float64 `json:"total"`
}

// TaxSplitSummary holds tax aggregation for templates.
type TaxSplitSummary struct {
	TaxableTotal float64
	IsIntraState bool
	Cgst         float64
	Sgst         float64
	Igst         float64
	Total        float64
}

// HSNSACRecord holds master lookup data.
type HSNSACRecord struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Rate        float64 `json:"rate"`
}

func (h *InvoiceHandlers) LineItemsEditor(w http.ResponseWriter, r *http.Request) {
	h.init()
	idParam := chi.URLParam(r, "id")
	session, _ := h.getUserFromContext(r)

	invDTO, err := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(idParam),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}

	// Customer + company tax identity (shared loaders with the PDF path).
	custName, custGST, _ := h.loadCustomerBillTo(r.Context(), invDTO.CustomerID)
	prof := h.loadCompanyProfile(r.Context())
	supplierState := prof.StateCode
	custState := ""
	if len(custGST) >= 2 {
		custState = custGST[:2]
	}
	isIntra := custState == "" || custState == supplierState

	items := h.loadLineItems(r.Context(), idParam)

	hsnList := h.loadHSNRecords(r.Context())
	// Calculate totals
	var sumTaxable, sumCGST, sumSGST, sumIGST, sumTotal float64
	for _, it := range items {
		sumTaxable += it.TaxableValue
		sumCGST += it.CGSTAmount
		sumSGST += it.SGSTAmount
		sumIGST += it.IGSTAmount
		sumTotal += it.Total
	}
	if len(items) == 0 {
		sumTaxable = invDTO.Subtotal
		sumTotal = invDTO.Total
		if isIntra {
			sumCGST = invDTO.Tax / 2
			sumSGST = invDTO.Tax / 2
		} else {
			sumIGST = invDTO.Tax
		}
	}

	taxSplit := TaxSplitSummary{
		TaxableTotal: sumTaxable,
		IsIntraState: isIntra,
		Cgst:         sumCGST,
		Sgst:         sumSGST,
		Igst:         sumIGST,
		Total:        sumTotal,
	}

	locked := invDTO.IRN != "" || invDTO.PaymentStatus != "pending"
	lockReason := invDTO.PaymentStatus
	if invDTO.IRN != "" {
		lockReason = "e-invoiced"
	}

	h.renderPage(w, r, "invoice_line_items.html", PageData{
		Title: fmt.Sprintf("Line Items - %s", invDTO.InvoiceNumber),
		User:  session,
		Extra: map[string]interface{}{
			"Invoice":      invDTO,
			"Customer":     map[string]string{"Name": custName, "GST": custGST, "State": custState},
			"IsIntraState": isIntra,
			"LineItems":    items,
			"HSNCodes":     hsnList,
			"TaxSplit":     taxSplit,
			"Locked":       locked,
			"LockReason":   lockReason,
		},
	})
}

func (h *InvoiceHandlers) AddLineItem(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	if err := h.Services.Invoices.EnsureLineItemsEditable(r.Context(), domain.InvoiceID(invoiceID)); err != nil {
		if invoiceGuardError(w, err) {
			return
		}
		http.Error(w, "Failed to verify invoice is editable", http.StatusInternalServerError)
		return
	}

	hsnCode := strings.TrimSpace(r.FormValue("hsn_sac_code"))
	description := strings.TrimSpace(r.FormValue("description"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	if unit == "" {
		unit = "NOS"
	}
	qty, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
	if qty <= 0 {
		qty = 1
	}
	rate, _ := strconv.ParseFloat(r.FormValue("rate"), 64)
	if rate <= 0 {
		rate = 1000.0
	}

	err := h.lineItems.AddLineItem(r.Context(), invoiceapp.AddLineItemCommand{
		TenantID:    tenantID,
		InvoiceID:   invoiceID,
		HSNSACCode:  hsnCode,
		Description: description,
		Unit:        unit,
		Quantity:    qty,
		Rate:        rate,
	})
	if err != nil {
		if errors.Is(err, invoiceapp.ErrUnknownHSN) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if writeLineItemFailure(w, err) {
			return
		}
		slog.Error("invoice line GST resolution failed", "invoice_id", invoiceID, "error", err)
		http.Error(w, "Failed to determine tax configuration", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%s/line-items", invoiceID), http.StatusSeeOther)
}

func (h *InvoiceHandlers) EditLineItem(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	lineID := chi.URLParam(r, "lineId")
	tenantID := shared.TenantIDFromContext(r.Context())

	if err := h.Services.Invoices.EnsureLineItemsEditable(r.Context(), domain.InvoiceID(invoiceID)); err != nil {
		if invoiceGuardError(w, err) {
			return
		}
		http.Error(w, "Failed to verify invoice is editable", http.StatusInternalServerError)
		return
	}

	hsnCode := strings.TrimSpace(r.FormValue("hsn_sac_code"))
	description := strings.TrimSpace(r.FormValue("description"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	qty, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
	if qty <= 0 {
		qty = 1
	}
	rate, _ := strconv.ParseFloat(r.FormValue("rate"), 64)

	err := h.lineItems.EditLineItem(r.Context(), invoiceapp.EditLineItemCommand{
		TenantID:    tenantID,
		InvoiceID:   invoiceID,
		LineID:      lineID,
		HSNSACCode:  hsnCode,
		Description: description,
		Unit:        unit,
		Quantity:    qty,
		Rate:        rate,
	})
	if err != nil {
		if errors.Is(err, invoiceapp.ErrUnknownHSN) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if writeLineItemFailure(w, err) {
			return
		}
		slog.Error("invoice line GST resolution failed", "invoice_id", invoiceID, "error", err)
		http.Error(w, "Failed to determine tax configuration", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%s/line-items", invoiceID), http.StatusSeeOther)
}

func (h *InvoiceHandlers) DeleteLineItem(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	lineID := chi.URLParam(r, "lineId")
	tenantID := shared.TenantIDFromContext(r.Context())

	// GST immutability: line items of an e-invoiced or paid invoice are locked.
	if err := h.Services.Invoices.EnsureLineItemsEditable(r.Context(), domain.InvoiceID(invoiceID)); err != nil {
		if invoiceGuardError(w, err) {
			return
		}
		http.Error(w, "Failed to verify invoice is editable", http.StatusInternalServerError)
		return
	}

	err := h.lineItems.DeleteLineItem(r.Context(), invoiceapp.DeleteLineItemCommand{
		TenantID:  tenantID,
		InvoiceID: invoiceID,
		LineID:    lineID,
	})
	if err != nil {
		if writeLineItemFailure(w, err) {
			return
		}
		http.Error(w, "Failed to delete line item", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%s/line-items", invoiceID), http.StatusSeeOther)
}

// writeLineItemFailure maps a line-item write stage failure to the legacy
// HTTP status/message pair and reports whether it handled the error.
func writeLineItemFailure(w http.ResponseWriter, err error) bool {
	var writeErr *invoiceapp.LineItemWriteError
	if !errors.As(err, &writeErr) {
		return false
	}
	switch writeErr.Stage {
	case invoiceapp.LineItemStageTxBegin:
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
	case invoiceapp.LineItemStageInsert:
		http.Error(w, fmt.Sprintf("Failed to add line item: %v", writeErr.Err), http.StatusInternalServerError)
	case invoiceapp.LineItemStageUpdate:
		http.Error(w, fmt.Sprintf("Failed to update line item: %v", writeErr.Err), http.StatusInternalServerError)
	case invoiceapp.LineItemStageDelete:
		http.Error(w, "Failed to delete line item", http.StatusInternalServerError)
	case invoiceapp.LineItemStageRecalculate:
		http.Error(w, "Failed to recalculate invoice totals", http.StatusInternalServerError)
	case invoiceapp.LineItemStageCommit:
		http.Error(w, fmt.Sprintf("Failed to save line item: %v", writeErr.Err), http.StatusInternalServerError)
	default:
		http.Error(w, "Failed to save line item", http.StatusInternalServerError)
	}
	return true
}

func (h *InvoiceHandlers) GenerateIRN(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	// Use factory to respect INTEGRATION_GSTN_USE_MOCK && APIKey (Spec 21 §5)
	integCfg := integration.LoadConfig()
	integCfg.GSTN.Enabled = true
	client := gstn.NewClient(integCfg.GSTN)

	res, err := h.irnUC.Execute(r.Context(), invoiceapp.GenerateIRNCommand{
		TenantID:  tenantID,
		InvoiceID: invoiceID,
		Generator: client,
	})
	if err != nil {
		var loadErr *invoiceapp.IRNQueryError
		var storeErr *invoiceapp.IRNStoreError
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, "Invoice not found", http.StatusNotFound)
		case errors.Is(err, invoiceapp.ErrIRNPaymentStatus):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, invoiceapp.ErrIRNAlreadyGenerated):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.As(err, &loadErr):
			http.Error(w, "Database error", http.StatusInternalServerError)
		case errors.As(err, &storeErr):
			http.Error(w, fmt.Sprintf("Failed to save IRN: %v", storeErr.Err), http.StatusInternalServerError)
		default:
			http.Error(w, fmt.Sprintf("GSTN IRN generation failed: %v", err), http.StatusBadGateway)
		}
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(res)
		return
	}

	invDTO, _ := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(invoiceID),
		TenantID: tenantID,
	})

	if isDatastarRequest(r) {
		w.Header().Set("HX-Trigger", `{"showToast":{"tone":"success","msg":"IRN generated"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.Templates.ExecuteTemplate(w, "irn_qr.html", map[string]interface{}{"Invoice": invDTO})
		// htmx 4 partials: hero badge + locked pill morph in same response
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#invoice-einvoiced-badge" hx-swap="outerMorph"><span id="invoice-einvoiced-badge" class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-bold uppercase tracking-wide bg-emerald-100 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-300"><svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg> E-Invoiced</span></template>`)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#invoice-locked-pill" hx-swap="outerMorph"><span id="invoice-locked-pill" class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-surface-container-low border border-border-subtle text-xs font-semibold text-text-muted" title="This invoice has an IRN or recorded payments, so it is immutable. Corrections are made via credit/debit notes only."><svg class="w-3.5 h-3.5 shrink-0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/></svg> Locked — corrections via credit/debit notes</span></template>`)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#invoice-manage-link" hx-swap="outerMorph"><span id="invoice-manage-link" class="hidden"></span></template>`)
		return
	}

	h.renderFragment(w, r, "irn_qr.html", map[string]interface{}{
		"Invoice": invDTO,
	})
}

func (h *InvoiceHandlers) GetIRNFragment(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	invDTO, err := h.getUC.Execute(r.Context(), invoiceapp.GetInvoiceQuery{
		ID:       invoiceagg.InvoiceID(invoiceID),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Invoice not found", http.StatusNotFound)
		return
	}
	h.renderFragment(w, r, "irn_qr.html", map[string]interface{}{
		"Invoice": invDTO,
	})
}

func (h *InvoiceHandlers) SearchHSNSAC(w http.ResponseWriter, r *http.Request) {
	h.init()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	records, err := h.docQ.SearchHSNRecords(r.Context(), q)
	if err != nil {
		http.Error(w, "Failed to query HSN master", http.StatusInternalServerError)
		return
	}

	var results []HSNSACRecord
	for _, rec := range records {
		results = append(results, HSNSACRecord{Code: rec.Code, Description: rec.Description, Type: rec.Type, Rate: rec.Rate})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// validGSTINFormat delegates to the shared GSTIN structural validator.
func validGSTINFormat(g string) bool {
	return gstin.Valid(g)
}

// validPANFormat delegates to the shared PAN format validator.
func validPANFormat(p string) bool {
	return validate.ValidPAN(p)
}
