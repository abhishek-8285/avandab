package domain

import "errors"

var (
	ErrTenantRequired = errors.New("dispatch tenant is required")
	ErrTargetRequired = errors.New("dispatch target is required")
)
