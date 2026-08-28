package test

import (
	"context"
	"testing"
	"transport-app/internal/shared"

	"github.com/stretchr/testify/require"
)

func TestRouteService_Validations(t *testing.T) {
	db := NewTestDB(t)
	services := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// 1. Invalid positive values (should fail)
	_, err := services.Routes.CreateRoute(ctx, "Mumbai", "Pune", -50, 2.5, 3000, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "distance must be greater than zero")

	_, err = services.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, -2.5, 3000, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "estimated hours must be greater than zero")

	_, err = services.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 2.5, -3000, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "base fare must be greater than zero")

	// 2. Successful creation
	r1, err := services.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 2.5, 3000, "Regular route")
	require.NoError(t, err)
	require.NotEmpty(t, r1.ID)

	// 3. Duplicate route (should fail)
	_, err = services.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 2.5, 3000, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "route from Mumbai to Pune already exists")
}
