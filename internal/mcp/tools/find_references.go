package tools

import (
	"context"
	"encoding/json"
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
//
// Name is the unqualified name, and it exists so the common question costs one
// call: with a single declaration of that name the tool answers about it, and
// with several it returns the candidates as `repository:path:line` instead of
// answering about one nobody chose. Repository and Path narrow it.
//
// View is the granularity of the answer: `compact` groups the rows by file and
// hoists whatever every row shares, `full` keeps the field-per-row shape, and
// `files` answers only which files hold references and how many each holds.
type FindReferencesInput struct {
	StableKey      string   `json:"stable_key,omitempty"`
	QualifiedName  string   `json:"qualified_name,omitempty"`
	Name           string   `json:"name,omitempty"`
	Repository     string   `json:"repository,omitempty"`
	Path           string   `json:"path,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Repo           string   `json:"repo,omitempty"`
	Language       string   `json:"language,omitempty"`
	EdgeKinds      []string `json:"edge_kinds,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	IncludeDerived bool     `json:"include_derived,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	View           string   `json:"view,omitempty"`
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
	// Via is the interface method this reference reached the subject through,
	// set only when the subject is the one implementation of it. Empty means
	// the row is a direct reference to the subject itself.
	Via string `json:"via,omitempty"`

	EvidenceKind  string `json:"evidence_kind,omitempty"`
	StableKey     string `json:"stable_key,omitempty"`
	FileKey       string `json:"file_key,omitempty"`
	RepositoryKey string `json:"repository_key,omitempty"`
}

// ReferenceResult is one page of relationships around a subject. View decides
// how it is written, never which facts it holds: see MarshalJSON below.
type ReferenceResult struct {
	Subject    ReferenceSubject   `json:"subject"`
	Direction  string             `json:"direction"`
	References []ReferenceSummary `json:"references"`
	// EdgeKindsExcluded names the kinds a filter the caller never asked for
	// left out. Present in every view, because a count that does not say it
	// was filtered understates the references to a symbol.
	EdgeKindsExcluded []string `json:"edge_kinds_default_excluded,omitempty"`
	// DispatchThrough names the interface methods whose references this answer
	// also holds, because the subject is their one implementation. Present only
	// when a row came that way, and every such row repeats it in `via`.
	DispatchThrough []string `json:"dispatch_through,omitempty"`
	View            string   `json:"-"`
}

// MarshalJSON writes the page under the view the caller asked for. The compact
// and files shapes carry the same edges as full: a column leaves a row only
// when the header states it for every row, and `confidence` and `provenance`
// are always readable in one of the two places.
func (result ReferenceResult) MarshalJSON() ([]byte, error) {
	type fullResult struct {
		Subject           ReferenceSubject   `json:"subject"`
		Direction         string             `json:"direction"`
		References        []ReferenceSummary `json:"references"`
		EdgeKindsExcluded []string           `json:"edge_kinds_default_excluded,omitempty"`
		DispatchThrough   []string           `json:"dispatch_through,omitempty"`
	}
	switch result.View {
	case ViewCompact:
		return json.Marshal(result.compact())
	case ViewFiles:
		return json.Marshal(result.files())
	default:
		return json.Marshal(fullResult{
			Subject:           result.Subject,
			Direction:         result.Direction,
			References:        result.References,
			EdgeKindsExcluded: result.EdgeKindsExcluded,
			DispatchThrough:   result.DispatchThrough,
		})
	}
}

func (result ReferenceResult) subjectLabel() string {
	return locationLabel(result.Subject.Repository, result.Subject.FilePath, result.Subject.StartLine)
}

// compact hoists to the page header what every row shares, then groups what is
// left. `kind`, `edge_kind`, `confidence` and `provenance` each hoist
// independently when the whole page agrees; the rows that do not all agree on
// one of them are grouped by the exact tuple they still share, so the tuple is
// stated once per group instead of once per row. Measured on a 66-row page
// where one dissenting export kept `kind` and `edge_kind` off the header: three
// groups replaced sixty-six row tails, one of them covering 62 rows.
func (result ReferenceResult) compact() compactReferenceResult {
	rows := result.References
	count := len(rows)
	compact := compactReferenceResult{
		Subject:    result.subjectLabel(),
		QN:         result.Subject.QualifiedName,
		Direction:  result.Direction,
		Repository: hoistString(count, func(index int) string { return rows[index].Repository }),
		Kind:       hoistString(count, func(index int) string { return rows[index].Kind }),
		EdgeKind:   hoistString(count, func(index int) string { return rows[index].EdgeKind }),
		Confidence: hoistString(count, func(index int) string { return rows[index].Confidence }),
		Provenance: hoistString(count, func(index int) string { return rows[index].Provenance }),
		Via:        hoistString(count, func(index int) string { return rows[index].Via }),

		EdgeKindsExcluded: result.EdgeKindsExcluded,
		DispatchThrough:   result.DispatchThrough,
	}

	residual := func(row ReferenceSummary) []string {
		return []string{
			blankWhenHoisted(row.Kind, compact.Kind),
			blankWhenHoisted(row.EdgeKind, compact.EdgeKind),
			blankWhenHoisted(row.Confidence, compact.Confidence),
			blankWhenHoisted(row.Provenance, compact.Provenance),
			blankWhenHoisted(row.Via, compact.Via),
		}
	}
	flatFiles := referenceFileGroups(rows, compact.Repository, compact.Kind, compact.EdgeKind, compact.Confidence, compact.Provenance)
	groups := groupByResidual(rows, residual)
	if len(groups) <= 1 {
		compact.Files = flatFiles
		return compact
	}
	candidateGroups := make([]compactReferenceGroup, 0, len(groups))
	for _, bucket := range groups {
		first := bucket[0]
		group := compactReferenceGroup{
			Kind:       blankWhenHoisted(first.Kind, compact.Kind),
			EdgeKind:   blankWhenHoisted(first.EdgeKind, compact.EdgeKind),
			Confidence: blankWhenHoisted(first.Confidence, compact.Confidence),
			Provenance: blankWhenHoisted(first.Provenance, compact.Provenance),
			Via:        blankWhenHoisted(first.Via, compact.Via),
		}
		// Every column left is now uniform inside the bucket, so a row inside
		// it carries nothing beyond its own declaration.
		group.Files = referenceFileGroups(bucket, compact.Repository, first.Kind, first.EdgeKind, first.Confidence, first.Provenance)
		candidateGroups = append(candidateGroups, group)
	}
	// Grouping only wins when a tuple repeats enough to pay for its own
	// header; a page where every row disagrees on everything is cheaper left
	// flat. Marshaling both candidates on a page this small costs nothing, and
	// it is the only way to guarantee grouping never costs more than not
	// grouping instead of hoping a heuristic holds.
	if flatBytes, err := json.Marshal(flatFiles); err == nil {
		if groupedBytes, err := json.Marshal(candidateGroups); err == nil && len(groupedBytes) >= len(flatBytes) {
			compact.Files = flatFiles
			return compact
		}
	}
	compact.Groups = candidateGroups
	return compact
}

// referenceFileGroups groups rows by file with a bare `qn@lines` label per
// row: valid once every one of kind, edge_kind, confidence and provenance is
// accounted for above the row, whether on the page header or on a group.
func referenceFileGroups(rows []ReferenceSummary, hoistedRepository, kind, edgeKind, confidence, provenance string) []compactReferenceFile {
	files := make([]compactReferenceFile, 0, len(rows))
	index := make(map[string]int, len(rows))
	for _, row := range rows {
		key := row.Repository + "\x00" + row.FilePath
		position, seen := index[key]
		if !seen {
			position = len(files)
			index[key] = position
			file := compactReferenceFile{File: row.FilePath}
			if row.Repository != hoistedRepository {
				file.Repository = row.Repository
			}
			files = append(files, file)
		}
		files[position].At = append(files[position].At, compactRowTail(
			declarationLabel(row.QualifiedName, row.StartLine, row.EndLine),
			blankWhenHoisted(row.Kind, kind),
			blankWhenHoisted(row.EdgeKind, edgeKind),
			blankWhenHoisted(row.Confidence, confidence),
			blankWhenHoisted(row.Provenance, provenance),
		))
	}
	return files
}

// files counts the references per file. The question it answers is which files
// to open, so a repeated caller in one file is a count and not a row.
func (result ReferenceResult) files() referenceFilesResult {
	files := referenceFilesResult{
		Subject:           result.subjectLabel(),
		QN:                result.Subject.QualifiedName,
		Direction:         result.Direction,
		Files:             make([]referenceFileCount, 0, len(result.References)),
		EdgeKindsExcluded: result.EdgeKindsExcluded,
	}
	index := make(map[string]int, len(result.References))
	for _, row := range result.References {
		path := row.Repository + "/" + row.FilePath
		position, seen := index[path]
		if !seen {
			position = len(files.Files)
			index[path] = position
			files.Files = append(files.Files, referenceFileCount{File: path})
		}
		files.Files[position].Count++
	}
	return files
}

// compactReferenceResult is one page with the repeated columns lifted out of
// the rows. Every field it hoists is one an agent read fifty times to learn
// one thing: `confidence` and `provenance` alone were 1.200 of the 4.236
// tokens of one page over `kena`.
//
// Files and Groups are mutually exclusive: Groups appears only when the page
// itself could not agree on kind, edge_kind, confidence or provenance, and a
// second tier of hoisting groups the rows by what they do still share instead
// of repeating it on each one; see compact() and groupByResidual.
type compactReferenceResult struct {
	Subject   string `json:"subject"`
	QN        string `json:"qn"`
	Direction string `json:"direction"`

	// Hoisted columns. Absent means the page disagreed; see Groups.
	Repository string `json:"repository,omitempty"`
	Kind       string `json:"kind,omitempty"`
	EdgeKind   string `json:"edge_kind,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Provenance string `json:"provenance,omitempty"`
	// Via is the interface method every row of this page came through. It
	// hoists and groups like the others so a bridged row is never read as a
	// direct call, whatever the page agreed on.
	Via string `json:"via,omitempty"`

	// EdgeKindsExcluded is what a filter the caller never asked for left out.
	EdgeKindsExcluded []string `json:"edge_kinds_default_excluded,omitempty"`
	// DispatchThrough names the interface methods this answer also holds the
	// references to, because the subject is their one implementation.
	DispatchThrough []string `json:"dispatch_through,omitempty"`

	Files  []compactReferenceFile  `json:"files,omitempty"`
	Groups []compactReferenceGroup `json:"groups,omitempty"`
}

// compactReferenceGroup is every row that shares one exact tuple of the
// columns the page header could not hoist. Absent means this group's rows
// hold the page's hoisted value too and it is not the field distinguishing
// them.
type compactReferenceGroup struct {
	Kind       string `json:"kind,omitempty"`
	EdgeKind   string `json:"edge_kind,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Provenance string `json:"provenance,omitempty"`
	Via        string `json:"via,omitempty"`

	Files []compactReferenceFile `json:"files"`
}

// compactReferenceFile is one file and the symbols in it that hold the fact.
// An entry is `qualified_name@line`, and becomes an array when this row had to
// carry a column neither the page nor its group could hoist.
type compactReferenceFile struct {
	Repository string `json:"repo,omitempty"`
	File       string `json:"file"`
	At         []any  `json:"at"`
}

// referenceFilesResult answers which files hold references, and how many each
// holds. It is the shape of the question "which files call this", which is what
// an agent asks before deciding what to open.
type referenceFilesResult struct {
	Subject   string `json:"subject"`
	QN        string `json:"qn"`
	Direction string `json:"direction"`
	// EdgeKindsExcluded is what a filter the caller never asked for left out.
	EdgeKindsExcluded []string             `json:"edge_kinds_default_excluded,omitempty"`
	Files             []referenceFileCount `json:"files"`
}

type referenceFileCount struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

type findReferencesOptions struct {
	Selector symbolSelector
	// Name is the unqualified name to resolve to its one declaration. It is
	// resolved against the snapshot, so it never reaches the query hash: the
	// hash covers the qualified name it resolved to, and a page stays valid
	// while the snapshot does.
	Name       string
	View       string
	Direction  string
	Repo       string
	Language   string
	EdgeKinds  referenceEdgeKindFilter
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
		Name: findReferencesToolName,
		// The last two sentences are worth their 33 tokens of resident surface,
		// both because the surface taught a dearer path than the tool supports.
		//
		// Without the first, a caller resolves the symbol before asking, and
		// `find_symbol` answered a 22-row page for `withRetry` where the
		// ambiguity error names the 7 candidates: 750 tokens against 129, on
		// every ambiguous question.
		//
		// Without the second, "which files call this" is answered with a line
		// per reference. Over four questions on a 37-repository workspace that
		// is 2,480 tokens against 912 for the same files, the same precision and
		// the same recall -- and one page instead of two where 66 references
		// collapse into 9 files. Both were already supported; nothing said so.
		Description: "Who calls or references a symbol. Type-checked, not name-matched: grep cannot separate homonyms, and an empty answer means nobody calls it. A bare name suffices: an ambiguous one returns its candidates, so no lookup call first. `view: \"files\"` answers which files without a line each.",
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
	if snapshotStore == nil {
		return nil, Response[ReferenceResult]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[ReferenceResult]{}, ErrIndexNotReady()
	}

	// A name resolves to its declaration before anything else: the query hash
	// covers the qualified name it resolved to, so a cursor keeps addressing
	// the same page whether the caller arrived by name or by triple.
	var startID hotsnapshot.SymbolID
	if options.Name != "" {
		resolvedID, qualifiedName, resolveErr := resolveDeclarationByName(
			snapshot, options.Name, options.Selector.Repository, options.Selector.Path,
		)
		if resolveErr != nil {
			return nil, Response[ReferenceResult]{}, resolveErr
		}
		startID = resolvedID
		options.Selector.QualifiedName = qualifiedName
	}

	queryHash, err := HashQuery(findReferencesQuery{
		Tool: findReferencesToolName, StableKey: options.Selector.StableKey,
		QualifiedName: options.Selector.QualifiedName, Repository: options.Selector.Repository,
		Path: options.Selector.Path, Direction: options.Direction,
		Repo: options.Repo, Language: options.Language, EdgeKinds: options.EdgeKinds.applied,
		Confidence: options.Confidence,
	})
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}

	if options.Name == "" {
		startID, err = resolveSymbolSelector(snapshot, options.Selector)
		if err != nil {
			return nil, Response[ReferenceResult]{}, err
		}
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

	candidates, dispatchThrough, err := referenceCandidates(snapshot, startID, options)
	if err != nil {
		return nil, Response[ReferenceResult]{}, err
	}
	results := make([]ReferenceSummary, 0, minReferenceInt(options.Limit, len(candidates)))
	coverage := Coverage{}
	total := 0
	for _, candidate := range candidates {
		edge := candidate.edge
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
			reference.Via = candidate.via
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

	// The verdict comes from the graph, not from a sentence. This tool sells an
	// empty list as proof that nobody calls the symbol, and the index keeps the
	// failures that would make that claim false: a reference it could not
	// resolve which asked for this very name. Without this check the answer
	// said "an absence rather than a miss" over a recorded miss.
	subjectRepository := hotsnapshot.InvalidRepositoryID
	if startSymbol, found := snapshot.Symbol(startID); found {
		if file, fileFound := snapshot.File(startSymbol.File); fileFound {
			subjectRepository = file.Repository
		}
	}
	completeness, unresolvedRelated, err := completenessFor(snapshot, subject.Name, subjectRepository)
	if err != nil {
		return nil, Response[ReferenceResult]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid unresolved metadata",
			err,
		)
	}
	coverage.UnresolvedRelated += unresolvedRelated

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
		Completeness:  &completeness,
		Guidance:      referenceGuidance(options.Direction, total, len(results), hasMore, completeness.Verdict),
		View:          options.View,
		Results: ReferenceResult{
			Subject: subject, Direction: options.Direction, References: results, View: options.View,
			EdgeKindsExcluded: options.EdgeKinds.defaultExcluded(),
			DispatchThrough:   dispatchThrough,
		},
	}, nil
}

func normalizeFindReferencesInput(arguments FindReferencesInput) (findReferencesOptions, error) {
	name := arguments.Name
	var selector symbolSelector
	if name != "" {
		if arguments.StableKey != "" || arguments.QualifiedName != "" {
			return findReferencesOptions{}, NewToolError(CodeInvalidArgument,
				"name identifies a declaration on its own; pass stable_key or qualified_name instead, not both")
		}
		if strings.TrimSpace(name) != name {
			return findReferencesOptions{}, NewToolError(CodeInvalidArgument, "name must not carry surrounding whitespace")
		}
		if arguments.Path != "" && arguments.Repository == "" {
			// `repo` filters which repositories the answer may come from;
			// `repository` says where the subject is declared. Four characters
			// apart, and a caller that means the second reaches for the first:
			// measured on a real session, where the model passed `repo` with a
			// path, was told the path needed `repository`, and spent another
			// call saying the same thing with the other name. The correction is
			// cheap to give and nothing else here can give it.
			if arguments.Repo != "" {
				return findReferencesOptions{}, NewToolError(CodeInvalidArgument,
					"path is repository-relative, so it requires repository; repo only filters which repositories the answer may come from")
			}
			return findReferencesOptions{}, NewToolError(CodeInvalidArgument, "path is repository-relative, so it requires repository")
		}
		selector = symbolSelector{Repository: arguments.Repository, Path: arguments.Path}
		if selector.Path != "" {
			normalized, err := normalizeOutlinePath(selector.Path)
			if err != nil {
				return findReferencesOptions{}, err
			}
			selector.Path = normalized
		}
	} else {
		resolved, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
		if err != nil {
			return findReferencesOptions{}, err
		}
		selector = resolved
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
	edgeKinds, err := normalizeReferenceEdgeKindFilter(arguments.EdgeKinds)
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
	view, err := normalizeView(arguments.View, true)
	if err != nil {
		return findReferencesOptions{}, err
	}
	// A file list is one line per file, so paging it is a cost with no payoff:
	// the question "which files" is answered wrong by a page that stops at 50.
	if view == ViewFiles && arguments.Limit == 0 {
		limit = MaximumReferenceLimit
	}
	return findReferencesOptions{
		Selector: selector, Name: name, View: view, Direction: direction, Repo: repo, Language: language,
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

// referenceDefaultExcludedEdgeKinds are the forwarding bindings an unasked-for
// reference answer leaves out. An `export { x }` or `export { x } from "./y"`
// names a path to the declaration, not a use of it, and this tool already
// refuses one as a subject -- "only imported or re-exported here, never
// declared" -- so answering with one contradicted its own error message.
//
// Nothing becomes unreachable. The checker resolves an import through however
// many barrels stand in the way and names the declaration, so every consumer
// that reaches the symbol through one carries its own IMPORTS_SYMBOL edge and
// is listed whether the barrel is or not: on `kena`, `withRetry` has five
// consumers behind four barrels and all five are in the answer without them.
// `edge_kinds` selects them back for the question they do answer -- a rename
// has to edit the barrel -- and get_blast_radius, which is the tool for what
// breaks, never excluded them.
var referenceDefaultExcludedEdgeKinds = []string{string(facts.Exports), string(facts.Reexports)}

// referenceEdgeKindAll is the wildcard that turns the filter off, spelled the
// way get_blast_radius spells its own.
const referenceEdgeKindAll = "*"

// referenceEdgeKindFilter decides which edges the answer reports. It gates the
// one place a candidate edge is admitted, so `total`, the page and the coverage
// counts all describe the same set: a filtered total that did not say it was
// filtered would understate the references to a symbol.
type referenceEdgeKindFilter struct {
	// selected is the caller's explicit list, nil when it asked for nothing.
	selected map[string]struct{}
	// all is `edge_kinds: ["*"]`.
	all bool
	// applied is the canonical spelling of the selection, sorted, and `["*"]`
	// for the wildcard. It is what the cursor's query hash covers, so no two
	// filters share a page.
	applied []string
}

// keeps answers whether an edge of this kind belongs in the answer.
func (filter referenceEdgeKindFilter) keeps(kind string) bool {
	if filter.all {
		return true
	}
	if filter.selected != nil {
		_, selected := filter.selected[kind]
		return selected
	}
	return !containsString(referenceDefaultExcludedEdgeKinds, kind)
}

// defaultExcluded is what the response must declare: the kinds left out by a
// filter the caller never asked for. An explicit selection declares nothing,
// because the caller wrote it.
func (filter referenceEdgeKindFilter) defaultExcluded() []string {
	if filter.all || filter.selected != nil {
		return nil
	}
	return referenceDefaultExcludedEdgeKinds
}

// normalizeReferenceEdgeKindFilter validates the requested kinds. The wildcard
// stands alone: mixing it with names asks for a filter and for no filter at
// once.
func normalizeReferenceEdgeKindFilter(values []string) (referenceEdgeKindFilter, error) {
	if len(values) == 0 {
		return referenceEdgeKindFilter{}, nil
	}
	for _, value := range values {
		if value != referenceEdgeKindAll {
			continue
		}
		if len(values) != 1 {
			return referenceEdgeKindFilter{}, NewToolError(CodeInvalidArgument,
				"edge_kinds must be \"*\" alone or a list of canonical kinds")
		}
		return referenceEdgeKindFilter{all: true, applied: []string{referenceEdgeKindAll}}, nil
	}
	normalized, err := normalizeReferenceEdgeKinds(values)
	if err != nil {
		return referenceEdgeKindFilter{}, err
	}
	selected := make(map[string]struct{}, len(normalized))
	for _, kind := range normalized {
		selected[kind] = struct{}{}
	}
	return referenceEdgeKindFilter{selected: selected, applied: normalized}, nil
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
		facts.Implements, facts.Extends, facts.Embeds, facts.Overrides, facts.PartOf:
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
	if !options.EdgeKinds.keeps(string(decoded.Kind)) {
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
			symbolStableKey(snapshot, other), nameOK, qualifiedNameOK, kindOK,
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
	summary.StableKey = symbolStableKey(snapshot, other)
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
			symbolStableKey(snapshot, symbol), nameOK, qualifiedNameOK, kindOK,
		)
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return ReferenceSubject{}, err
	}
	return ReferenceSubject{
		StableKey:     symbolStableKey(snapshot, symbol),
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
	stableKey := symbolStableKey(snapshot, symbol)
	file, found := snapshot.File(symbol.File)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references missing file %d", stableKey, symbol.File)
	}
	repository, found := snapshot.Repository(file.Repository)
	if !found {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references missing repository %d", stableKey, file.Repository)
	}
	table := snapshot.Strings()
	fileKey, fileKeyOK := table.String(file.Key)
	filePath, filePathOK := table.String(file.Path)
	repositoryName, repositoryNameOK := table.String(repository.Name)
	repositoryKey, repositoryKeyOK := table.String(repository.Key)
	if !fileKeyOK || !filePathOK || !repositoryNameOK || !repositoryKeyOK {
		return hotsnapshot.SymbolRecord{}, symbolReferenceFile{}, symbolReferenceRepository{}, nil, fmt.Errorf("symbol %q references invalid file or repository strings", stableKey)
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
	stableKey := symbolStableKey(snapshot, symbol)
	file, found := snapshot.File(symbol.File)
	if !found {
		return hotsnapshot.RepositoryRecord{}, nil, fmt.Errorf("symbol %q references missing file %d", stableKey, symbol.File)
	}
	repository, found := snapshot.Repository(file.Repository)
	if !found {
		return hotsnapshot.RepositoryRecord{}, nil, fmt.Errorf("symbol %q references missing repository %d", stableKey, file.Repository)
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

// referenceCandidate is one edge the page may report, and the interface method
// it arrived through when it is not a direct reference to the subject.
type referenceCandidate struct {
	edge hotsnapshot.PackedEdge
	via  string
}

// referenceCandidates is every edge an answer may hold, direct references
// first. It is assembled before the page is walked so `total`, the rows and
// `coverage` describe one set, and its order is stable, which is what a cursor
// addresses.
//
// A method reached only through the interface that declares it used to answer
// that nothing referenced it: the call site names the interface method, the
// checker resolves it there, and the implementation carried no incoming edge at
// all. That absence was reported in the same words as a real one.
//
// So an incoming answer about a method also holds the references to the
// interface methods it implements -- but only where it is the ONLY
// implementation, which is the case where a call through the interface can
// reach nothing else. With two implementations a call reaches one of them and
// naming both as called would trade a false absence for a false presence; those
// stay out and get_blast_radius, which is the tool for what a change reaches,
// still crosses IMPLEMENTS in either direction. Each bridged row says which
// interface method it came through, so nothing is passed off as a direct call.
func referenceCandidates(
	snapshot *hotsnapshot.GraphSnapshot, startID hotsnapshot.SymbolID, options findReferencesOptions,
) ([]referenceCandidate, []string, error) {
	direct := snapshot.Incoming(startID)
	if options.Direction == FindReferencesDirectionOutgoing {
		direct = snapshot.Outgoing(startID)
	}
	candidates := make([]referenceCandidate, 0, len(direct))
	for _, edge := range direct {
		candidates = append(candidates, referenceCandidate{edge: edge})
	}
	if options.Direction == FindReferencesDirectionOutgoing {
		return candidates, nil, nil
	}
	implemented, implementsCode, err := solelyImplementedMethods(snapshot, startID)
	if err != nil {
		return nil, nil, err
	}
	dispatch := make([]string, 0, len(implemented))
	for _, method := range implemented {
		subject, err := referenceSubject(snapshot, method)
		if err != nil {
			return nil, nil, WrapToolError(
				CodeSnapshotUnavailable, "active snapshot contains invalid symbol metadata", err,
			)
		}
		label := subject.QualifiedName
		dispatch = append(dispatch, label)
		for _, edge := range snapshot.Incoming(method) {
			// An IMPLEMENTS edge into the interface method states that
			// something implements it, which is never a use of it. The one
			// there is here is the subject's own, and reporting it would have
			// the subject referencing itself.
			if edge.Kind == implementsCode {
				continue
			}
			candidates = append(candidates, referenceCandidate{edge: edge, via: label})
		}
	}
	return candidates, dispatch, nil
}

// solelyImplementedMethods returns the interface methods this symbol is the one
// implementation of, in snapshot order so a cursor keeps addressing the same
// page. A symbol that implements nothing returns nothing, which is every
// symbol that is not a method.
func solelyImplementedMethods(
	snapshot *hotsnapshot.GraphSnapshot, startID hotsnapshot.SymbolID,
) ([]hotsnapshot.SymbolID, uint8, error) {
	implementsCode, err := facts.Implements.Code()
	if err != nil {
		return nil, 0, WrapToolError(CodeSnapshotUnavailable, "IMPLEMENTS has no edge code", err)
	}
	out := []hotsnapshot.SymbolID{}
	for _, edge := range snapshot.Outgoing(startID) {
		if edge.Kind != implementsCode {
			continue
		}
		implementations := 0
		for _, incoming := range snapshot.Incoming(edge.Target) {
			if incoming.Kind == implementsCode {
				implementations++
			}
		}
		if implementations == 1 {
			out = append(out, edge.Target)
		}
	}
	return out, implementsCode, nil
}
