package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gated-off orgs keep no recomputed scores; gated-on orgs compute normally.
// min_events default would mark thin drivers insufficient but still writes
// the history row — absence of any row proves the skip.
func TestScorecard_GatedOrgSkipped(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := context.Background()

	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('da','D-A','Arun','A','9900000001','SC-A','2028-01-01','available','tenant-a'),
		       ('db','D-B','Balu','B','9900000002','SC-B','2028-01-01','available','tenant-b')`)
	require.NoError(t, err)
	seedBehaviour(t, db, "ev-a1", "da", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "ev-b1", "db", "idling", "low", 3, scorecardTestNow(), "")

	svcs.Scorecard.WithFeatureGate(func(tenantID string) bool { return tenantID == "tenant-a" })

	_, err = svcs.Scorecard.RecomputeDriverScore(ctx, "da")
	require.NoError(t, err)
	_, err = svcs.Scorecard.RecomputeDriverScore(ctx, "db")
	require.NoError(t, err)

	var aCount, bCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_scores WHERE driver_id = 'da'`).Scan(&aCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_scores WHERE driver_id = 'db'`).Scan(&bCount))
	assert.Equal(t, 1, aCount, "gated-on org computes")
	assert.Equal(t, 0, bCount, "gated-off org skipped")
}
