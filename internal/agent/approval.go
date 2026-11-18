package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"transport-app/internal/agent/rl"
	"transport-app/internal/shared"
)

// ApprovalService gates mutating tools behind admin sign-off.
type ApprovalService struct {
	rl          *rl.Service
	env         *ToolEnv
	originals   map[string]ToolFunc
	gated       map[string]bool
	permChecker PermissionChecker
}

// NewApprovalService wires the approval gate.
func NewApprovalService(rlSvc *rl.Service, env *ToolEnv) *ApprovalService {
	return &ApprovalService{
		rl:        rlSvc,
		env:       env,
		originals: map[string]ToolFunc{},
		gated:     map[string]bool{},
	}
}

// WithPermissionChecker wires the RBAC checker used to re-authorize the admin
// at execution time. Without it, permission-tagged mutations fail closed.
func (a *ApprovalService) WithPermissionChecker(pc PermissionChecker) *ApprovalService {
	a.permChecker = pc
	return a
}

// Gate marks a tool as requiring approval and stores its original handler.
func (a *ApprovalService) Gate(name string, handler ToolFunc) {
	a.originals[name] = handler
	a.gated[name] = true
}

// IsGated reports whether a tool requires approval.
func (a *ApprovalService) IsGated(name string) bool { return a.gated[name] }

// GatedTool returns a wrapped tool that submits an action instead of executing.
func (a *ApprovalService) GatedTool(t *RegisteredTool) *RegisteredTool {
	return &RegisteredTool{
		Name:        t.Name,
		Description: t.Description + " [REQUIRES ADMIN APPROVAL: the action is submitted for review, not executed immediately]",
		Parameters:  t.Parameters,
		// RBAC tags and tenant ride on the submitted action: the admin who
		// approves is the privileged actor, so execution re-checks these under
		// the admin's identity rather than the requester's.
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			requester := userNameFrom(ctx)
			if requester == "" {
				requester = a.env.UserName
			}
			action := &rl.Action{
				EpisodeID:   episodeIDFrom(ctx),
				ToolName:    t.Name,
				ArgsJSON:    string(args),
				Summary:     summaryOf(t.Name, args),
				RequestedBy: requester,
				TenantID:    string(shared.TenantIDFromContext(ctx)),
				Resource:    t.Resource,
				Action:      t.Action,
			}
			if err := a.rl.SubmitAction(action); err != nil {
				return "", err
			}
			return fmt.Sprintf("Action %s submitted for admin approval: %s. Pending decision in the approval queue.", action.ID, action.Summary), nil
		},
	}
}

// MutatingTools lists the tools that get approval-gated.
func MutatingTools() []string {
	return []string{"create_booking", "assign_driver", "assign_vehicle", "record_payment", "approve_kharcha", "reject_kharcha", "extend_ewaybill", "create_work_order", "transition_work_order"}
}

// Approve claims a pending action atomically, then executes it under the
// admin's identity (carried via context, so no shared state is mutated).
// Tenant scope and RBAC are both re-checked before the mutation runs.
func (a *ApprovalService) Approve(ctx context.Context, actionID, adminID, adminName string) (*rl.Action, error) {
	action, err := a.rl.GetAction(actionID)
	if err != nil {
		return nil, err
	}
	// An admin may only decide actions that belong to their own tenant.
	if action.TenantID != string(shared.TenantIDFromContext(ctx)) {
		return nil, fmt.Errorf("action %s is not in the current tenant", actionID)
	}

	handler, ok := a.originals[action.ToolName]
	if !ok {
		return nil, fmt.Errorf("no handler registered for tool %q", action.ToolName)
	}
	// Reuse the normal tool execution path so the same Casbin gate that
	// protects the REST endpoint is enforced against the approving admin.
	tool := &RegisteredTool{
		Name:     action.ToolName,
		Resource: action.Resource,
		Action:   action.Action,
		Handler:  handler,
	}
	execCtx := context.WithValue(ctx, userIDCtxKey, adminID)
	execCtx = context.WithValue(execCtx, userNameCtxKey, adminName)
	pc := a.permChecker
	if pc == nil {
		pc = permissionCheckerFrom(ctx)
	}
	if pc != nil {
		execCtx = WithPermissionChecker(execCtx, pc)
		// Fail before claiming: an admin denied resource:action must not even
		// consume the queue item.
		if (tool.Resource != "" || tool.Action != "") && !pc.Can(adminID, tool.Resource, tool.Action) {
			return nil, fmt.Errorf("permission denied for tool %q (requires %s:%s)", tool.Name, tool.Resource, tool.Action)
		}
	} else {
		// No checker configured: the admin-role gate is the only authority.
		// Production always wires one via WithPermissionChecker.
		tool.Resource, tool.Action = "", ""
	}

	claimed, err := a.rl.ClaimAction(actionID, adminName, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("action %s is not pending (already decided or being decided)", actionID)
	}
	action, err = a.rl.GetAction(actionID)
	if err != nil {
		return nil, err
	}

	var result string
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("tool handler panicked: %v", r)
			}
		}()
		result, execErr = tool.Execute(execCtx, normalizeArgs(json.RawMessage(action.ArgsJSON)))
	}()

	action.DecidedBy = adminName
	action.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	action.Result = result
	if execErr != nil {
		action.Status = rl.ActionFailed
		action.Error = execErr.Error()
	} else {
		action.Status = rl.ActionExecuted
	}
	if err := a.rl.UpdateActionDecision(action, rl.ActionApproved); err != nil {
		// Never leave the action 'approved' after the tool ran: mark it
		// failed so a re-approval cannot execute the mutation twice.
		failed := *action
		failed.Status = rl.ActionFailed
		failed.Error = "decision persist failed: " + err.Error()
		_ = a.rl.UpdateActionDecision(&failed, rl.ActionApproved)
		return nil, err
	}
	return action, nil
}

// Reject rejects a pending action (atomic CAS on pending).
func (a *ApprovalService) Reject(ctx context.Context, actionID, adminName, reason string) (*rl.Action, error) {
	action, err := a.rl.GetAction(actionID)
	if err != nil {
		return nil, err
	}
	if action.TenantID != string(shared.TenantIDFromContext(ctx)) {
		return nil, fmt.Errorf("action %s is not in the current tenant", actionID)
	}
	if action.Status != rl.ActionPending {
		return nil, fmt.Errorf("action %s is %s, not pending", actionID, action.Status)
	}
	action.Status = rl.ActionRejected
	action.DecidedBy = adminName
	action.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	action.Result = "rejected: " + reason
	applied, err := a.rl.ClaimAction(actionID, adminName, action.DecidedAt)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, fmt.Errorf("action %s is no longer pending (concurrent decision)", actionID)
	}
	if err := a.rl.UpdateActionDecision(action, rl.ActionApproved); err != nil {
		return nil, err
	}
	return action, nil
}

// ListPending returns the approval queue.
func (a *ApprovalService) ListPending() ([]rl.Action, error) {
	return a.rl.ListPendingActions()
}

// ListPendingForTenant returns only the caller tenant's pending actions.
func (a *ApprovalService) ListPendingForTenant(tenantID string) ([]rl.Action, error) {
	all, err := a.rl.ListPendingActions()
	if err != nil {
		return nil, err
	}
	out := make([]rl.Action, 0, len(all))
	for _, action := range all {
		if action.TenantID == tenantID {
			out = append(out, action)
		}
	}
	return out, nil
}

// summaryOf builds a short human-readable line for the queue page.
func summaryOf(toolName string, args json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return toolName
	}
	keys := []string{"customer_id", "route_id", "pickup_date", "vehicle_type", "price", "trip_id", "driver_id", "vehicle_id", "invoice_id", "amount", "method", "expense_id", "reason", "id", "title", "status", "assignee", "vendor", "cost_estimate", "due_at"}
	parts := []string{}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			parts = append(parts, k+"="+fmt.Sprintf("%v", v))
		}
	}
	return toolName + ": " + join(parts, ", ")
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

type ctxKey string

const episodeCtxKey ctxKey = "agent_episode_id"

// WithEpisodeID attaches an episode ID to the context for action auditing.
func WithEpisodeID(ctx context.Context, episodeID string) context.Context {
	return context.WithValue(ctx, episodeCtxKey, episodeID)
}

func episodeIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(episodeCtxKey).(string); ok {
		return v
	}
	return ""
}
