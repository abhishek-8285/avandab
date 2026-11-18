package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Regression: legacy calculateTDS must round to 2dp like settlement.domain.CalculateSettlement.
func TestCalculateTDS_RoundsToTwoDecimals(t *testing.T) {
	name := fmt.Sprintf("tds_round_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE drivers (id TEXT PRIMARY KEY, pan TEXT);
		CREATE TABLE company_config (key TEXT, value TEXT);
	`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, pan) VALUES ('drv-tds', 'ABCDE1234F')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_config (key, value) VALUES ('tds_rate_with_pan', '1.0')`)
	require.NoError(t, err)

	s := &DriverSettlementService{}
	rate, amount, section, err := s.calculateTDS(context.Background(), db, "drv-tds", 12345.67)
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
	assert.Equal(t, "194C", section)
	assert.Equal(t, 123.46, amount, "12345.67 @ 1%% must round to 123.46, not 123.4567")
}
