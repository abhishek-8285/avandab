package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type routerFailClient struct{ calls int }

func (c *routerFailClient) Complete(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	if len(messages) > 0 && strings.HasPrefix(messages[0].Content, "You are a router") {
		return Message{}, errors.New("router LLM unavailable")
	}
	c.calls++
	if c.calls == 1 {
		return Message{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: FunctionCall{Name: "identify", Arguments: json.RawMessage(`{}`)},
		}}}, nil
	}
	return Message{Role: "assistant", Content: "done"}, nil
}

func TestHandleKeepsKeywordRouteWhenRouterFails(t *testing.T) {
	var which string
	client := &routerFailClient{}
	o := NewOrchestrator(client, &ToolEnv{}, nil, 5)
	o.AddAgent(&SubAgent{
		Name: "payments", Description: "payments",
		Tools: []*RegisteredTool{{Name: "identify", Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			which = "payments"
			return "ok", nil
		}}},
	})
	o.AddAgent(&SubAgent{
		Name: "ops", Description: "ops",
		Tools: []*RegisteredTool{{Name: "identify", Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			which = "ops"
			return "ok", nil
		}}},
	})

	if _, _, err := o.Handle(context.Background(), []Message{{Role: "user", Content: "list unpaid invoices"}}, "u1", "Tester"); err != nil {
		t.Fatal(err)
	}
	if which != "payments" {
		t.Fatalf("router failure discarded keyword fallback: routed to %q, want payments", which)
	}
}
