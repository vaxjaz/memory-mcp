package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type CleanupInput struct {
	Action string `json:"action" jsonschema:"inspect, preview, confirm, apply, undo, read_plan, defer, acknowledge, configure or recheck_sources. apply and undo need the approval of the exact preview the user saw."`
	Scope  string `json:"scope" jsonschema:"project:<identifier>"`

	// inspect
	Cursor string `json:"cursor,omitempty" jsonschema:"inspect only: inventory.next_cursor of the previous page, for the next one. inventory.review_next_cursor goes to memory_search with needs_review=true."`

	// preview and recheck_sources
	Operations []memory.CleanupOperation `json:"operations,omitempty" jsonschema:"preview only: the proposed operations. Each names entry_id, action (archive, supersede, distill or recheck), reason and the evidence_ids actually read; supersede also names replacement_id, distill a summary."`
	EntryID    string                    `json:"entry_id,omitempty" jsonschema:"recheck_sources only: the record whose sources were rechecked."`
	SourceIDs  []string                  `json:"source_ids,omitempty" jsonschema:"recheck_sources only: 1-32 IDs of the evidence that was actually read."`
	Note       string                    `json:"note,omitempty" jsonschema:"recheck_sources only: the concrete reason."`

	// confirm, apply, undo and read_plan
	PlanID           string `json:"plan_id,omitempty" jsonschema:"The plan.id returned by preview. confirm, apply, undo and read_plan need it; a confirm with purpose configure leaves it out."`
	Purpose          string `json:"purpose,omitempty" jsonschema:"confirm only: apply, undo or configure."`
	ApprovalQuote    string `json:"approval_quote,omitempty" jsonschema:"confirm, apply and undo: the user's own words approving this exact plan, sent unchanged in all three calls."`
	RequestID        string `json:"request_id,omitempty" jsonschema:"confirm only: a stable ID for this exact submission, so a retry does not record the approval twice."`
	ApprovalSourceID string `json:"approval_source_id,omitempty" jsonschema:"apply and undo only: the approval_source_id returned by confirm for that purpose."`

	// defer
	DeferChanges int `json:"defer_changes,omitempty" jsonschema:"defer only: postpone the reminder by this many stored experiences, 1-1000000."`

	// configure
	Policy *memory.MaintenanceConfig `json:"policy,omitempty" jsonschema:"configure only: scope, interval, enabled, repeating, and source_id set to the approval_source_id of a confirm with purpose configure."`
}

type CleanupOutput struct {
	ApprovalSourceID string                    `json:"approval_source_id,omitempty"`
	Inventory        *memory.CleanupInventory  `json:"inventory,omitempty"`
	Plan             *memory.CleanupPlan       `json:"plan,omitempty"`
	Maintenance      *memory.MaintenanceStatus `json:"maintenance,omitempty"`
	Done             bool                      `json:"done"`
	Notice           string                    `json:"notice"`
}

const cleanupDescription = "Maintain the library reversibly. A change is applied only after the user approves the exact preview, and evidence is never permanently deleted. " +
	"1. inspect: read-only inventory of this project (counts, items, pending plans, reminder policy); follow inventory.next_cursor. " +
	"2. preview: store a proposed plan from operations and get plan.id. Nothing changes yet. Show the user the affected entries, what is kept or lost, the reasons and the unknowns, and offer apply, adjust or later. " +
	"3. confirm: record the approval. Send plan_id, purpose \"apply\", the user's words as approval_quote and a request_id; the response carries approval_source_id. " +
	"4. apply: send plan_id, approval_source_id and the same approval_quote. To reverse it, confirm again with purpose \"undo\" and then call undo. read_plan shows a stored plan. " +
	"Reminders: defer postpones the next one by defer_changes; acknowledge resets it. To change it, confirm with purpose \"configure\" and the user's instruction as approval_quote (no plan_id), then configure with a policy whose source_id is that approval_source_id. " +
	"recheck_sources previews a recheck of one record's evidence. " +
	"Installation permission is not approval, and no semantic correctness is certified."

func addGovernanceTools(s *mcp.Server, store *memory.Store) {
	mcp.AddTool(s, &mcp.Tool{Name: "memory_cleanup", Description: cleanupDescription, Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)}}, func(ctx context.Context, req *mcp.CallToolRequest, in CleanupInput) (*mcp.CallToolResult, any, error) {
		out := CleanupOutput{Notice: "Management records are excluded from ordinary memory search. Preserved bodies/links/history remain readable. Fresh user confirmation is required before applying any cleanup."}
		var err error
		switch in.Action {
		case "confirm":
			id, e := store.ConfirmMaintenance(ctx, in.Scope, in.PlanID, in.Purpose, in.ApprovalQuote, in.RequestID)
			out.ApprovalSourceID = id
			err = e
		case "inspect":
			r, e := store.CleanupInventory(ctx, in.Scope, in.Cursor)
			out.Inventory = &r
			err = e
		case "configure":
			if in.Policy == nil || in.Policy.Scope != in.Scope {
				return experienceTextResult(out, errors.New("policy must specify the same scope"))
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
				return experienceTextResult(out, e)
			}
			if p.Scope != in.Scope {
				return experienceTextResult(out, errors.New("plan outside requested scope"))
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
		return experienceTextResult(out, err)
	})
}
