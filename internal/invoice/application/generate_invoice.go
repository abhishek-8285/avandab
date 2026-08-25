package application

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"sync"

	bookingdomain "transport-app/internal/booking/domain"
	bookingaggregate "transport-app/internal/booking/domain/aggregate"
	companydomain "transport-app/internal/domain/company"
	"transport-app/internal/invoice/domain"
	"transport-app/internal/invoice/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
)

const moneyEpsilon = 0.01

type PricingResolver interface {
	GetCompanySettings(ctx context.Context) (companydomain.CompanySettings, error)
}

// Per-tenant config keys mirrored from the canonical inventory (Spec 24
// §Business logic overlay). Declared locally because importing
// internal/service here would create an import cycle.
const (
	configKeyGSTEnabled = "billing.gst_enabled"
	configKeyGSTRate    = "billing.gst_rate"
)

// TenantOverlay resolves per-tenant configuration overrides with a global-
// default fallback chain (Spec 24 §Business logic overlay). Implemented by
// *service.TenantConfigReader; an optional dependency so existing
// constructors keep compiling unchanged.
type TenantOverlay interface {
	GetBool(ctx context.Context, tenantID, key string, def bool) bool
	GetFloat(ctx context.Context, tenantID, key string, def float64) float64
}

// Process-wide overlay default. SetDefaultTenantOverlay is wired ONCE from
// service.NewServices rather than threading the reader through
// NewGenerateInvoiceUseCase's 45+ call sites (handlers, facade, main,
// geofence detention, dozens of tests); instances may still override via
// SetTenantOverlay. Nil default = legacy company_settings-only behavior.
var (
	defaultOverlayMu sync.RWMutex
	defaultOverlay   TenantOverlay
)

// SetDefaultTenantOverlay registers the process-wide tenant config overlay.
func SetDefaultTenantOverlay(o TenantOverlay) {
	defaultOverlayMu.Lock()
	defer defaultOverlayMu.Unlock()
	defaultOverlay = o
}

func currentDefaultOverlay() TenantOverlay {
	defaultOverlayMu.RLock()
	defer defaultOverlayMu.RUnlock()
	return defaultOverlay
}

// SetTenantOverlay attaches a per-instance overlay (tests, alternate wiring).
func (uc *GenerateInvoiceUseCase) SetTenantOverlay(o TenantOverlay) { uc.overlay = o }

// effectiveOverlay prefers the instance overlay over the process default.
func (uc *GenerateInvoiceUseCase) effectiveOverlay() TenantOverlay {
	if uc.overlay != nil {
		return uc.overlay
	}
	return currentDefaultOverlay()
}

type derivedPricing struct {
	subtotal float64
	tax      float64
	discount float64
	total    float64
}

// GenerateInvoiceCommand contains parameters to create a new invoice.
type GenerateInvoiceCommand struct {
	TenantID   shared.TenantID
	BookingID  string
	CustomerID string
	TripID     *string
	Subtotal   float64
	Tax        float64
	Discount   float64
	Total      float64
	// LineItems are optional lines appended on create or attach (detention
	// billing, Spec 02 §6). Existing paid/partially-paid invoices are
	// returned untouched.
	LineItems []InvoiceLineItemInput
}

// InvoiceLineItemInput describes a line to attach to an invoice.
type InvoiceLineItemInput struct {
	TripID      *string
	LineType    string // freight | detention | accessorial
	Description string
	Quantity    float64 // e.g. billable hours for detention
	UnitPrice   float64 // e.g. rate per hour
	RefID       *string // source row id (trip_detentions.id) for dedupe
}

// GenerateInvoiceUseCase generates an invoice aggregate.
type GenerateInvoiceUseCase struct {
	uow   ports.UnitOfWork
	idGen ports.IDGenerator
	clock ports.Clock

	// overlay optionally overrides billing.* per tenant; nil falls back to
	// the process-wide default registered by SetDefaultTenantOverlay.
	overlay TenantOverlay
}

// NewGenerateInvoiceUseCase constructs a new GenerateInvoiceUseCase.
func NewGenerateInvoiceUseCase(uow ports.UnitOfWork, idGen ports.IDGenerator, clock ports.Clock) *GenerateInvoiceUseCase {
	return &GenerateInvoiceUseCase{uow: uow, idGen: idGen, clock: clock}
}

// Execute performs the generation and transaction commit.
func (uc *GenerateInvoiceUseCase) Execute(ctx context.Context, cmd GenerateInvoiceCommand) (aggregate.InvoiceID, error) {
	var id aggregate.InvoiceID
	err := uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		got, _, err := uc.GenerateInTx(txCtx, cmd)
		id = got
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// GenerateInTx runs generation/attachment inside an already-open transaction.
// It exists so callers (e.g. CompleteTrip) can atomically complete a trip,
// attach detention line items and mark detentions attached in one UnitOfWork
// (Spec 02 §6 — no torn states). Returns the invoice ID and whether line
// items were attached. A paid/partially-paid existing invoice is returned
// untouched (attached=false).
func (uc *GenerateInvoiceUseCase) GenerateInTx(txCtx ports.TxContext, cmd GenerateInvoiceCommand) (aggregate.InvoiceID, bool, error) {
	if cmd.BookingID == "" && cmd.TripID == nil {
		return "", false, errors.New("booking ID is required")
	}

	repo, ok := txCtx.Repositories().Invoices().(domain.InvoiceRepository)
	if !ok {
		return "", false, errors.New("failed to retrieve invoice repository")
	}

	var existing *aggregate.InvoiceAggregate
	var err error
	if cmd.BookingID != "" {
		existing, err = repo.FindByBookingID(txCtx, cmd.BookingID, cmd.TenantID)
	} else {
		existing, err = repo.FindByTripID(txCtx, *cmd.TripID, cmd.TenantID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	if existing != nil {
		if existing.PaymentStatus == aggregate.PaymentStatusPaid ||
			existing.PaymentStatus == aggregate.PaymentStatusPartiallyPaid {
			return existing.ID, false, nil
		}
		if len(cmd.LineItems) > 0 {
			attachLineItems(existing, cmd, uc.idGen)
			if err := repo.Save(txCtx, existing); err != nil {
				return "", false, err
			}
			return existing.ID, true, nil
		}
		return existing.ID, false, nil
	}

	subtotal, tax, discount, total := cmd.Subtotal, cmd.Tax, cmd.Discount, cmd.Total
	if pricing, ok := resolveBookingPricing(txCtx, cmd.TenantID, cmd.BookingID, uc.effectiveOverlay()); ok {
		subtotal, tax, discount, total = pricing.subtotal, pricing.tax, pricing.discount, pricing.total
	} else if err := validateInvoiceAmounts(subtotal, tax, discount, total); err != nil {
		return "", false, err
	}

	id := aggregate.InvoiceID(uc.idGen.GenerateUUID())
	num := uc.idGen.GenerateDisplayID("INV")

	inv := aggregate.NewInvoiceAggregate(
		id,
		cmd.TenantID,
		num,
		cmd.BookingID,
		cmd.CustomerID,
		cmd.TripID,
		subtotal,
		tax,
		discount,
		total,
		aggregate.PaymentStatusPending,
		uc.clock.Now(),
	)

	attached := false
	if len(cmd.LineItems) > 0 {
		attachLineItems(inv, cmd, uc.idGen)
		attached = true
	}

	if err := repo.Save(txCtx, inv); err != nil {
		return "", false, err
	}
	return inv.ID, attached, nil
}

// attachLineItems reconciles the booking freight with new lines: it upserts a
// freight line at the invoice's current subtotal, then appends any lines not
// already present (dedupe by ref_id), so Subtotal equals the sum of all lines
// (Spec 02 §6 gotcha #1).
func attachLineItems(inv *aggregate.InvoiceAggregate, cmd GenerateInvoiceCommand, idGen ports.IDGenerator) {
	hasFreight := false
	existingRefs := make(map[string]bool, len(inv.LineItems))
	for _, li := range inv.LineItems {
		if li.LineType == aggregate.LineTypeFreight {
			hasFreight = true
		}
		if li.RefID != nil {
			existingRefs[*li.RefID] = true
		}
	}

	if !hasFreight {
		freight := inv.Subtotal
		inv.AddLineItem(aggregate.LineItem{
			ID:          idGen.GenerateUUID(),
			TenantID:    inv.TenantID,
			InvoiceID:   inv.ID,
			TripID:      inv.TripID,
			LineType:    aggregate.LineTypeFreight,
			Description: "Freight",
			Quantity:    1,
			UnitPrice:   freight,
			Amount:      freight,
		})
	}

	for _, in := range cmd.LineItems {
		if in.RefID != nil && existingRefs[*in.RefID] {
			continue
		}
		inv.AddLineItem(aggregate.LineItem{
			ID:          idGen.GenerateUUID(),
			TenantID:    inv.TenantID,
			InvoiceID:   inv.ID,
			TripID:      in.TripID,
			LineType:    in.LineType,
			Description: in.Description,
			Quantity:    in.Quantity,
			UnitPrice:   in.UnitPrice,
			Amount:      aggregate.RoundMoney(in.Quantity * in.UnitPrice),
			RefID:       in.RefID,
		})
	}
}

// resolveBookingPricing derives invoice money from the booking price and
// company tax settings, overriding any client-supplied amounts. The optional
// overlay lets a tenant's billing.* rows win over the company_settings
// globals (Spec 24 §Business logic overlay).
func resolveBookingPricing(txCtx ports.TxContext, tenantID shared.TenantID, bookingID string, overlay TenantOverlay) (derivedPricing, bool) {
	bookingRepo, ok := txCtx.Repositories().Bookings().(bookingdomain.BookingRepository)
	if !ok {
		return derivedPricing{}, false
	}

	booking, err := bookingRepo.GetReadModel(txCtx, bookingaggregate.BookingID(bookingID), tenantID)
	if err != nil {
		return derivedPricing{}, false
	}

	subtotalMinor := int64(math.Round(booking.Price * 100))
	if subtotalMinor < 0 {
		subtotalMinor = 0
	}

	gstEnabled, gstRate := false, 0.0
	if settingsRepo, ok := txCtx.Repositories().AuditLogs().(PricingResolver); ok {
		if settings, err := settingsRepo.GetCompanySettings(txCtx); err == nil {
			gstEnabled, gstRate = settings.GSTEnabled, settings.GSTRate
		}
	}
	if overlay != nil {
		gstEnabled = overlay.GetBool(txCtx, string(tenantID), configKeyGSTEnabled, gstEnabled)
		gstRate = overlay.GetFloat(txCtx, string(tenantID), configKeyGSTRate, gstRate)
	}

	var taxMinor int64
	if gstEnabled {
		taxMinor = int64(math.Round(float64(subtotalMinor) * gstRate / 100.0))
	}

	subtotal := float64(subtotalMinor) / 100.0
	tax := float64(taxMinor) / 100.0
	var discount float64
	total := subtotal + tax - discount

	return derivedPricing{subtotal: subtotal, tax: tax, discount: discount, total: total}, true
}

// validateInvoiceAmounts rejects client-supplied money that is negative or
// arithmetically inconsistent, using a small float epsilon.
func validateInvoiceAmounts(subtotal, tax, discount, total float64) error {
	if total < 0 {
		return errors.New("total cannot be negative")
	}
	if subtotal < 0 || tax < 0 || discount < 0 {
		return errors.New("invoice amounts cannot be negative")
	}
	if math.Abs(total-(subtotal+tax-discount)) > moneyEpsilon {
		return errors.New("invoice total does not match subtotal plus tax minus discount")
	}
	return nil
}

// StateCodeFromGSTIN extracts the 2-digit state code prefix from a GSTIN.
func StateCodeFromGSTIN(gstin string) string {
	if len(gstin) < 2 {
		return ""
	}
	return strings.ToUpper(gstin[:2])
}

// IsIntraState determines if supplier and recipient are in the same state.
func IsIntraState(supplierGSTIN, recipientGSTIN string) bool {
	supState := StateCodeFromGSTIN(supplierGSTIN)
	recState := StateCodeFromGSTIN(recipientGSTIN)
	if supState == "" || recState == "" {
		return true
	}
	return supState == recState
}

// ComputeLineTax calculates line-item CGST/SGST/IGST tax split (Spec 07 §5.1).
func ComputeLineTax(taxable float64, rate float64, intraState bool) (cgst, sgst, igst float64) {
	if intraState {
		half := rate / 2.0
		cgst = round2(taxable * half / 100.0)
		sgst = round2(taxable * half / 100.0)
	} else {
		igst = round2(taxable * rate / 100.0)
	}
	return
}

func round2(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}
