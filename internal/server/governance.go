package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type CleanupInput struct {
	Action           string                    `json:"action" jsonschema:"inspect, configure, preview, read_plan, apply, undo, defer, acknowledge or recheck_sources. Apply/undo ONLY after user reviews and explicitly approves the exact displayed plan."`
	Scope            string                    `json:"scope"`
	Cursor           string                    `json:"cursor,omitempty"`
	Operations       []memory.CleanupOperation `json:"operations,omitempty"`
	PlanID           string                    `json:"plan_id,omitempty"`
	Policy           *memory.MaintenanceConfig `json:"policy,omitempty" jsonschema:"Translate an actual user instruction such as every N changes, after N more changes, or disable; echo settings to user"`
	ApprovalSourceID string                    `json:"approval_source_id,omitempty"`
	ApprovalQuote    string                    `json:"approval_quote,omitempty"`
	DeferChanges     int                       `json:"defer_changes,omitempty"`
	EntryID          string                    `json:"entry_id,omitempty"`
	SourceIDs        []string                  `json:"source_ids,omitempty"`
	Note             string                    `json:"note,omitempty"`
}
type CleanupOutput struct {
	Inventory   *memory.CleanupInventory  `json:"inventory,omitempty"`
	Plan        *memory.CleanupPlan       `json:"plan,omitempty"`
	Maintenance *memory.MaintenanceStatus `json:"maintenance,omitempty"`
	Done        bool                      `json:"done"`
	Notice      string                    `json:"notice"`
}

func addGovernanceTools(s *mcp.Server, store *memory.Store, mode string, read, write *mcp.ToolAnnotations) {
	contextDescription := "Optional context-aware lookup: reconstruct the task from current project, visible conversation and already read relevant memory; call action=lookup with grounded keywords. No source_id, prepare/complete, skip or receipt is required for lookup. Compact candidates are <=8192 JSON bytes; both result streams paginate. Read full relevant evidence only as needed; reuse already-read material for an unchanged task, verify again when conditions or versions may have changed. candidate/pending expose explicit outstanding findings without automatically reviewing them. Memory never replaces current user instructions, AGENTS or existing domain routing. Legacy prepare/complete are compatibility tools, not an assist-mode obligation."
	if mode == memory.ModeStrict {
		contextDescription = "Strict compatibility: prepare using current project/conversation and actual source_id; read selected full entries with context_source_id, then complete before the configured action check. skip needs a concrete non-applicability reason. Optional lookup is stateless. Explicit review_at_stop may request one legacy model continuation for a meaningful candidate. Receipts verify bookkeeping, not judgment or AGENTS compliance."
	}
	mcp.AddTool(s, &mcp.Tool{Name: "memory_context", Description: contextDescription, Annotations: write}, func(ctx context.Context, req *mcp.CallToolRequest, in memory.ContextInput) (*mcp.CallToolResult, memory.ContextResult, error) {
		if mode == memory.ModeAssist && in.ReviewAtStop {
			return nil, memory.ContextResult{}, errors.New("assist has no Stop model review; save inline or keep an explicit pending candidate, without switching modes during the task")
		}
		if in.ResponseMode == "" {
			in.ResponseMode = "compact"
		}
		r, err := store.Context(ctx, in)
		return nil, r, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "memory_cleanup", Description: "Manage a separate, reversible maintenance ledger, not business cases. inspect is read-only inventory; preview stores an exact proposed plan without changing memories. Show affected entries, retained/lost content, reasons and unknowns, then offer apply/adjust/later. apply and undo require a NEW actual user approval source and exact quote after preview; installation permission is insufficient. No semantic correctness is certified. configure persists per-project natural-language schedule intent; raw capture/reuse do not count. Never permanently deletes evidence.", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)}}, func(ctx context.Context, req *mcp.CallToolRequest, in CleanupInput) (*mcp.CallToolResult, CleanupOutput, error) {
		out := CleanupOutput{Notice: "Management records are excluded from ordinary memory search. Preserved bodies/links/history remain readable. Fresh user confirmation is required before applying any cleanup."}
		var err error
		switch in.Action {
		case "inspect":
			r, e := store.CleanupInventory(ctx, in.Scope, in.Cursor)
			out.Inventory = &r
			err = e
		case "configure":
			if in.Policy == nil || in.Policy.Scope != in.Scope {
				return nil, out, errors.New("policy must specify the same scope")
			}
			r, e := store.ConfigureMaintenance(ctx, *in.Policy)
			out.Maintenance = &r
			err = e
		case "preview":
			r, e := store.PreviewCleanup(ctx, memory.CleanupPreview{Scope: in.Scope, Operations: in.Operations})
			out.Plan = &r
			err = e
		case "read_plan":
			r, e := store.CleanupPlan(ctx, in.PlanID)
			if e == nil && r.Scope != in.Scope {
				e = errors.New("plan outside requested scope")
			}
			out.Plan = &r
			err = e
		case "apply", "undo":
			p, e := store.CleanupPlan(ctx, in.PlanID)
			if e != nil {
				return nil, out, e
			}
			if p.Scope != in.Scope {
				return nil, out, errors.New("plan outside requested scope")
			}
			r, e := store.DecideCleanup(ctx, memory.CleanupDecision{PlanID: in.PlanID, ApprovalSourceID: in.ApprovalSourceID, ApprovalQuote: in.ApprovalQuote}, in.Action == "undo")
			out.Plan = &r
			err = e
		case "defer":
			err = store.DeferMaintenance(ctx, in.Scope, in.DeferChanges)
		case "acknowledge":
			err = store.AcknowledgeMaintenance(ctx, in.Scope)
		case "recheck_sources":
			r, e := store.PreviewCleanup(ctx, memory.CleanupPreview{Scope: in.Scope, Operations: []memory.CleanupOperation{{EntryID: in.EntryID, Action: "recheck", Reason: in.Note, EvidenceIDs: in.SourceIDs}}})
			out.Plan = &r
			err = e
		default:
			err = errors.New("unknown cleanup action")
		}
		out.Done = err == nil
		return nil, out, err
	})
}
