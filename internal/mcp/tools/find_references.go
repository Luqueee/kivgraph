package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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
	StableKey      string   `json:"stable_key,omitempty"`
	QualifiedName  string   `json:"qualified_name,omitempty"`
	Repository     string   `json:"repository,omitempty"`
	Path           string   `json:"path,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Repo           string   `json:"repo,omitempty"`
	Language       string   `json:"language,omitempty"`
	EdgeKinds      []string `json:"edge_kinds,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	IncludeDerived bool     `json:"include_derived,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
}

// ReferenceSubject is the symbol the query asked about. It is stated once per
// response instead of on every row: it is the argument, not a result, and
// repeating it on each row was most of the payload.
type ReferenceSubject struct {
	StableKey     string `json:"stable_key"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Repository    string `json:"repository"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
}

// ReferenceSummary is the other end of one exact relationship: the symbol
// holding the reference for an incoming query, the one being reached for an
// outgoing one. It is named and located, because a caller identified only by
// an opaque key costs one more call per row before it means anything.
//
// StartLine and EndLine bound the declaration of that symbol, not the position
// of the token. The snapshot records which symbol contains a reference and
// never where inside it: publishing a line nobody observed would be inventing
// evidence. The range is what makes the row openable without a second call --
// without EndLine every row cost one `get_symbol` first, which measured 15
// extra calls across the six questions of `benchmarks/mcp-token-cost`.
type ReferenceSummary struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Repository    string `json:"repository"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
	EndLine       uint32 `json:"end_line"`
	Language      string `json:"language"`
	EdgeKind      string `json:"edge_kind"`
	Confidence    string `json:"confidence"`
	Provenance    string `json:"provenance"`

	EvidenceKind  string `json:"evidence_kind,omitempty"`
	StableKey     string `json:"stable_key,omitempty"`
	FileKey       string `json:"file_key,omitempty"`
	RepositoryKey string `json:"repository_key,omitempty"`
}

// ReferenceResult is one page of relationships around a subject.
type ReferenceResult struct {
	Subject    ReferenceSubject   `json:"subject"`
	Direction  string             `json:"direction"`
	References []ReferenceSummary `json:"references"`
}

type findReferencesOptions struct {
	Selector   symbolSelector
	Direction  string
	Repo       string
	Language   string
	EdgeKinds  []string
	Confidence string
	Derived    derivedFilter
	Limit      int
}

type findReferencesQuery struct {
	Tool          string   `json:"tool"`
	StableKey     string   `json:"stable_key,omitempty"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	Repository    string   `json:"repository,omitempty"`
	Path          string   `json:"path,omitempty"`
	Direction     string   `json:"direction"`
	Repo          string   `json:"repo,omitempty"`
	Language      string   `json:"language,omitempty"`
	EdgeKinds     []string `json:"edge_kinds,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
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
	) (*sdkmcp.CallToolResult, Response[ReferenceResult], error) {
		return findReferences(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindReferencesInput,
		) (*sdkmcp.CallToolResult, Response[ReferenceResult], error) {
			start := time.Now()
			result, references, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findReferencesToolName, start, references, err)
			return result, references, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        findReferencesToolName,
		Description: "Who calls or references a symbol. Type-checked, not name-matched: grep cannot separate homonyms, and an empty answer means nobody calls it. A rare name in one repository is cheaper to grep.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		Meta:        alwaysLoadMeta(),
	}, handler)
}

func findReferences(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindReferencesInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[ReferenceResult], error) {
	options, err := normalizeFindReferencesInput(arguments)
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}
	queryHash, err := HashQuery(findReferencesQuery{
		Tool: findReferencesToolName, StableKey: options.Selector.StableKey,
		QualifiedName: options.Selector.QualifiedName, Repository: options.Selector.Repository,
		Path: options.Selector.Path, Direction: options.Direction,
		Repo: options.Repo, Language: options.Language, EdgeKinds: options.EdgeKinds,
		Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[ReferenceResult]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[ReferenceResult]{}, ErrIndexNotReady()
	}

	startID, err := resolveSymbolSelector(snapshot, options.Selector)
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}
	// referenceSubject resolves the start symbol and everything it needs to
	// be named, so a missing index shows up here rather than twice.
	subject, err := referenceSubject(snapshot, startID)
	if err != nil {
		return nil, Response[ReferenceResult]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot symbol index is inconsistent",
			err,
		)
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[ReferenceResult]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionReferencesV1); err != nil {
			return nil, Response[ReferenceResult]{}, err
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
			return nil, Response[ReferenceResult]{}, WrapToolError(
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
			return nil, Response[ReferenceResult]{}, WrapToolError(
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
			reference, err := referenceSummary(snapshot, edge.Target, edge, decoded, format)
			if err != nil {
				return nil, Response[ReferenceResult]{}, WrapToolError(
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
			return nil, Response[ReferenceResult]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[ReferenceResult]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[ReferenceResult]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         total,
		Returned:      len(results),
		Truncated:     hasMore,
		NextCursor:    nextCursor,
		Coverage:      coverage,
		Guidance:      referenceGuidance(options.Direction, total, len(results), hasMore),
		Results:       ReferenceResult{Subject: subject, Direction: options.Direction, References: results},
	}, nil
}

func normalizeFindReferencesInput(arguments FindReferencesInput) (findReferencesOptions, error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
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
		Selector: selector, Direction: direction, Repo: repo, Language: language,
		EdgeKinds: edgeKinds, Confidence: confidence, Limit: limit,
		Derived: newDerivedFilter(arguments.IncludeDerived, repo),
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
		name, ok := snapshot.Strings().String(repository.Name)
		if !ok {
			return false, fmt.Errorf("repository has an invalid name: %v", repository)
		}
		if name != options.Repo {
			return false, nil
		}
	}
	if options.Language != "" && !containsString(languages, options.Language) {
		return false, nil
	}
	if !options.Derived.keepsAll() {
		name, ok := snapshot.Strings().String(repository.Name)
		if !ok {
			return false, fmt.Errorf("repository has an invalid name: %v", repository)
		}
		if !options.Derived.keepsRepository(name) {
			return false, nil
		}
	}
	return true, nil
}

// referenceSummary describes the other end of an edge. Which end that is
// depends on the direction the caller asked for, and both CSR directions put
// it in the same place: the subject is the traversal start, never a row.
func referenceSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	otherID hotsnapshot.SymbolID,
	edge hotsnapshot.PackedEdge,
	decoded decodedReferenceEdge,
	format string,
) (ReferenceSummary, error) {
	other, file, repository, languages, err := symbolReferenceLocation(snapshot, otherID)
	if err != nil {
		return ReferenceSummary{}, err
	}
	table := snapshot.Strings()
	name, nameOK := table.String(other.Name)
	qualifiedName, qualifiedNameOK := table.String(other.QualifiedName)
	kind, kindOK := table.String(other.Kind)
	if !nameOK || !qualifiedNameOK || !kindOK {
		return ReferenceSummary{}, fmt.Errorf(
			"symbol %q has invalid metadata (name_ok=%t qualified_name_ok=%t kind_ok=%t)",
			other.StableKey, nameOK, qualifiedNameOK, kindOK,
		)
	}
	location, err := resolveSymbolLocation(snapshot, other)
	if err != nil {
		return ReferenceSummary{}, err
	}
	summary := ReferenceSummary{
		Name:          name,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Repository:    location.RepositoryName,
		FilePath:      file.path,
		StartLine:     other.StartLine,
		EndLine:       other.EndLine,
		Language:      firstString(languages),
		EdgeKind:      string(decoded.Kind),
		Confidence:    string(decoded.Confidence),
		Provenance:    string(decoded.Provenance),
	}
	if format != ResponseFormatDetailed {
		return summary, nil
	}
	evidence, found := snapshot.Evidence(edge.Evidence)
	if !found {
		return ReferenceSummary{}, fmt.Errorf("edge evidence index %d is missing", edge.Evidence)
	}
	evidenceKind, ok := table.String(evidence.Kind)
	if !ok {
		return ReferenceSummary{}, fmt.Errorf("edge evidence %d has an invalid kind", edge.Evidence)
	}
	summary.EvidenceKind = evidenceKind
	summary.StableKey = string(other.StableKey)
	summary.FileKey = file.key
	// The detailed format restores the derived identifiers, and a repository key
	// is derived from its name by construction.
	summary.RepositoryKey = repository.key
	return summary, nil
}

// referenceSubject describes the symbol the query is about, once.
func referenceSubject(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.SymbolID) (ReferenceSubject, error) {
	symbol, file, _, _, err := symbolReferenceLocation(snapshot, id)
	if err != nil {
		return ReferenceSubject{}, err
	}
	table := snapshot.Strings()
	name, nameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	kind, kindOK := table.String(symbol.Kind)
	if !nameOK || !qualifiedNameOK || !kindOK {
		return ReferenceSubject{}, fmt.Errorf(
			"symbol %q has invalid metadata (name_ok=%t qualified_name_ok=%t kind_ok=%t)",
			symbol.StableKey, nameOK, qualifiedNameOK, kindOK,
		)
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return ReferenceSubject{}, err
	}
	return ReferenceSubject{
		StableKey:     string(symbol.StableKey),
		Name:          name,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Repository:    location.RepositoryName,
		FilePath:      file.path,
		StartLine:     symbol.StartLine,
	}, nil
}

type symbolReferenceFile struct {
	key  string
	path string
}

// symbolReferenceRepository is the repository a row belongs to, under both
// identities the snapshot stores.
//
// A row carries the name, because the triple every tool accepts takes a name and
// a row exists to be reopened; handing back `repository:app` made a caller strip
// a prefix nobody documented. The key is what the detailed format restores, and
// it is read rather than derived: the graph stores it, so composing it here would
// be a second source of truth for the same fact.
type symbolReferenceRepository struct {
	name string
	key  string
}

// symbolReferenceLocation answers the symbol, its file and its repository.
func symbolReferenceLocation(
	snapshot *hotsnapshot.GraphSnapshot,
	id hotsnapshot.SymbolID,
) (hotsnapshot.SymbolRecord, symbolReferenceFile, symbolReferenceRepository, []string, error) {
	symbol, found := snapshot.Symbol(id)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol index %d is missing", id)
	}
	file, found := snapshot.File(symbol.File)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	repository, found := snapshot.Repository(file.Repository)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references missing repository %d", symbol.StableKey, file.Repository)
	}
	table := snapshot.Strings()
	fileKey, fileKeyOK := table.String(file.Key)
	filePath, filePathOK := table.String(file.Path)
	repositoryName, repositoryNameOK := table.String(repository.Name)
	repositoryKey, repositoryKeyOK := table.String(repository.Key)
	if !fileKeyOK || !filePathOK || !repositoryNameOK || !repositoryKeyOK {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references invalid file or repository strings", symbol.StableKey)
	}
	languages, err := symbolLanguages(snapshot, symbol, file, repository)
	if err != nil {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, err
	}
	return symbol, symbolReferenceFile{key: fileKey, path: filePath},
		symbolReferenceRepository{name: repositoryName, key: repositoryKey}, languages, nil
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
