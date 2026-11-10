package application

// Consolidated (monthly) freight invoicing use-case, extracted from
// internal/handlers/invoices_consolidate.go (handlers-end-state ADR, Phase 2).
// The credit-policy precedence, GST derivation and per-line split math, the
// financial-year invoice numbering (with the legacy random fallback), the
// single transaction boundary, detention attachment and the exact legacy
// failure stages are preserved; the handler keeps HTTP parsing, validation
// and rendering.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/domain/customer"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

// ErrNoUnbilledTrips reports an empty trip selection: the handler maps it to
// the legacy 400 "No unbilled delivered trips found for consolidation".
var ErrNoUnbilledTrips = errors.New("no unbilled delivered trips found for consolidation")

// ConsolidationStage identifies which step of the consolidation transaction
// failed so the HTTP adapter can preserve the legacy status/message mapping.
type ConsolidationStage string

// Consolidation failure stages (one per legacy error branch).
const (
	ConsolidationStageTxBegin         ConsolidationStage = "tx_begin"
	ConsolidationStageInsertHeader    ConsolidationStage = "insert_invoice_header"
	ConsolidationStageInsertFreight   ConsolidationStage = "insert_freight"
	ConsolidationStageInsertToll      ConsolidationStage = "insert_toll"
	ConsolidationStageInsertDetention ConsolidationStage = "insert_detention"
	ConsolidationStageUpdateTotals    ConsolidationStage = "update_totals"
	ConsolidationStageCommit          ConsolidationStage = "commit"
)

// ConsolidationError reports the failed step of a consolidation write.
type ConsolidationError struct {
	Stage ConsolidationStage
	Err   error
}

// Error implements error.
func (e *ConsolidationError) Error() string {
	if e.Err == nil {
		return "consolidated invoice write failed at " + string(e.Stage)
	}
	return "consolidated invoice write failed at " + string(e.Stage) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying driver error to errors.Is/As callers.
func (e *ConsolidationError) Unwrap() error { return e.Err }

// ConsolidationSettings is the company billing profile needed by the GST
// derivation, projected from domain.CompanySettings at the HTTP boundary.
type ConsolidationSettings struct {
	GSTEnabled    bool
	GSTRate       float64
	StateCode     string
	GSTNumber     *string
	InvoicePrefix string
}

// ConsolidateInvoicesCommand is the input of the consolidated-invoice write.
type ConsolidateInvoicesCommand struct {
	TenantID             shared.TenantID
	CustomerID           string
	Customer             *customer.Customer
	Settings             ConsolidationSettings
	Trips                []UnbilledTrip
	DueDateOverride      string
	PaymentTermsOverride *int
}

// ConsolidatedInvoice is the persisted consolidated invoice header.
type ConsolidatedInvoice struct {
	InvoiceID     string
	InvoiceNumber string
	Subtotal      float64
	Tax           float64
	CGST          float64
	SGST          float64
	IGST          float64
	Total         float64
	DueDate       time.Time
}

// ConsolidateInvoicesUseCase creates one consolidated GST invoice for a set
// of delivered trips in a single transaction.
type ConsolidateInvoicesUseCase struct {
	db *sql.DB
}

// detentionItem is one attachable detention row of a trip.
type detentionItem struct {
	id       string
	zoneKind string
	sec      int64
	rate     float64
	amt      float64
}

// NewConsolidateInvoicesUseCase wires the consolidation use-case over the app DB.
func NewConsolidateInvoicesUseCase(db *sql.DB) *ConsolidateInvoicesUseCase {
	return &ConsolidateInvoicesUseCase{db: db}
}

// Execute inserts the invoice header, one annexure line per freight/toll/
// detention component of every selected trip, then reconciles the header
// totals and commits. Statements are tenant-scoped exactly as before.
func (uc *ConsolidateInvoicesUseCase) Execute(ctx context.Context, cmd ConsolidateInvoicesCommand) (*ConsolidatedInvoice, error) {
	if len(cmd.Trips) == 0 {
		return nil, ErrNoUnbilledTrips
	}
	if cmd.Customer == nil {
		return nil, errors.New("consolidation requires a customer record")
	}

	tenantID := string(cmd.TenantID)
	cust := cmd.Customer
	invoiceDate := time.Now()
	dueDate := uc.resolveDueDate(ctx, cmd, invoiceDate)
	cgstRate, sgstRate, igstRate := consolidationGSTRates(cust, cmd.Settings)

	// Begin atomic transaction
	tx, err := uc.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, &ConsolidationError{Stage: ConsolidationStageTxBegin, Err: err}
	}
	defer func() { _ = tx.Rollback() }()

	invoiceUUID := uuid.NewString()

	// Generate sequential GST invoice number
	invoicePrefix := "INV"
	if trimmed := strings.TrimSpace(cmd.Settings.InvoicePrefix); trimmed != "" {
		invoicePrefix = trimmed
	}
	seqRepo := sqlite.NewRepository(uc.db)
	invoiceNumber, err := seqRepo.NextInvoiceNumber(ctx, tenantID, invoicePrefix)
	if err != nil {
		invoiceNumber = fmt.Sprintf("%s-%s", invoicePrefix, uuid.NewString()[:8])
	}

	primaryBookingID := cmd.Trips[0].BookingID
	primaryTripID := cmd.Trips[0].ID

	// Insert invoice header
	_, err = tx.ExecContext(ctx, `
		INSERT INTO invoices (
			id, invoice_number, booking_id, customer_id, trip_id,
			subtotal, tax, cgst, sgst, igst, discount, total,
			payment_status, status, due_date, tenant_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 0, 0, 0, 0, 0, 0, 0, 'pending', 'outstanding', $6, $7, $8, $9)
	`, invoiceUUID, invoiceNumber, primaryBookingID, cmd.CustomerID, primaryTripID,
		dueDate.Format("2006-01-02 15:04:05"), tenantID, invoiceDate, invoiceDate)
	if err != nil {
		return nil, &ConsolidationError{Stage: ConsolidationStageInsertHeader, Err: err}
	}

	var sumTaxable, sumCGST, sumSGST, sumIGST float64

	// Build Annexure Line Items for each selected trip
	for i := range cmd.Trips {
		tr := &cmd.Trips[i]

		// 1. Freight Line Item
		if tr.Freight > 0 {
			fTaxable := tr.Freight
			fCGST := round2(fTaxable * cgstRate / 100.0)
			fSGST := round2(fTaxable * sgstRate / 100.0)
			fIGST := round2(fTaxable * igstRate / 100.0)
			fTotal := round2(fTaxable + fCGST + fSGST + fIGST)

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
				) VALUES ($1, $2, $3, $4, 'freight', $5, 1, $6, $7, '996511', 'TRIP', $8, $9, $10, $11, $12, $13, $14, $15, $16, CURRENT_TIMESTAMP)
			`, lineID, tenantID, invoiceUUID, tr.ID, desc,
				tr.Freight, tr.Freight, tr.Freight,
				fTaxable, cgstRate, sgstRate, igstRate,
				fCGST, fSGST, fIGST, fTotal)
			if err != nil {
				return nil, &ConsolidationError{Stage: ConsolidationStageInsertFreight, Err: err}
			}

			sumTaxable += fTaxable
			sumCGST += fCGST
			sumSGST += fSGST
			sumIGST += fIGST
		}

		// 2. Toll Costs Line Item
		if tr.Tolls > 0 {
			tTaxable := tr.Tolls
			tCGST := round2(tTaxable * cgstRate / 100.0)
			tSGST := round2(tTaxable * sgstRate / 100.0)
			tIGST := round2(tTaxable * igstRate / 100.0)
			tTotal := round2(tTaxable + tCGST + tSGST + tIGST)

			desc := fmt.Sprintf("Toll Charges: Trip %s", tr.TripNumber)
			lineID := uuid.NewString()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO invoice_line_items (
					id, tenant_id, invoice_id, trip_id, line_type, description,
					quantity, unit_price, amount, hsn_sac_code, unit, rate,
					taxable_value, cgst_rate, sgst_rate, igst_rate,
					cgst_amount, sgst_amount, igst_amount, total, created_at
				) VALUES ($1, $2, $3, $4, 'accessorial', $5, 1, $6, $7, '996511', 'NOS', $8, $9, $10, $11, $12, $13, $14, $15, $16, CURRENT_TIMESTAMP)
			`, lineID, tenantID, invoiceUUID, tr.ID, desc,
				tr.Tolls, tr.Tolls, tr.Tolls,
				tTaxable, cgstRate, sgstRate, igstRate,
				tCGST, tSGST, tIGST, tTotal)
			if err != nil {
				return nil, &ConsolidationError{Stage: ConsolidationStageInsertToll, Err: err}
			}

			sumTaxable += tTaxable
			sumCGST += tCGST
			sumSGST += tSGST
			sumIGST += tIGST
		}

		// 3. Detention Charges Line Items
		dets, detErr := uc.tripDetentions(ctx, tx, tr.ID, tenantID)
		if detErr == nil && len(dets) > 0 {
			for j := range dets {
				d := &dets[j]
				dTaxable := d.amt
				dCGST := round2(dTaxable * cgstRate / 100.0)
				dSGST := round2(dTaxable * sgstRate / 100.0)
				dIGST := round2(dTaxable * igstRate / 100.0)
				dTotal := round2(dTaxable + dCGST + dSGST + dIGST)

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
					) VALUES ($1, $2, $3, $4, 'detention', $5, $6, $7, $8, $9, '996511', 'HRS', $10, $11, $12, $13, $14, $15, $16, $17, $18, CURRENT_TIMESTAMP)
				`, lineID, tenantID, invoiceUUID, tr.ID, desc,
					hrs, d.rate, d.amt, d.id, d.rate,
					dTaxable, cgstRate, sgstRate, igstRate,
					dCGST, dSGST, dIGST, dTotal)
				if err != nil {
					return nil, &ConsolidationError{Stage: ConsolidationStageInsertDetention, Err: err}
				}

				// Mark detention record as attached
				_, _ = tx.ExecContext(ctx, `
					UPDATE trip_detentions SET status = 'attached', updated_at = CURRENT_TIMESTAMP
					WHERE id = $1 AND tenant_id = $2
				`, d.id, tenantID)

				sumTaxable += dTaxable
				sumCGST += dCGST
				sumSGST += dSGST
				sumIGST += dIGST
			}
		} else if tr.Detention > 0 {
			dTaxable := tr.Detention
			dCGST := round2(dTaxable * cgstRate / 100.0)
			dSGST := round2(dTaxable * sgstRate / 100.0)
			dIGST := round2(dTaxable * igstRate / 100.0)
			dTotal := round2(dTaxable + dCGST + dSGST + dIGST)

			desc := fmt.Sprintf("Detention Charges: Trip %s", tr.TripNumber)
			lineID := uuid.NewString()
			_, _ = tx.ExecContext(ctx, `
				INSERT INTO invoice_line_items (
					id, tenant_id, invoice_id, trip_id, line_type, description,
					quantity, unit_price, amount, hsn_sac_code, unit, rate,
					taxable_value, cgst_rate, sgst_rate, igst_rate,
					cgst_amount, sgst_amount, igst_amount, total, created_at
				) VALUES ($1, $2, $3, $4, 'detention', $5, 1, $6, $7, '996511', 'NOS', $8, $9, $10, $11, $12, $13, $14, $15, $16, CURRENT_TIMESTAMP)
			`, lineID, tenantID, invoiceUUID, tr.ID, desc,
				tr.Detention, tr.Detention, tr.Detention,
				dTaxable, cgstRate, sgstRate, igstRate,
				dCGST, dSGST, dIGST, dTotal)

			sumTaxable += dTaxable
			sumCGST += dCGST
			sumSGST += dSGST
			sumIGST += dIGST
		}
	}

	totalTax := round2(sumCGST + sumSGST + sumIGST)
	finalTotal := round2(sumTaxable + totalTax)

	// Update invoice header with reconciled totals
	_, err = tx.ExecContext(ctx, `
		UPDATE invoices
		SET subtotal = $1, tax = $2, cgst = $3, sgst = $4, igst = $5, total = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $7 AND tenant_id = $8
	`, sumTaxable, totalTax, sumCGST, sumSGST, sumIGST, finalTotal, invoiceUUID, tenantID)
	if err != nil {
		return nil, &ConsolidationError{Stage: ConsolidationStageUpdateTotals, Err: err}
	}

	if err := tx.Commit(); err != nil {
		return nil, &ConsolidationError{Stage: ConsolidationStageCommit, Err: err}
	}

	return &ConsolidatedInvoice{
		InvoiceID:     invoiceUUID,
		InvoiceNumber: invoiceNumber,
		Subtotal:      sumTaxable,
		Tax:           totalTax,
		CGST:          sumCGST,
		SGST:          sumSGST,
		IGST:          sumIGST,
		Total:         finalTotal,
		DueDate:       dueDate,
	}, nil
}

// resolveDueDate applies the legacy 100% dynamic credit-policy precedence:
// direct due-date override, payment-terms override, per-customer terms, then
// the company default (billing.default_payment_terms_days, best effort).
func (uc *ConsolidateInvoicesUseCase) resolveDueDate(ctx context.Context, cmd ConsolidateInvoicesCommand, invoiceDate time.Time) time.Time {
	var dueDate time.Time
	if cmd.DueDateOverride != "" {
		if parsed, err := time.Parse("2006-01-02", cmd.DueDateOverride); err == nil {
			dueDate = parsed
		} else if parsed, err := time.Parse("2006-01-02 15:04:05", cmd.DueDateOverride); err == nil {
			dueDate = parsed
		}
	}
	if !dueDate.IsZero() {
		return dueDate
	}

	termsDays := cmd.Customer.PaymentTermsDays
	if cmd.PaymentTermsOverride != nil {
		termsDays = *cmd.PaymentTermsOverride
	} else if termsDays <= 0 {
		// Company config default when the customer has no explicit override.
		if defaultTerms, ok := NewCustomerBillingQueries(uc.db).DefaultPaymentTermsDays(ctx, cmd.TenantID); ok {
			termsDays = defaultTerms
		}
	}
	if termsDays < 0 {
		termsDays = 0
	}
	return invoiceDate.AddDate(0, 0, termsDays)
}

// consolidationGSTRates derives the intra/inter-state split exactly like the
// legacy handler: supplier state defaults to 27 / the bootstrap GSTIN, the
// customer state comes from its GSTIN prefix else its state code, and the
// company GST toggle/rate govern whether tax applies at all.
func consolidationGSTRates(cust *customer.Customer, settings ConsolidationSettings) (cgstRate, sgstRate, igstRate float64) {
	supplierState := "27"
	supplierGSTIN := "27AABCU9603R1ZX"
	if settings.StateCode != "" {
		supplierState = settings.StateCode
	}
	if settings.GSTNumber != nil && *settings.GSTNumber != "" {
		supplierGSTIN = *settings.GSTNumber
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
	if settings.GSTEnabled {
		gstRate = settings.GSTRate
		if gstRate <= 0 {
			gstRate = 18.0
		}
	}

	if gstRate > 0 {
		if intraState {
			cgstRate = gstRate / 2.0
			sgstRate = gstRate / 2.0
		} else {
			igstRate = gstRate
		}
	}
	return cgstRate, sgstRate, igstRate
}

// tripDetentions lists the attachable detention rows of one trip inside the
// consolidation transaction (waived and zero-amount rows are excluded).
func (uc *ConsolidateInvoicesUseCase) tripDetentions(ctx context.Context, tx *sql.Tx, tripID, tenantID string) ([]detentionItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, zone_kind, billable_seconds, rate_per_hour, amount
		FROM trip_detentions
		WHERE trip_id = $1 AND tenant_id = $2 AND status != 'waived' AND amount > 0
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
