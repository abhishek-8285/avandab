package domain

import "errors"

var (
	ErrTenantRequired  = errors.New("dispatch tenant is required")
	ErrTargetRequired  = errors.New("dispatch target is required")
	ErrRunNotFound     = errors.New("planner run not found")
	ErrNoStops         = errors.New("at least one stop required to plan a run")
	ErrNoVehicles      = errors.New("at least one vehicle required to plan a run")
	ErrStopNotFound    = errors.New("stop not found in run")
	ErrRunCommitted    = errors.New("run already committed")
	ErrInvalidStopType = errors.New("stop source_type must be pickup, dropoff or waypoint")
	ErrBookingUnknown  = errors.New("booking not found or missing facility coordinates")
	ErrRouteNotFound   = errors.New("route not found in run")
	ErrInvalidOp       = errors.New("invalid tune operation")
	ErrInvalidSeq      = errors.New("invalid sequence positions")
)
