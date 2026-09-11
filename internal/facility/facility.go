package facility

import (
	"errors"
	"strings"
	"time"
)

type FacilityType string

const (
	FacilityTypeDepot       FacilityType = "depot"
	FacilityTypeHub         FacilityType = "hub"
	FacilityTypeBranch      FacilityType = "branch"
	FacilityTypeWorkshop    FacilityType = "workshop"
	FacilityTypeOffice      FacilityType = "office"
	FacilityTypeFuelStation FacilityType = "fuel_station"
)

func IsValidFacilityType(t FacilityType) bool {
	switch t {
	case FacilityTypeDepot, FacilityTypeHub, FacilityTypeBranch,
		FacilityTypeWorkshop, FacilityTypeOffice, FacilityTypeFuelStation:
		return true
	default:
		return false
	}
}

type Facility struct {
	ID           string       `json:"id"`
	TenantID     string       `json:"tenant_id"`
	FacilityCode string       `json:"facility_code"`
	Name         string       `json:"name"`
	FacilityType FacilityType `json:"facility_type"`
	Plant        string       `json:"plant"`
	Circle       string       `json:"circle"`
	ProfitCenter string       `json:"profit_center"`
	CostCenter   string       `json:"cost_center"`
	Address      string       `json:"address"`
	City         string       `json:"city"`
	State        string       `json:"state"`
	Pincode      string       `json:"pincode"`
	Latitude     *float64     `json:"latitude,omitempty"`
	Longitude    *float64     `json:"longitude,omitempty"`
	ValidFrom    *time.Time   `json:"valid_from,omitempty"`
	ValidTo      *time.Time   `json:"valid_to,omitempty"`
	IsActive     bool         `json:"is_active"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type CreateFacilityInput struct {
	FacilityCode string       `json:"facility_code"`
	Name         string       `json:"name"`
	FacilityType FacilityType `json:"facility_type"`
	Plant        string       `json:"plant"`
	Circle       string       `json:"circle"`
	ProfitCenter string       `json:"profit_center"`
	CostCenter   string       `json:"cost_center"`
	Address      string       `json:"address"`
	City         string       `json:"city"`
	State        string       `json:"state"`
	Pincode      string       `json:"pincode"`
	Latitude     *float64     `json:"latitude,omitempty"`
	Longitude    *float64     `json:"longitude,omitempty"`
	ValidFrom    *time.Time   `json:"valid_from,omitempty"`
	ValidTo      *time.Time   `json:"valid_to,omitempty"`
	IsActive     *bool        `json:"is_active,omitempty"`
}

func (in *CreateFacilityInput) Validate() error {
	in.FacilityCode = strings.TrimSpace(in.FacilityCode)
	if in.FacilityCode == "" {
		return errors.New("facility_code is required")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if in.FacilityType == "" {
		in.FacilityType = FacilityTypeDepot
	}
	if !IsValidFacilityType(in.FacilityType) {
		return errors.New("invalid facility_type: must be depot, hub, branch, workshop, office, or fuel_station")
	}
	return nil
}

type UpdateFacilityInput struct {
	Name         string       `json:"name"`
	FacilityType FacilityType `json:"facility_type"`
	Plant        string       `json:"plant"`
	Circle       string       `json:"circle"`
	ProfitCenter string       `json:"profit_center"`
	CostCenter   string       `json:"cost_center"`
	Address      string       `json:"address"`
	City         string       `json:"city"`
	State        string       `json:"state"`
	Pincode      string       `json:"pincode"`
	Latitude     *float64     `json:"latitude,omitempty"`
	Longitude    *float64     `json:"longitude,omitempty"`
	ValidFrom    *time.Time   `json:"valid_from,omitempty"`
	ValidTo      *time.Time   `json:"valid_to,omitempty"`
	IsActive     *bool        `json:"is_active,omitempty"`
}

func (in *UpdateFacilityInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if in.FacilityType == "" {
		in.FacilityType = FacilityTypeDepot
	}
	if !IsValidFacilityType(in.FacilityType) {
		return errors.New("invalid facility_type: must be depot, hub, branch, workshop, office, or fuel_station")
	}
	return nil
}

type FacilityFilter struct {
	Search       string
	FacilityType string
	ActiveOnly   *bool
	Limit        int
	Offset       int
}
