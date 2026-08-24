package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message is a chat message in the OpenAI format.
type Message struct {
	Role         string     `json:"role"`
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	Name         string     `json:"name,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the function name and JSON arguments.
type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Tool is a JSON schema description for function calling.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable tool.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Client calls an OpenAI-compatible chat completions API.
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

func NewClient(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Complete sends messages + tools and returns the model's reply.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	if c.apiKey == "" {
		return Message{}, fmt.Errorf("agent LLM not configured: set AGENT_API_KEY to enable model calls")
	}
	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
	})
	if err != nil {
		return Message{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("API error %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}

	var result chatResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return Message{}, fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return Message{}, fmt.Errorf("API error: %s (%s)", result.Error.Message, result.Error.Type)
	}
	if len(result.Choices) == 0 {
		return Message{}, fmt.Errorf("empty response")
	}

	choice := result.Choices[0]
	reply := choice.Message
	reply.FinishReason = choice.FinishReason
	if reply.FinishReason == "length" {
		return Message{}, fmt.Errorf("LLM response truncated (finish_reason 'length')")
	}
	if reply.FinishReason == "tool_calls" && len(reply.ToolCalls) == 0 {
		return Message{}, fmt.Errorf("LLM protocol error: finish_reason 'tool_calls' with no tool calls")
	}
	return reply, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
