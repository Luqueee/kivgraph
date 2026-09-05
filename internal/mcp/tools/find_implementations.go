package tools

import (
	"context"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const findImplementationsToolName = "find_implementations"

// FindImplementationsInput selects a declaration and a page of exact typed relations.
type FindImplementationsInput struct {
	Paths         []string `json:"paths,omitempty" jsonschema:"Repository-relative files or directories to include in implementation rows."`
	Profile       []string `json:"profile,omitempty" jsonschema:"Profiles; omit for default or use * alone for all."`
	StableKey     string   `json:"stable_key,omitempty" jsonschema:"Canonical subject key; alternatively use name or qualified_name."`
	Name          string   `json:"name,omitempty" jsonschema:"Subject name. Ambiguity returns candidates."`
	QualifiedName string   `json:"qualified_name,omitempty" jsonschema:"Fully qualified subject name."`
	Repository    string   `json:"repository,omitempty" jsonschema:"Repository declaring the subject."`
	Path          string   `json:"path,omitempty" jsonschema:"Repository-relative file declaring the subject."`
	Repo          string   `json:"repo,omitempty" jsonschema:"Filter implementation rows by repository."`
	Language      string   `json:"language,omitempty" jsonschema:"Filter rows by language."`
	Detection     string   `json:"detection,omitempty" jsonschema:"Filter evidence: declared or structural; omit for both and other typed mechanisms."`
	Limit         int      `json:"limit,omitempty" jsonschema:"Rows per page; default 50."`
	Cursor        string   `json:"cursor,omitempty" jsonschema:"Previous next_cursor; keep filters unchanged. Bound to the served generation."`
}

type ImplementationSummary struct {
	ReferenceSummary
	Detection string `json:"detection"`
}

type ImplementationResult struct {
	Subject         ReferenceSubject        `json:"subject"`
	Implementations []ImplementationSummary `json:"implementations"`
	Scope           string                  `json:"scope"`
}

func implementationDetection(provenance facts.Provenance) string {
	switch provenance {
	case facts.TypeScriptImplementationDeclared:
		return "declared"
	case facts.TypeScriptImplementationStructural:
		return "structural"
	case facts.GoTypesSelection, facts.GoTypesUse:
		return "structural"
	default:
		return "typed"
	}
}

func findImplementations(ctx context.Context, request *sdkmcp.CallToolRequest, arguments FindImplementationsInput, store *hotsnapshot.SnapshotStore) (*sdkmcp.CallToolResult, Response[ImplementationResult], error) {
	if arguments.Detection != "" && arguments.Detection != "declared" && arguments.Detection != "structural" {
		return nil, Response[ImplementationResult]{}, NewToolError(CodeInvalidArgument, "detection must be declared or structural")
	}
	paths := slices.Clone(arguments.Paths)
	for _, prefix := range paths {
		if prefix == "" || path.IsAbs(prefix) || path.Clean(prefix) != strings.TrimSuffix(prefix, "/") || prefix == ".." || strings.HasPrefix(prefix, "../") {
			return nil, Response[ImplementationResult]{}, NewToolError(CodeInvalidArgument, "paths must be clean repository-relative paths")
		}
	}
	slices.Sort(paths)
	query := FindReferencesInput{implementationPaths: paths, Profile: arguments.Profile, StableKey: arguments.StableKey, Name: arguments.Name, QualifiedName: arguments.QualifiedName, Repository: arguments.Repository, Path: arguments.Path, Repo: arguments.Repo, Language: arguments.Language, Limit: arguments.Limit, Cursor: arguments.Cursor, EdgeKinds: []string{string(facts.Implements), string(facts.Overrides)}, Direction: FindReferencesDirectionIncoming, ResponseFormat: ResponseFormatDetailed, View: ViewFull, implementationsOnly: true, implementationDetection: arguments.Detection}
	selected, count, err := resolveProfileSelection(store, arguments.Profile, arguments.StableKey)
	if err != nil {
		return nil, Response[ImplementationResult]{}, err
	}
	for i := range selected {
		selected[i].Store = hotsnapshot.NewSnapshotStore(selected[i].Store.Load())
	}
	var raw *sdkmcp.CallToolResult
	var references Response[ReferenceResult]
	if len(selected) > 1 {
		raw, references, err = findReferencesAcrossProfiles(ctx, request, query, selected)
	} else {
		raw, references, err = findReferences(ctx, request, query, selected[0].Store)
		scopeResponse(&references, selected[0].Name, count)
	}
	if err != nil {
		return raw, Response[ImplementationResult]{}, err
	}
	rows := make([]ImplementationSummary, 0, len(references.Results.References))
	for _, row := range references.Results.References {
		rows = append(rows, ImplementationSummary{ReferenceSummary: row, Detection: implementationDetection(facts.Provenance(row.Provenance))})
	}
	// Schema 5 attests that this generation ran implementation analysis. An old
	// graph can still answer Go relations, but its empty TS page proves nothing.
	for _, profile := range selected {
		if snapshot := profile.Store.Load(); snapshot != nil && snapshot.Metadata().SchemaVersion < 5 {
			if references.Completeness == nil {
				references.Completeness = &Completeness{}
			}
			references.Completeness.Verdict = VerdictLowerBound
			for i := range references.Profiles {
				if references.Profiles[i].Name == profile.Name {
					references.Profiles[i].Completeness = &Completeness{Verdict: VerdictLowerBound, InvisibleScopes: []BlindSpot{{Reason: "IMPLEMENTATIONS_NOT_ANALYZED", Detail: "Rebuild this legacy generation with implementation analysis."}}}
				}
			}
			references.Completeness.InvisibleScopes = append(references.Completeness.InvisibleScopes, BlindSpot{Reason: "IMPLEMENTATIONS_NOT_ANALYZED", Detail: "Rebuild this legacy generation with implementation analysis."})
		}
	}
	return raw, Response[ImplementationResult]{SnapshotID: references.SnapshotID, Profile: references.Profile, Profiles: references.Profiles, CrossProfileEdges: references.CrossProfileEdges, SnapshotAgeMS: references.SnapshotAgeMS, Total: references.Total, Returned: references.Returned, Truncated: references.Truncated, NextCursor: references.NextCursor, Coverage: references.Coverage, Completeness: references.Completeness, Guidance: "Absence is established only for COMPLETE coverage within the analyzed corpus. Concrete classes and observed type instances; no unknown generic arguments are replaced with any.", View: ViewFull, Results: ImplementationResult{Subject: references.Results.Subject, Implementations: rows, Scope: "Typed IMPLEMENTS and OVERRIDES relations in the selected published generation(s)."}}, nil
}

func RegisterFindImplementationsWithObserverAndSnapshotStore(server *sdkmcp.Server, observer Observer, store *hotsnapshot.SnapshotStore, callObservers ...CallObserver) {
	handler := func(ctx context.Context, request *sdkmcp.CallToolRequest, arguments FindImplementationsInput) (*sdkmcp.CallToolResult, Response[ImplementationResult], error) {
		start := time.Now()
		result, response, err := findImplementations(ctx, request, arguments, store)
		observe(observer, firstCallObserver(callObservers), findImplementationsToolName, start, response, err)
		return result, response, err
	}
	addQueryTool(server, &sdkmcp.Tool{Name: findImplementationsToolName, Description: "Implementations of types or methods, including structural TypeScript. Paged, with compiler evidence and coverage.", Annotations: readOnlyClosedWorld(), Meta: alwaysLoadMeta()}, handler)
}
