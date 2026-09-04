package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/repository"
)

func TestZeroFillSeries(t *testing.T) {
	today := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	rev := ZeroFillRevenueByDay([]repository.RevenueByDay{
		{Day: "2026-09-04", Total: 100.0},
		{Day: "2026-09-04", Total: 50.0},  // duplicate days sum
		{Day: "2026-08-01", Total: 999.0}, // outside window dropped
	}, today)
	require.Len(t, rev, 30)
	assert.Equal(t, "2026-08-06", rev[0].Day)
	assert.Equal(t, "2026-09-04", rev[29].Day)
	assert.EqualValues(t, 0, rev[0].Total, "gap filled with zero")
	assert.EqualValues(t, 150, rev[29].Total, "today sums duplicates")

	bk := ZeroFillBookingsByDay([]repository.BookingsByDay{
		{Day: "2026-09-03", Count: 2},
	}, today)
	require.Len(t, bk, 30)
	assert.EqualValues(t, 0, bk[29].Count)
	assert.EqualValues(t, 2, bk[28].Count)

	empty := ZeroFillRevenueByDay(nil, today)
	require.Len(t, empty, 30)
	assert.EqualValues(t, 0, empty[15].Total)
}
