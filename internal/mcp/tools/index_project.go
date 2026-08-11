package tools

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/indexing"
)

const indexProjectToolName = "index_project"

// IndexProjectInput is the explicit request accepted by index_project.
//
// Projects is the form to prefer: every project in one call is registered
// together and the graph is rebuilt once. A rebuild resolves
// cross-repository edges over the complete fact set, so it costs the whole
// corpus however many projects were added -- calling the tool once per
// project pays that cost once per project and keeps only the last result.
//
// The single-project fields remain for one repository and for clients that
// already send them.
//
// The confirmed flag is used by clients that do not implement MCP
// elicitation; those clients must obtain user approval before sending true.
type IndexProjectInput struct {
	Projects  []IndexProjectEntry `json:"projects,omitempty"`
	Name      string              `json:"name,omitempty"`
	Path      string              `json:"path,omitempty"`
	Languages []string            `json:"languages,omitempty"`
	Confirmed *bool               `json:"confirmed,omitempty"`
}

// IndexProjectEntry is one repository of a batch.
type IndexProjectEntry struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
}

// projects resolves the request into the batch to index, rejecting a request
// that names none and one that uses both forms at once, which could only
// disagree.
func (input IndexProjectInput) projects() ([]indexing.Project, error) {
	single := strings.TrimSpace(input.Name) != "" ||
		strings.TrimSpace(input.Path) != "" ||
		len(input.Languages) != 0
	switch {
	case len(input.Projects) != 0 && single:
		return nil, NewToolError(CodeInvalidArgument,
			"use projects for a batch or name/path/languages for one project, not both")
	case len(input.Projects) != 0:
		batch := make([]indexing.Project, 0, len(input.Projects))
		for _, entry := range input.Projects {
			batch = append(batch, indexing.Project{
				Name: entry.Name, Path: entry.Path, Languages: entry.Languages,
			})
		}
		return batch, nil
	case single:
		return []indexing.Project{{
			Name: input.Name, Path: input.Path, Languages: input.Languages,
		}}, nil
	default:
		return nil, NewToolError(CodeInvalidArgument,
			"index_project needs projects, or name, path and languages for a single project")
	}
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
		Description: "Registers one or more projects and rebuilds Ladygraph once, after explicit user approval. Pass every project in a single call: the rebuild costs the whole corpus, so calling this per project multiplies that cost and keeps only the last result. It never writes inside the source projects.",
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
		batch, err := arguments.projects()
		if err != nil {
			return nil, indexing.ProjectResult{}, err
		}
		if err := requireIndexConsent(ctx, request, batch, arguments.Confirmed); err != nil {
			return nil, indexing.ProjectResult{}, err
		}
		result, err := indexer.IndexProjects(ctx, batch, progressReporter(ctx, request))
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

// progressReporter forwards index progress to the client that asked for it.
//
// A full rebuild takes minutes on a large registry while a client applies its
// own timeout to the call -- thirty seconds in some -- and cancels work that
// is progressing fine. The protocol answers exactly this: a request that
// carries a progress token gets notifications, and a client that honours them
// waits for as long as the work reports.
//
// A client that sent no token gets no notifications and no callback at all, so
// the index does not pay for a channel nobody reads. Progress counts up and
// never repeats a value, which the protocol requires; a notification that
// cannot be delivered is dropped rather than failing the index.
func progressReporter(ctx context.Context, request *sdkmcp.CallToolRequest) func(indexing.ProjectProgress) {
	if request == nil || request.Session == nil || request.Params == nil {
		return nil
	}
	token := request.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	var steps atomic.Int64
	return func(update indexing.ProjectProgress) {
		message := strings.TrimSpace(strings.Join([]string{update.Phase, update.Repository, update.Detail}, " "))
		_ = request.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      float64(steps.Add(1)),
			Message:       message,
		})
	}
}

func requireIndexConsent(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	batch []indexing.Project,
	confirmed *bool,
) error {
	if request != nil && request.Session != nil {
		initialize := request.Session.InitializeParams()
		if initialize != nil && initialize.Capabilities != nil && initialize.Capabilities.Elicitation != nil {
			result, err := request.Session.Elicit(ctx, &sdkmcp.ElicitParams{
				Message: fmt.Sprintf(
					"Allow Ladygraph to register and index %s? This updates the registry and publishes a new graph generation.",
					describeBatch(batch),
				),
				RequestedSchema: &jsonschema.Schema{
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"confirmed": {
							Type:        "boolean",
							Description: "Approve registering and indexing these projects.",
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
	if confirmed == nil || !*confirmed {
		return NewToolError(
			CodePermissionRequired,
			"user approval is required; confirm the operation before setting confirmed=true",
		)
	}
	return nil
}

// describeBatch names what approval covers. Approving "11 projects" without
// seeing which ones is not approval.
func describeBatch(batch []indexing.Project) string {
	if len(batch) == 1 {
		return fmt.Sprintf("project %q at %q", batch[0].Name, batch[0].Path)
	}
	names := make([]string, 0, len(batch))
	for _, project := range batch {
		names = append(names, project.Name)
	}
	return fmt.Sprintf("%d projects (%s)", len(batch), strings.Join(names, ", "))
}

func elicitationConfirmed(content map[string]any) bool {
	confirmed, ok := content["confirmed"].(bool)
	return ok && confirmed
}
