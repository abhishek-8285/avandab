package customer

import (
	"errors"
	"strings"
	"time"

	"transport-app/internal/domain/types"
)

var (
	ErrInvalidCustomerCode = errors.New("customer code is required")
	ErrInvalidCompanyName  = errors.New("company name is required")
	ErrInvalidPhone        = errors.New("phone number is required")
)

// Customer represents a corporate shipper customer who books transport services.
// Fleetbase parity: title, internal_id, photo_url, place_uuid, meta overlay the ARCH model.
type Customer struct {
	ID               types.CustomerID
	CustomerCode     string
	Name             string
	Title            *string
	Company          *string
	ContactPerson    *string
	Phone            string
	Email            *string
	GST              *string
	Address          *string
	BillingAddress   *string
	InternalID       *string
	PhotoURL         *string
	PlaceUUID        *string
	Meta             string // JSON, default '{}'
	Type             string // individual|company|customer|supplier|facilitator|contact
	Status           string // active|inactive
	PaymentTermsDays int
	TenantID         string
	StateCode        *string // 2-digit GST state code derived from GSTIN, for e-invoice place_of_supply
	Notes            *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate validates the required customer fields.
func (c Customer) Validate() error {
	if strings.TrimSpace(c.CustomerCode) == "" {
		return ErrInvalidCustomerCode
	}
	if strings.TrimSpace(c.Name) == "" && (c.Company == nil || strings.TrimSpace(*c.Company) == "") {
		return ErrInvalidCompanyName
	}
	if strings.TrimSpace(c.Phone) == "" {
		return ErrInvalidPhone
	}
	if c.Type != "" && c.Type != "individual" && c.Type != "company" && c.Type != "customer" && c.Type != "supplier" && c.Type != "facilitator" && c.Type != "contact" {
		return errors.New("invalid customer type")
	}
	if c.Status != "" && c.Status != "active" && c.Status != "inactive" {
		return errors.New("invalid customer status")
	}
	return nil
}

// AllowedTypes is Fleetbase contact types + legacy ARCH types.
var AllowedTypes = []string{"individual", "company", "customer", "supplier", "facilitator", "contact"}

var AllowedStatuses = []string{"active", "inactive"}
