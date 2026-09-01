# 07. AI Operations Assistant (Autonomous Multi-Agent System)

> **LLM-Powered Fleet Operations, Dispatch & Financial Copilot**
> Built-in orchestrator with domain sub-agents, reinforcement learning loop, and human-in-the-loop safety approval gate.

---

## 1. Multi-Agent Architecture (`internal/agent/`)

The assistant operates at `/assistant` (web interface) and `/api/agent/chat` (API endpoint):

```
                               [ User Message / Chat Prompt ]
                                             │
                                             ▼
                             [ Router / Orchestrator LLM ]
                             (internal/agent/orchestrator.go)
                                             │
               ┌──────────────┬──────────────┼──────────────┬──────────────┐
               ▼              ▼              ▼              ▼              ▼
          [ Booking ]   [ Payments ]    [ Kharcha ]      [ Ops ]       [ Support ]
          Create / Edit Record / Audit  Approve/Reject   Live Tracking RAG Search
          Bookings      Invoices        Expenses         & Deviations  Docs & Policies
```

---

## 2. Safety Approval Gate (`internal/agent/approval.go`)

- **Principle**: Mutating tools (e.g. `create_booking`, `assign_driver`, `record_payment`, `approve_kharcha`) cannot modify the live database directly when `AGENT_REQUIRE_APPROVAL=true`.
- **Workflow**:
  1. The AI Agent stages a **Pending Action** with full parameters.
  2. The Fleet Admin is notified and views the pending action at `/agent-actions`.
  3. The Admin clicks **Approve** or **Reject**.
  4. Only upon explicit human approval does the action execute under the Admin's authenticated identity.

---

## 3. Online Reinforcement Learning (RL) Loop (`internal/agent/rl/`)

- Stored in a local-first SQLite database: `agent_rl.db`.
- Every chat session is treated as an episode:
  - High-reward episodes (successful task completions without errors) are automatically synthesized into few-shot examples injected into sub-agent prompts.
  - Failing tools receive deprioritization warnings.
- Wiping `agent_rl.db` only resets agent learning metrics without ever affecting live fleet business data.
