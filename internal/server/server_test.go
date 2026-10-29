package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestMCPDiscoveryAndLearningRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(store, "test").Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "generic-host", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools.Tools))
	}
	call := func(name string, args any, output any) {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError {
			t.Fatalf("%s failed: %+v", name, r.Content)
		}
		if output != nil {
			if len(r.Content) == 0 {
				t.Fatal("no text fallback for legacy clients")
			}
			text, ok := r.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatal("non-text fallback")
			}
			if err := json.Unmarshal([]byte(text.Text), output); err != nil {
				t.Fatal(err)
			}
		}
	}
	var boot BootstrapOutput
	call("memory_bootstrap", map[string]any{}, &boot)
	if boot.Guide == "" || boot.DataDir != store.Dir {
		t.Fatal("bootstrap incomplete")
	}
	if boot.SchemaVersion != 2 {
		t.Fatal("missing storage capability version")
	}
	var e memory.Entry
	draft := memory.Draft{Kind: "record", Scope: "project:mcp-test", Title: "反馈", Body: "方案B的状态反馈更清楚，未评价其他场景。", Origin: "host_quote", RequestID: "transport-write-001"}
	call("memory_record", draft, &e)
	var retried memory.Entry
	call("memory_record", draft, &retried)
	if e.ID != retried.ID || retried.BodyBytes != len(draft.Body) || len(retried.BodySHA256) != 64 {
		t.Fatal("MCP retry receipt or integrity metadata lost")
	}
	var h memory.Entry
	call("memory_revise", ReviseInput{Draft: memory.Draft{Kind: "hypothesis", Scope: e.Scope, Title: "局部偏好假设", Body: "可能更重视明确状态反馈，其他场景未知。", Origin: "model_inference", Links: []memory.Link{{TargetID: e.ID, Relation: "supports"}}}}, &h)
	var search SearchOutput
	call("memory_search", memory.SearchInput{Scope: e.Scope, Query: "状态"}, &search)
	if len(search.Results) != 2 {
		t.Fatalf("unexpected search: %+v", search)
	}
	call("memory_search", memory.SearchInput{Scope: e.Scope, Query: "状态", Limit: 1}, &search)
	if !search.HasMore || search.NextCursor == "" {
		t.Fatal("MCP search hid continuation")
	}
	var next SearchOutput
	call("memory_search", memory.SearchInput{Scope: e.Scope, Query: "状态", Limit: 1, Cursor: search.NextCursor}, &next)
	if next.HasMore || len(next.Results) != 1 || next.Results[0].ID == search.Results[0].ID {
		t.Fatal("MCP cursor did not advance")
	}
	var read memory.ReadResult
	call("memory_read", ReadInput{ID: e.ID}, &read)
	if len(read.ReferencedBy) != 1 || read.ReferencedBy[0].EntryID != h.ID {
		t.Fatal("provenance unavailable over MCP")
	}
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_record", Arguments: map[string]any{"kind": "hypothesis", "scope": e.Scope, "title": "bad", "body": "bad", "origin": "model_inference"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError {
		t.Fatal("invalid write not reported as tool error")
	}
	// Missing required semantic data must fail without terminating the session.
	r, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError {
		t.Fatal("missing scope accepted")
	}
	call("memory_bootstrap", map[string]any{}, nil)
}
