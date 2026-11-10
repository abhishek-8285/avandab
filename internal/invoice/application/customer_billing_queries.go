package application

// Customer billing read models and the statement-of-account (Khata / ledger +
// credit aging) read path, extracted from
// internal/handlers/invoices_consolidate.go (handlers-end-state ADR, Phase 2).
// The legacy SQL, tenant scoping, aging-bucket arithmetic, deferential
// credit/debit-note read and the exact rounding are preserved; handlers keep
// HTTP parsing, validation and rendering.

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	appdb "transport-app/internal/database"
	"transport-app/internal/domain/customer"
	"transport-app/internal/shared"
)

// UnbilledTrip is a delivered/completed trip ready for consolidated invoicing.
type UnbilledTrip struct {
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

// CustomerStatement is the template/JSON payload for the customer statement
// of account (Khata / ledger + credit aging).
type CustomerStatement struct {
	Customer            customer.Customer      `json:"customer"`
	StatementDate       time.Time              `json:"statement_date"`
	UnbilledTrips       []UnbilledTrip         `json:"unbilled_trips"`
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

// statementNote is one credit/debit note row joined to its invoice number.
type statementNote struct {
	id            string
	invoiceNumber string
	noteNumber    string
	noteType      string
	reason        string
	total         float64
	createdAt     time.Time
}

// UnbilledTripsQuery selects delivered/completed trips that are not attached
// to a live invoice yet.
type UnbilledTripsQuery struct {
	TenantID   shared.TenantID
	CustomerID string
	TripIDs    []string
	DateFrom   string
	DateTo     string
}

// CustomerBillingQueries is the read-model facade behind the customer
// unbilled-trips list and the statement of account. It is stateless and cheap
// to construct per request.
type CustomerBillingQueries struct {
	db *sql.DB
}

// NewCustomerBillingQueries wires the customer billing read model over the app DB.
func NewCustomerBillingQueries(db *sql.DB) *CustomerBillingQueries {
	return &CustomerBillingQueries{db: db}
}

// UnbilledTrips retrieves delivered/completed trips not yet invoiced.
func (q *CustomerBillingQueries) UnbilledTrips(ctx context.Context, sel UnbilledTripsQuery) ([]UnbilledTrip, error) {
	sqlText := `
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
		JOIN bookings b ON t.booking_id = b.id AND b.tenant_id = $2
		LEFT JOIN routes r ON t.route_id = r.id
		LEFT JOIN vehicles v ON t.vehicle_id = v.id
		LEFT JOIN drivers d ON t.driver_id = d.id
		WHERE b.customer_id = $1
		  AND t.tenant_id = $2
		  AND t.status IN ('delivered', 'completed')
		  AND t.id NOT IN (
			  SELECT COALESCE(trip_id, '') FROM invoices
			  WHERE tenant_id = $3 AND status != 'cancelled' AND trip_id IS NOT NULL AND trip_id != ''
		  )
		  AND t.id NOT IN (
			  SELECT COALESCE(ili.trip_id, '')
			  FROM invoice_line_items ili
			  JOIN invoices inv ON ili.invoice_id = inv.id
			  WHERE ili.tenant_id = $4 AND inv.status != 'cancelled' AND ili.trip_id IS NOT NULL AND ili.trip_id != ''
		  )
	`
	var args []interface{}
	args = append(args, sel.CustomerID, string(sel.TenantID), string(sel.TenantID), string(sel.TenantID))

	if len(sel.TripIDs) > 0 {
		placeholders := make([]string, len(sel.TripIDs))
		for i, tid := range sel.TripIDs {
			placeholders[i] = "?"
			args = append(args, tid)
		}
		sqlText += fmt.Sprintf(" AND t.id IN (%s)", strings.Join(placeholders, ","))
	}

	if sel.DateFrom != "" {
		sqlText += " AND substr(CAST(COALESCE(t.delivered_at, t.completed_at, t.departure_time) AS TEXT), 1, 10) >= substr(CAST(? AS TEXT), 1, 10)"
		args = append(args, sel.DateFrom)
	}
	if sel.DateTo != "" {
		sqlText += " AND substr(CAST(COALESCE(t.delivered_at, t.completed_at, t.departure_time) AS TEXT), 1, 10) <= substr(CAST(? AS TEXT), 1, 10)"
		args = append(args, sel.DateTo)
	}

	sqlText += " ORDER BY t.departure_time ASC"

	rebound, rerr := appdb.Rebind(sqlText)
	if rerr != nil {
		return nil, rerr
	}
	rows, err := q.db.QueryContext(ctx, rebound, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var unbilled []UnbilledTrip
	for rows.Next() {
		var (
			u          UnbilledTrip
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

		u.Freight = round2(freight)
		u.Tolls = round2(tolls)
		u.Detention = round2(detention)
		u.Total = round2(u.Freight + u.Tolls + u.Detention)

		unbilled = append(unbilled, u)
	}

	return unbilled, nil
}

// DefaultPaymentTermsDays reads the company default billing terms
// (billing.default_payment_terms_days). The legacy path ignored the lookup
// error and treated a missing/non-positive row as "no default".
func (q *CustomerBillingQueries) DefaultPaymentTermsDays(ctx context.Context, tenantID shared.TenantID) (int, bool) {
	var defaultTerms sql.NullInt64
	_ = q.db.QueryRowContext(ctx, `
		SELECT CAST(value AS INTEGER) FROM company_config
		WHERE tenant_id = $1 AND key = 'billing.default_payment_terms_days'
	`, string(tenantID)).Scan(&defaultTerms)
	if defaultTerms.Valid && defaultTerms.Int64 > 0 {
		return int(defaultTerms.Int64), true
	}
	return 0, false
}

// CustomerStatement builds the full ledger, aging and WIP metrics for one
// customer. The unbilled-trips failure aborts the payload (legacy behavior);
// the payments/notes reads are tenant-scoped by the invoice join.
func (q *CustomerBillingQueries) CustomerStatement(ctx context.Context, tenantID shared.TenantID, cust *customer.Customer) (*CustomerStatement, error) {
	now := time.Now()
	customerID := string(cust.ID)

	// 1. Fetch Unbilled Trips (WIP Freight)
	unbilledTrips, err := q.UnbilledTrips(ctx, UnbilledTripsQuery{TenantID: tenantID, CustomerID: customerID})
	if err != nil {
		return nil, fmt.Errorf("fetch unbilled trips: %w", err)
	}
	var totalUnbilled float64
	for i := range unbilledTrips {
		totalUnbilled += unbilledTrips[i].Total
	}

	// 2. Fetch all Invoices for this customer
	invRows, err := q.db.QueryContext(ctx, `
		SELECT id, invoice_number, created_at, due_date, subtotal, tax, total,
		       COALESCE(paid_amount, 0), payment_status, status
		FROM invoices
		WHERE customer_id = $1 AND tenant_id = $2 AND status != 'cancelled'
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
		inv.Balance = round2(balance)

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
	aging.TotalOutstanding = round2(totalOutstandingInvoices)
	aging.CurrentOr15 = round2(aging.CurrentOr15)
	aging.Days16To30 = round2(aging.Days16To30)
	aging.Days31To60 = round2(aging.Days31To60)
	aging.Days60Plus = round2(aging.Days60Plus)
	aging.TotalOverdue = round2(aging.TotalOverdue)

	// 3. Fetch all Payments applied to customer invoices
	payRows, err := q.db.QueryContext(ctx, `
		SELECT p.id, p.invoice_id, p.payment_date, p.amount, p.method,
		       COALESCE(p.reference, ''), COALESCE(p.remarks, ''), i.invoice_number
		FROM payments p
		JOIN invoices i ON p.invoice_id = i.id
		WHERE i.customer_id = $1 AND p.tenant_id = $2 AND i.tenant_id = $2
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
	var notes []statementNote
	nRows, errNotes := q.db.QueryContext(ctx, `
		SELECT n.id, i.invoice_number, n.note_number, n.note_type, n.reason, n.total, n.created_at
		FROM credit_debit_notes n
		JOIN invoices i ON n.invoice_id = i.id
		WHERE i.customer_id = $1 AND n.tenant_id = $2 AND i.tenant_id = $2
		ORDER BY n.created_at ASC
	`, customerID, string(tenantID))
	if errNotes == nil {
		defer func() { _ = nRows.Close() }()
		for nRows.Next() {
			var nr statementNote
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
	for i := range invoices {
		inv := &invoices[i]
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
	for i := range payments {
		p := &payments[i]
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
	for i := range notes {
		n := &notes[i]
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
	for i := range rawTxs {
		rt := &rawTxs[i]
		runningBalance += (rt.debit - rt.credit)
		ledger = append(ledger, StatementTransaction{
			Date:        rt.date,
			Type:        rt.txType,
			Reference:   rt.reference,
			Description: rt.description,
			Debit:       round2(rt.debit),
			Credit:      round2(rt.credit),
			Balance:     round2(runningBalance),
		})
	}

	// Calculate Final Net Balance Due
	totalInvoiced = round2(totalInvoiced)
	totalPaid = round2(totalPaid)
	totalUnbilled = round2(totalUnbilled)
	netBalanceDue := round2(totalOutstandingInvoices + totalUnbilled)

	// Clean phone and prefill WhatsApp sharing text
	cleanPhone := CleanPhoneForWhatsApp(cust.Phone)
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

	return &CustomerStatement{
		Customer:            *cust,
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

// CleanPhoneForWhatsApp normalizes phone numbers into E.164 digits for
// WhatsApp wa.me links (10-digit numbers default to India's 91 country code).
func CleanPhoneForWhatsApp(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if len(d) == 10 {
		return "91" + d
	}
	return d
}
