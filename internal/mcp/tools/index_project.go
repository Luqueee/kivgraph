package tools

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/indexing"
)

const indexProjectToolName = "index_project"

type profileProjectIndexer interface {
	IndexProjectsInProfile(context.Context, string, []indexing.Project, func(indexing.ProjectProgress)) (indexing.ProjectResult, error)
}

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
	Profile   string              `json:"profile,omitempty" jsonschema:"Profile to index; omit for the default. A missing name creates it."`
	Projects  []IndexProjectEntry `json:"projects,omitempty" jsonschema:"Every project to register, in one call. Prefer this form: a rebuild costs the whole corpus however many are added."`
	Name      string              `json:"name,omitempty" jsonschema:"Name for a single project. Use it with path and languages, never together with projects."`
	Path      string              `json:"path,omitempty" jsonschema:"Absolute directory of a single project. Nothing is written inside it."`
	Languages []string            `json:"languages,omitempty" jsonschema:"Languages to index in the single project, such as go, typescript, rust, python or dart."`
	Confirmed *bool               `json:"confirmed,omitempty" jsonschema:"Set true only after the user approved this call, and only from a client that cannot answer an elicitation."`
}

// IndexProjectEntry is one repository of a batch.
type IndexProjectEntry struct {
	Name      string   `json:"name" jsonschema:"Name to register the repository under. It is an identifier, compared exactly."`
	Path      string   `json:"path" jsonschema:"Absolute directory of the repository. Nothing is written inside it."`
	Languages []string `json:"languages" jsonschema:"Languages to index here, such as go, typescript, rust, python or dart."`
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
//
// The observers are variadic because every other Register* in this package
// takes them that way, and because this tool acquired them late: it is the one
// call that costs minutes, so leaving it untimed made the log quietest exactly
// where a reader most needs it.
func RegisterIndexProject(
	server *sdkmcp.Server,
	indexer indexing.ProjectIndexer,
	callObservers ...CallObserver,
) {
	if server == nil || indexer == nil {
		return
	}
	callObserver := firstCallObserver(callObservers)
	confirmed := true
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        indexProjectToolName,
		Title:       "Index project",
		Description: "Registers projects and rebuilds the graph once, after explicit user approval. Pass every project in one call: a rebuild costs the whole corpus. It never writes inside the source projects.",
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
		start := time.Now()
		batch, err := arguments.projects()
		if err != nil {
			observeCall(nil, callObserver, indexProjectToolName, start, err)
			return nil, indexing.ProjectResult{}, err
		}
		if err := requireIndexConsent(ctx, request, batch, arguments.Confirmed); err != nil {
			observeCall(nil, callObserver, indexProjectToolName, start, err)
			return nil, indexing.ProjectResult{}, err
		}
		progress := progressReporter(ctx, request)
		var result indexing.ProjectResult
		if profile := strings.TrimSpace(arguments.Profile); profile != "" {
			profileIndexer, ok := indexer.(profileProjectIndexer)
			if !ok {
				err = fmt.Errorf("project indexer does not support profile %q", profile)
			} else {
				result, err = profileIndexer.IndexProjectsInProfile(ctx, profile, batch, progress)
			}
		} else {
			result, err = indexer.IndexProjects(ctx, batch, progress)
		}
		if err != nil {
			// This tool fails on the caller's own configuration: a
			// module that needs a newer toolchain, a path that is not
			// a repository, a dependency the module cache does not
			// hold. The stable code still leads the message, but the
			// observed cause travels with it: the transport renders
			// the error as text, so anything left in a side channel
			// forces an operator to reproduce the whole run on the
			// CLI to read one line.
			failure := WrapToolError(
				CodeIndexingFailed,
				"project indexing failed: "+err.Error(),
				err,
			)
			observeCall(nil, callObserver, indexProjectToolName, start, failure)
			return nil, indexing.ProjectResult{}, failure
		}
		observeCall(nil, callObserver, indexProjectToolName, start, nil)
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
					"Allow Kivgraph to register and index %s? This updates the registry and publishes a new graph generation.",
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
