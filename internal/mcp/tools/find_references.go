package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const (
	FindReferencesDirectionIncoming = "incoming"
	FindReferencesDirectionOutgoing = "outgoing"
	DefaultReferenceLimit           = 50
	MaximumReferenceLimit           = hotsnapshot.MaxExactResults
	SortingVersionReferencesV1      = "references-v1"
	findReferencesToolName          = "find_references"
)

// FindReferencesInput identifies one symbol and the direct relationship page
// to inspect around it.
type FindReferencesInput struct {
	StableKey  string   `json:"stable_key"`
	Direction  string   `json:"direction,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	Language   string   `json:"language,omitempty"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
}

// ReferenceSummary is one exact adjacent symbol relationship.
type ReferenceSummary struct {
	SourceKey           string `json:"source_key"`
	TargetKey           string `json:"target_key"`
	Kind                string `json:"kind"`
	Confidence          string `json:"confidence"`
	Provenance          string `json:"provenance"`
	EvidenceKind        string `json:"evidence_kind"`
	SourceRepositoryKey string `json:"source_repository_key"`
	TargetRepositoryKey string `json:"target_repository_key"`
	SourceLanguage      string `json:"source_language"`
	TargetLanguage      string `json:"target_language"`
	SourceFileKey       string `json:"source_file_key"`
	TargetFileKey       string `json:"target_file_key"`
	SourceFilePath      string `json:"source_file_path"`
	TargetFilePath      string `json:"target_file_path"`
}

type findReferencesOptions struct {
	StableKey  string
	Direction  string
	Repo       string
	Language   string
	EdgeKinds  []string
	Confidence string
	Limit      int
}

type findReferencesQuery struct {
	Tool       string   `json:"tool"`
	StableKey  string   `json:"stable_key"`
	Direction  string   `json:"direction"`
	Repo       string   `json:"repo,omitempty"`
	Language   string   `json:"language,omitempty"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
}

type decodedReferenceEdge struct {
	Kind       facts.EdgeKind
	Confidence facts.Confidence
	Provenance facts.Provenance
}

// RegisterFindReferences adds the read-only reference lookup tool without a
// graph source. Calls require a snapshot-backed registration to return data.
func RegisterFindReferences(server *sdkmcp.Server) {
	RegisterFindReferencesWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterFindReferencesWithObserver adds find_references and optionally
// observes handler latency.
func RegisterFindReferencesWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterFindReferencesWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterFindReferencesWithSnapshotStore registers find_references over the
// immutable snapshot currently published by snapshotStore.
func RegisterFindReferencesWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterFindReferencesWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterFindReferencesWithObserverAndSnapshotStore registers
// find_references over a snapshot store and optionally observes latency.
func RegisterFindReferencesWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments FindReferencesInput,
	) (*sdkmcp.CallToolResult, Response[[]ReferenceSummary], error) {
		return findReferences(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindReferencesInput,
		) (*sdkmcp.CallToolResult, Response[[]ReferenceSummary], error) {
			start := time.Now()
			result, references, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findReferencesToolName, start, references, err)
			return result, references, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        findReferencesToolName,
		Description: "Finds direct symbol references in the incoming or outgoing direction.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func findReferences(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindReferencesInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[[]ReferenceSummary], error) {
	options, err := normalizeFindReferencesInput(arguments)
	if err != nil {
		return nil, Response[[]ReferenceSummary]{}, err
	}
	queryHash, err := HashQuery(findReferencesQuery{
		Tool: findReferencesToolName, StableKey: options.StableKey, Direction: options.Direction,
		Repo: options.Repo, Language: options.Language, EdgeKinds: options.EdgeKinds,
		Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[[]ReferenceSummary]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[[]ReferenceSummary]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[[]ReferenceSummary]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
	}

	startID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(options.StableKey))
	if !found {
		return nil, Response[[]ReferenceSummary]{}, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", options.StableKey))
	}
	if _, found := snapshot.Symbol(startID); !found {
		return nil, Response[[]ReferenceSummary]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot symbol index is inconsistent",
			fmt.Errorf("symbol index %d is missing", startID),
		)
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[[]ReferenceSummary]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionReferencesV1); err != nil {
			return nil, Response[[]ReferenceSummary]{}, err
		}
		offset = cursor.Offset
	}

	edges := snapshot.Incoming(startID)
	if options.Direction == FindReferencesDirectionOutgoing {
		edges = snapshot.Outgoing(startID)
	}
	results := make([]ReferenceSummary, 0, minReferenceInt(options.Limit, len(edges)))
	coverage := Coverage{}
	total := 0
	for _, edge := range edges {
		decoded, relevant, err := decodeReferenceEdge(edge)
		if err != nil {
			return nil, Response[[]ReferenceSummary]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains an invalid reference edge",
				err,
			)
		}
		if !relevant {
			continue
		}
		sourceID, targetID := edge.Target, startID
		if options.Direction == FindReferencesDirectionOutgoing {
			sourceID, targetID = startID, edge.Target
		}
		matches, err := referenceMatches(snapshot, sourceID, targetID, decoded, options)
		if err != nil {
			return nil, Response[[]ReferenceSummary]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains invalid reference metadata",
				err,
			)
		}
		if !matches {
			continue
		}
		addReferenceCoverage(&coverage, decoded.Confidence)
		if total >= offset && len(results) < options.Limit {
			reference, err := referenceSummary(snapshot, sourceID, targetID, edge, decoded)
			if err != nil {
				return nil, Response[[]ReferenceSummary]{}, WrapToolError(
					CodeSnapshotUnavailable,
					"active snapshot contains invalid reference evidence",
					err,
				)
			}
			results = append(results, reference)
		}
		total++
	}

	hasMore := offset <= total && total-offset > len(results)
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, offset+len(results), SortingVersionReferencesV1)
		if err != nil {
			return nil, Response[[]ReferenceSummary]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[[]ReferenceSummary]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[[]ReferenceSummary]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         total,
		Returned:      len(results),
		Truncated:     hasMore,
		NextCursor:    nextCursor,
		Coverage:      coverage,
		Results:       results,
	}, nil
}

func normalizeFindReferencesInput(arguments FindReferencesInput) (findReferencesOptions, error) {
	stableKey, err := normalizeSymbolStableKey(arguments.StableKey)
	if err != nil {
		return findReferencesOptions{}, err
	}
	direction := arguments.Direction
	if direction == "" {
		direction = FindReferencesDirectionIncoming
	}
	if direction != FindReferencesDirectionIncoming && direction != FindReferencesDirectionOutgoing {
		return findReferencesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("direction %q is unsupported", direction))
	}
	repo, err := normalizeReferenceFilter(arguments.Repo, "repo")
	if err != nil {
		return findReferencesOptions{}, err
	}
	language, err := normalizeReferenceFilter(arguments.Language, "language")
	if err != nil {
		return findReferencesOptions{}, err
	}
	edgeKinds, err := normalizeReferenceEdgeKinds(arguments.EdgeKinds)
	if err != nil {
		return findReferencesOptions{}, err
	}
	confidence, err := normalizeReferenceConfidence(arguments.Confidence)
	if err != nil {
		return findReferencesOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultReferenceLimit
	}
	if limit < 1 || limit > MaximumReferenceLimit {
		return findReferencesOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumReferenceLimit))
	}
	return findReferencesOptions{
		StableKey: stableKey, Direction: direction, Repo: repo, Language: language,
		EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit,
	}, nil
}

func normalizeReferenceFilter(value, field string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf("%s must not have surrounding whitespace", field))
	}
	return value, nil
}

func normalizeReferenceEdgeKinds(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" {
			return nil, NewToolError(CodeInvalidArgument, "edge_kinds must contain non-empty canonical values")
		}
		kind := facts.EdgeKind(value)
		if _, err := kind.Code(); err != nil || !isReferenceEdgeKind(kind) {
			return nil, NewToolError(CodeInvalidArgument, fmt.Sprintf("edge kind %q is unsupported for symbol references", value))
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeReferenceConfidence(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, "confidence must not have surrounding whitespace")
	}
	if _, err := facts.Confidence(value).Code(); err != nil {
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf("confidence %q is unsupported", value))
	}
	return value, nil
}

func decodeReferenceEdge(edge hotsnapshot.PackedEdge) (decodedReferenceEdge, bool, error) {
	kind, err := facts.EdgeKindFromCode(edge.Kind)
	if err != nil {
		return decodedReferenceEdge{}, false, err
	}
	if !isReferenceEdgeKind(kind) {
		return decodedReferenceEdge{}, false, nil
	}
	confidence, err := facts.ConfidenceFromCode(edge.Confidence)
	if err != nil {
		return decodedReferenceEdge{}, false, err
	}
	provenance, err := facts.ProvenanceFromCode(edge.Provenance)
	if err != nil {
		return decodedReferenceEdge{}, false, err
	}
	return decodedReferenceEdge{Kind: kind, Confidence: confidence, Provenance: provenance}, true, nil
}

func isReferenceEdgeKind(kind facts.EdgeKind) bool {
	switch kind {
	case facts.ImportsSymbol, facts.Exports, facts.Reexports,
		facts.References, facts.CallsDirect, facts.PassesAsCallback,
		facts.AssignsFunction, facts.ReturnsFunction, facts.TypeUses,
		facts.Implements, facts.Extends, facts.Embeds, facts.Overrides:
		return true
	default:
		return false
	}
}

func referenceMatches(
	snapshot *hotsnapshot.GraphSnapshot,
	sourceID, targetID hotsnapshot.SymbolID,
	decoded decodedReferenceEdge,
	options findReferencesOptions,
) (bool, error) {
	if len(options.EdgeKinds) > 0 && !containsString(options.EdgeKinds, string(decoded.Kind)) {
		return false, nil
	}
	if options.Confidence != "" && options.Confidence != string(decoded.Confidence) {
		return false, nil
	}
	relatedID := sourceID
	if options.Direction == FindReferencesDirectionOutgoing {
		relatedID = targetID
	}
	repository, languages, err := symbolRepositoryAndLanguages(snapshot, relatedID)
	if err != nil {
		return false, err
	}
	if options.Repo != "" {
		repositoryKey, ok := snapshot.Strings().String(repository.Key)
		if !ok {
			return false, fmt.Errorf("repository has an invalid stable key: %v", repository)
		}
		if repositoryKey != options.Repo {
			return false, nil
		}
	}
	if options.Language != "" && !containsString(languages, options.Language) {
		return false, nil
	}
	return true, nil
}

func referenceSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	sourceID, targetID hotsnapshot.SymbolID,
	edge hotsnapshot.PackedEdge,
	decoded decodedReferenceEdge,
) (ReferenceSummary, error) {
	source, sourceFile, sourceRepository, sourceLanguages, err := symbolReferenceLocation(snapshot, sourceID)
	if err != nil {
		return ReferenceSummary{}, err
	}
	target, targetFile, targetRepository, targetLanguages, err := symbolReferenceLocation(snapshot, targetID)
	if err != nil {
		return ReferenceSummary{}, err
	}
	evidence, found := snapshot.Evidence(edge.Evidence)
	if !found {
		return ReferenceSummary{}, fmt.Errorf("edge evidence index %d is missing", edge.Evidence)
	}
	evidenceKind, ok := snapshot.Strings().String(evidence.Kind)
	if !ok {
		return ReferenceSummary{}, fmt.Errorf("edge evidence %d has an invalid kind", edge.Evidence)
	}
	return ReferenceSummary{
		SourceKey:           string(source.StableKey),
		TargetKey:           string(target.StableKey),
		Kind:                string(decoded.Kind),
		Confidence:          string(decoded.Confidence),
		Provenance:          string(decoded.Provenance),
		EvidenceKind:        evidenceKind,
		SourceRepositoryKey: sourceRepository,
		TargetRepositoryKey: targetRepository,
		SourceLanguage:      firstString(sourceLanguages),
		TargetLanguage:      firstString(targetLanguages),
		SourceFileKey:       sourceFile.key,
		TargetFileKey:       targetFile.key,
		SourceFilePath:      sourceFile.path,
		TargetFilePath:      targetFile.path,
	}, nil
}

type symbolReferenceFile struct {
	key  string
	path string
}

func symbolReferenceLocation(
	snapshot *hotsnapshot.GraphSnapshot,
	id hotsnapshot.SymbolID,
) (hotsnapshot.SymbolRecord, symbolReferenceFile, string, []string, error) {
	symbol, found := snapshot.Symbol(id)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, "", nil, fmt.Errorf("symbol index %d is missing", id)
	}
	file, found := snapshot.File(symbol.File)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, "", nil, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	repository, found := snapshot.Repository(file.Repository)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, "", nil, fmt.Errorf("symbol %q references missing repository %d", symbol.StableKey, file.Repository)
	}
	table := snapshot.Strings()
	fileKey, fileKeyOK := table.String(file.Key)
	filePath, filePathOK := table.String(file.Path)
	repositoryKey, repositoryKeyOK := table.String(repository.Key)
	if !fileKeyOK || !filePathOK || !repositoryKeyOK {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, "", nil, fmt.Errorf("symbol %q references invalid file or repository strings", symbol.StableKey)
	}
	languages, err := symbolLanguages(snapshot, symbol, file, repository)
	if err != nil {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, "", nil, err
	}
	return symbol, symbolReferenceFile{key: fileKey, path: filePath}, repositoryKey, languages, nil
}

func symbolRepositoryAndLanguages(
	snapshot *hotsnapshot.GraphSnapshot,
	id hotsnapshot.SymbolID,
) (hotsnapshot.RepositoryRecord, []string, error) {
	symbol, found := snapshot.Symbol(id)
	if !found {
		return hotsnapshot.RepositoryRecord{}, nil, fmt.Errorf("symbol index %d is missing", id)
	}
	file, found := snapshot.File(symbol.File)
	if !found {
		return hotsnapshot.RepositoryRecord{}, nil, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	repository, found := snapshot.Repository(file.Repository)
	if !found {
		return hotsnapshot.RepositoryRecord{}, nil, fmt.Errorf("symbol %q references missing repository %d", symbol.StableKey, file.Repository)
	}
	languages, err := symbolLanguages(snapshot, symbol, file, repository)
	return repository, languages, err
}

func symbolLanguages(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	file hotsnapshot.FileRecord,
	repository hotsnapshot.RepositoryRecord,
) ([]string, error) {
	table := snapshot.Strings()
	if language, ok := table.String(symbol.Language); ok && language != "" {
		return []string{language}, nil
	}
	if language, ok := table.String(file.Language); ok && language != "" {
		return []string{language}, nil
	}
	repositoryLanguages, ok := table.String(repository.Languages)
	if !ok {
		return nil, fmt.Errorf("repository %d has invalid languages", file.Repository)
	}
	return splitRepositoryLanguages(repositoryLanguages), nil
}

func addReferenceCoverage(coverage *Coverage, confidence facts.Confidence) {
	if confidence.Exact() {
		coverage.Exact++
		return
	}
	if confidence == facts.Unresolved {
		coverage.UnresolvedRelated++
		return
	}
	coverage.Candidate++
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func minReferenceInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
