package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The schema library reports a rejected call in its own vocabulary:
//
//	validating "arguments": validating root: validating /properties/experience: required: missing properties: ["cues"]
//
// readableArguments says the same with the names of the reference:
//
//	experience.cues is required
var (
	validationHead  = regexp.MustCompile(`^validating "arguments": validating root: `)
	validationLevel = regexp.MustCompile(`validating (/[^:]+): `)
	missingList     = regexp.MustCompile(`^required: missing properties: \[(.*)\]$`)
	quoted          = regexp.MustCompile(`"([^"]+)"`)
	wrongType       = regexp.MustCompile(`^type: .* has type "[^"]*", want (?:one of )?"([^"]+)"$`)
)

// readableArguments rewrites a schema validation message; any other text is
// returned as it is. Only types are repeated, never the submitted values.
func readableArguments(text string) string {
	if !validationHead.MatchString(text) {
		return text
	}
	rest := validationHead.ReplaceAllString(text, "")
	path := ""
	for {
		level := validationLevel.FindStringSubmatchIndex(rest)
		if level == nil {
			break
		}
		path = parameterPath(rest[level[2]:level[3]])
		rest = rest[level[1]:]
	}
	if list := missingList.FindStringSubmatch(rest); list != nil {
		missing := []string{}
		for _, name := range quoted.FindAllStringSubmatch(list[1], -1) {
			missing = append(missing, strings.TrimPrefix(path+"."+name[1], "."))
		}
		if len(missing) > 1 {
			last := missing[len(missing)-1]
			return strings.Join(missing[:len(missing)-1], ", ") + " and " + last + " are required"
		}
		return missing[0] + " is required"
	}
	if want := wrongType.FindStringSubmatch(rest); want != nil {
		kinds := strings.ReplaceAll(want[1], ", ", " or ")
		if base, ok := strings.CutSuffix(path, "[]"); ok {
			return fmt.Sprintf("%s must be an array of %s", base, kinds)
		}
		return fmt.Sprintf("%s must be %s", path, kinds)
	}
	if path == "" {
		return rest
	}
	return path + ": " + rest
}

// parameterPath turns a schema location into the reference's spelling:
// /properties/operations/items/properties/entry_id -> operations[].entry_id.
func parameterPath(location string) string {
	var parts []string
	segments := strings.Split(location, "/")
	for i, segment := range segments {
		branch := i > 0 && (segments[i-1] == "anyOf" || segments[i-1] == "oneOf") && isDigits(segment)
		last := func() string {
			if len(parts) == 0 {
				return ""
			}
			return parts[len(parts)-1]
		}
		switch {
		case segment == "" || segment == "properties" || branch, segment == "anyOf", segment == "oneOf":
		case segment == "items":
			parts = append(parts, "[]")
		case isDigits(segment) && last() == "[]":
			parts[len(parts)-1] = "[" + segment + "]"
		default:
			if last() != "" && last() != "." {
				parts = append(parts, ".")
			}
			parts = append(parts, segment)
		}
	}
	return strings.Join(parts, "")
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// readableArgumentErrors is applied to every tools/call result: the tool
// handlers' own messages pass through untouched. A call that failed before a
// result existed, such as an unknown tool, comes back as a nil result with an
// error and is handed on as it is.
func readableArgumentErrors(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if call, ok := result.(*mcp.CallToolResult); err == nil && ok && call != nil && call.IsError && len(call.Content) == 1 {
			if text, ok := call.Content[0].(*mcp.TextContent); ok && validationHead.MatchString(text.Text) {
				call.Content[0] = &mcp.TextContent{Text: readableArguments(text.Text)}
			}
		}
		return result, err
	}
}
