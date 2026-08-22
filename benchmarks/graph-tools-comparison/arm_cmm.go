package main

// The codebase-memory-mcp arm.
//
// This tool indexes a repository into a property graph and serves it over MCP.
// Its advertised path for "who calls this" is `trace_path`, and the previous
// measurement in benchmarks/codebase-memory-comparison recorded that path
// returning nothing on 0.8.1. It still does not answer on its own here, so the
// arm keeps calling it -- the cost of asking the documented tool and being told
// nothing is part of what this benchmark exists to count -- and then falls back
// to `query_graph`, whose Cypher over the same `CALLS` edges does answer.
//
// The graph resolves a call edge by callee name, not by type. Seven files in
// this corpus declare `withRetry`, and the callers of all of them land on one
// node, so a reference answer here is a union across homonyms rather than a
// resolution between them. That is a fact about the tool, and it is left in the
// claimed set rather than filtered out.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// cmmPageSize is the page `search_graph` is asked for. Its own default is 200
// and it truncates silently past that, so the arm sets the limit and follows
// `has_more` instead of trusting one page.
const cmmPageSize = 500

// cmmMaxPages bounds the pagination loop. A server that always answers
// `has_more: true` is a bug to be survived, not to be spun on.
const cmmMaxPages = 20

// cmmNoiseLabels are the graph's own scaffolding nodes. `search_graph` emits a
// `File` node named after the file and a `Module` node named after its path for
// every file matched, and neither is a declaration -- the tool's own
// documentation calls File/Folder/Module noise labels and filters them from its
// full-text mode. `Variable` is deliberately kept: a top-level `const` is a
// declaration, and one of the outline questions turns on it.
var cmmNoiseLabels = map[string]bool{"File": true, "Folder": true, "Module": true}

// cmmNode is one row of a `search_graph` answer.
type cmmNode struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Label         string `json:"label"`
	FilePath      string `json:"file_path"`
}

type cmmSearch struct {
	Total   int       `json:"total"`
	HasMore bool      `json:"has_more"`
	Results []cmmNode `json:"results"`
}

// cmmRows is a `query_graph` answer. The queries here project a single string
// column, and a null projects as the zero value rather than an error.
type cmmRows struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Total   int        `json:"total"`
}

type cmmTrace struct {
	Callers []cmmNode `json:"callers"`
}

// cmmProject mirrors how the tool names a project: the absolute repository path
// with every separator turned into a dash. The name is required on every call
// and is derivable, so the arm derives it rather than spending a `list_projects`
// call on every question.
func cmmProject(root string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = root
	}
	cleaned := strings.Trim(filepath.ToSlash(filepath.Clean(absolute)), "/")
	return strings.ReplaceAll(cleaned, "/", "-")
}

// cmmLiteral renders a Cypher string literal.
func cmmLiteral(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return "'" + strings.ReplaceAll(escaped, "'", `\'`) + "'"
}

// indexCodebaseMemory builds the graph. The isolated HOME is what keeps this
// off the user's own index: the tool stores every project under
// `$HOME/.cache/codebase-memory-mcp`.
func indexCodebaseMemory(ctx context.Context, binary, repoPath, home string) (float64, error) {
	tokens, err := newCounter()
	if err != nil {
		return 0, err
	}
	arguments, err := json.Marshal(map[string]string{"repo_path": repoPath})
	if err != nil {
		return 0, fmt.Errorf("encode index_repository arguments: %w", err)
	}
	// The working directory is the isolated home, never the corpus: nothing
	// this benchmark runs may write inside the thing it measures.
	out := runCLI(ctx, tokens, map[string]string{}, "cmm-index", home,
		map[string]string{"HOME": home}, binary, "cli", "index_repository", string(arguments))
	if out.Failed {
		return out.MS, fmt.Errorf("codebase-memory-mcp index %s: %s", repoPath, out.Error)
	}
	return out.MS, nil
}

// measureCodebaseMemory answers one question and prices every call it took.
func measureCodebaseMemory(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, corpusRoot, home string, q question) (*armResult, error) {
	switch q.Family {
	case familyConsumers:
		return &armResult{Unsupported: true, Note: "codebase-memory-mcp indexes one repository per call to " +
			"index_repository and its rows carry no repository, so it cannot say which side of a boundary a use is on"}, nil
	case familyDependencies:
		return &armResult{Unsupported: true, Note: "`search_graph` answers callers; the outward direction is not " +
			"a query it exposes"}, nil
	case familyLocate:
		return cmmLocate(ctx, tokens, repos, captures, binary, corpusRoot, home, q)
	case familyBodies:
		return &armResult{Unsupported: true, Note: "it returns graph nodes, not source text"}, nil
	case familyFacts:
		return &armResult{Unsupported: true, Note: "its nodes carry no kind and span per declaration"}, nil
	}
	srv, err := dial(ctx, "codebase-memory-mcp", binary, nil, map[string]string{"HOME": home})
	if err != nil {
		return nil, err
	}
	defer func() {
		srv.close()
		for key, value := range srv.captures {
			captures[key] = value
		}
	}()

	project := cmmProject(corpusRoot)
	if q.Family == familyOutline {
		return cmmOutline(ctx, tokens, srv, project, q)
	}
	return cmmCallers(ctx, tokens, srv, repos, project, q)
}

// cmmOutline answers "what is declared in this file" with `search_graph`
// scoped by `file_pattern`. Rows are kept only when the file path matches
// exactly: `file_pattern` is a pattern, so a neighbouring `x.test.ts` can match
// a request for `x.ts`, and the question asked about one file.
func cmmOutline(ctx context.Context, tokens *counter, srv *server, project string, q question) (*armResult, error) {
	result := &armResult{}
	path := q.Subject.corpusPath()
	claimed := []string{}
	offset := 0
	for page := range cmmMaxPages {
		out := srv.call(ctx, tokens, fmt.Sprintf("%s-cmm-search_graph-p%d", q.ID, page), "search_graph",
			map[string]any{
				"project":      project,
				"file_pattern": path,
				"limit":        cmmPageSize,
				"offset":       offset,
			})
		result.add(out)
		if out.Failed {
			break
		}
		answer := cmmSearch{}
		if err := json.Unmarshal([]byte(out.Text), &answer); err != nil {
			return nil, fmt.Errorf("%s: parse search_graph: %w", q.ID, err)
		}
		for _, node := range answer.Results {
			if node.FilePath != path || cmmNoiseLabels[node.Label] {
				continue
			}
			claimed = append(claimed, node.Name)
		}
		if !answer.HasMore {
			break
		}
		offset += cmmPageSize
	}
	result.Claimed = claimed
	// Outline truth is declaration names, which are already canonical.
	result.Score = scoreAgainst(claimed, q.Truth)
	return result, nil
}

// cmmCallers answers the reference and impact families. Both are the same walk
// over `CALLS` at a different hop count, so they share a path and differ only
// in depth.
func cmmCallers(ctx context.Context, tokens *counter, srv *server, repos repositories,
	project string, q question) (*armResult, error) {
	result := &armResult{}
	path := q.Subject.corpusPath()
	declaring := repos.canonical(path)
	hops := 1
	if q.Family == familyImpact && q.Depth > 0 {
		hops = q.Depth
	}

	// A caller has a bare name, so the first call is the one that turns it into
	// the address the graph indexes by.
	pattern := "^" + regexp.QuoteMeta(q.Subject.Symbol) + "$"
	find := srv.call(ctx, tokens, q.ID+"-cmm-search_graph", "search_graph",
		map[string]any{"project": project, "name_pattern": pattern, "limit": cmmPageSize})
	result.add(find)
	if find.Failed {
		result.Note = fmt.Sprintf("search_graph failed to locate %s: %s", q.Subject.Symbol, find.Error)
		result.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
		return result, nil
	}
	found := cmmSearch{}
	if err := json.Unmarshal([]byte(find.Text), &found); err != nil {
		return nil, fmt.Errorf("%s: parse search_graph: %w", q.ID, err)
	}

	// More than one declaration of the bare name is the ambiguity this
	// benchmark counts. The tool does not refuse -- it returns all of them, and
	// picking one is left to the caller -- so the arm records how many it named
	// and re-asks narrowed to the subject's own file, which is what a caller
	// who knows the answer's address would do.
	candidates := found.Results
	if found.Total > 1 {
		result.Ambiguous = found.Total
		narrow := srv.call(ctx, tokens, q.ID+"-cmm-search_graph-narrowed", "search_graph",
			map[string]any{
				"project": project, "name_pattern": pattern,
				"file_pattern": path, "limit": cmmPageSize,
			})
		result.add(narrow)
		if !narrow.Failed {
			narrowed := cmmSearch{}
			if err := json.Unmarshal([]byte(narrow.Text), &narrowed); err != nil {
				return nil, fmt.Errorf("%s: parse narrowed search_graph: %w", q.ID, err)
			}
			if len(narrowed.Results) > 0 {
				candidates = narrowed.Results
			}
		}
	}
	subject := cmmPick(candidates, path)
	if subject == nil {
		result.Note = fmt.Sprintf("search_graph named no declaration of %s in %s.", q.Subject.Symbol, path)
		result.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
		return result, nil
	}

	// The documented callers tool, asked the way its schema says to ask.
	trace := srv.call(ctx, tokens, q.ID+"-cmm-trace_path", "trace_path",
		map[string]any{
			"project": project, "function_name": q.Subject.Symbol,
			"mode": "calls", "direction": "inbound", "depth": hops, "include_tests": true,
		})
	result.add(trace)
	traced := cmmTrace{}
	if !trace.Failed {
		if err := json.Unmarshal([]byte(trace.Text), &traced); err != nil {
			return nil, fmt.Errorf("%s: parse trace_path: %w", q.ID, err)
		}
	}

	// The fallback that actually answers. `trace_path` addresses its callers by
	// qualified name only, which is not a file, so the file set comes from
	// Cypher over the same edges either way.
	cypher := fmt.Sprintf(
		"MATCH (c)-[:CALLS*1..%d]->(t) WHERE t.qualified_name = %s RETURN DISTINCT c.file_path AS file",
		hops, cmmLiteral(subject.QualifiedName))
	answer := srv.call(ctx, tokens, q.ID+"-cmm-query_graph", "query_graph",
		map[string]any{"project": project, "query": cypher})
	result.add(answer)
	if answer.Failed {
		result.Note = fmt.Sprintf("trace_path named %d callers and the query_graph fallback failed: %s",
			len(traced.Callers), answer.Error)
		result.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
		return result, nil
	}
	rows := cmmRows{}
	if err := json.Unmarshal([]byte(answer.Text), &rows); err != nil {
		return nil, fmt.Errorf("%s: parse query_graph: %w", q.ID, err)
	}

	claimed := []string{}
	for _, row := range rows.Rows {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		// The declaring file is excluded: every tool was told where the
		// subject lives, so counting it would flatter all of them equally.
		if file := repos.canonical(row[0]); file != declaring {
			claimed = append(claimed, file)
		}
	}
	result.Claimed = claimed
	result.Score = scoreAgainst(claimed, repos.canonicalAll(q.Truth))
	if len(traced.Callers) == 0 {
		result.Note = "trace_path returned no callers; the answer is the query_graph Cypher fallback over CALLS."
	} else {
		result.Note = fmt.Sprintf(
			"trace_path named %d callers by qualified name only; the file set is the query_graph Cypher over the same CALLS edges.",
			len(traced.Callers))
	}
	return result, nil
}

// cmmPick prefers the declaration in the subject's own file, which is the one
// the question is about, and falls back to the first row.
func cmmPick(nodes []cmmNode, path string) *cmmNode {
	for index := range nodes {
		if nodes[index].FilePath == path {
			return &nodes[index]
		}
	}
	if len(nodes) > 0 {
		return &nodes[0]
	}
	return nil
}

// cmmLocate answers "which files declare this name" with `search_graph`.
//
// The box read "not implemented rather than absent", which is a half promise:
// search_graph takes a name and answers with nodes that carry a file, so it can
// be asked. What it cannot do is separate a declaration from a use, so the note
// says that on the row rather than leaving the number to be read as precision.
func cmmLocate(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, corpusRoot, home string, q question) (*armResult, error) {
	srv, err := dial(ctx, "codebase-memory-mcp", binary, nil, map[string]string{"HOME": home})
	if err != nil {
		return nil, err
	}
	defer func() {
		srv.close()
		for key, value := range srv.captures {
			captures[key] = value
		}
	}()
	arm := &armResult{}
	answer := srv.call(ctx, tokens, q.ID+"-cmm-search_graph", "search_graph", map[string]any{
		"project": cmmProject(corpusRoot), "query": q.Subject.Symbol, "limit": 100,
	})
	arm.add(answer)
	decoded := cmmSearch{}
	claimed := []string{}
	if json.Unmarshal([]byte(answer.Text), &decoded) == nil {
		for _, node := range decoded.Results {
			if node.FilePath == "" {
				continue
			}
			if canonical := repos.canonical(node.FilePath); canonical != "" {
				claimed = append(claimed, canonical)
			}
		}
	}
	arm.Claimed = claimed
	arm.Score = scoreAgainst(claimed, repos.canonicalAll(q.Truth))
	arm.Note = "`search_graph` matches a name and returns the nodes carrying it; it does not separate a " +
		"declaration from a use, so a file that only calls the name is a hit"
	return arm, nil
}
