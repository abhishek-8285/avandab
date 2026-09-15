package shared

import (
	"os"
	"time"
)

// FleetTimezoneEnv overrides the calendar zone operator-facing day filters
// are interpreted in (IANA name, e.g. "Asia/Kolkata").
const FleetTimezoneEnv = "FLEET_TIMEZONE"

// DefaultFleetTimezone is the calendar zone used by the list-page date
// filters. Company settings default to Asia/Kolkata
// (repository/sqlite/company_settings.go) and the ops console is an
// India-first product, so calendar days mean IST days here.
const DefaultFleetTimezone = "Asia/Kolkata"

// FleetLocation resolves the zone DayBoundsUTC interprets calendar days in.
// A distroless host has no zoneinfo database, so an unresolvable name falls
// back to a fixed +05:30 zone (IST has no DST, so this is exact) rather than
// silently comparing in UTC.
func FleetLocation() *time.Location {
	name := os.Getenv(FleetTimezoneEnv)
	if name == "" {
		name = DefaultFleetTimezone
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}

// dateLayoutISO is the calendar-date format list filters submit (YYYY-MM-DD).
const dateLayoutISO = "2006-01-02"

// sqliteInstantLayout is the format UTC day bounds are rendered in for SQL
// comparisons. Storage mixes 'YYYY-MM-DD HH:MM:SS' (SQLite CURRENT_TIMESTAMP)
// and RFC3339; SQLite datetime() normalizes both to this shape, so plain
// string comparison is then a correct instant comparison.
const sqliteInstantLayout = "2006-01-02 15:04:05"

// DayBoundsUTC converts an inclusive [from,to] pair of calendar dates
// (YYYY-MM-DD, as the UI submits after parseDateParam) into inclusive UTC
// instant bounds for SQL timestamp columns stored in UTC
// (created_at/payment_date via CURRENT_TIMESTAMP or clock.Now()).
//
// Timestamps are stored UTC but operators read days in the fleet timezone:
// truncating the raw UTC string (substr(col,1,10)) silently files every
// 00:00–05:30 IST event under the previous day, so "Today" missed the first
// 5.5 hours of the Indian working day. Comparing datetime(col) against these
// bounds fixes the window to [day start, day end] in FleetLocation.
//
// Empty from/to stays empty so the SQL (? = ”) guard keeps working.
func DayBoundsUTC(from, to string) (fromInstant, toInstant string) {
	return dayStartUTC(from), dayEndUTC(to)
}

func dayStartUTC(date string) string {
	t, err := time.ParseInLocation(dateLayoutISO, date, FleetLocation())
	if err != nil {
		return ""
	}
	return t.UTC().Format(sqliteInstantLayout)
}

func dayEndUTC(date string) string {
	t, err := time.ParseInLocation(dateLayoutISO, date, FleetLocation())
	if err != nil {
		return ""
	}
	return t.Add(24*time.Hour - time.Second).UTC().Format(sqliteInstantLayout)
}
