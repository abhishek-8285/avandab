package sql

import (
	"strings"

	"transport-app/internal/trip/domain/aggregate"
)

// StatusFilterActive is the list-filter pseudo-status for every trip that is
// neither terminal (completed/cancelled) nor draft. Dashboards link here
// (e.g. /trips?status=active); the aggregate owns the lifecycle vocabulary,
// so the expansion references its constants rather than string literals.
const StatusFilterActive = "active"

// expandTripStatusFilter maps a list-filter value to real trip statuses.
// Unknown values pass through as a single exact status (no rows match a
// bogus status, same as before); "" means unfiltered.
func expandTripStatusFilter(status string) []string {
	if status == StatusFilterActive {
		return []string{
			string(aggregate.TripScheduled),
			string(aggregate.TripAssigned),
			string(aggregate.TripStarted),
			string(aggregate.TripReachedPickup),
			string(aggregate.TripInTransit),
			string(aggregate.TripDelivered),
		}
	}
	if status == "" {
		return nil
	}
	return []string{status}
}

// tripStatusPredicate builds the status WHERE fragment + args for the trips
// list queries. Single status → exact match; pseudo-status → IN list;
// empty → no-op. Callers splice the args into their positional arg list.
func tripStatusPredicate(status string) (string, []any) {
	statuses := expandTripStatusFilter(status)
	if len(statuses) == 0 {
		return "1 = 1", nil
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}
	return "t.status IN (" + strings.Join(placeholders, ",") + ")", args
}
