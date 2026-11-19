package server

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// The SDK's inferred schemas reject recursive Go types. Keep inference for the
// record envelope, and describe the recursive cue expression with a local ref.
func cueWriteSchema[T any]() *jsonschema.Schema {
	const name = "cueMatch"
	ref := func() *jsonschema.Schema { return &jsonschema.Schema{Ref: "#/$defs/" + name} }
	input, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[matching.Expression](): ref(),
	}})
	if err != nil {
		panic(err)
	}
	expr, err := jsonschema.For[matching.Rule](nil)
	if err != nil {
		panic(err)
	}
	expr.Description = "One comparison, all, any, not, or fact, with an optional when prerequisite. Groups are OR alternatives; each successful group needs a positive entrance."
	stringType := func() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }
	expr.Properties["field"] = &jsonschema.Schema{Type: "string", Description: "A named tool field from the effective registration. Omit to compare entrances against available text fields."}
	expr.Properties["entrances"] = &jsonschema.Schema{Type: "array", Items: stringType(), Description: "Literal entrance strings with their original spelling. Any one can match."}
	expr.Properties["value"] = &jsonschema.Schema{Types: []string{"string", "number", "boolean"}, Description: "An additional comparison guard. Use entrances for indexable keywords."}
	when := ref()
	when.Description = "Optional prerequisite. It must pass before the rest of this match is evaluated. Compare the native tool name, exposed fields or earlier facts. Use value in the prerequisite and entrances alongside it."
	expr.Properties["when"] = when
	expr.Properties["all"] = &jsonschema.Schema{Type: "array", Items: ref()}
	expr.Properties["any"] = &jsonschema.Schema{Type: "array", Items: ref()}
	expr.Properties["not"] = ref()
	expr.Properties["fact"] = &jsonschema.Schema{
		Type: "object", Required: []string{"name", "key", "match"},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		Properties: map[string]*jsonschema.Schema{
			"name":  {Type: "string", Description: "Registered result observation name."},
			"key":   {Type: "array", Items: stringType(), Description: "Current named fields in producer-key order."},
			"match": ref(),
		},
	}
	input.Defs = map[string]*jsonschema.Schema{name: expr}
	return input
}
