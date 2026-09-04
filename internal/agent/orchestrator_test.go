package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"transport-app/internal/agent/rl"
)

type routeClient struct {
	reply string
}

func (c *routeClient) Complete(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	return Message{Role: "assistant", Content: c.reply}, nil
}

func TestKeywordRoute(t *testing.T) {
	o := &Orchestrator{}
	cases := map[string]string{
		"approve the kharcha for trip TR-5":     "kharcha",
		"list unpaid invoices":                  "payments",
		"give me a quote from Delhi to Jaipur":  "booking",
		"how do I cancel a booking per policy?": "support",
		"how many trips today":                  "ops",
		"anything unusual":                      "ops",
		"open a job card for brake repair":      "maintenance",
		"which work orders are open":            "maintenance",
		"vehicle maintenance due soon":          "maintenance",
	}
	for q, want := range cases {
		if got := o.keywordRoute(q); got != want {
			t.Errorf("keywordRoute(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestRouteUsesLLMWhenAvailable(t *testing.T) {
	o := &Orchestrator{client: &routeClient{reply: "payments"}}
	o.agents = map[string]*SubAgent{
		"payments": {Name: "payments"},
		"ops":      {Name: "ops"},
	}
	o.order = []string{"payments", "ops"}

	got, err := o.Route(context.Background(), "show unpaid invoices")
	if err != nil {
		t.Fatal(err)
	}
	if got != "payments" {
		t.Errorf("expected payments, got %q", got)
	}
}

func TestRouteJSONTolerance(t *testing.T) {
	o := &Orchestrator{client: &routeClient{reply: `{"agent": "booking"}`}}
	o.agents = map[string]*SubAgent{"booking": {Name: "booking"}}
	o.order = []string{"booking"}

	got, err := o.Route(context.Background(), "book a trip")
	if err != nil {
		t.Fatal(err)
	}
	if got != "booking" {
		t.Errorf("expected booking, got %q", got)
	}
}

func TestRouteFallsBackOnUnknownAgent(t *testing.T) {
	o := &Orchestrator{client: &routeClient{reply: "mars"}}
	o.agents = map[string]*SubAgent{"ops": {Name: "ops"}}
	o.order = []string{"ops"}

	got, err := o.Route(context.Background(), "random query")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ops" {
		t.Errorf("expected fallback ops, got %q", got)
	}
}

// toolThenAnswerClient: first call answers the router, then one turn with two
// parallel tool calls, then the final answer (2 turns, 2 tool calls).
type toolThenAnswerClient struct{ calls int }

func (c *toolThenAnswerClient) Complete(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	switch c.calls {
	case 0: // router call
		c.calls++
		return Message{Role: "assistant", Content: "ops"}, nil
	case 1: // executor: two parallel tool calls in one turn
		c.calls++
		return Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function", Function: FunctionCall{Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}},
				{ID: "call_2", Type: "function", Function: FunctionCall{Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}},
			},
		}, nil
	default:
		return Message{Role: "assistant", Content: "done: hi"}, nil
	}
}

func TestOrchestratorHandleRecordsEpisode(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	toolEnv := &ToolEnv{}
	o := NewOrchestrator(&toolThenAnswerClient{}, toolEnv, svc, 5)
	o.AddAgent(&SubAgent{
		Name:        "ops",
		Description: "trips",
		Tools: []*RegisteredTool{{
			Name: "echo", Description: "echo",
			Parameters: map[string]any{},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				return "echoed", nil
			},
		}},
	})

	answer, episodeID, err := o.Handle(context.Background(), []Message{{Role: "user", Content: "say hi"}}, "usr-1", "Tester")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "done") {
		t.Errorf("unexpected answer: %q", answer)
	}
	if episodeID == "" {
		t.Error("expected episode id")
	}

	examples := svc.ExamplesFor("ops", "say hi", 3)
	if len(examples) != 1 {
		t.Errorf("expected 1 recorded example, got %d", len(examples))
	}
}

func TestOrchestratorFewShotInjection(t *testing.T) {
	svc, err := rl.New(filepath.Join(t.TempDir(), "rl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	ep := &rl.Episode{
		ID: svc.NewEpisodeID(), AgentName: "ops", Query: "how many trips", Answer: "4 trips",
		TurnCount: 2,
		Traces: []rl.ToolTrace{
			{Name: "list_trips", OK: true},
			{Name: "get_dashboard", OK: true},
		},
	}
	if err := svc.RecordEpisode(ep); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(nil, &ToolEnv{}, svc, 5)
	sub := &SubAgent{Name: "ops", Description: "trips", SystemPrompt: "Base prompt."}
	system := o.buildSystem(sub, "how many trips today", "Ramesh")
	if !strings.Contains(system, "how many trips") {
		t.Error("expected few-shot example injected into system prompt")
	}
	if !strings.Contains(system, "Base prompt.") {
		t.Error("expected base prompt preserved")
	}
}
