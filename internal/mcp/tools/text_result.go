package tools

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// addQueryTool registers a query tool that answers in exactly one channel.
//
// Left to itself the SDK returns every response twice: it marshals the typed
// handler result into `structuredContent` and, finding no content, repeats the
// same JSON in a text block. Measured on the six questions of
// `benchmarks/mcp-token-cost`, that duplicate was `24.066` bytes in one pass.
// Oh My Pi drops the structured channel and reads the text; a client that
// renders both is billed for the answer twice.
//
// So the text block is built here and the typed output is dropped. The SDK
// skips `structuredContent` when the output type is `any` and the tool declares
// no output schema, which is why no tool declares one: a tool that advertises
// an output schema and never fills it describes a response it does not send.
func addQueryTool[In, T any](
	server *sdkmcp.Server,
	tool *sdkmcp.Tool,
	handler func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, Response[T], error),
) {
	addTextTool(server, tool, handler)
}

// addTextTool registers a tool whose typed payload travels in one text block.
// Query tools use it through addQueryTool; control tools use it directly when
// their result is not the graph-query Response envelope.
func addTextTool[In, Out any](
	server *sdkmcp.Server,
	tool *sdkmcp.Tool,
	handler func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, Out, error),
) {
	if tool.OutputSchema != nil {
		// A programming error, not a runtime condition: it would resurrect the
		// duplicate channel this function exists to remove.
		panic(fmt.Sprintf("tool %q declares an output schema but answers in text only", tool.Name))
	}
	single := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments In,
	) (*sdkmcp.CallToolResult, any, error) {
		result, payload, err := handler(ctx, request, arguments)
		if err != nil {
			return result, nil, err
		}
		if result == nil {
			result = &sdkmcp.CallToolResult{}
		}
		if result.Content == nil {
			encoded, marshalErr := json.Marshal(payload)
			if marshalErr != nil {
				return nil, nil, fmt.Errorf("marshal %s response: %w", tool.Name, marshalErr)
			}
			result.Content = []sdkmcp.Content{&sdkmcp.TextContent{Text: string(encoded)}}
		}
		return result, nil, nil
	}
	sdkmcp.AddTool(server, tool, single)
}
