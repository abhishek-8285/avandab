# ADR: Legacy-delegation end-state for `handlers/` god-files and `agent/tools.go`

Date: 2026-09-08. Status: accepted. Closes roadmap A8 (and doc-09 §6 item 2).

## Context
- `internal/handlers/trips.go` (1,818), `app.go` (1,482), `invoices.go` (1,262),
  `invoices_consolidate.go` (1,065) predate the DDD boundaries in
  `docs/CONTRIBUTING.md`. `internal/agent/tools.go` (1,153) grew the same way:
  tool implementations with inline repo access, JSON shaping, SQL clauses.
- The delegation pattern already exists in-tree: `internal/handlers/bookings.go`
  is a thin adapter over `bookingapp.*UseCase` (constructs UCs, maps HTTP↔DTO,
  no business rules). The fleet SOP parity spec (§8 step 3) prescribes the same
  build order: aggregate → converters → UCs → legacy delegation.
- A big-bang rewrite was rejected: these handlers serve production traffic and
  their behavior is pinned by e2e/coverage gates, not by unit tests.

## Decision
1. **Freeze:** no new business logic in `internal/handlers/*.go` (except
   `bookings.go`-style adapters) or `internal/agent/tools.go`. New behavior
   lands in `*/application/` use-cases; adapters map and delegate.
2. **Delegate opportunistically:** when a handler/tool is touched for a bug or
   feature, extract its rules into the owning domain's UC/facade first, leaving
   the adapter thin — exactly how `bookings.go` reads today. No dedicated
   rewrite tickets.
3. **Precedence:** on conflict between an old handler rule and a domain
   aggregate rule, the aggregate wins; the adapter is the legacy bug
   (per the Master Directive's spec-over-code principle applied to DDD).
4. **No new god-files:** any single non-test Go file approaching ~800 lines
   with mixed HTTP/domain/SQL concerns gets split at the adapter boundary
   before further growth.

## Consequences
- Two architectures coexist indefinitely, but growth flows one way (toward
  UCs). God-files shrink over time instead of growing.
- Review rule: PRs adding business branches inside `handlers/` or `tools.go`
  get asked to place them behind a UC/facade.
