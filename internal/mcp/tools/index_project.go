package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// The confirmed flag is used by clients that cannot answer MCP form
// elicitation; those clients must obtain user approval before sending true.
type IndexProjectInput struct {
	Profile   string              `json:"profile,omitempty" jsonschema:"Profile to index; omit for the default. A missing name creates it."`
	Projects  []IndexProjectEntry `json:"projects,omitempty" jsonschema:"Every project to register, in one call. Prefer this form: a rebuild costs the whole corpus however many are added."`
	Name      string              `json:"name,omitempty" jsonschema:"Name for a single project. Use it with path and languages, never together with projects."`
	Path      string              `json:"path,omitempty" jsonschema:"Absolute directory of a single project. Nothing is written inside it."`
	Languages []string            `json:"languages,omitempty" jsonschema:"Languages to index in the single project, such as go, typescript, rust, python or dart."`
	Confirmed *bool               `json:"confirmed,omitempty" jsonschema:"Set true only after the user approved this call, and only from a client that cannot answer MCP form elicitation."`
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
		Description: "Registers projects and rebuilds after approval. Pass all projects together; never writes to them.",
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
			observeCall(nil, callObserver, indexProjectToolName, request, start, err)
			return nil, indexing.ProjectResult{}, err
		}
		pending, err := requireIndexConsent(request, batch, arguments.Confirmed)
		if err != nil {
			observeCall(nil, callObserver, indexProjectToolName, request, start, err)
			return nil, indexing.ProjectResult{}, err
		}
		if pending != nil {
			return pending, indexing.ProjectResult{}, nil
		}
		progress := progressReporter(ctx, request)
		result, err := runProjectIndex(ctx, indexer, arguments.Profile, batch, progress)
		if err != nil {
			if ErrorCode(err) == CodeInvalidArgument {
				observeCall(nil, callObserver, indexProjectToolName, request, start, err)
				return nil, indexing.ProjectResult{}, err
			}
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
			observeCall(nil, callObserver, indexProjectToolName, request, start, failure)
			return nil, indexing.ProjectResult{}, failure
		}
		observeCall(nil, callObserver, indexProjectToolName, request, start, nil)
		return nil, result, nil
	})
}

func runProjectIndex(
	ctx context.Context,
	indexer indexing.ProjectIndexer,
	profile string,
	batch []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return indexer.IndexProjects(ctx, batch, progress)
	}
	profileIndexer, ok := indexer.(profileProjectIndexer)
	if !ok {
		return indexing.ProjectResult{}, NewToolError(
			CodeInvalidArgument,
			fmt.Sprintf("project indexer does not support profile %q", profile),
		)
	}
	return profileIndexer.IndexProjectsInProfile(ctx, profile, batch, progress)
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

// indexConsentRequestID names the one question the indexing tools ask. The
// client echoes it back, so it is a label of ours and never shown.
const indexConsentRequestID = "consent"

// requireIndexConsent settles whether the caller approved registering and
// indexing this batch. A non-nil result is a question for the client, which is
// returned in place of an answer: the handler returns it as it is, and is called
// again with the response.
//
// This is the multi round-trip shape of SEP-2322, which protocol
// `2026-07-28` made the only one -- a server may no longer send
// `elicitation/create` while it serves a request -- and which the SDK carries
// back to earlier clients by asking the question itself and calling the handler
// again. One spelling serves both, so the consent does not depend on which
// version a client negotiated.
//
// The state echoed with the answer is a digest of the batch, so an approval
// belongs to what was asked: a response kept from one call does not approve
// another set of projects.
func requireIndexConsent(
	request *sdkmcp.CallToolRequest,
	batch []indexing.Project,
	confirmed *bool,
) (*sdkmcp.CallToolResult, error) {
	if request != nil && request.Session != nil && usesMCPFormElicitation(request.Session.InitializeParams()) {
		var responses sdkmcp.InputResponseMap
		state := ""
		if request.Params != nil {
			responses, state = request.Params.InputResponses, request.Params.RequestState
		}
		digest := batchDigest(batch)
		approved, answered := consentAnswer(responses, state, digest)
		switch {
		case answered && approved:
			return nil, nil
		case answered:
			return nil, NewToolError(CodePermissionDenied, "project indexing was not approved")
		}
		return &sdkmcp.CallToolResult{
			InputRequests: sdkmcp.InputRequestMap{
				indexConsentRequestID: &sdkmcp.ElicitParams{
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
				},
			},
			RequestState: digest,
		}, nil
	}
	if confirmed == nil || !*confirmed {
		return nil, NewToolError(
			CodePermissionRequired,
			"user approval is required; confirm the operation before setting confirmed=true",
		)
	}
	return nil, nil
}

// consentAnswer reads what the client answered to the consent question.
// `answered` is false when there is no answer to this batch -- none at all, or
// one that was given for another -- and the question is asked again; `approved`
// is only meaningful when it is true.
func consentAnswer(responses sdkmcp.InputResponseMap, state, digest string) (approved, answered bool) {
	response, present := responses[indexConsentRequestID]
	if !present || state != digest {
		return false, false
	}
	result, ok := response.(*sdkmcp.ElicitResult)
	if !ok || result == nil {
		return false, true
	}
	return result.Action == "accept" && elicitationConfirmed(result.Content), true
}

// batchDigest identifies a batch of projects by what the approval names: each
// project's name, path and languages, in the order given.
func batchDigest(batch []indexing.Project) string {
	hash := sha256.New()
	for _, project := range batch {
		fmt.Fprintf(hash, "%q\x00%q\x00%q\x00", project.Name, project.Path, strings.Join(project.Languages, ","))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// usesMCPFormElicitation selects the interactive consent path only when the
// client can actually answer a form elicitation. URL-only clients must use
// the explicit confirmed fallback because Elicit defaults to form requests.
// Codex exposes its own approval UI while currently returning a declined
// response to server-side elicitation, so its native approval is paired with
// the same confirmed fallback used by clients without elicitation support.
func usesMCPFormElicitation(initialize *sdkmcp.InitializeParams) bool {
	if initialize == nil || initialize.Capabilities == nil {
		return false
	}
	elicitation := initialize.Capabilities.Elicitation
	if elicitation == nil || isCodexClient(initialize.ClientInfo) {
		return false
	}
	return elicitation.Form != nil || elicitation.URL == nil
}

func isCodexClient(clientInfo *sdkmcp.Implementation) bool {
	if clientInfo == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(clientInfo.Name)), "codex")
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
