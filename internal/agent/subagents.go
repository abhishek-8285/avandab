package agent

import (
	"context"
	"encoding/json"
	"strings"

	"transport-app/internal/rag"
)

// SubAgent is one specialized agent in the multi-agent fleet.
type SubAgent struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []*RegisteredTool
}

// AgentSetOptions configures how the standard agent fleet is built.
type AgentSetOptions struct {
	RequireApproval bool
	RagService      *rag.Service // optional: enables the support agent
}

// BuildAgentSet assembles the standard sub-agents from the full tool registry.
// mutating tools are approval-gated when RequireApproval is set.
// Fail-closed: if approvals are required but no gate service exists, mutating
// tools are OMITTED entirely — they never run ungated.
func BuildAgentSet(toolsByName map[string]*RegisteredTool, approval *ApprovalService, opts AgentSetOptions) []*SubAgent {
	pick := func(names ...string) []*RegisteredTool {
		var out []*RegisteredTool
		for _, n := range names {
			t, ok := toolsByName[n]
			if !ok {
				continue
			}
			if opts.RequireApproval {
				if approval == nil {
					// Fail closed for MUTATING tools only; read-only tools stay.
					if isMutatingTool(n) {
						continue
					}
					out = append(out, t)
					continue
				}
				if approval.IsGated(n) {
					out = append(out, approval.GatedTool(t))
					continue
				}
			}
			out = append(out, t)
		}
		return out
	}

	agents := []*SubAgent{
		{
			Name:        "booking",
			Description: "fare quotes, routes, customers, creating bookings",
			SystemPrompt: `You are the BOOKING specialist of the Avandab AI operations assistant. You handle fare quotes, route lookup, customer search and new bookings.
Rules:
1. Compute quotes from the route's standard fare; add GST if enabled. State base fare, GST and total in INR.
2. To create a booking you need: customer id, route id, pickup date/time (YYYY-MM-DD HH:MM), vehicle type, passengers, price. Ask for anything missing.
3. Never invent customer or route ids — search first.
4. After creating a booking, state the booking number and price.`,
			Tools: pick("search_routes", "get_quote", "search_customers", "get_booking", "create_booking"),
		},
		{
			Name:        "payments",
			Description: "invoices, outstanding balances, recording payments",
			SystemPrompt: `You are the PAYMENTS specialist of the Avandab AI operations assistant. You handle invoices and payments.
Rules:
1. Report invoice totals, amounts paid and outstanding balances in INR.
2. To record a payment you need: invoice id, amount, method (cash, upi, bank_transfer, cheque). Ask for anything missing.
3. Never record a payment larger than the outstanding balance; say so instead.`,
			Tools: pick("get_invoice", "list_unpaid_invoices", "record_payment"),
		},
		{
			Name:        "kharcha",
			Description: "driver expense (kharcha) review, approvals and rejections",
			SystemPrompt: `You are the KHARCHA specialist of the Avandab AI operations assistant. You review driver expenses.
Rules:
1. List pending expenses with trip, driver, category, amount and description.
2. To approve or reject you need the expense id and (for rejection) a reason.
3. Approvals and rejections require admin sign-off — submit the action and tell the user it is pending approval.`,
			Tools: pick("list_pending_kharcha", "approve_kharcha", "reject_kharcha"),
		},
		{
			Name:        "ops",
			Description: "trips, drivers, vehicles, assignments, dashboard, revenue",
			SystemPrompt: `You are the OPERATIONS specialist of the Avandab AI operations assistant. You handle trips, drivers, vehicles and daily operations.
Rules:
1. For "today's trips" list status, trip number, route and driver.
2. Before assigning a driver or vehicle, confirm the trip id with the user if not given. Assignments require admin sign-off.
3. Report revenue and dashboard figures in INR.`,
			Tools: pick("list_trips", "get_trip", "list_available_drivers", "list_available_vehicles", "assign_driver", "assign_vehicle", "get_dashboard", "get_revenue", "get_open_alerts", "extend_ewaybill"),
		},
		{
			Name:        "maintenance",
			Description: "maintenance job cards (work orders): listing, opening, assigning and closing",
			SystemPrompt: `You are the MAINTENANCE specialist of the Avandab AI operations assistant. You handle job cards for vehicle servicing.
Rules:
1. To open a job card you need: vehicle id and title. Ask for anything missing. Never invent vehicle ids — confirm the vehicle first.
2. Move cards along open → assigned → in_progress → done (on_hold loops back, cancelled closes). Closing as done writes the service record.
3. Opening and transitioning cards require admin sign-off — submit the action and tell the user it is pending approval.`,
			Tools: pick("list_work_orders", "get_work_order", "create_work_order", "transition_work_order"),
		},
	}

	if opts.RagService != nil {
		agents = append(agents, &SubAgent{
			Name:        "support",
			Description: "questions about how the system works, policies, documents",
			SystemPrompt: `You are the SUPPORT specialist of the Avandab AI operations assistant. You answer how-to and policy questions using the knowledge base.
Rules:
1. Always search the knowledge base before answering; cite the document source when found.
2. If the search returns nothing relevant, say you could not find it and suggest the admin upload the document.`,
			Tools: []*RegisteredTool{ragSearchTool(opts.RagService)},
		})
	}

	return agents
}

// isMutatingTool reports whether a tool name is on the approval-gated list.
func isMutatingTool(name string) bool {
	for _, m := range MutatingTools() {
		if m == name {
			return true
		}
	}
	return false
}

// ragSearchTool exposes the RAG knowledge base as an agent tool.
func ragSearchTool(svc *rag.Service) *RegisteredTool {
	return &RegisteredTool{
		Name:        "knowledge_search",
		Description: "Search the company knowledge base (policies, procedures, documents) by topic.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "what to look up, e.g. 'cancellation policy' or 'driver onboarding'"},
				"top_k": map[string]any{"type": "integer", "description": "number of results (default 5)"},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Query string `json:"query"`
				TopK  int    `json:"top_k"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", err
			}
			if in.TopK <= 0 {
				in.TopK = 5
			}
			res, err := svc.Query(in.Query, in.TopK)
			if err != nil {
				return "", err
			}
			if res == nil || len(res.Chunks) == 0 {
				return "No knowledge base results for: " + in.Query, nil
			}
			type hit struct {
				Source  string  `json:"source"`
				Content string  `json:"content"`
				Score   float64 `json:"score"`
			}
			out := make([]hit, 0, len(res.Chunks))
			for i, c := range res.Chunks {
				score := 0.0
				if i < len(res.Scores) {
					score = res.Scores[i]
				}
				out = append(out, hit{c.Source, strings.TrimSpace(c.Content), score})
			}
			return jsonString(out)
		},
	}
}
