package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"transport-app/internal/service"
)

func TestCustomerFleetbaseParity_CreateFull(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := context.Background()

	pts := 30
	c, err := services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name:             "Fleetbase Co",
		Title:            "M/s",
		Company:          "Fleetbase Logistics Pvt Ltd",
		ContactPerson:    "Amit Sharma",
		Phone:            "9876500010",
		Email:            "billing@fleetbase.example",
		GST:              "27AAPCA1234A1Z5",
		Address:          "12, Industrial Estate, Pune",
		BillingAddress:   "GST Billing, Pune - 411001",
		InternalID:       "ERP-1001",
		PhotoURL:         "https://example.com/logo.png",
		PlaceUUID:        "",
		Meta:             `{"loyalty_tier":"gold","account_no":"ACC-123"}`,
		Type:             "customer",
		Status:           "active",
		PaymentTermsDays: pts,
		Notes:            "Fleetbase parity test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, c.CustomerCode, "customer_code auto-generated")
	require.NotEmpty(t, c.ID)
	require.Equal(t, "Fleetbase Co", c.Name)
	require.NotNil(t, c.Title)
	require.Equal(t, "M/s", *c.Title)
	require.NotNil(t, c.ContactPerson)
	require.Equal(t, "Amit Sharma", *c.ContactPerson)
	require.NotNil(t, c.BillingAddress)
	require.Equal(t, "GST Billing, Pune - 411001", *c.BillingAddress)
	require.NotNil(t, c.InternalID)
	require.Equal(t, "ERP-1001", *c.InternalID)
	require.Equal(t, `{"loyalty_tier":"gold","account_no":"ACC-123"}`, c.Meta)
	require.Equal(t, "customer", c.Type)
	require.Equal(t, "active", c.Status)
	require.Equal(t, 30, c.PaymentTermsDays)
	require.Equal(t, "1", c.TenantID)

	// Retrieval preserves fields
	fetched, err := services.Customers.GetCustomer(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, c.CustomerCode, fetched.CustomerCode)
	require.Equal(t, c.InternalID, fetched.InternalID)
	require.Equal(t, c.Meta, fetched.Meta)
	require.Equal(t, c.BillingAddress, fetched.BillingAddress)

	// Search by code and contact_person and internal_id (fleetbase query expansion)
	list, total, err := services.Customers.ListCustomers(ctx, c.CustomerCode, 10, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, int64(1))
	found := false
	for _, cc := range list {
		if cc.ID == c.ID {
			found = true
		}
	}
	require.True(t, found, "search by customer_code should find it")

	list2, _, err := services.Customers.ListCustomers(ctx, "Amit Sharma", 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, list2)

	list3, _, err := services.Customers.ListCustomers(ctx, "ERP-1001", 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, list3)
}

func TestCustomerFleetbaseParity_TypeAndMetaValidation(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := context.Background()

	// Invalid type
	_, err := services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name: "Bad Type", Phone: "9876500020", Type: "invalid_type",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid customer type")

	// Invalid status
	_, err = services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name: "Bad Status", Phone: "9876500021", Status: "archived",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid status")

	// Invalid meta JSON
	_, err = services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name: "Bad Meta", Phone: "9876500022", Meta: "{not json",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "meta must be valid JSON")

	// Payment terms out of range
	_, err = services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name: "Bad Terms", Phone: "9876500023", PaymentTermsDays: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "payment_terms_days")

	// Valid supplier type (fleetbase)
	sup, err := services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name: "Supplier Co", Phone: "9876500024", Type: "supplier", Status: "active",
	})
	require.NoError(t, err)
	require.Equal(t, "supplier", sup.Type)
}

func TestCustomerFleetbaseParity_UpdateFull(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := context.Background()

	base, err := services.Customers.CreateCustomer(ctx, "Update Test Co", "UT", "9876500030", "ut@example.com", "", "addr", "notes")
	require.NoError(t, err)

	newTerms := 45
	req := service.UpdateCustomerRequest{
		Company:          "Updated Corp",
		ContactPerson:    "Priya",
		BillingAddress:   "New Billing",
		InternalID:       "ERP-NEW",
		Type:             "facilitator",
		Status:           "inactive",
		PaymentTermsDays: &newTerms,
		Meta:             `{"tier":"silver"}`,
		Notes:            "updated notes",
	}
	for _, k := range []string{"company", "contact_person", "billing_address", "internal_id", "type", "status", "payment_terms_days", "meta", "notes"} {
		req.SetPresent(k)
	}
	updated, err := services.Customers.UpdateCustomerFull(ctx, base.ID, req)
	require.NoError(t, err)
	require.NotNil(t, updated.Company)
	require.Equal(t, "Updated Corp", *updated.Company)
	require.NotNil(t, updated.ContactPerson)
	require.Equal(t, "Priya", *updated.ContactPerson)
	require.NotNil(t, updated.BillingAddress)
	require.Equal(t, "New Billing", *updated.BillingAddress)
	require.NotNil(t, updated.InternalID)
	require.Equal(t, "ERP-NEW", *updated.InternalID)
	require.Equal(t, "facilitator", updated.Type)
	require.Equal(t, "inactive", updated.Status)
	require.Equal(t, 45, updated.PaymentTermsDays)
	require.Equal(t, `{"tier":"silver"}`, updated.Meta)
}

func TestCustomerFleetbaseParity_AutoCodeAndLegacyCreate(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := context.Background()

	// Legacy 7-arg still auto-generates customer_code and defaults
	c, err := services.Customers.CreateCustomer(ctx, "Legacy Co", "LC", "9876500040", "legacy@example.com", "", "addr", "")
	require.NoError(t, err)
	require.NotEmpty(t, c.CustomerCode)
	require.Equal(t, "customer", c.Type)
	require.Equal(t, "active", c.Status)
	require.Equal(t, "1", c.TenantID)
}

func TestCustomerFleetbaseParity_StateCodeDerived(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := context.Background()

	c, err := services.Customers.CreateCustomerFull(ctx, service.CreateCustomerRequest{
		Name:  "State Co",
		Phone: "9876500050",
		GST:   "27AAPCA1234A1Z5",
	})
	require.NoError(t, err)
	require.NotNil(t, c.StateCode)
	require.Equal(t, "27", *c.StateCode)

	// Update GST changes state_code
	req := service.UpdateCustomerRequest{GST: "07AAPCA1234A1Z5"}
	req.SetPresent("gst")
	updated, err := services.Customers.UpdateCustomerFull(ctx, c.ID, req)
	require.NoError(t, err)
	require.NotNil(t, updated.StateCode)
	require.Equal(t, "07", *updated.StateCode)

	// Clear GST clears state_code
	req2 := service.UpdateCustomerRequest{GST: ""}
	req2.SetPresent("gst")
	updated2, err := services.Customers.UpdateCustomerFull(ctx, c.ID, req2)
	require.NoError(t, err)
	require.Nil(t, updated2.StateCode)
}
