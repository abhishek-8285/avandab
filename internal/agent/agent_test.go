package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeClient implements the LLM client contract for tests.
type fakeClient struct {
	turns   int
	replies []Message
}

func (f *fakeClient) Complete(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	msg := f.replies[f.turns]
	f.turns++
	return msg, nil
}

func TestAgent_AnswersWithoutTools(t *testing.T) {
	client := &fakeClient{replies: []Message{{Role: "assistant", Content: "24 hours notice"}}}
	ag := NewAgent(client, nil, "test system", 5)

	answer, _, err := ag.Run(context.Background(), []Message{{Role: "user", Content: "what is the cancellation policy?"}})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "24 hours notice" {
		t.Errorf("expected direct answer, got %q", answer)
	}
}

func TestAgent_ToolCallLoop(t *testing.T) {
	client := &fakeClient{replies: []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "echo_tool",
					Arguments: json.RawMessage(`{"text":"hello"}`),
				},
			}},
		},
		{Role: "assistant", Content: "tool said hello"},
	}}

	tools := []*RegisteredTool{{
		Name:        "echo_tool",
		Description: "echo text",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Text string `json:"text"`
			}
			json.Unmarshal(args, &in)
			return "echo: " + in.Text, nil
		},
	}}

	ag := NewAgent(client, tools, "", 5)
	answer, _, err := ag.Run(context.Background(), []Message{{Role: "user", Content: "call the tool"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "tool said hello") {
		t.Errorf("expected tool result in answer, got %q", answer)
	}

	// Verify the tool result was appended as a tool message
	if client.turns != 2 {
		t.Errorf("expected 2 LLM calls, got %d", client.turns)
	}
}

func TestAgent_UnknownTool(t *testing.T) {
	client := &fakeClient{replies: []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "nonexistent",
					Arguments: json.RawMessage(`{}`),
				},
			}},
		},
		{Role: "assistant", Content: "done"},
	}}

	ag := NewAgent(client, []*RegisteredTool{}, "", 5)
	_, _, err := ag.Run(context.Background(), []Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgent_MaxTurns(t *testing.T) {
	// Always returns a tool call → agent should hit the turn limit
	client := &fakeClient{replies: []Message{}}
	for i := 0; i < 20; i++ {
		client.replies = append(client.replies, Message{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:       "call_x",
				Type:     "function",
				Function: FunctionCall{Name: "echo_tool", Arguments: json.RawMessage(`{"text":"x"}`)},
			}},
		})
	}

	tools := []*RegisteredTool{{
		Name:        "echo_tool",
		Description: "echo",
		Parameters:  map[string]any{},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "ok", nil
		},
	}}

	ag := NewAgent(client, tools, "", 3)
	_, _, err := ag.Run(context.Background(), []Message{{Role: "user", Content: "loop"}})
	if err == nil {
		t.Error("expected turn-limit error")
	}
	if !strings.Contains(err.Error(), "turns") {
		t.Errorf("expected turn limit error, got: %v", err)
	}
}

func TestClient_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("test-key", srv.URL, "test-model")
	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "ping"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "pong" {
		t.Errorf("expected 'pong', got %q", msg.Content)
	}
}

func TestClient_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"boom","type":"server_error"}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("key", srv.URL, "model")
	_, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestToolEnv_ToolsRegistered(t *testing.T) {
	env := &ToolEnv{DB: nil, UserID: "usr-1", UserName: "test"}
	tools := RegisterTools(env)
	if len(tools) < 10 {
		t.Errorf("expected at least 10 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tl := range tools {
		if names[tl.Name] {
			t.Errorf("duplicate tool name: %s", tl.Name)
		}
		names[tl.Name] = true
		if tl.Description == "" {
			t.Errorf("tool %s missing description", tl.Name)
		}
	}
}

func TestSystemPromptFor(t *testing.T) {
	p := SystemPromptFor("Ramesh")
	if !strings.Contains(p, "Ramesh") {
		t.Error("expected operator name in prompt")
	}
	if !strings.Contains(p, "INR") {
		t.Error("expected INR guidance")
	}
}

func TestNormalizeArgs(t *testing.T) {
	// string-encoded (OpenAI style)
	got := string(normalizeArgs(json.RawMessage(`"{\"a\":1}"`)))
	if got != `{"a":1}` {
		t.Errorf("expected unwrapped object, got %s", got)
	}
	// raw object (some providers)
	got = string(normalizeArgs(json.RawMessage(`{"a":1}`)))
	if got != `{"a":1}` {
		t.Errorf("expected passthrough, got %s", got)
	}
}
