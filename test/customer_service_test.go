package test

import (
	"context"
	"testing"
	"transport-app/internal/shared"

	"github.com/stretchr/testify/require"
)

func TestCustomerService_Validations(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// 1. Create a customer successfully
	c1, err := services.Customers.CreateCustomer(
		ctx,
		"Alice Company",
		"Alice Corp",
		"9876543210",
		"alice@corp.com",
		"27AAPCA1234A1Z5",
		"123 Alice St",
		"Primary customer",
	)
	require.NoError(t, err)
	require.NotEmpty(t, c1.ID)

	// 2. Try to create another customer with the same phone (should fail)
	_, err = services.Customers.CreateCustomer(
		ctx,
		"Duplicate Phone Customer",
		"Dup Corp",
		"9876543210",
		"dup@corp.com",
		"",
		"",
		"",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "customer with phone number 9876543210 already exists")

	// 3. Create a second customer successfully with a unique phone
	c2, err := services.Customers.CreateCustomer(
		ctx,
		"Bob Company",
		"Bob Corp",
		"1234567890",
		"bob@corp.com",
		"",
		"",
		"",
	)
	require.NoError(t, err)
	require.NotEmpty(t, c2.ID)

	// 4. Update Bob's customer entry to use Alice's phone number (should fail)
	_, err = services.Customers.UpdateCustomer(
		ctx,
		c2.ID,
		"Bob Company",
		"Bob Corp",
		"9876543210",
		"bob@corp.com",
		"",
		"",
		"",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "customer with phone number 9876543210 already exists")

	// 5. Update Bob's customer entry to keep his own phone number or update name (should succeed)
	updated, err := services.Customers.UpdateCustomer(
		ctx,
		c2.ID,
		"Bob Updated Name",
		"Bob Corp",
		"1234567890",
		"bob@corp.com",
		"",
		"",
		"",
	)
	require.NoError(t, err)
	require.Equal(t, "Bob Updated Name", updated.Name)
}
