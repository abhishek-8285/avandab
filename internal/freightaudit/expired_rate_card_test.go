package freightaudit_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/freightaudit"
)

// Expired-but-active rate cards must not be selected by the matching lookup.
func TestFindMatchingRateCard_ExcludesExpiredCard(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	repo := freightaudit.NewRepository(db)
	ctx := context.Background()

	past := time.Now().UTC().Add(-30 * 24 * time.Hour)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	now := time.Now().UTC()

	insert := func(id, contract string, effFrom, effTo time.Time, createdAt time.Time) {
		_, err := db.Exec(`
			INSERT INTO freight_rate_cards
				(id, tenant_id, contract_number, name, route_id, vehicle_type, billing_basis,
				 rate, effective_from, effective_to, is_active, created_at, updated_at)
			VALUES (?, 't-fa-1', ?, ?, 'route-del-mum', '32FT', 'PER_KM', ?, ?, ?, 1, ?, ?)`,
			id, contract, contract, 50.0, effFrom, effTo, createdAt, createdAt)
		require.NoError(t, err)
	}

	// Expired card is newest, so an unguarded ORDER BY created_at DESC picks it.
	insert("rc-expired", "RC-EXPIRED", past, past, now)
	insert("rc-valid", "RC-VALID", past, future, past)

	matched, err := repo.FindMatchingRateCard(ctx, "t-fa-1", "route-del-mum", "32FT")
	require.NoError(t, err)
	assert.Equal(t, "rc-valid", matched.ID, "expired rate card must not be selected")
}
