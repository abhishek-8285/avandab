package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/domain/customer"
	"transport-app/internal/logging"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// UnbilledTripDTO represents a delivered/completed trip ready for consolidated invoicing.
type UnbilledTripDTO struct {
	ID               string     `json:"id"`
	TripNumber       string     `json:"trip_number"`
	BookingID        string     `json:"booking_id"`
	BookingNumber    string     `json:"booking_number"`
	RouteSource      string     `json:"route_source"`
	RouteDestination string     `json:"route_destination"`
	VehicleNumber    string     `json:"vehicle_number"`
	DriverName       string     `json:"driver_name"`
	DepartureTime    time.Time  `json:"departure_time"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	Freight          float64    `json:"freight"`
	Tolls            float64    `json:"tolls"`
	Detention        float64    `json:"detention"`
	Total            float64    `json:"total"`
}

// CreditAgingSummary holds the 4 overdue aging buckets.
type CreditAgingSummary struct {
	CurrentOr15      float64 `json:"current_or_15"`     // 0 - 15 days (including not yet due)
	Days16To30       float64 `json:"days_16_to_30"`     // 16 - 30 days overdue
	Days31To60       float64 `json:"days_31_to_60"`     // 31 - 60 days overdue
	Days60Plus       float64 `json:"days_60_plus"`      // 60+ days overdue
	TotalOverdue     float64 `json:"total_overdue"`     // sum of strictly overdue (> 0 days)
	TotalOutstanding float64 `json:"total_outstanding"` // all unpaid balance
}

// StatementTransaction represents a row in the combined ledger timeline.
type StatementTransaction struct {
	Date        time.Time `json:"date"`
	Type        string    `json:"type"` // "Invoice", "Payment", "Credit Note", "Debit Note"
	Reference   string    `json:"reference"`
	Description string    `json:"description"`
	Debit       float64   `json:"debit"`   // Increases balance (charges)
	Credit      float64   `json:"credit"`  // Decreases balance (receipts / reductions)
	Balance     float64   `json:"balance"` // Running balance
}

// StatementInvoiceRow represents an invoice in the statement view.
type StatementInvoiceRow struct {
	ID            string     `json:"id"`
	InvoiceNumber string     `json:"invoice_number"`
	CreatedAt     time.Time  `json:"created_at"`
	DueDate       *time.Time `json:"due_date,omitempty"`
	Subtotal      float64    `json:"subtotal"`
	Tax           float64    `json:"tax"`
	Total         float64    `json:"total"`
	PaidAmount    float64    `json:"paid_amount"`
	Balance       float64    `json:"balance"`
	PaymentStatus string     `json:"payment_status"`
	Status        string     `json:"status"`
	DaysOverdue   int        `json:"days_overdue"`
}

// StatementPaymentRow represents a payment in the statement view.
type StatementPaymentRow struct {
	ID            string    `json:"id"`
	InvoiceID     string    `json:"invoice_id"`
	InvoiceNumber string    `json:"invoice_number"`
	PaymentDate   time.Time `json:"payment_date"`
	Amount        float64   `json:"amount"`
	Method        string    `json:"method"`
	Reference     string    `json:"reference"`
	Remarks       string    `json:"remarks"`
}

// CustomerStatementData is the template payload for customer_statement.html.
type CustomerStatementData struct {
	Customer            customer.Customer      `json:"customer"`
	StatementDate       time.Time              `json:"statement_date"`
	UnbilledTrips       []UnbilledTripDTO      `json:"unbilled_trips"`
	TotalUnbilledAmount float64                `json:"total_unbilled_amount"`
	Invoices            []StatementInvoiceRow  `json:"invoices"`
	Payments            []StatementPaymentRow  `json:"payments"`
	Aging               CreditAgingSummary     `json:"aging"`
	TotalInvoiced       float64                `json:"total_invoiced"`
	TotalPaid           float64                `json:"total_paid"`
	OutstandingInvoices float64                `json:"outstanding_invoices"`
	NetBalanceDue       float64                `json:"net_balance_due"`
	Transactions        []StatementTransaction `json:"transactions"`
	CleanPhone          string                 `json:"clean_phone"`
	WhatsAppShareURL    string                 `json:"whatsapp_share_url"`
	WhatsAppSummaryText string                 `json:"whatsapp_summary_text"`
}

// UnbilledTrips handles GET /customers/{id}/unbilled-trips.
func (h *CustomerHandlers) UnbilledTrips(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	if customerID == "" {
		http.Error(w, "missing customer id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tenantID := shared.TenantIDFromContext(ctx)

	// Verify customer exists
	_, err := h.Services.Customers.GetCustomer(ctx, domain.CustomerID(customerID))
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}

	dateFrom := strings.TrimSpace(r.URL.Query().Get("date_from"))
	dateTo := strings.TrimSpace(r.URL.Query().Get("date_to"))

	trips, err := h.fetchUnbilledTrips(ctx, tenantID, customerID, nil, dateFrom, dateTo)
	if err != nil {
		slog.ErrorContext(ctx, "failed to fetch unbilled trips", "customer_id", customerID, "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to load unbilled trips", http.StatusInternalServerError)
		return
	}

	if isJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"customer_id":    customerID,
			"unbilled_trips": trips,
			"count":          len(trips),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(trips)
}

// ConsolidateInvoices handles POST /customers/{id}/invoices/consolidate.
func (h *CustomerHandlers) ConsolidateInvoices(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	if customerID == "" {
		http.Error(w, "missing customer id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tenantID := shared.TenantIDFromContext(ctx)

	cust, err := h.Services.Customers.GetCustomer(ctx, domain.CustomerID(customerID))
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}

	var tripIDs []string
	var dateFrom, dateTo, notes, dueDateOverride string
	var paymentTermsOverride *int

	// Handle JSON or Form input
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			TripIDs          []string `json:"trip_ids"`
			DateFrom         string   `json:"date_from"`
			DateTo           string   `json:"date_to"`
			DueDate          string   `json:"due_date"`
			PaymentTermsDays *int     `json:"payment_terms_days"`
			Notes            string   `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			tripIDs = req.TripIDs
			dateFrom = strings.TrimSpace(req.DateFrom)
			dateTo = strings.TrimSpace(req.DateTo)
			dueDateOverride = strings.TrimSpace(req.DueDate)
			paymentTermsOverride = req.PaymentTermsDays
			notes = strings.TrimSpace(req.Notes)
		}
	} else {
		if err := r.ParseForm(); err == nil {
			// trip_ids can be repeated form values or comma-separated
			if rawIDs, ok := r.PostForm["trip_ids"]; ok {
				for _, val := range rawIDs {
					for _, part := range strings.Split(val, ",") {
						p := strings.TrimSpace(part)
						if p != "" {
							tripIDs = append(tripIDs, p)
						}
					}
				}
			} else if raw := strings.TrimSpace(r.PostFormValue("trip_id")); raw != "" {
				tripIDs = append(tripIDs, raw)
			}
			dateFrom = strings.TrimSpace(r.PostFormValue("date_from"))
			dateTo = strings.TrimSpace(r.PostFormValue("date_to"))
			dueDateOverride = strings.TrimSpace(r.PostFormValue("due_date"))
			if rawTerms := strings.TrimSpace(r.PostFormValue("payment_terms_days")); rawTerms != "" {
				if n, err := strconv.Atoi(rawTerms); err == nil && n >= 0 {
					paymentTermsOverride = &n
				}
			}
			notes = strings.TrimSpace(r.PostFormValue("notes"))
		}
	}

	// Fetch matching unbilled trips
	unbilledTrips, err := h.fetchUnbilledTrips(ctx, tenantID, customerID, tripIDs, dateFrom, dateTo)
	if err != nil {
		slog.ErrorContext(ctx, "failed to query unbilled trips for consolidation", "customer_id", customerID, "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to load unbilled trips", http.StatusInternalServerError)
		return
	}

	if len(unbilledTrips) == 0 {
		http.Error(w, "No unbilled delivered trips found for consolidation", http.StatusBadRequest)
		return
	}

	invoiceDate := time.Now()

	// 100% Dynamic Credit Policy:
	// Priority 1: Direct Due Date override in request
	// Priority 2: Payment Terms Days override in request
	// Priority 3: Per-Customer configured PaymentTermsDays
	// Priority 4: Company Default Payment Terms
	var dueDate time.Time
	if dueDateOverride != "" {
		if parsed, err := time.Parse("2006-01-02", dueDateOverride); err == nil {
			dueDate = parsed
		} else if parsed, err := time.Parse("2006-01-02 15:04:05", dueDateOverride); err == nil {
			dueDate = parsed
		}
	}

	if dueDate.IsZero() {
		termsDays := cust.PaymentTermsDays
		if paymentTermsOverride != nil {
			termsDays = *paymentTermsOverride
		} else if termsDays <= 0 {
			// Check company config default payment terms if customer has no explicit override
			var defaultTerms sql.NullInt64
			_ = h.DB.QueryRowContext(ctx, `
				SELECT CAST(value AS INTEGER) FROM company_config
				WHERE tenant_id = ? AND key = 'billing.default_payment_terms_days'
			`, string(tenantID)).Scan(&defaultTerms)
			if defaultTerms.Valid && defaultTerms.Int64 > 0 {
				termsDays = int(defaultTerms.Int64)
			}
		}
		if termsDays < 0 {
			termsDays = 0
		}
		dueDate = invoiceDate.AddDate(0, 0, termsDays)
	}

	// Company settings & GST derivation
	companySettings, _ := h.Services.Settings.GetSettings(ctx)
	supplierState := "27"
	supplierGSTIN := "27AABCU9603R1ZX"
	if companySettings.StateCode != "" {
		supplierState = companySettings.StateCode
	}
	if companySettings.GSTNumber != nil && *companySettings.GSTNumber != "" {
		supplierGSTIN = *companySettings.GSTNumber
	}

	custGSTIN := ""
	if cust.GST != nil {
		custGSTIN = strings.TrimSpace(*cust.GST)
	}
	custState := ""
	if len(custGSTIN) >= 2 {
		custState = custGSTIN[:2]
	} else if cust.StateCode != nil {
		custState = strings.TrimSpace(*cust.StateCode)
	}
	if custState == "" {
		custState = supplierState
	}

	supStatePrefix := supplierState
	if len(supplierGSTIN) >= 2 {
		supStatePrefix = supplierGSTIN[:2]
	}
	intraState := (custState == supStatePrefix) || (custGSTIN == "" && custState == supplierState)

	gstRate := 0.0
	gstEnabled := companySettings.GSTEnabled
	if gstEnabled {
		gstRate = companySettings.GSTRate
		if gstRate <= 0 {
			gstRate = 18.0
		}
	}

	cgstRate, sgstRate, igstRate := 0.0, 0.0, 0.0
	if gstRate > 0 {
		if intraState {
			cgstRate = gstRate / 2.0
			sgstRate = gstRate / 2.0
		} else {
			igstRate = gstRate
		}
	}

	// Begin atomic transaction
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		http.Error(w, "Database transaction error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	invoiceUUID := uuid.NewString()

	// Generate sequential GST invoice number
	invoicePrefix := "INV"
	if trimmed := strings.TrimSpace(companySettings.InvoicePrefix); trimmed != "" {
		invoicePrefix = trimmed
	}
	seqRepo := sqlite.NewRepository(h.DB)
	invoiceNumber, err := seqRepo.NextInvoiceNumber(ctx, string(tenantID), invoicePrefix)
	if err != nil {
		invoiceNumber = fmt.Sprintf("%s-%s", invoicePrefix, uuid.NewString()[:8])
	}

	primaryBookingID := unbilledTrips[0].BookingID
	primaryTripID := unbilledTrips[0].ID

	// Insert invoice header
	_, err = tx.ExecContext(ctx, `
		INSERT INTO invoices (
			id, invoice_number, booking_id, customer_id, trip_id,
			subtotal, tax, cgst, sgst, igst, discount, total,
			payment_status, status, due_date, tenant_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 'pending', 'outstanding', ?, ?, ?, ?)
	`, invoiceUUID, invoiceNumber, primaryBookingID, customerID, primaryTripID,
		dueDate.Format("2006-01-02 15:04:05"), string(tenantID), invoiceDate, invoiceDate)
	if err != nil {
		slog.ErrorContext(ctx, "failed to insert consolidated invoice header", "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to create invoice", http.StatusInternalServerError)
		return
	}

	var sumTaxable, sumCGST, sumSGST, sumIGST, sumTotal float64

	// Build Annexure Line Items for each selected trip
	for _, tr := range unbilledTrips {
		// 1. Freight Line Item
		if tr.Freight > 0 {
			fTaxable := tr.Freight
			fCGST := roundMoney(fTaxable * cgstRate / 100.0)
			fSGST := roundMoney(fTaxable * sgstRate / 100.0)
			fIGST := roundMoney(fTaxable * igstRate / 100.0)
			fTotal := roundMoney(fTaxable + fCGST + fSGST + fIGST)

			desc := fmt.Sprintf("Freight: Trip %s (%s to %s) - Booking %s", tr.TripNumber, tr.RouteSource, tr.RouteDestination, tr.BookingNumber)
			if tr.RouteSource == "" && tr.RouteDestination == "" {
				desc = fmt.Sprintf("Freight: Trip %s - Booking %s", tr.TripNumber, tr.BookingNumber)
			}

			lineID := uuid.NewString()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO invoice_line_items (
					id, tenant_id, invoice_id, trip_id, line_type, description,
					quantity, unit_price, amount, hsn_sac_code, unit, rate,
					taxable_value, cgst_rate, sgst_rate, igst_rate,
					cgst_amount, sgst_amount, igst_amount, total, created_at
				) VALUES (?, ?, ?, ?, 'freight', ?, 1, ?, ?, '996511', 'TRIP', ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
			`, lineID, string(tenantID), invoiceUUID, tr.ID, desc,
				tr.Freight, tr.Freight, tr.Freight,
				fTaxable, cgstRate, sgstRate, igstRate,
				fCGST, fSGST, fIGST, fTotal)
			if err != nil {
				slog.ErrorContext(ctx, "failed to insert freight line item", "error", logging.Redact(err.Error()))
				http.Error(w, "Failed to add invoice line items", http.StatusInternalServerError)
				return
			}

			sumTaxable += fTaxable
			sumCGST += fCGST
			sumSGST += fSGST
			sumIGST += fIGST
			sumTotal += fTotal
		}

		// 2. Toll Costs Line Item
		if tr.Tolls > 0 {
			tTaxable := tr.Tolls
			tCGST := roundMoney(tTaxable * cgstRate / 100.0)
			tSGST := roundMoney(tTaxable * sgstRate / 100.0)
			tIGST := roundMoney(tTaxable * igstRate / 100.0)
			tTotal := roundMoney(tTaxable + tCGST + tSGST + tIGST)

			desc := fmt.Sprintf("Toll Charges: Trip %s", tr.TripNumber)
			lineID := uuid.NewString()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO invoice_line_items (
					id, tenant_id, invoice_id, trip_id, line_type, description,
					quantity, unit_price, amount, hsn_sac_code, unit, rate,
					taxable_value, cgst_rate, sgst_rate, igst_rate,
					cgst_amount, sgst_amount, igst_amount, total, created_at
				) VALUES (?, ?, ?, ?, 'accessorial', ?, 1, ?, ?, '996511', 'NOS', ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
			`, lineID, string(tenantID), invoiceUUID, tr.ID, desc,
				tr.Tolls, tr.Tolls, tr.Tolls,
				tTaxable, cgstRate, sgstRate, igstRate,
				tCGST, tSGST, tIGST, tTotal)
			if err != nil {
				slog.ErrorContext(ctx, "failed to insert toll line item", "error", logging.Redact(err.Error()))
				http.Error(w, "Failed to add invoice line items", http.StatusInternalServerError)
				return
			}

			sumTaxable += tTaxable
			sumCGST += tCGST
			sumSGST += tSGST
			sumIGST += tIGST
			sumTotal += tTotal
		}

		// 3. Detention Charges Line Items
		dets, err := fetchTripDetentions(ctx, tx, tr.ID, string(tenantID))
		if err == nil && len(dets) > 0 {
			for _, d := range dets {
				dTaxable := d.amt
				dCGST := roundMoney(dTaxable * cgstRate / 100.0)
				dSGST := roundMoney(dTaxable * sgstRate / 100.0)
				dIGST := roundMoney(dTaxable * igstRate / 100.0)
				dTotal := roundMoney(dTaxable + dCGST + dSGST + dIGST)

				hrs := float64(d.sec) / 3600.0
				if hrs < 0.1 {
					hrs = 1.0
				}
				desc := fmt.Sprintf("Detention (%s zone): Trip %s (%.1f hrs)", d.zoneKind, tr.TripNumber, hrs)
				lineID := uuid.NewString()

				_, err = tx.ExecContext(ctx, `
					INSERT INTO invoice_line_items (
						id, tenant_id, invoice_id, trip_id, line_type, description,
						quantity, unit_price, amount, ref_id, hsn_sac_code, unit, rate,
						taxable_value, cgst_rate, sgst_rate, igst_rate,
						cgst_amount, sgst_amount, igst_amount, total, created_at
					) VALUES (?, ?, ?, ?, 'detention', ?, ?, ?, ?, ?, '996511', 'HRS', ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
				`, lineID, string(tenantID), invoiceUUID, tr.ID, desc,
					hrs, d.rate, d.amt, d.id, d.rate,
					dTaxable, cgstRate, sgstRate, igstRate,
					dCGST, dSGST, dIGST, dTotal)
				if err != nil {
					slog.ErrorContext(ctx, "failed to insert detention line item", "error", logging.Redact(err.Error()))
					http.Error(w, "Failed to add invoice line items", http.StatusInternalServerError)
					return
				}

				// Mark detention record as attached
				_, _ = tx.ExecContext(ctx, `
					UPDATE trip_detentions SET status = 'attached', updated_at = datetime('now')
					WHERE id = ? AND tenant_id = ?
				`, d.id, string(tenantID))

				sumTaxable += dTaxable
				sumCGST += dCGST
				sumSGST += dSGST
				sumIGST += dIGST
				sumTotal += dTotal
			}
		} else if tr.Detention > 0 {
			dTaxable := tr.Detention
			dCGST := roundMoney(dTaxable * cgstRate / 100.0)
			dSGST := roundMoney(dTaxable * sgstRate / 100.0)
			dIGST := roundMoney(dTaxable * igstRate / 100.0)
			dTotal := roundMoney(dTaxable + dCGST + dSGST + dIGST)

			desc := fmt.Sprintf("Detention Charges: Trip %s", tr.TripNumber)
			lineID := uuid.NewString()
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO invoice_line_items (
					id, tenant_id, invoice_id, trip_id, line_type, description,
					quantity, unit_price, amount, hsn_sac_code, unit, rate,
					taxable_value, cgst_rate, sgst_rate, igst_rate,
					cgst_amount, sgst_amount, igst_amount, total, created_at
				) VALUES (?, ?, ?, ?, 'detention', ?, 1, ?, ?, '996511', 'NOS', ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
			`, lineID, string(tenantID), invoiceUUID, tr.ID, desc,
				tr.Detention, tr.Detention, tr.Detention,
				dTaxable, cgstRate, sgstRate, igstRate,
				dCGST, dSGST, dIGST, dTotal)

			sumTaxable += dTaxable
			sumCGST += dCGST
			sumSGST += dSGST
			sumIGST += dIGST
			sumTotal += dTotal
		}
	}

	totalTax := roundMoney(sumCGST + sumSGST + sumIGST)
	finalTotal := roundMoney(sumTaxable + totalTax)

	// Update invoice header with reconciled totals
	_, err = tx.ExecContext(ctx, `
		UPDATE invoices
		SET subtotal = ?, tax = ?, cgst = ?, sgst = ?, igst = ?, total = ?, updated_at = datetime('now')
		WHERE id = ? AND tenant_id = ?
	`, sumTaxable, totalTax, sumCGST, sumSGST, sumIGST, finalTotal, invoiceUUID, string(tenantID))
	if err != nil {
		slog.ErrorContext(ctx, "failed to update consolidated invoice totals", "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to finalize invoice", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.ErrorContext(ctx, "failed to commit consolidated invoice tx", "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to commit invoice", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "consolidated invoice generated successfully",
		"invoice_id", invoiceUUID,
		"invoice_number", invoiceNumber,
		"customer_id", customerID,
		"trips_count", len(unbilledTrips),
		"due_date", dueDate.Format("2006-01-02"),
		"total", finalTotal)

	if isJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"invoice_id":     invoiceUUID,
			"invoice_number": invoiceNumber,
			"customer_id":    customerID,
			"subtotal":       sumTaxable,
			"tax":            totalTax,
			"cgst":           sumCGST,
			"sgst":           sumSGST,
			"igst":           sumIGST,
			"total":          finalTotal,
			"due_date":       dueDate.Format("2006-01-02"),
			"trip_count":     len(unbilledTrips),
			"notes":          notes,
		})
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/invoices/%s", invoiceUUID), http.StatusSeeOther)
}

// Statement handles GET /customers/{id}/statement (Statement of Account / Khata / Ledger & Credit Aging).
func (h *CustomerHandlers) Statement(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "id")
	if customerID == "" {
		http.Error(w, "missing customer id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tenantID := shared.TenantIDFromContext(ctx)

	cust, err := h.Services.Customers.GetCustomer(ctx, domain.CustomerID(customerID))
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}

	statementData, err := h.computeCustomerStatement(ctx, tenantID, cust)
	if err != nil {
		slog.ErrorContext(ctx, "failed to compute customer statement", "customer_id", customerID, "error", logging.Redact(err.Error()))
		http.Error(w, "Failed to compute statement of account", http.StatusInternalServerError)
		return
	}

	if isJSONRequest(r) || r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statementData)
		return
	}

	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "customer_statement.html", PageData{
		Title: fmt.Sprintf("Statement of Account - %s", cust.Name),
		User:  session,
		Extra: map[string]interface{}{
			"Statement": statementData,
			"Customer":  statementData.Customer,
		},
	})
}

// computeCustomerStatement builds the full ledger, aging, and WIP metrics.
func (h *CustomerHandlers) computeCustomerStatement(ctx context.Context, tenantID shared.TenantID, cust customer.Customer) (*CustomerStatementData, error) {
	now := time.Now()
	customerID := string(cust.ID)

	// 1. Fetch Unbilled Trips (WIP Freight)
	unbilledTrips, err := h.fetchUnbilledTrips(ctx, tenantID, customerID, nil, "", "")
	if err != nil {
		return nil, fmt.Errorf("fetch unbilled trips: %w", err)
	}
	var totalUnbilled float64
	for _, tr := range unbilledTrips {
		totalUnbilled += tr.Total
	}

	// 2. Fetch all Invoices for this customer
	invRows, err := h.DB.QueryContext(ctx, `
		SELECT id, invoice_number, created_at, due_date, subtotal, tax, total,
		       COALESCE(paid_amount, 0), payment_status, status
		FROM invoices
		WHERE customer_id = ? AND tenant_id = ? AND status != 'cancelled'
		ORDER BY created_at ASC
	`, customerID, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("fetch invoices: %w", err)
	}
	defer func() { _ = invRows.Close() }()

	var invoices []StatementInvoiceRow
	var totalInvoiced float64
	var totalOutstandingInvoices float64
	var aging CreditAgingSummary

	for invRows.Next() {
		var (
			inv         StatementInvoiceRow
			dueDateNull sql.NullTime
		)
		if err := invRows.Scan(
			&inv.ID, &inv.InvoiceNumber, &inv.CreatedAt, &dueDateNull,
			&inv.Subtotal, &inv.Tax, &inv.Total,
			&inv.PaidAmount, &inv.PaymentStatus, &inv.Status,
		); err != nil {
			continue
		}

		if dueDateNull.Valid {
			inv.DueDate = &dueDateNull.Time
		}

		balance := inv.Total - inv.PaidAmount
		if balance < 0 {
			balance = 0
		}
		inv.Balance = roundMoney(balance)

		// Calculate days overdue
		daysOverdue := 0
		if inv.DueDate != nil {
			if now.After(*inv.DueDate) {
				daysOverdue = int(now.Sub(*inv.DueDate).Hours() / 24.0)
			}
		} else {
			// Fallback: created_at + customer payment terms
			termsDays := cust.PaymentTermsDays
			calcDue := inv.CreatedAt.AddDate(0, 0, termsDays)
			if now.After(calcDue) {
				daysOverdue = int(now.Sub(calcDue).Hours() / 24.0)
			}
		}
		inv.DaysOverdue = daysOverdue

		totalInvoiced += inv.Total
		totalOutstandingInvoices += inv.Balance

		// Categorize into Credit Aging buckets if outstanding
		if inv.Balance > 0 && inv.PaymentStatus != "paid" {
			if daysOverdue <= 15 {
				aging.CurrentOr15 += inv.Balance
			} else if daysOverdue <= 30 {
				aging.Days16To30 += inv.Balance
				aging.TotalOverdue += inv.Balance
			} else if daysOverdue <= 60 {
				aging.Days31To60 += inv.Balance
				aging.TotalOverdue += inv.Balance
			} else {
				aging.Days60Plus += inv.Balance
				aging.TotalOverdue += inv.Balance
			}
		}

		invoices = append(invoices, inv)
	}
	aging.TotalOutstanding = roundMoney(totalOutstandingInvoices)
	aging.CurrentOr15 = roundMoney(aging.CurrentOr15)
	aging.Days16To30 = roundMoney(aging.Days16To30)
	aging.Days31To60 = roundMoney(aging.Days31To60)
	aging.Days60Plus = roundMoney(aging.Days60Plus)
	aging.TotalOverdue = roundMoney(aging.TotalOverdue)

	// 3. Fetch all Payments applied to customer invoices
	payRows, err := h.DB.QueryContext(ctx, `
		SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method,
		       COALESCE(p.reference, ''), COALESCE(p.remarks, ''), i.invoice_number
		FROM payments p
		JOIN invoices i ON p.invoice_id = i.id
		WHERE i.customer_id = ? AND p.tenant_id = ?
		ORDER BY p.payment_date ASC
	`, customerID, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("fetch payments: %w", err)
	}
	defer func() { _ = payRows.Close() }()

	var payments []StatementPaymentRow
	var totalPaid float64
	for payRows.Next() {
		var p StatementPaymentRow
		if err := payRows.Scan(
			&p.ID, &p.InvoiceID, &p.PaymentDate, &p.Amount,
			&p.Method, &p.Reference, &p.Remarks, &p.InvoiceNumber,
		); err == nil {
			totalPaid += p.Amount
			payments = append(payments, p)
		}
	}

	// 4. Fetch Credit/Debit Notes if available
	type noteRow struct {
		id            string
		invoiceNumber string
		noteNumber    string
		noteType      string
		reason        string
		total         float64
		createdAt     time.Time
	}
	var notes []noteRow
	nRows, errNotes := h.DB.QueryContext(ctx, `
		SELECT n.id, i.invoice_number, n.note_number, n.note_type, n.reason, n.total, n.created_at
		FROM credit_debit_notes n
		JOIN invoices i ON n.invoice_id = i.id
		WHERE i.customer_id = ? AND n.tenant_id = ?
		ORDER BY n.created_at ASC
	`, customerID, string(tenantID))
	if errNotes == nil {
		defer func() { _ = nRows.Close() }()
		for nRows.Next() {
			var nr noteRow
			if err := nRows.Scan(&nr.id, &nr.invoiceNumber, &nr.noteNumber, &nr.noteType, &nr.reason, &nr.total, &nr.createdAt); err == nil {
				notes = append(notes, nr)
			}
		}
	}

	// 5. Build Unified Chronological Transaction Ledger (Khata)
	type rawTx struct {
		date        time.Time
		txType      string
		reference   string
		description string
		debit       float64
		credit      float64
	}
	var rawTxs []rawTx

	// Add Invoices (Debits)
	for _, inv := range invoices {
		rawTxs = append(rawTxs, rawTx{
			date:        inv.CreatedAt,
			txType:      "Invoice",
			reference:   inv.InvoiceNumber,
			description: fmt.Sprintf("Tax Invoice #%s", inv.InvoiceNumber),
			debit:       inv.Total,
			credit:      0,
		})
	}

	// Add Payments (Credits)
	for _, p := range payments {
		desc := fmt.Sprintf("Payment via %s", strings.ToUpper(p.Method))
		if p.Reference != "" {
			desc += fmt.Sprintf(" (Ref: %s)", p.Reference)
		}
		rawTxs = append(rawTxs, rawTx{
			date:        p.PaymentDate,
			txType:      "Payment",
			reference:   p.InvoiceNumber,
			description: desc,
			debit:       0,
			credit:      p.Amount,
		})
	}

	// Add Credit / Debit Notes
	for _, n := range notes {
		if n.noteType == "credit" {
			rawTxs = append(rawTxs, rawTx{
				date:        n.createdAt,
				txType:      "Credit Note",
				reference:   n.noteNumber,
				description: fmt.Sprintf("Credit Note #%s for Inv #%s - %s", n.noteNumber, n.invoiceNumber, n.reason),
				debit:       0,
				credit:      n.total,
			})
		} else {
			rawTxs = append(rawTxs, rawTx{
				date:        n.createdAt,
				txType:      "Debit Note",
				reference:   n.noteNumber,
				description: fmt.Sprintf("Debit Note #%s for Inv #%s - %s", n.noteNumber, n.invoiceNumber, n.reason),
				debit:       n.total,
				credit:      0,
			})
		}
	}

	// Sort ledger chronologically
	sort.SliceStable(rawTxs, func(i, j int) bool {
		return rawTxs[i].date.Before(rawTxs[j].date)
	})

	var runningBalance float64
	var ledger []StatementTransaction
	for _, rt := range rawTxs {
		runningBalance += (rt.debit - rt.credit)
		ledger = append(ledger, StatementTransaction{
			Date:        rt.date,
			Type:        rt.txType,
			Reference:   rt.reference,
			Description: rt.description,
			Debit:       roundMoney(rt.debit),
			Credit:      roundMoney(rt.credit),
			Balance:     roundMoney(runningBalance),
		})
	}

	// Calculate Final Net Balance Due
	totalInvoiced = roundMoney(totalInvoiced)
	totalPaid = roundMoney(totalPaid)
	totalUnbilled = roundMoney(totalUnbilled)
	netBalanceDue := roundMoney(totalOutstandingInvoices + totalUnbilled)

	// Clean phone and prefill WhatsApp sharing text
	cleanPhone := cleanPhoneForWhatsApp(cust.Phone)
	summaryText := fmt.Sprintf(
		"Statement of Account for *%s* as of %s:\n\n"+
			"• Total Invoiced: ₹%.2f\n"+
			"• Total Payments Received: ₹%.2f\n"+
			"• Outstanding Invoices: ₹%.2f\n"+
			"• Unbilled WIP Freight: ₹%.2f\n"+
			"• *Net Balance Due: ₹%.2f*\n\n"+
			"Credit Aging:\n"+
			"  - 0-15 Days: ₹%.2f\n"+
			"  - 16-30 Days: ₹%.2f\n"+
			"  - 31-60 Days: ₹%.2f\n"+
			"  - 60+ Days: ₹%.2f\n\n"+
			"Please review your statement and arrange payment.",
		cust.Name,
		now.Format("02 Jan 2006"),
		totalInvoiced,
		totalPaid,
		totalOutstandingInvoices,
		totalUnbilled,
		netBalanceDue,
		aging.CurrentOr15,
		aging.Days16To30,
		aging.Days31To60,
		aging.Days60Plus,
	)

	waURL := fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(summaryText))

	return &CustomerStatementData{
		Customer:            cust,
		StatementDate:       now,
		UnbilledTrips:       unbilledTrips,
		TotalUnbilledAmount: totalUnbilled,
		Invoices:            invoices,
		Payments:            payments,
		Aging:               aging,
		TotalInvoiced:       totalInvoiced,
		TotalPaid:           totalPaid,
		OutstandingInvoices: totalOutstandingInvoices,
		NetBalanceDue:       netBalanceDue,
		Transactions:        ledger,
		CleanPhone:          cleanPhone,
		WhatsAppShareURL:    waURL,
		WhatsAppSummaryText: summaryText,
	}, nil
}

// fetchUnbilledTrips retrieves delivered/completed trips not yet invoiced.
func (h *CustomerHandlers) fetchUnbilledTrips(
	ctx context.Context,
	tenantID shared.TenantID,
	customerID string,
	tripIDs []string,
	dateFrom string,
	dateTo string,
) ([]UnbilledTripDTO, error) {
	query := `
		SELECT
			t.id, t.trip_number, COALESCE(t.booking_id, ''),
			COALESCE(b.booking_number, ''),
			COALESCE(r.source, ''), COALESCE(r.destination, ''),
			COALESCE(v.vehicle_number, ''),
			COALESCE(d.first_name || ' ' || d.last_name, ''),
			t.departure_time, t.delivered_at, t.completed_at,
			COALESCE(t.toll_costs, 0),
			COALESCE(b.price, 0) AS freight_price,
			COALESCE((
				SELECT SUM(td.amount)
				FROM trip_detentions td
				WHERE td.trip_id = t.id AND td.tenant_id = t.tenant_id AND td.status != 'waived' AND td.amount > 0
			), 0) AS detention_amount
		FROM trips t
		JOIN bookings b ON t.booking_id = b.id
		LEFT JOIN routes r ON t.route_id = r.id
		LEFT JOIN vehicles v ON t.vehicle_id = v.id
		LEFT JOIN drivers d ON t.driver_id = d.id
		WHERE b.customer_id = ?
		  AND t.tenant_id = ?
		  AND t.status IN ('delivered', 'completed')
		  AND t.id NOT IN (
			  SELECT COALESCE(trip_id, '') FROM invoices
			  WHERE tenant_id = ? AND status != 'cancelled' AND trip_id IS NOT NULL AND trip_id != ''
		  )
		  AND t.id NOT IN (
			  SELECT COALESCE(ili.trip_id, '')
			  FROM invoice_line_items ili
			  JOIN invoices inv ON ili.invoice_id = inv.id
			  WHERE ili.tenant_id = ? AND inv.status != 'cancelled' AND ili.trip_id IS NOT NULL AND ili.trip_id != ''
		  )
	`
	var args []interface{}
	args = append(args, customerID, string(tenantID), string(tenantID), string(tenantID))

	if len(tripIDs) > 0 {
		placeholders := make([]string, len(tripIDs))
		for i, tid := range tripIDs {
			placeholders[i] = "?"
			args = append(args, tid)
		}
		query += fmt.Sprintf(" AND t.id IN (%s)", strings.Join(placeholders, ","))
	}

	if dateFrom != "" {
		query += " AND date(COALESCE(t.delivered_at, t.completed_at, t.departure_time)) >= date(?)"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		query += " AND date(COALESCE(t.delivered_at, t.completed_at, t.departure_time)) <= date(?)"
		args = append(args, dateTo)
	}

	query += " ORDER BY t.departure_time ASC"

	rows, err := h.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var unbilled []UnbilledTripDTO
	for rows.Next() {
		var (
			u          UnbilledTripDTO
			delAtNull  sql.NullTime
			compAtNull sql.NullTime
			tolls      float64
			freight    float64
			detention  float64
		)
		if err := rows.Scan(
			&u.ID, &u.TripNumber, &u.BookingID,
			&u.BookingNumber,
			&u.RouteSource, &u.RouteDestination,
			&u.VehicleNumber,
			&u.DriverName,
			&u.DepartureTime, &delAtNull, &compAtNull,
			&tolls, &freight, &detention,
		); err != nil {
			continue
		}

		if delAtNull.Valid {
			u.DeliveredAt = &delAtNull.Time
		} else if compAtNull.Valid {
			u.DeliveredAt = &compAtNull.Time
		}

		u.Freight = roundMoney(freight)
		u.Tolls = roundMoney(tolls)
		u.Detention = roundMoney(detention)
		u.Total = roundMoney(u.Freight + u.Tolls + u.Detention)

		unbilled = append(unbilled, u)
	}

	return unbilled, nil
}

// cleanPhoneForWhatsApp normalizes phone numbers into E.164 digits for WhatsApp wa.me links.
func cleanPhoneForWhatsApp(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if len(d) == 10 {
		return "91" + d // Default to India country code 91 if 10 digits
	}
	return d
}

// isJSONRequest checks if the client asked for JSON.
func isJSONRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.Contains(r.Header.Get("Content-Type"), "application/json") ||
		r.URL.Query().Get("format") == "json"
}

func roundMoney(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}

type detentionItem struct {
	id       string
	zoneKind string
	sec      int64
	rate     float64
	amt      float64
}

func fetchTripDetentions(ctx context.Context, tx *sql.Tx, tripID string, tenantID string) ([]detentionItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, zone_kind, billable_seconds, rate_per_hour, amount
		FROM trip_detentions
		WHERE trip_id = ? AND tenant_id = ? AND status != 'waived' AND amount > 0
	`, tripID, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []detentionItem
	for rows.Next() {
		var d detentionItem
		if err := rows.Scan(&d.id, &d.zoneKind, &d.sec, &d.rate, &d.amt); err == nil {
			items = append(items, d)
		}
	}
	return items, nil
}
