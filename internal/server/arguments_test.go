package server

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type (
	CallToolResult = mcp.CallToolResult
	Content        = mcp.Content
	Request        = mcp.Request
	Result         = mcp.Result
	TextContent    = mcp.TextContent
)

// The left column is the schema library's wording, captured from a real call.
func TestSchemaValidationMessagesUseTheNamesOfTheReference(t *testing.T) {
	cases := map[string]string{
		`validating "arguments": validating root: required: missing properties: ["id"]`:                                                                                                                                                          "id is required",
		`validating "arguments": validating root: required: missing properties: ["experience" "admission"]`:                                                                                                                                      "experience and admission are required",
		`validating "arguments": validating root: validating /properties/experience: required: missing properties: ["cues"]`:                                                                                                                     "experience.cues is required",
		`validating "arguments": validating root: validating /properties/policy: required: missing properties: ["scope" "enabled" "repeating" "source_id"]`:                                                                                      "policy.scope, policy.enabled, policy.repeating and policy.source_id are required",
		`validating "arguments": validating root: validating /properties/operations: validating /properties/operations/items: required: missing properties: ["action" "reason" "evidence_ids"]`:                                                  "operations[].action, operations[].reason and operations[].evidence_ids are required",
		`validating "arguments": validating root: validating /properties/title: type: 5 has type "integer", want "string"`:                                                                                                                       "title must be string",
		`validating "arguments": validating root: validating /properties/limit: type: ten has type "string", want "integer"`:                                                                                                                     "limit must be integer",
		`validating "arguments": validating root: validating /properties/operations: type: map[entry_id:a] has type "object", want one of "null, array"`:                                                                                         "operations must be null or array",
		`validating "arguments": validating root: validating /properties/operations: validating /properties/operations/items/properties/entry_id: type: 5 has type "integer", want "string"`:                                                     "operations[].entry_id must be string",
		`validating "arguments": validating root: validating /properties/queries: validating /properties/queries/items/properties/terms: validating /properties/queries/items/properties/terms/items: type: 5 has type "integer", want "string"`: "queries[].terms must be an array of string",
	}
	for given, want := range cases {
		if got := readableArguments(given); got != want {
			t.Errorf("readableArguments(%q)\n got %q\nwant %q", given, got, want)
		}
	}
}

// A message that is not a schema validation passes through byte for byte.
func TestOtherMessagesAreNotRewritten(t *testing.T) {
	for _, text := range []string{
		`memory_cleanup (data_dir="x"): a plan ID is required: preview returns plan.id`,
		`{"indexed":true}`,
		`limit must be 1..30`,
		``,
	} {
		if got := readableArguments(text); got != text {
			t.Errorf("readableArguments(%q) = %q", text, got)
		}
	}
}

type callResult struct {
	result *CallToolResult
	err    error
}

func through(result *CallToolResult, err error) callResult {
	handler := readableArgumentErrors(func(context.Context, string, Request) (Result, error) { return result, err })
	out, outErr := handler(context.Background(), "tools/call", nil)
	call, _ := out.(*CallToolResult)
	return callResult{call, outErr}
}

func TestMiddlewareRewritesOnlySchemaValidationResults(t *testing.T) {
	validation := &CallToolResult{IsError: true, Content: []Content{&TextContent{Text: `validating "arguments": validating root: required: missing properties: ["admission"]`}}}
	if got := through(validation, nil); got.err != nil || got.result != validation || got.result.Content[0].(*TextContent).Text != "admission is required" || !got.result.IsError {
		t.Errorf("validation result: %+v", got)
	}
	own := &CallToolResult{IsError: true, Content: []Content{&TextContent{Text: "limit must be 1..30"}}}
	if got := through(own, nil); got.err != nil || got.result.Content[0].(*TextContent).Text != "limit must be 1..30" {
		t.Errorf("a tool's own message was changed: %+v", got)
	}
	success := &CallToolResult{Content: []Content{&TextContent{Text: `{"indexed":true}`}}}
	if got := through(success, nil); got.err != nil || got.result.Content[0].(*TextContent).Text != `{"indexed":true}` || got.result.IsError {
		t.Errorf("a success result was changed: %+v", got)
	}
}

// An unknown tool fails before any result exists: the SDK hands the middleware
// a nil result with an error. Reading it crashed the server once.
func TestMiddlewareHandsOnAFailureWithoutAResult(t *testing.T) {
	failure := errors.New("unknown tool \"old_tool\"")
	got := through(nil, failure)
	if got.result != nil || !errors.Is(got.err, failure) {
		t.Errorf("a failure without a result was altered: %+v", got)
	}
}
