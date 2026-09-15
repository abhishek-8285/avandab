package agent

import (
	"testing"

	"transport-app/internal/rag"
)

func TestBuildDevAgentSetNames(t *testing.T) {
	agents := BuildDevAgentSet(AgentSetOptions{})
	want := []string{"product", "backend", "frontend", "qa"}
	if len(agents) != len(want) {
		t.Fatalf("expected %d dev agents, got %d", len(want), len(agents))
	}
	for i, w := range want {
		if agents[i].Name != w {
			t.Errorf("agent %d = %q, want %q", i, agents[i].Name, w)
		}
		if agents[i].SystemPrompt == "" || agents[i].Description == "" {
			t.Errorf("agent %q needs description + system prompt", w)
		}
	}
}

func TestDevAgentsHaveNoMutatingTools(t *testing.T) {
	for _, sub := range BuildDevAgentSet(AgentSetOptions{}) {
		for _, tl := range sub.Tools {
			if isMutatingTool(tl.Name) {
				t.Errorf("dev agent %s carries mutating tool %s (must stay approval-gated)", sub.Name, tl.Name)
			}
		}
	}
}

func TestDevKnowledgeSearchGatedOnRag(t *testing.T) {
	bare := BuildDevAgentSet(AgentSetOptions{})
	for _, sub := range bare {
		if len(sub.Tools) != 0 {
			t.Errorf("agent %s should be tool-less without RAG, has %d tools", sub.Name, len(sub.Tools))
		}
	}

	wired := BuildDevAgentSet(AgentSetOptions{RagService: &rag.Service{}})
	byName := map[string]*SubAgent{}
	for _, sub := range wired {
		byName[sub.Name] = sub
	}
	for _, n := range []string{"product", "qa"} {
		if len(byName[n].Tools) != 1 || byName[n].Tools[0].Name != "knowledge_search" {
			t.Errorf("agent %s should carry knowledge_search with RAG enabled", n)
		}
	}
	if len(byName["backend"].Tools) != 0 || len(byName["frontend"].Tools) != 0 {
		t.Error("backend/frontend stay tool-less even with RAG (guidance-only)")
	}
}
