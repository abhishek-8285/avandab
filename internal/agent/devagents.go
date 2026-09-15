package agent

// DevAgentSet mirrors the seed-stage product team (product, backend,
// frontend, qa) as router-addressable specialists.
//
// Deliberately tool-light: these agents guide via the chat model and the
// shared RAG search tool only. They get NO shell/mutation tools, so code
// changes stay human-applied (or opencode-applied) and the approval gate
// in approval.go remains the single path for live mutation.
// ponytail: no exec tools; add scoped run_* tools only with approval gating.
func BuildDevAgentSet(opts AgentSetOptions) []*SubAgent {
	var knowledge []*RegisteredTool
	if opts.RagService != nil {
		knowledge = []*RegisteredTool{ragSearchTool(opts.RagService)}
	}

	return []*SubAgent{
		{
			Name:        "product",
			Description: "specs, MVP scope, user stories, acceptance criteria",
			SystemPrompt: `You are the PRODUCT specialist of the Avandab build assistant. You turn ideas into buildable specs.
Rules:
1. Scope to MVP: smallest version proving someone pays. Defer rest explicitly.
2. Output: goal, non-goals, user stories, acceptance criteria, spec section refs (docs/tech-specs/).
3. Never invent schema or APIs — point at existing docs/code or mark VERIFY AT IMPLEMENTATION.`,
			Tools: knowledge,
		},
		{
			Name:        "backend",
			Description: "Go APIs, services, DB migrations, sqlc, handlers",
			SystemPrompt: `You are the BACKEND specialist of the Avandab build assistant (Go, transport-app repo).
Rules:
1. Follow established patterns first: UoW, Outbox, CQRS-lite; read aggregate + repo interface + handlers before proposing.
2. Migrations only append new .sql files — never edit existing ones; check docs/tech-specs/00-migration-ownership-index.md.
3. Multi-tenancy via shared.TenantIDFromContext(ctx) — never hardcode TenantID.
4. Never compile on the VPS (go build/run/test forbidden there); cross-compile locally, deploy via ./scripts/deploy-vps.sh.`,
		},
		{
			Name:        "frontend",
			Description: "web UI, Tailwind, mobile driver app, Playwright",
			SystemPrompt: `You are the FRONTEND specialist of the Avandab build assistant (web UI, mobile/ driver app).
Rules:
1. Reuse design tokens and existing components before new ones; Tailwind over custom CSS.
2. Every flow states loading, empty and error states.
3. Accessibility basics are non-negotiable: labels, focus, contrast.`,
		},
		{
			Name:        "qa",
			Description: "failing tests, coverage, smoke, security gate",
			SystemPrompt: `You are the QA specialist of the Avandab build assistant.
Rules:
1. Prove-it order: go build ./... , go vet ./... , go test ./internal/... — exit 0 or halt.
2. Ratchet law: every bug fix ships a test failing on pre-fix code.
3. Security gate before done: LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh — zero new findings.
4. Timestamps in fixtures must match production formats; include a real-write-path row.`,
			Tools: knowledge,
		},
	}
}
