package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
)

// InvoiceHandlers handles invoice management.
type InvoiceHandlers struct {
	*App
	getUC      *invoiceapp.GetInvoiceUseCase
	listUC     *invoiceapp.ListInvoicesUseCase
	generateUC *invoiceapp.GenerateInvoiceUseCase
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
		h.renderFragment(w, "invoice_list_table.html", map[string]interface{}{
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
		Extra: map[string]interface{}{"Invoices": res.Invoices, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.invoiceKPIs(r.Context())},
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

	return map[string]interface{}{
		"Invoice":  invoice,
		"Payments": payments,
		"Balance":  balance,
		"Locked":   locked,
		"Notes":    notes,
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
			StateCode: prof.StateCode,
			Phone:     prof.Phone,
			Email:     prof.Email,
		},
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

// loadInvoiceExtras reads due_date + ewb_number straight from the
// invoices table (not surfaced on the read-model DTO).
func (h *InvoiceHandlers) loadInvoiceExtras(ctx context.Context, invoiceID string) (dueDate, ewb string) {
	var d sql.NullString
	var e sql.NullString
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(due_date, ''), COALESCE(ewb_number, '')
		 FROM invoices WHERE id = ?`, invoiceID).Scan(&d, &e); err != nil {
		slog.Warn("invoice extras lookup failed", "invoice_id", invoiceID, "error", err)
		return "", ""
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", d.String); err == nil && d.String != "" {
		return t.Format("02 Jan 2006"), e.String
	}
	if d.String != "" {
		return d.String[:10], e.String // ISO date prefix fallback
	}
	return "", e.String
}

// companyProfile mirrors the tenant's company_settings row.
type companyProfile struct {
	Name      string
	Address   string
	GSTNumber string
	StateCode string
	Phone     string
	Email     string
}

// loadCompanyProfile reads company_settings for the seller block.
// StateCode falls back to "27" (Maharashtra) matching the GST editor;
// Name is empty when unset — the renderer prints a placeholder rather
// than any hardcoded company name.
func (h *InvoiceHandlers) loadCompanyProfile(ctx context.Context) companyProfile {
	p := companyProfile{StateCode: "27"}
	row := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(company_name, ''), COALESCE(address, ''),
		       COALESCE(gst_number, ''), COALESCE(state_code, '27'),
		       COALESCE(phone, ''), COALESCE(email, '')
		FROM company_settings WHERE id = 1`)
	if err := row.Scan(&p.Name, &p.Address, &p.GSTNumber, &p.StateCode,
		&p.Phone, &p.Email); err != nil {
		slog.Warn("company_settings unavailable for invoice PDF", "error", err)
	}
	if p.StateCode == "" {
		p.StateCode = "27"
	}
	return p
}

// loadCustomerBillTo reads the buyer identity for the Bill To block.
func (h *InvoiceHandlers) loadCustomerBillTo(ctx context.Context, customerID string) (name, gstin, address string) {
	err := h.DB.QueryRowContext(ctx, `
		SELECT name, COALESCE(gst, ''), COALESCE(address, '')
		FROM customers WHERE id = ?`, customerID).
		Scan(&name, &gstin, &address)
	if err != nil {
		slog.Warn("customer lookup failed for invoice PDF", "customer_id", customerID, "error", err)
	}
	return name, gstin, address
}

// loadLineItems reads the itemized rows with per-line GST data.
func (h *InvoiceHandlers) loadLineItems(ctx context.Context, invoiceID string) []LineItemRecord {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT id, invoice_id, COALESCE(hsn_sac_code, ''), description, COALESCE(unit, 'NOS'),
		       quantity, COALESCE(rate, unit_price), COALESCE(taxable_value, amount),
		       COALESCE(cgst_rate, 0), COALESCE(sgst_rate, 0), COALESCE(igst_rate, 0),
		       COALESCE(cgst_amount, 0), COALESCE(sgst_amount, 0), COALESCE(igst_amount, 0),
		       COALESCE(total, amount)
		FROM invoice_line_items
		WHERE invoice_id = ?
		ORDER BY created_at ASC
	`, invoiceID)
	if err != nil {
		slog.Warn("line items query failed for invoice PDF", "invoice_id", invoiceID, "error", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	var items []LineItemRecord
	for rows.Next() {
		var it LineItemRecord
		if err := rows.Scan(
			&it.ID, &it.InvoiceID, &it.HSNSACCode, &it.Description, &it.Unit,
			&it.Quantity, &it.Rate, &it.TaxableValue,
			&it.CGSTRate, &it.SGSTRate, &it.IGSTRate,
			&it.CGSTAmount, &it.SGSTAmount, &it.IGSTAmount,
			&it.Total,
		); err == nil {
			items = append(items, it)
		}
	}
	return items
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

	// Fetch HSN master for datalist
	var hsnList []HSNSACRecord
	hRows, err := h.DB.QueryContext(r.Context(), `SELECT code, description, type, rate FROM hsn_sac_master WHERE active = 1 ORDER BY code ASC`)
	if err == nil {
		defer hRows.Close()
		for hRows.Next() {
			var hrec HSNSACRecord
			if err := hRows.Scan(&hrec.Code, &hrec.Description, &hrec.Type, &hrec.Rate); err == nil {
				hsnList = append(hsnList, hrec)
			}
		}
	}

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

	_, cgstRate, sgstRate, igstRate, err := h.resolveLineGST(r.Context(), h.DB, tenantID, invoiceID, hsnCode)
	if err != nil {
		if errors.Is(err, errUnknownHSN) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			slog.Error("invoice line GST resolution failed", "invoice_id", invoiceID, "error", err)
			http.Error(w, "Failed to determine tax configuration", http.StatusInternalServerError)
		}
		return
	}

	taxable := qty * rate
	var cgstAmt, sgstAmt, igstAmt float64
	if cgstRate > 0 || sgstRate > 0 {
		cgstAmt = taxable * (cgstRate / 100.0)
		sgstAmt = taxable * (sgstRate / 100.0)
	} else if igstRate > 0 {
		igstAmt = taxable * (igstRate / 100.0)
	}
	lineTotal := taxable + cgstAmt + sgstAmt + igstAmt

	lineID := uuid.NewString()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO invoice_line_items (
			id, tenant_id, invoice_id, line_type, description, quantity, unit_price, amount,
			hsn_sac_code, unit, rate, taxable_value, cgst_rate, sgst_rate, igst_rate,
			cgst_amount, sgst_amount, igst_amount, total, created_at
		) VALUES (?, ?, ?, 'freight', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, lineID, string(tenantID), invoiceID, description, qty, rate, taxable,
		hsnCode, unit, rate, taxable, cgstRate, sgstRate, igstRate,
		cgstAmt, sgstAmt, igstAmt, lineTotal)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add line item: %v", err), http.StatusInternalServerError)
		return
	}

	if !h.recalculateInvoiceTotalsTx(r.Context(), tx, tenantID, invoiceID) {
		http.Error(w, "Failed to recalculate invoice totals", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save line item: %v", err), http.StatusInternalServerError)
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

	_, cgstRate, sgstRate, igstRate, err := h.resolveLineGST(r.Context(), h.DB, tenantID, invoiceID, hsnCode)
	if err != nil {
		if errors.Is(err, errUnknownHSN) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			slog.Error("invoice line GST resolution failed", "invoice_id", invoiceID, "error", err)
			http.Error(w, "Failed to determine tax configuration", http.StatusInternalServerError)
		}
		return
	}

	taxable := qty * rate
	var cgstAmt, sgstAmt, igstAmt float64
	if cgstRate > 0 || sgstRate > 0 {
		cgstAmt = taxable * (cgstRate / 100.0)
		sgstAmt = taxable * (sgstRate / 100.0)
	} else if igstRate > 0 {
		igstAmt = taxable * (igstRate / 100.0)
	}
	lineTotal := taxable + cgstAmt + sgstAmt + igstAmt

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(r.Context(), `
		UPDATE invoice_line_items
		SET hsn_sac_code = ?, description = ?, unit = ?, quantity = ?, rate = ?, unit_price = ?,
		    taxable_value = ?, amount = ?, cgst_rate = ?, sgst_rate = ?, igst_rate = ?,
		    cgst_amount = ?, sgst_amount = ?, igst_amount = ?, total = ?
		WHERE id = ? AND invoice_id = ? AND tenant_id = ?
	`, hsnCode, description, unit, qty, rate, rate, taxable, taxable,
		cgstRate, sgstRate, igstRate, cgstAmt, sgstAmt, igstAmt, lineTotal,
		lineID, invoiceID, string(tenantID))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update line item: %v", err), http.StatusInternalServerError)
		return
	}

	if !h.recalculateInvoiceTotalsTx(r.Context(), tx, tenantID, invoiceID) {
		http.Error(w, "Failed to recalculate invoice totals", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save line item: %v", err), http.StatusInternalServerError)
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

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	_, err = tx.ExecContext(r.Context(), `DELETE FROM invoice_line_items WHERE id = ? AND invoice_id = ? AND tenant_id = ?`, lineID, invoiceID, string(tenantID))
	if err != nil {
		http.Error(w, "Failed to delete line item", http.StatusInternalServerError)
		return
	}

	if !h.recalculateInvoiceTotalsTx(r.Context(), tx, tenantID, invoiceID) {
		http.Error(w, "Failed to recalculate invoice totals", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save line item: %v", err), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/invoices/%s/line-items", invoiceID), http.StatusSeeOther)
}

// dbtx is satisfied by both *sql.DB and *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var errUnknownHSN = errors.New("unknown HSN/SAC code")

// resolveLineGST determines the GST rate and intra/inter-state split for a
// new or edited invoice line item. It FAILS CLOSED: an unknown HSN code or
// an unreadable tax configuration is an error — never a silent default that
// could misfile taxes on a legal invoice. Every lookup is tenant-scoped.
func (h *InvoiceHandlers) resolveLineGST(ctx context.Context, q dbtx, tenantID shared.TenantID, invoiceID, hsnCode string) (gstRate, cgstRate, sgstRate, igstRate float64, err error) {
	gstRate = 18.0 // default only when no HSN code supplied
	if hsnCode != "" {
		switch err := q.QueryRowContext(ctx, `SELECT rate FROM hsn_sac_master WHERE code = ?`, hsnCode).Scan(&gstRate); {
		case errors.Is(err, sql.ErrNoRows):
			return 0, 0, 0, 0, fmt.Errorf("%w %q", errUnknownHSN, hsnCode)
		case err != nil:
			return 0, 0, 0, 0, fmt.Errorf("lookup HSN rate: %w", err)
		}
	}

	var custGST string
	var compState sql.NullString
	err = q.QueryRowContext(ctx, `
		SELECT COALESCE(c.gst, ''), cs.state_code
		FROM invoices inv
		JOIN customers c ON inv.customer_id = c.id
		LEFT JOIN company_settings cs ON 1=1
		WHERE inv.id = ? AND inv.tenant_id = ?
	`, invoiceID, string(tenantID)).Scan(&custGST, &compState)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("lookup invoice tax context: %w", err)
	}

	supplierState := "27"
	if compState.Valid && compState.String != "" {
		supplierState = compState.String
	}
	custState := ""
	if len(custGST) >= 2 {
		custState = custGST[:2]
	}
	if custState == "" || custState == supplierState {
		cgstRate = gstRate / 2.0
		sgstRate = gstRate / 2.0
	} else {
		igstRate = gstRate
	}
	return gstRate, cgstRate, sgstRate, igstRate, nil
}

// recalculateInvoiceTotalsTx re-aggregates line items and updates the invoice
// header inside the caller's transaction. Every statement is tenant-scoped.
// Returns false when the recalculation failed (tx should be rolled back).
func (h *InvoiceHandlers) recalculateInvoiceTotalsTx(ctx context.Context, q dbtx, tenantID shared.TenantID, invoiceID string) bool {
	var sumTaxable, sumCGST, sumSGST, sumIGST, sumTotal float64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(taxable_value), 0), COALESCE(SUM(cgst_amount), 0),
		       COALESCE(SUM(sgst_amount), 0), COALESCE(SUM(igst_amount), 0),
		       COALESCE(SUM(total), 0)
		FROM invoice_line_items
		WHERE invoice_id = ? AND tenant_id = ?
	`, invoiceID, string(tenantID)).Scan(&sumTaxable, &sumCGST, &sumSGST, &sumIGST, &sumTotal)
	if err != nil {
		return false
	}

	totalTax := sumCGST + sumSGST + sumIGST
	_, err = q.ExecContext(ctx, `
		UPDATE invoices
		SET subtotal = ?, tax = ?, cgst = ?, sgst = ?, igst = ?, total = ?, updated_at = datetime('now')
		WHERE id = ? AND tenant_id = ?
	`, sumTaxable, totalTax, sumCGST, sumSGST, sumIGST, sumTotal, invoiceID, string(tenantID))
	return err == nil
}

func (h *InvoiceHandlers) GenerateIRN(w http.ResponseWriter, r *http.Request) {
	h.init()
	invoiceID := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())

	// 1. Fetch invoice and status
	var invNum, custID, invStatus, existingIRN, invDate sql.NullString
	var totalVal, cgstVal, sgstVal, igstVal float64
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT invoice_number, customer_id, status, irn, date(created_at), total, cgst, sgst, igst
		FROM invoices
		WHERE id = ?
	`, invoiceID).Scan(&invNum, &custID, &invStatus, &existingIRN, &invDate, &totalVal, &cgstVal, &sgstVal, &igstVal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Invoice not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 2. Block on paid / partially paid invoices per Spec 07 §5.3
	statusStr := strings.ToLower(invStatus.String)
	if statusStr == "paid" || statusStr == "partially_paid" {
		http.Error(w, "Cannot generate IRN for paid/partially paid invoices", http.StatusConflict)
		return
	}

	// 3. Block if IRN already generated
	if existingIRN.Valid && existingIRN.String != "" {
		http.Error(w, "IRN already generated for this invoice", http.StatusConflict)
		return
	}

	// 4. Fetch customer & company GSTIN
	var custGST string
	var compGST sql.NullString
	_ = h.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(c.gst, ''), cs.gst_number
		FROM customers c
		LEFT JOIN company_settings cs ON 1=1
		WHERE c.id = ?
	`, custID.String).Scan(&custGST, &compGST)

	supplierGST := "27AABCU9603R1ZX"
	if compGST.Valid && compGST.String != "" {
		supplierGST = compGST.String
	}

	// 5. Fetch line items
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT COALESCE(hsn_sac_code, '996511'), description, COALESCE(unit, 'NOS'),
		       quantity, COALESCE(rate, unit_price), COALESCE(taxable_value, amount),
		       COALESCE(cgst_rate, 0), COALESCE(sgst_rate, 0), COALESCE(igst_rate, 0),
		       COALESCE(cgst_amount, 0), COALESCE(sgst_amount, 0), COALESCE(igst_amount, 0),
		       COALESCE(total, amount)
		FROM invoice_line_items
		WHERE invoice_id = ?
	`, invoiceID)
	var lineViews []gstn.LineItemView
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var lv gstn.LineItemView
			if err := rows.Scan(
				&lv.HSNSACCode, &lv.Description, &lv.Unit,
				&lv.Quantity, &lv.Rate, &lv.TaxableValue,
				&lv.CGSTRate, &lv.SGSTRate, &lv.IGSTRate,
				&lv.CGSTAmount, &lv.SGSTAmount, &lv.IGSTAmount,
				&lv.Total,
			); err == nil {
				lineViews = append(lineViews, lv)
			}
		}
	}

	// Fallback single line item if none entered
	if len(lineViews) == 0 {
		lineViews = append(lineViews, gstn.LineItemView{
			HSNSACCode:   "996511",
			Description:  "Freight Services",
			Unit:         "NOS",
			Quantity:     1,
			Rate:         totalVal,
			TaxableValue: totalVal,
			Total:        totalVal,
		})
	}

	invView := gstn.InvoiceView{
		InvoiceID:      invoiceID,
		InvoiceNumber:  invNum.String,
		InvoiceDate:    invDate.String,
		SupplierGSTIN:  supplierGST,
		RecipientGSTIN: custGST,
		TotalValue:     totalVal,
		CGST:           cgstVal,
		SGST:           sgstVal,
		IGST:           igstVal,
		LineItems:      lineViews,
	}

	// Use factory to respect INTEGRATION_GSTN_USE_MOCK && APIKey (Spec 21 §5)
	integCfg := integration.LoadConfig()
	integCfg.GSTN.Enabled = true
	client := gstn.NewClient(integCfg.GSTN)
	res, err := client.GenerateIRN(r.Context(), invView)
	if err != nil {
		http.Error(w, fmt.Sprintf("GSTN IRN generation failed: %v", err), http.StatusBadGateway)
		return
	}

	// 6. Update invoices row
	_, err = h.DB.ExecContext(r.Context(), `
		UPDATE invoices
		SET irn = ?, irn_ack_no = ?, irn_ack_date = ?, signed_qr = ?, updated_at = datetime('now')
		WHERE id = ? AND tenant_id = ?
	`, res.IRN, res.AckNo, res.AckDate, res.SignedQR, invoiceID, string(tenantID))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save IRN: %v", err), http.StatusInternalServerError)
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

	h.renderFragment(w, "irn_qr.html", map[string]interface{}{
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
	h.renderFragment(w, "irn_qr.html", map[string]interface{}{
		"Invoice": invDTO,
	})
}

func (h *InvoiceHandlers) SearchHSNSAC(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	pattern := "%" + q + "%"
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT code, description, type, rate
		FROM hsn_sac_master
		WHERE active = 1 AND (code LIKE ? OR description LIKE ?)
		ORDER BY code ASC LIMIT 10
	`, pattern, pattern)
	if err != nil {
		http.Error(w, "Failed to query HSN master", http.StatusInternalServerError)
		return
	}
	defer func() { _ = rows.Close() }()

	var results []HSNSACRecord
	for rows.Next() {
		var rec HSNSACRecord
		if err := rows.Scan(&rec.Code, &rec.Description, &rec.Type, &rec.Rate); err == nil {
			results = append(results, rec)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// validGSTINFormat delegates to the shared GSTIN structural validator.
func validGSTINFormat(g string) bool {
	return gstin.Valid(g)
}
