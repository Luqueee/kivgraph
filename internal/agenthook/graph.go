package agenthook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
)

// clientName identifies the gate to a daemon's event log, so a reader can tell
// a hook's traffic from an editor's.
const clientName = "kivgraph-hook"

// sampleRows is how many declarations a refusal quotes back.
//
// Three is the most a person reads inside an error message. The point of the
// rows is to make the redirect concrete, not to answer the question the tool
// call will answer properly.
const sampleRows = 3

// referenceCeiling bounds the page the gate asks for.
//
// The gate only needs to know which side of `crowdedAt` a name falls on, so a
// page that stops short only ever understates the count -- and an understated
// count allows a call the gate might have refused, which is the direction to be
// wrong in.
const referenceCeiling = 50

// daemonGraph answers the gate's two questions from a running daemon.
type daemonGraph struct {
	session *sdkmcp.ClientSession
}

// Dial connects the gate to the daemon a state directory publishes.
//
// It never starts one. A hook runs on the user's keystroke, before a tool they
// asked for, and a gate that spawned an indexer there would turn a `grep` into
// a minute of waiting. No endpoint, a stale endpoint or a refused connection
// are all the same answer -- the gate has no graph and will stand aside -- so
// they are one error rather than a taxonomy no caller could act on.
func Dial(ctx context.Context, stateDirectory string) (Graph, func(), error) {
	endpoint, err := daemon.ReadEndpoint(stateDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("no daemon published an endpoint: %w", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: clientName, Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   endpoint.URL,
		HTTPClient: &http.Client{Transport: bearer{token: endpoint.Token, next: http.DefaultTransport}},
	}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", endpoint.URL, err)
	}
	return &daemonGraph{session: session}, func() { _ = session.Close() }, nil
}

// bearer attaches the token on every request, because the transport reconnects
// on its own and a token sent only on the first would fail the second.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (attach bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", daemon.BearerHeader(attach.token))
	return attach.next.RoundTrip(cloned)
}

// Symbol answers what the graph knows about a name, in one call.
//
// One call is the whole trick. `find_references` already refuses to choose when
// a name declares more than one symbol, and names the candidates when it does,
// so the ambiguity the gate is looking for arrives as a classified error rather
// than as a second question. Asking `find_symbol` first would double the round
// trips a hook pays for on every search.
func (graph *daemonGraph) Symbol(ctx context.Context, name string) (Facts, error) {
	result, err := graph.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "find_references",
		Arguments: map[string]any{
			"name": name, "view": "files", "limit": referenceCeiling,
		},
	})
	if err != nil {
		return Facts{}, fmt.Errorf("call find_references: %w", err)
	}
	if result.IsError {
		return ambiguityFacts(resultText(result)), nil
	}
	return referenceFacts(resultText(result))
}

// ambiguityFacts reads a failed call.
//
// It branches on the stable code and nothing else, which is what
// `internal/mcp/tools/errors.go` requires of a client. The message is read only
// to quote candidates back at the caller, and a message it cannot parse costs a
// less concrete refusal rather than a wrong one -- `AMBIGUOUS_SYMBOL` is
// already the whole reason to refuse.
func ambiguityFacts(text string) Facts {
	if !strings.Contains(text, tools.CodeAmbiguousSymbol) {
		// SYMBOL_NOT_FOUND, INDEX_NOT_READY and every other code mean the
		// gate learned nothing it can refuse on.
		return Facts{}
	}
	candidates := []string{}
	if _, tail, found := strings.Cut(text, "the one you mean:"); found {
		for _, candidate := range strings.Split(tail, ",") {
			if trimmed := strings.TrimSpace(candidate); trimmed != "" {
				candidates = append(candidates, trimmed)
			}
		}
	}
	declarations := len(candidates)
	if declarations < ambiguousAt {
		declarations = ambiguousAt
	}
	return Facts{
		Declarations: declarations,
		Repositories: countRepositories(candidates),
		Sample:       clip(candidates, sampleRows),
	}
}

// referenceEnvelope is a reference answer as the server actually sends it.
//
// The `files` view is nested under `results`, and the count the gate weighs is
// `total` at the envelope's root -- not the sum of the page, which the limit
// clips. Reading `files` from the root instead, which is what its own view type
// looks like in the source, decodes without error and reports zero references
// for every symbol in the graph.
type referenceEnvelope struct {
	Total   int `json:"total"`
	Results struct {
		Subject string `json:"subject"`
		Files   []struct {
			File  string `json:"file"`
			Count int    `json:"count"`
		} `json:"files"`
	} `json:"results"`
}

// referenceFacts reads a successful call.
func referenceFacts(text string) (Facts, error) {
	var envelope referenceEnvelope
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return Facts{}, fmt.Errorf("decode find_references: %w", err)
	}
	// The call resolved, so exactly one symbol carries the name: two would
	// have come back as AMBIGUOUS_SYMBOL instead of an answer.
	facts := Facts{Declarations: 1, References: envelope.Total}
	repositories, files := map[string]bool{}, []string{}
	for _, file := range envelope.Results.Files {
		repositories[repositoryOf(file.File)] = true
		files = append(files, file.File)
	}
	facts.Repositories = len(repositories)
	if facts.Repositories == 0 {
		// Nothing references it, which is a fact about the graph rather
		// than a failure: the subject resolved or this would be an error.
		facts.Repositories = 1
	}
	facts.Sample = clip(files, sampleRows)
	return facts, nil
}

// Intent answers what a loose description most likely names.

func (graph *daemonGraph) Intent(ctx context.Context, intent, repository string) (Facts, error) {
	arguments := map[string]any{"intent": intent, "limit": sampleRows, "view": "compact"}
	if repository != "" {
		arguments["repo"] = repository
	}
	result, err := graph.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "find_by_intent",
		Arguments: arguments,
	})
	if err != nil {
		return Facts{}, fmt.Errorf("call find_by_intent: %w", err)
	}
	if result.IsError {
		return Facts{}, nil
	}
	var matches intentEnvelope
	if err := json.Unmarshal([]byte(resultText(result)), &matches); err != nil {
		return Facts{}, fmt.Errorf("decode find_by_intent: %w", err)
	}
	symbols := matches.Results.Symbols
	if symbols == nil {
		// Daemons before profiles wrapped tool responses put the result at
		// the root. Keep accepting that shape while an updated hook may
		// briefly reach the daemon an older installation still owns.
		symbols = matches.Symbols
	}
	facts := Facts{Declarations: len(symbols)}
	repositories := map[string]bool{}
	for _, symbol := range symbols {
		name := symbol.QualifiedName
		if name == "" {
			name = symbol.QN
		}
		repositories[symbol.Repository] = true
		facts.Sample = append(facts.Sample,
			strings.TrimSpace(symbol.Repository+" "+symbol.FilePath+" "+name))
	}
	facts.Repositories = len(repositories)
	return facts, nil
}

type intentCandidate struct {
	QualifiedName string `json:"qualified_name"`
	QN            string `json:"qn"`
	Repository    string `json:"repository"`
	FilePath      string `json:"file_path"`
}

// intentEnvelope carries both response generations the hook can meet during
// an update: the current profile-aware envelope and the former root result.
type intentEnvelope struct {
	Symbols []intentCandidate `json:"symbols"`
	Results struct {
		Symbols []intentCandidate `json:"symbols"`
	} `json:"results"`
}

// resultText is the text a tool answered with.
func resultText(result *sdkmcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

// repositoryOf is the repository a `repository/path` row names.
//
// The `files` view joins the two with a slash; a candidate in an ambiguity
// message is `repository:path:line` instead. The two separators are the two
// call sites below, and reading one with the other's rule would count every row
// as the same repository.
func repositoryOf(row string) string {
	repository, _, _ := strings.Cut(strings.TrimSpace(row), "/")
	return repository
}

// countRepositories counts the distinct repositories a candidate list names.
func countRepositories(candidates []string) int {
	repositories := map[string]bool{}
	for _, candidate := range candidates {
		repository, _, _ := strings.Cut(strings.TrimSpace(candidate), ":")
		if repository != "" {
			repositories[repository] = true
		}
	}
	if len(repositories) == 0 {
		return 1
	}
	return len(repositories)
}

// clip keeps the first few rows.
func clip(rows []string, most int) []string {
	if len(rows) <= most {
		return rows
	}
	return rows[:most]
}
