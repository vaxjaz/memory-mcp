package server

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

func strictToolArguments(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				if err := textguard.JSON(p.Arguments); err != nil {
					sum := sha256.Sum256(p.Arguments)
					message := fmt.Sprintf("MCP argument encoding error before tool decoding: %v; arguments_sha256=%x. No write performed. Resend intact UTF-8 from the original source; do not retry the damaged payload.", err, sum)
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, nil
				}
			}
		}
		return next(ctx, method, req)
	}
}
