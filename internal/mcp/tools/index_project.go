package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/indexing"
)

const indexProjectToolName = "index_project"

// IndexProjectInput is the explicit request accepted by index_project. The
// confirmed flag is used by clients that do not implement MCP elicitation;
// those clients must obtain user approval before sending true.
type IndexProjectInput struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
	Confirmed *bool    `json:"confirmed,omitempty"`
}

// RegisterIndexProject adds the mutating project-indexing tool. The tool is
// absent when no configured indexer is supplied, so the empty/read-only MCP
// server cannot accidentally gain filesystem or storage access.
func RegisterIndexProject(server *sdkmcp.Server, indexer indexing.ProjectIndexer) {
	if server == nil || indexer == nil {
		return
	}
	confirmed := true
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        indexProjectToolName,
		Title:       "Index project",
		Description: "Registers a project and rebuilds Ladygraph after explicit user approval. It never writes inside the source project.",
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &confirmed,
			OpenWorldHint:   &confirmed,
		},
	}, func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments IndexProjectInput,
	) (*sdkmcp.CallToolResult, indexing.ProjectResult, error) {
		if err := requireIndexConsent(ctx, request, arguments); err != nil {
			return nil, indexing.ProjectResult{}, err
		}
		result, err := indexer.IndexProject(ctx, indexing.Project{
			Name:      arguments.Name,
			Path:      arguments.Path,
			Languages: arguments.Languages,
		})
		if err != nil {
			// This tool fails on the caller's own configuration: a
			// module that needs a newer toolchain, a path that is not
			// a repository, a dependency the module cache does not
			// hold. The stable code still leads the message, but the
			// observed cause travels with it: the transport renders
			// the error as text, so anything left in a side channel
			// forces an operator to reproduce the whole run on the
			// CLI to read one line.
			return nil, indexing.ProjectResult{}, WrapToolError(
				CodeIndexingFailed,
				"project indexing failed: "+err.Error(),
				err,
			)
		}
		return nil, result, nil
	})
}

func requireIndexConsent(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	arguments IndexProjectInput,
) error {
	if request != nil && request.Session != nil {
		initialize := request.Session.InitializeParams()
		if initialize != nil && initialize.Capabilities != nil && initialize.Capabilities.Elicitation != nil {
			result, err := request.Session.Elicit(ctx, &sdkmcp.ElicitParams{
				Message: fmt.Sprintf(
					"Allow Ladygraph to register and index project %q at %q? This updates the registry and publishes a new graph generation.",
					arguments.Name,
					arguments.Path,
				),
				RequestedSchema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"confirmed": {
							Type:        "boolean",
							Description: "Approve registering and indexing this project.",
						},
					},
					Required: []string{"confirmed"},
				},
			})
			if err != nil {
				return WrapToolError(CodePermissionRequired, "the client could not obtain indexing permission", err)
			}
			if result == nil || result.Action != "accept" || !elicitationConfirmed(result.Content) {
				return NewToolError(CodePermissionDenied, "project indexing was not approved")
			}
			return nil
		}
	}
	if arguments.Confirmed == nil || !*arguments.Confirmed {
		return NewToolError(
			CodePermissionRequired,
			"user approval is required; confirm the operation before setting confirmed=true",
		)
	}
	return nil
}

func elicitationConfirmed(content map[string]any) bool {
	confirmed, ok := content["confirmed"].(bool)
	return ok && confirmed
}
