package agent

import (
	"context"
	"encoding/json"
	"strings"

	"transport-app/internal/agent/rl"
)

// Orchestrator routes requests to specialized sub-agents and runs the
// online RL loop: episode recording, reward signals, few-shot learning.
type Orchestrator struct {
	client   Completer
	env      *ToolEnv
	rl       *rl.Service
	maxTurns int
	agents   map[string]*SubAgent
	order    []string
}

// NewOrchestrator creates the multi-agent orchestrator.
func NewOrchestrator(client Completer, env *ToolEnv, rlSvc *rl.Service, maxTurns int) *Orchestrator {
	return &Orchestrator{
		client:   client,
		env:      env,
		rl:       rlSvc,
		maxTurns: maxTurns,
		agents:   map[string]*SubAgent{},
	}
}

// AddAgent registers a sub-agent.
func (o *Orchestrator) AddAgent(a *SubAgent) {
	o.agents[a.Name] = a
	o.order = append(o.order, a.Name)
}

// AgentNames lists registered sub-agents.
func (o *Orchestrator) AgentNames() []string { return o.order }

// Handle runs the full pipeline: route -> delegate -> record episode.
func (o *Orchestrator) Handle(ctx context.Context, messages []Message, userID, userName string) (string, string, error) {
	query := lastUserQuery(messages)

	agentName, err := o.Route(ctx, query)
	if err != nil {
		agentName = "ops"
	}
	sub := o.agents[agentName]
	if sub == nil {
		sub = o.agents["ops"]
		agentName = "ops"
	}

	system := o.buildSystem(sub, query, userName)
	ag := NewAgent(o.client, sub.Tools, system, o.maxTurns)

	episode := &rl.Episode{
		UserID:    userID,
		AgentName: agentName,
		Query:     query,
	}
	if o.rl != nil {
		episode.ID = o.rl.NewEpisodeID()
		ctx = context.WithValue(ctx, episodeCtxKey, episode.ID)
	}

	var traces []rl.ToolTrace
	ag.WithTracer(func(name string, ok bool, err error) {
		traces = append(traces, rl.ToolTrace{Name: name, OK: ok, Error: errString(err)})
	})

	answer, turnCount, err := ag.Run(ctx, messages)
	episode.Answer = answer
	episode.TurnCount = turnCount
	episode.Messages = rl.MarshalMessages(messages)
	episode.Traces = traces
	if o.rl != nil {
		// Record failures too: the RL loop must learn from broken turns
		// (tool errors, truncations, runaways), not only successes.
		if err := o.rl.RecordEpisode(episode); err != nil {
			// learning failures must not break the chat
			_ = err
		}
	}
	if err != nil {
		return "", episode.ID, err
	}
	return answer, episode.ID, nil
}

// Route classifies which sub-agent should handle a query.
func (o *Orchestrator) Route(ctx context.Context, query string) (string, error) {
	if o.client == nil {
		return o.keywordRoute(query), nil
	}

	var b strings.Builder
	b.WriteString("You are a router. Choose exactly one agent for the user request.\nAgents:\n")
	for _, name := range o.order {
		b.WriteString("- " + name + ": " + o.agents[name].Description + "\n")
	}
	b.WriteString("Respond with ONLY the agent name, nothing else.\nUser request: " + query)

	reply, err := o.client.Complete(ctx, []Message{{Role: "user", Content: b.String()}}, nil)
	if err != nil {
		return o.keywordRoute(query), err
	}

	name := strings.TrimSpace(strings.ToLower(reply.Content))
	// Tolerate JSON-ish output like {"agent":"booking"} — try JSON first,
	// then fall back to stripping stray quotes/brackets.
	if json.Unmarshal([]byte(name), &map[string]string{}) == nil {
		var m map[string]string
		if err := json.Unmarshal([]byte(name), &m); err == nil {
			if v, ok := m["agent"]; ok && v != "" {
				name = strings.ToLower(strings.TrimSpace(v))
			}
		}
	} else {
		name = strings.Trim(name, "`\"{}'[]")
	}
	if _, ok := o.agents[name]; ok {
		return name, nil
	}
	return o.keywordRoute(query), nil
}

// keywordRoute is a dependency-free fallback when the router LLM fails.
// Multi-word phrases are checked before short tokens to cut false positives.
func (o *Orchestrator) keywordRoute(query string) string {
	q := strings.ToLower(query)
	hasAll := func(words ...string) bool {
		for _, w := range words {
			if !strings.Contains(q, w) {
				return false
			}
		}
		return true
	}
	switch {
	case hasAll("alert"), hasAll("alerts"), hasAll("eway"), hasAll("eway bill"), hasAll("e-way"), hasAll("sos"), hasAll("emergency"):
		return "ops"
	case hasAll("driver", "expense"), hasAll("kharcha"), hasAll("expense", "approve"), hasAll("expense", "reject"), hasAll("diesel"), hasAll("toll"):
		return "kharcha"
	case hasAll("job card"), hasAll("jobcard"), hasAll("work order"), hasAll("maintenance"), hasAll("service due"), hasAll("garage"), hasAll("mechanic"), hasAll("repair"):
		return "maintenance"
	case hasAll("invoice"), hasAll("payment"), hasAll("unpaid"), hasAll("outstanding"), hasAll("upi"), hasAll("cheque"), hasAll("balance"):
		return "payments"
	case hasAll("policy"), hasAll("procedure"), hasAll("rule"), hasAll("document"), hasAll("how does"), hasAll("how do"), hasAll("what is the rule"):
		return "support"
	case hasAll("quote"), hasAll("fare"), hasAll("price for"), hasAll("book"), hasAll("booking"), hasAll("customer"), hasAll("route between"):
		return "booking"
	default:
		return "ops"
	}
}

// buildSystem assembles the sub-agent prompt with the operator identity,
// learned few-shot examples and policy warnings about unreliable tools.
func (o *Orchestrator) buildSystem(sub *SubAgent, query, userName string) string {
	system := sub.SystemPrompt
	if userName != "" && userName != "Unknown" {
		system += "\n\nYour authenticated operator is: " + userName
	}

	if o.rl != nil {
		if examples := o.rl.ExamplesFor(sub.Name, query, 3); len(examples) > 0 {
			system += rl.FormatExamples(examples)
		}
		if notes := o.rl.PolicyNotesFor(sub.Name); len(notes) > 0 {
			system += "\n\nTool warnings learned from past failures (only use as last resort):\n"
			for _, n := range notes {
				system += "- " + n.Tool + ": " + n.Reason + "\n"
			}
		}
	}
	return system
}

func lastUserQuery(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
