package shared

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The fleet default is Asia/Kolkata: an IST calendar day starts 5.5h before
// the UTC day boundary, and that offset is exactly what the old
// substr(col,1,10) trick misreported.
func TestDayBoundsUTC_FleetDayIsNotTheUTCDay(t *testing.T) {
	from, to := DayBoundsUTC("2026-08-10", "2026-08-10")
	// IST midnight 2026-08-10 = 2026-08-09 18:30:00 UTC.
	assert.Equal(t, "2026-08-09 18:30:00", from)
	// IST 23:59:59 finishes at 2026-08-10 18:29:59 UTC.
	assert.Equal(t, "2026-08-10 18:29:59", to)
}

func TestDayBoundsUTC_OpenBoundsStayOpen(t *testing.T) {
	from, to := DayBoundsUTC("", "2026-08-10")
	assert.Equal(t, "", from)
	assert.Equal(t, "2026-08-10 18:29:59", to)

	from, to = DayBoundsUTC("2026-08-10", "")
	assert.Equal(t, "2026-08-09 18:30:00", from)
	assert.Equal(t, "", to)

	from, to = DayBoundsUTC("garbage", "")
	assert.Equal(t, "", from)
	assert.Equal(t, "", to)
}
