package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
	"transport-app/internal/shared/gstin"
)

// ErrDuplicateCustomerEmail is returned when a customer update would use an
// email already assigned to another customer.
var ErrDuplicateCustomerEmail = errors.New("customer with this email already exists")

// CustomerService handles customer management.
type CustomerService struct {
	baseService
}

// CreateCustomer creates a new customer.
// Fleetbase parity: supports title, contact_person, internal_id, photo_url, place_uuid, meta,
// billing_address, type, status, payment_terms_days, tenant scoping, auto customer_code.
func (s *CustomerService) CreateCustomer(ctx context.Context, name, company, phone, email, gst, address, notes string) (domain.Customer, error) {
	return s.CreateCustomerFull(ctx, CreateCustomerRequest{
		Name:    name,
		Company: company,
		Phone:   phone,
		Email:   email,
		GST:     gst,
		Address: address,
		Notes:   notes,
	})
}

// CreateCustomerRequest is fleetbase-grade create payload (all optional except name/phone).
type CreateCustomerRequest struct {
	CustomerCode     string
	Name             string
	Title            string
	Company          string
	ContactPerson    string
	Phone            string
	Email            string
	GST              string
	Address          string
	BillingAddress   string
	InternalID       string
	PhotoURL         string
	PlaceUUID        string
	Meta             string // JSON
	Type             string
	Status           string
	PaymentTermsDays int
	Notes            string
}

func (s *CustomerService) CreateCustomerFull(ctx context.Context, req CreateCustomerRequest) (domain.Customer, error) {
	name := strings.TrimSpace(req.Name)
	phone := strings.TrimSpace(req.Phone)
	if name == "" || phone == "" {
		return domain.Customer{}, fmt.Errorf("name and phone are required")
	}

	// Normalize & validate GSTIN
	gst := gstin.Normalize(req.GST)
	if gst != "" && !gstin.Valid(gst) {
		return domain.Customer{}, fmt.Errorf("invalid GSTIN %q: expected 15 chars like 27ABCDE1234F1Z5", gst)
	}

	// Normalize type/status (defaults match migration)
	custType := strings.ToLower(strings.TrimSpace(req.Type))
	if custType == "" {
		custType = "customer"
	}
	// Map legacy ARCH values to fleetbase-compatible enum
	if custType == "individual" {
		custType = "customer"
	}
	validTypes := map[string]bool{"individual": true, "company": true, "customer": true, "supplier": true, "facilitator": true, "contact": true}
	if !validTypes[custType] {
		return domain.Customer{}, fmt.Errorf("invalid customer type %q", req.Type)
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "inactive" {
		return domain.Customer{}, fmt.Errorf("invalid status %q", req.Status)
	}

	if req.PaymentTermsDays < 0 || req.PaymentTermsDays > 365 {
		return domain.Customer{}, fmt.Errorf("payment_terms_days must be 0..365")
	}

	// Meta must be valid JSON if provided
	meta := strings.TrimSpace(req.Meta)
	if meta == "" {
		meta = "{}"
	}
	if !json.Valid([]byte(meta)) {
		return domain.Customer{}, fmt.Errorf("meta must be valid JSON")
	}

	// Tenant scoping — fail-closed when tenant is set, fallback to DefaultTenant
	// for single-tenant / test contexts (background context). Lint enforces awareness.
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	// Check phone uniqueness (tenant-scoped at app layer; DB index is global)
	if _, err := s.store.GetCustomerByPhone(ctx, phone); err == nil {
		return domain.Customer{}, fmt.Errorf("customer with phone number %s already exists", phone)
	}

	// Auto-generate customer_code if not supplied
	custCode := strings.TrimSpace(req.CustomerCode)
	if custCode == "" {
		// CUST- + 8 hex of generated id prefix for uniqueness
		idHint := generateID()
		custCode = "CUST-" + strings.ToUpper(idHint[:8])
		// ensure unique (retry once on collision)
		if _, err := s.store.SearchCustomers(ctx, custCode, 1, 0); err == nil {
			// SearchCustomers returns slice; if found, suffix random
			custCode = custCode + "-" + strings.ToUpper(generateID()[:4])
		}
	}

	// Derive state_code from GSTIN first 2 digits (e-invoice place_of_supply)
	var stateCode *string
	if gst != "" && len(gst) >= 2 {
		sc := gst[:2]
		if sc >= "01" && sc <= "38" {
			stateCode = &sc
		}
	}
	customer := domain.Customer{
		ID:               domain.CustomerID(generateID()),
		CustomerCode:     custCode,
		Name:             name,
		Title:            strPtr(req.Title),
		Company:          strPtr(req.Company),
		ContactPerson:    strPtr(req.ContactPerson),
		Phone:            phone,
		Email:            strPtr(req.Email),
		GST:              strPtr(gst),
		Address:          strPtr(req.Address),
		BillingAddress:   strPtr(req.BillingAddress),
		InternalID:       strPtr(req.InternalID),
		PhotoURL:         strPtr(req.PhotoURL),
		PlaceUUID:        strPtr(req.PlaceUUID),
		Meta:             meta,
		Type:             custType,
		Status:           status,
		PaymentTermsDays: req.PaymentTermsDays,
		TenantID:         tenantID,
		StateCode:        stateCode,
		Notes:            strPtr(req.Notes),
	}
	if err := customer.Validate(); err != nil {
		return domain.Customer{}, err
	}

	created, err := s.store.CreateCustomer(ctx, customer)
	if err != nil {
		return domain.Customer{}, err
	}

	s.log.Info("customer created", "customer_id", created.ID, "customer_code", created.CustomerCode)
	return created, nil
}

// GetCustomer retrieves a customer by ID.
func (s *CustomerService) GetCustomer(ctx context.Context, id domain.CustomerID) (domain.Customer, error) {
	return s.store.GetCustomerByID(ctx, id)
}

// ListCustomers retrieves customers with search and pagination.
func (s *CustomerService) ListCustomers(ctx context.Context, query string, limit, offset int) ([]domain.Customer, int64, error) {
	customers, err := s.store.SearchCustomers(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountCustomers(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	return customers, total, nil
}

// UpdateCustomer updates an existing customer (legacy 8-arg form).
func (s *CustomerService) UpdateCustomer(ctx context.Context, id domain.CustomerID, name, company, phone, email, gst, address, notes string) (domain.Customer, error) {
	return s.UpdateCustomerFull(ctx, id, UpdateCustomerRequest{
		Name:    name,
		Company: company,
		Phone:   phone,
		Email:   email,
		GST:     gst,
		Address: address,
		Notes:   notes,
	})
}

// UpdateCustomerRequest mirrors Create but for updates (all fields optional except identity).
type UpdateCustomerRequest struct {
	Name             string
	Title            string
	Company          string
	ContactPerson    string
	Phone            string
	Email            string
	GST              string
	Address          string
	BillingAddress   string
	InternalID       string
	PhotoURL         string
	PlaceUUID        string
	Meta             string
	Type             string
	Status           string
	PaymentTermsDays *int
	CustomerCode     string
	Notes            string
	_present         map[string]bool
}

func (s *CustomerService) UpdateCustomerFull(ctx context.Context, id domain.CustomerID, req UpdateCustomerRequest) (domain.Customer, error) {
	customer, err := s.store.GetCustomerByID(ctx, id)
	if err != nil {
		return domain.Customer{}, domain.ErrCustomerNotFound
	}

	// Merge: keep existing if req field empty (except explicit clears handled via strPtr empty→nil)
	name := customer.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	phone := customer.Phone
	if strings.TrimSpace(req.Phone) != "" {
		phone = strings.TrimSpace(req.Phone)
	}

	var gst string
	if req.HasField("gst") {
		gst = gstin.Normalize(req.GST)
	} else if customer.GST != nil {
		gst = gstin.Normalize(*customer.GST)
	} else {
		gst = ""
	}
	if gst != "" && !gstin.Valid(gst) {
		return domain.Customer{}, fmt.Errorf("invalid GSTIN %q: expected 15 chars like 27ABCDE1234F1Z5", gst)
	}

	// Check phone uniqueness for other customers
	if existing, err := s.store.GetCustomerByPhone(ctx, phone); err == nil && existing.ID != id {
		return domain.Customer{}, fmt.Errorf("customer with phone number %s already exists", phone)
	}

	// Check email uniqueness for other customers when the email is being changed
	if req.Email != "" && (customer.Email == nil || !strings.EqualFold(*customer.Email, req.Email)) {
		matches, err := s.store.SearchCustomers(ctx, req.Email, 1000, 0)
		if err != nil {
			return domain.Customer{}, err
		}
		for _, c := range matches {
			if c.ID != id && c.Email != nil && strings.EqualFold(*c.Email, req.Email) {
				return domain.Customer{}, ErrDuplicateCustomerEmail
			}
		}
	}

	// Type/Status validation if supplied
	if req.Type != "" {
		t := strings.ToLower(strings.TrimSpace(req.Type))
		if t == "individual" {
			t = "customer"
		}
		valid := map[string]bool{"individual": true, "company": true, "customer": true, "supplier": true, "facilitator": true, "contact": true}
		if !valid[t] {
			return domain.Customer{}, fmt.Errorf("invalid type %q", req.Type)
		}
		customer.Type = t
	}
	if req.Status != "" {
		ss := strings.ToLower(strings.TrimSpace(req.Status))
		if ss != "active" && ss != "inactive" {
			return domain.Customer{}, fmt.Errorf("invalid status %q", req.Status)
		}
		customer.Status = ss
	}
	if req.PaymentTermsDays != nil {
		if *req.PaymentTermsDays < 0 || *req.PaymentTermsDays > 365 {
			return domain.Customer{}, fmt.Errorf("payment_terms_days must be 0..365")
		}
		customer.PaymentTermsDays = *req.PaymentTermsDays
	}
	if strings.TrimSpace(req.Meta) != "" {
		if !json.Valid([]byte(req.Meta)) {
			return domain.Customer{}, fmt.Errorf("meta must be valid JSON")
		}
		customer.Meta = req.Meta
	}
	if strings.TrimSpace(req.CustomerCode) != "" {
		customer.CustomerCode = strings.TrimSpace(req.CustomerCode)
	}

	customer.Name = name
	customer.Company = mergeStrPtr(customer.Company, req.Company, req.HasField("company"))
	customer.ContactPerson = mergeStrPtr(customer.ContactPerson, req.ContactPerson, req.HasField("contact_person"))
	customer.BillingAddress = mergeStrPtr(customer.BillingAddress, req.BillingAddress, req.HasField("billing_address"))
	customer.InternalID = mergeStrPtr(customer.InternalID, req.InternalID, req.HasField("internal_id"))
	customer.PhotoURL = mergeStrPtr(customer.PhotoURL, req.PhotoURL, req.HasField("photo_url"))
	customer.PlaceUUID = mergeStrPtr(customer.PlaceUUID, req.PlaceUUID, req.HasField("place_uuid"))
	customer.Title = mergeStrPtr(customer.Title, req.Title, req.HasField("title"))

	customer.Phone = phone
	customer.Email = mergeStrPtr(customer.Email, req.Email, req.HasField("email"))
	customer.GST = strPtr(gst)
	// Keep state_code in sync with GST
	if gst != "" && len(gst) >= 2 {
		sc := gst[:2]
		if sc >= "01" && sc <= "38" {
			customer.StateCode = &sc
		} else {
			customer.StateCode = nil
		}
	} else if gst == "" {
		customer.StateCode = nil
	}
	customer.Address = mergeStrPtr(customer.Address, req.Address, req.HasField("address"))
	customer.Notes = mergeStrPtr(customer.Notes, req.Notes, req.HasField("notes"))
	if err := customer.Validate(); err != nil {
		return domain.Customer{}, err
	}

	updated, err := s.store.UpdateCustomer(ctx, customer)
	if err != nil {
		return domain.Customer{}, err
	}

	s.log.Info("customer updated", "customer_id", id)
	return updated, nil
}

// HasField checks if request had explicit field (non-zero string sentinel via meta map). Simplified: treat empty string as not set unless caller used full form where empty means clear.
// For backward compat we expose explicit check via non-empty or we track via separate presence map — here we approximate with != "" OR caller used UpdateCustomerFull where empty means clear if field was intentionally blank.
// To avoid breaking legacy 8-arg callers (which pass empty for unset), we only clear if HasField true via presence map populated by handler.
// This helper is used when handler populates presence via request map.
func (r UpdateCustomerRequest) HasField(name string) bool {
	// Presence is inferred: if the field was set in the HTTP form, handler will set presence map.
	// We store presence via an internal map set by handlers; for service-direct calls, treat non-empty as present.
	// To keep simple, return true only if the corresponding struct field was explicitly set to non-empty OR handler marked it.
	// This is overridden by handler's withPresence helper below — see customer_handlers presence logic.
	switch name {
	case "title":
		return r.Title != "" || r._present["title"]
	case "company":
		return r.Company != "" || r._present["company"]
	case "contact_person":
		return r.ContactPerson != "" || r._present["contact_person"]
	case "billing_address":
		return r.BillingAddress != "" || r._present["billing_address"]
	case "internal_id":
		return r.InternalID != "" || r._present["internal_id"]
	case "photo_url":
		return r.PhotoURL != "" || r._present["photo_url"]
	case "place_uuid":
		return r.PlaceUUID != "" || r._present["place_uuid"]
	case "email":
		return r.Email != "" || r._present["email"]
	case "address":
		return r.Address != "" || r._present["address"]
	case "notes":
		return r.Notes != "" || r._present["notes"]
	case "customer_code":
		return r.CustomerCode != "" || r._present["customer_code"]
	case "gst":
		return r.GST != "" || r._present["gst"]
	}
	return false
}

// SetPresent marks a field as explicitly present (even if empty string — means clear).
func (r *UpdateCustomerRequest) SetPresent(k string) {
	if r._present == nil {
		r._present = map[string]bool{}
	}
	r._present[k] = true
}

// Unexported presence holder — keep after exported fields for JSON compat.
func mergeStrPtr(existing *string, incoming string, present bool) *string {
	if !present {
		return existing
	}
	return strPtr(incoming)
}

// DeleteCustomer deletes a customer.
func (s *CustomerService) DeleteCustomer(ctx context.Context, id domain.CustomerID) error {
	if err := s.store.DeleteCustomer(ctx, id); err != nil {
		return err
	}
	s.log.Info("customer deleted", "customer_id", id)
	return nil
}
