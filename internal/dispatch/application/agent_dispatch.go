package application

// DB-backed dispatcher construction for the agent assignment tools, extracted
// from internal/agent/tools.go's legacy env.Services.Trips fallback
// (handlers-end-state ADR, Phase 3). Production wiring still injects the
// Service built by cmd/server; this constructor covers callers that only have
// a database handle (tests/tools) and keeps the same assignment use cases.

import (
	"database/sql"

	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	tripapp "transport-app/internal/trip/application"
)

// NewAgentDispatcher builds the assignment service against the database.
func NewAgentDispatcher(db *sql.DB) *Service {
	u := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	return NewService(tripapp.NewAssignDriverUseCase(u, clk), tripapp.NewAssignVehicleUseCase(u, clk))
}
