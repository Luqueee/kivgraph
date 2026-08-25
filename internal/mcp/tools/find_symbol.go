package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	FindSymbolModeExact          = "exact"
	FindSymbolModeQualifiedExact = "qualified_exact"
	FindSymbolModePrefix         = "prefix"
	FindSymbolModeSubstring      = "substring"

	DefaultSymbolLimit = 50
	MaximumSymbolLimit = hotsnapshot.MaxExactResults
	findSymbolToolName = "find_symbol"
)

// FindSymbolInput contains the search mode, the filters and the page controls
// for find_symbol. An empty mode is the exact unqualified-name search.
//
// Kind, Repo and PathPrefix narrow the page without changing its cost class:
// prefix and substring already walk every symbol name in the snapshot, so
// filtering while walking is free.
//
// View is the granularity of the answer. The default, `compact`, hoists what
// every row shares into a header and leaves `stable_key` out; `full` is the
// field-per-row shape of SymbolSummary. `files` is rejected here: find_symbol
// answers declarations, not files.
type FindSymbolInput struct {
	Name           string `json:"name"`
	Mode           string `json:"mode,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Repo           string `json:"repo,omitempty"`
	IncludeDerived bool   `json:"include_derived,omitempty"`
	PathPrefix     string `json:"path_prefix,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	View           string `json:"view,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
}

// SymbolSummary is the stable public result shape for symbol discovery. It
// carries where the symbol is, because a search result the agent cannot open
// costs a second call to become useful.
//
// CanonicalIdentity is omitted unless the caller asks for the detailed
// format: it is the concatenation of language, repository, package, qualified
// name, kind and discriminator, every one of which is already a field here or
// is the signature itself.
type SymbolSummary struct {
	StableKey         string `json:"stable_key"`
	Name              string `json:"name"`
	QualifiedName     string `json:"qualified_name"`
	Kind              string `json:"kind"`
	Signature         string `json:"signature"`
	Exported          bool   `json:"exported"`
	Repository        string `json:"repository"`
	FilePath          string `json:"file_path"`
	StartLine         uint32 `json:"start_line"`
	EndLine           uint32 `json:"end_line"`
	CanonicalIdentity string `json:"canonical_identity,omitempty"`
}

// SymbolResults is one page of declarations together with the granularity to
// spell it with. The rows are always the full shape in memory: MarshalJSON is
// the single place that decides whether the payload repeats what every row
// shares.
type SymbolResults struct {
	Symbols []SymbolSummary
	// View is the granularity the caller asked for and never travels in the
	// payload.
	View string `json:"-"`
	// Format is the response format asked for. Only the detailed one restores
	// the keys a compact row drops.
	Format string `json:"-"`
}

// MarshalJSON writes the rows as they are under the full view, and the compact
// page otherwise. An absent page is an empty page, never a null: `results` is
// documented as always present.
func (results SymbolResults) MarshalJSON() ([]byte, error) {
	if results.View == ViewFull || results.View == "" {
		if results.Symbols == nil {
			return []byte("[]"), nil
		}
		return json.Marshal(results.Symbols)
	}
	return json.Marshal(results.compact())
}

// compactSymbolPage is what every row shares written once, then one entry per
// declaration -- flat when kind and exported both hoist to the page, grouped
// by whichever of the two does not; see compact.
type compactSymbolPage struct {
	Name       string                   `json:"name,omitempty"`
	Kind       string                   `json:"kind,omitempty"`
	Exported   *bool                    `json:"exported,omitempty"`
	Repository string                   `json:"repository,omitempty"`
	Symbols    []compactSymbol          `json:"symbols,omitempty"`
	Groups     []compactSymbolKindGroup `json:"groups,omitempty"`
}

// compactSymbolKindGroup is every declaration that shares one exact (kind,
// exported) pair the page could not hoist. Absent means this group's rows
// hold the page's hoisted value too.
type compactSymbolKindGroup struct {
	Kind     string          `json:"kind,omitempty"`
	Exported *bool           `json:"exported,omitempty"`
	Symbols  []compactSymbol `json:"symbols"`
}

// compactSymbol is one declaration. At addresses it: `path:line` under a header
// that names the repository, and the whole `repository:path:line` triple when
// the rows come from more than one repository. Both forms reconstruct the triple
// every tool accepts by reading the header, so the repository is never spelled
// twice and never invented. End is present only when the declaration does not
// start and finish on the same line.
//
// What is absent is not lost: it is in the header, on its group, or it is the
// tail of the qualified name.
type compactSymbol struct {
	At                string `json:"at"`
	End               uint32 `json:"end,omitempty"`
	Name              string `json:"name,omitempty"`
	QualifiedName     string `json:"qn,omitempty"`
	Kind              string `json:"kind,omitempty"`
	Exported          *bool  `json:"exported,omitempty"`
	Signature         string `json:"sig,omitempty"`
	StableKey         string `json:"stable_key,omitempty"`
	CanonicalIdentity string `json:"canonical_identity,omitempty"`
}

// compact spells the page without repeating what every row shares. Measured
// over `kena`, `stable_key` was 885 of the 2.293 tokens of a 22-row page and no
// tool needs it: every one accepts the repository, path and qualified name that
// the rows already carry. See ADR 0046.
//
// A second tier groups by (kind, exported) when the page cannot hoist either:
// on a real 453-row search, 222 shared one pair and repeating it on every row
// was `kind` and `exported` twice over. Grouping is measured against the flat
// page and only kept when it is smaller -- a page where every row disagrees
// costs more grouped, one object per row instead of one value.
func (results SymbolResults) compact() compactSymbolPage {
	rows := results.Symbols
	page := compactSymbolPage{
		Name:       hoistString(len(rows), func(index int) string { return rows[index].Name }),
		Kind:       hoistString(len(rows), func(index int) string { return rows[index].Kind }),
		Repository: hoistString(len(rows), func(index int) string { return rows[index].Repository }),
	}
	exportedHoisted, exportedShared := hoistExported(rows)
	if exportedShared {
		page.Exported = &exportedHoisted
	}
	detailed := results.Format == ResponseFormatDetailed

	flat := make([]compactSymbol, 0, len(rows))
	for index := range rows {
		flat = append(flat, compactSymbolEntry(&rows[index], page.Repository, page.Name, page.Kind, page.Exported, detailed))
	}
	if page.Kind != "" && exportedShared {
		// Both grouping dimensions are already on the page: nothing left to
		// group by, so this is the whole answer, not a candidate.
		page.Symbols = flat
		return page
	}

	residual := func(row SymbolSummary) []string {
		kindResidual := ""
		if page.Kind == "" {
			kindResidual = row.Kind
		}
		exportedResidual := ""
		if !exportedShared {
			exportedResidual = strconv.FormatBool(row.Exported)
		}
		return []string{kindResidual, exportedResidual}
	}
	buckets := groupByResidual(rows, residual)
	if len(buckets) <= 1 {
		page.Symbols = flat
		return page
	}

	groups := make([]compactSymbolKindGroup, 0, len(buckets))
	for _, bucket := range buckets {
		first := bucket[0]
		group := compactSymbolKindGroup{}
		effectiveKind := page.Kind
		if effectiveKind == "" {
			effectiveKind = first.Kind
			group.Kind = first.Kind
		}
		effectiveExported := page.Exported
		if effectiveExported == nil {
			exported := first.Exported
			effectiveExported = &exported
			group.Exported = &exported
		}
		group.Symbols = make([]compactSymbol, 0, len(bucket))
		for index := range bucket {
			group.Symbols = append(group.Symbols, compactSymbolEntry(&bucket[index], page.Repository, page.Name, effectiveKind, effectiveExported, detailed))
		}
		groups = append(groups, group)
	}
	// Grouping only wins when a (kind, exported) pair repeats enough to pay
	// for its own header; a page where every row disagrees is cheaper flat.
	// Marshaling both candidates costs nothing on a page this small, and it is
	// the only way to guarantee grouping never costs more than not grouping.
	if flatBytes, err := json.Marshal(flat); err == nil {
		if groupedBytes, err := json.Marshal(groups); err == nil && len(groupedBytes) >= len(flatBytes) {
			page.Symbols = flat
			return page
		}
	}
	page.Groups = groups
	return page
}

// compactSymbolEntry writes one declaration against the header and, when it
// has one, the group above it: kind and exported are its own only when
// neither already states them.
func compactSymbolEntry(row *SymbolSummary, hoistedRepository, hoistedName, effectiveKind string, effectiveExported *bool, detailed bool) compactSymbol {
	symbol := compactSymbol{At: symbolLocationLabel(hoistedRepository, row.Repository, row.FilePath, row.StartLine)}
	if row.EndLine != row.StartLine {
		symbol.End = row.EndLine
	}
	if effectiveKind == "" {
		symbol.Kind = row.Kind
	}
	if row.QualifiedName != row.Name {
		symbol.QualifiedName = row.QualifiedName
	}
	// The name is a fact of its own only when the header does not carry it
	// and it cannot be read off the qualified name. An omitted qualified
	// name means it equals the name, so the name has to stay.
	if hoistedName == "" && (symbol.QualifiedName == "" || !nameIsQualifiedTail(row.QualifiedName, row.Name)) {
		symbol.Name = row.Name
	}
	if effectiveExported == nil {
		exported := row.Exported
		symbol.Exported = &exported
	}
	if signatureAnswersHowToCall(row.Kind) {
		symbol.Signature = row.Signature
	}
	if detailed {
		symbol.StableKey = row.StableKey
		symbol.CanonicalIdentity = row.CanonicalIdentity
	}
	return symbol
}

// symbolLocationLabel addresses a row against the header it sits under. A
// hoisted repository is the one fact the whole page shares, so repeating it on
// every row is the repetition ADR 0046 exists to remove; when the rows disagree
// the row carries the full triple instead.
func symbolLocationLabel(hoistedRepository, repository, path string, line uint32) string {
	if hoistedRepository == "" {
		return locationLabel(repository, path, line)
	}
	return path + ":" + strconv.FormatUint(uint64(line), 10)
}

// hoistExported returns the visibility every row shares. Unlike the string
// hoists it cannot read the zero value as "not shared": false is a fact, and
// dropping it would make an unexported page indistinguishable from a mixed one.
func hoistExported(rows []SymbolSummary) (bool, bool) {
	if len(rows) == 0 {
		return false, false
	}
	first := rows[0].Exported
	for index := 1; index < len(rows); index++ {
		if rows[index].Exported != first {
			return false, false
		}
	}
	return first, true
}

// signatureAnswersHowToCall reports whether a signature earns its bytes. For a
// callable or a class it is how to use the symbol; for a field, a constant or a
// variable it restates the type its own declaration shows, and signatures were
// 360 of the 2.293 tokens of a 22-row page (ADR 0046). The full view keeps them
// all.
func signatureAnswersHowToCall(kind string) bool {
	switch kind {
	case "function", "func", "method", "class":
		return true
	default:
		return false
	}
}

// nameIsQualifiedTail reports whether the qualified name ends in the name after
// a segment separator, whichever one the language uses (`.`, `::`, `#`, `/`).
// Then the name is a suffix the reader already has.
func nameIsQualifiedTail(qualifiedName, name string) bool {
	if name == "" || !strings.HasSuffix(qualifiedName, name) {
		return false
	}
	prefix := qualifiedName[:len(qualifiedName)-len(name)]
	if prefix == "" {
		return true
	}
	switch prefix[len(prefix)-1] {
	case '.', ':', '#', '/':
		return true
	default:
		return false
	}
}

type findSymbolQuery struct {
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Kind       string `json:"kind,omitempty"`
	Repo       string `json:"repo,omitempty"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

// RegisterFindSymbol adds the read-only symbol search tool without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterFindSymbol(server *sdkmcp.Server) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterFindSymbolWithObserver adds find_symbol and optionally observes
// handler latency.
func RegisterFindSymbolWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterFindSymbolWithSnapshotStore registers find_symbol over the
// immutable snapshot currently published by snapshotStore.
func RegisterFindSymbolWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterFindSymbolWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterFindSymbolWithObserverAndSnapshotStore registers find_symbol over a
// snapshot store and optionally observes latency.
func RegisterFindSymbolWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments FindSymbolInput,
	) (*sdkmcp.CallToolResult, Response[SymbolResults], error) {
		return findSymbol(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindSymbolInput,
		) (*sdkmcp.CallToolResult, Response[SymbolResults], error) {
			start := time.Now()
			result, symbols, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findSymbolToolName, start, symbols, err)
			return result, symbols, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        findSymbolToolName,
		Description: "Where a symbol is declared, by name, qualified name, prefix or substring. Narrow with kind, repo and path_prefix.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		Meta:        alwaysLoadMeta(),
	}, handler)
}

func findSymbol(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindSymbolInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[SymbolResults], error) {
	name, err := normalizeSymbolName(arguments.Name)
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	mode, err := normalizeFindSymbolMode(arguments.Mode)
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	limit, err := normalizeSymbolLimit(arguments.Limit)
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	// A declaration is not a file, so `files` is not one of the granularities
	// this tool can answer in: asking for it fails instead of quietly
	// answering something else.
	view, err := normalizeView(arguments.View, false)
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	filter := hotsnapshot.SymbolFilter{
		Kind:           arguments.Kind,
		RepositoryName: arguments.Repo,
		PathPrefix:     arguments.PathPrefix,
	}
	// A name search reaches the whole graph, so the standard library has to be
	// withheld here or `Clone` answers with `core`. Naming a derived provider
	// through `repo` is a request for it and overrides the default.
	if !newDerivedFilter(arguments.IncludeDerived, arguments.Repo).keepsAll() {
		filter.ExcludeRepositoryPrefix = workspace.SyntheticRepositoryPrefix
	}
	// The cursor is bound to the whole query, filters included: a page taken
	// with one filter is not a page of another.
	queryHash, err := HashQuery(findSymbolQuery{
		Tool: findSymbolToolName, Name: name, Mode: mode,
		Kind: filter.Kind, Repo: filter.RepositoryName, PathPrefix: filter.PathPrefix,
	})
	if err != nil {
		return nil, Response[SymbolResults]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[SymbolResults]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[SymbolResults]{}, ErrIndexNotReady()
	}
	metadata := snapshot.Metadata()

	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[SymbolResults]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionStableKeyV1); err != nil {
			return nil, Response[SymbolResults]{}, err
		}
		offset = cursor.Offset
	}

	page, err := searchSymbolPage(snapshot, name, mode, filter, offset, limit)
	if err != nil {
		return nil, Response[SymbolResults]{}, WrapToolError(CodeInvalidArgument, "symbol search pagination is invalid", err)
	}
	results := make([]SymbolSummary, 0, len(page.IDs))
	for _, id := range page.IDs {
		symbol, found := snapshot.Symbol(id)
		if !found {
			return nil, Response[SymbolResults]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot symbol index is inconsistent",
				fmt.Errorf("symbol index %d is missing", id),
			)
		}
		summary, err := symbolSummary(snapshot, symbol, format)
		if err != nil {
			return nil, Response[SymbolResults]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains invalid symbol metadata",
				err,
			)
		}
		results = append(results, summary)
	}

	var nextCursor *string
	if page.HasMore {
		nextOffset := page.Offset + len(page.IDs)
		cursor, err := NewCursor(metadata.ID, queryHash, nextOffset, SortingVersionStableKeyV1)
		if err != nil {
			return nil, Response[SymbolResults]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[SymbolResults]{}, err
		}
		nextCursor = &encoded
	}

	// A search that found nothing and reports no uncertainty is claiming the
	// name does not exist. It may only mean its provider was never indexed,
	// and the index recorded exactly that, with a file and a line. Counting it
	// was not enough: a caller saw a number with nothing telling it what the
	// number meant or where to look.
	//
	// The scope half follows the question. A search of the whole graph is
	// bounded by every unreadable package in it; one narrowed to a repository
	// is bounded only by that repository's, and charging it for another's would
	// make the verdict a constant on any corpus with a single bad package.
	scope := hotsnapshot.InvalidRepositoryID
	if filter.RepositoryName != "" {
		if id, found := snapshot.RepositoryByName(filter.RepositoryName); found {
			scope = id
		}
	}
	completeness, unresolvedRelated, err := completenessFor(snapshot, name, scope)
	if err != nil {
		return nil, Response[SymbolResults]{}, WrapToolError(
			CodeSnapshotUnavailable, "active snapshot contains invalid unresolved metadata", err)
	}
	// Measured, not assumed: `"completeness":{"verdict":"COMPLETE"}` is 10
	// tokens (cl100k_base), which is 16 % of a one-row answer here and 50 % of
	// an empty one. This is the most frequent call in the surface, so the
	// verdict is spent where the answer could be mistaken for a proof -- empty,
	// or partial -- and on every lower bound. A page of declarations claims no
	// absence: the rows are the answer.
	//
	// The four relational tools always carry it, and that is deliberate: for
	// "who calls this" and "what breaks if I change this", COMPLETE on a
	// non-empty answer *is* the claim being bought.
	var verdict *Completeness
	if page.Total == 0 || page.HasMore || completeness.Verdict == VerdictLowerBound {
		verdict = &completeness
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[SymbolResults]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         page.Total,
		Returned:      len(results),
		Truncated:     page.HasMore,
		NextCursor:    nextCursor,
		// No `exact`: the rows are declarations, not resolved relations, so
		// every one of them is exact and the counter could only ever repeat
		// `returned`. A number that cannot vary is not evidence. See ADR 0064.
		Coverage:     Coverage{UnresolvedRelated: unresolvedRelated},
		Completeness: verdict,
		Guidance:     symbolGuidance(page.Total, len(results), page.HasMore, completeness.Verdict),
		Results:      SymbolResults{Symbols: results, View: view, Format: format},
		View:         view,
	}, nil
}

func normalizeSymbolName(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, "name must be a non-empty value without surrounding whitespace")
	}
	return value, nil
}

func normalizeFindSymbolMode(value string) (string, error) {
	if value == "" {
		return FindSymbolModeExact, nil
	}
	switch value {
	case FindSymbolModeExact, FindSymbolModeQualifiedExact, FindSymbolModePrefix, FindSymbolModeSubstring:
		return value, nil
	default:
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf("mode %q is unsupported", value))
	}
}

func normalizeSymbolLimit(value int) (int, error) {
	if value == 0 {
		return DefaultSymbolLimit, nil
	}
	if value < 1 || value > MaximumSymbolLimit {
		return 0, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumSymbolLimit))
	}
	return value, nil
}

func searchSymbolPage(
	snapshot *hotsnapshot.GraphSnapshot,
	name, mode string,
	filter hotsnapshot.SymbolFilter,
	offset, limit int,
) (hotsnapshot.SymbolPage, error) {
	nameID, found := snapshot.Strings().Lookup(name)
	switch mode {
	case FindSymbolModeExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByName(nameID, filter, offset, limit)
	case FindSymbolModeQualifiedExact:
		if !found {
			nameID = hotsnapshot.InvalidInternedString
		}
		return snapshot.SearchSymbolsByQName(nameID, filter, offset, limit)
	case FindSymbolModePrefix:
		return snapshot.SearchSymbolsByNamePrefix(name, filter, offset, limit)
	case FindSymbolModeSubstring:
		return snapshot.SearchSymbolsByNameSubstring(name, filter, offset, limit)
	default:
		return hotsnapshot.SymbolPage{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

// symbolSummary builds one result row. The location is not optional: a search
// result the agent cannot open is a result it has to ask about again.
func symbolSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	format string,
) (SymbolSummary, error) {
	table := snapshot.Strings()
	canonical, canonicalOK := table.String(symbol.CanonicalIdentity)
	name, nameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	kind, kindOK := table.String(symbol.Kind)
	signature, signatureOK := table.String(symbol.Signature)
	if !canonicalOK || !nameOK || !qualifiedNameOK || !kindOK || !signatureOK {
		return SymbolSummary{}, fmt.Errorf(
			"symbol metadata references invalid strings (canonical_ok=%t name_ok=%t qualified_name_ok=%t kind_ok=%t signature_ok=%t)",
			canonicalOK, nameOK, qualifiedNameOK, kindOK, signatureOK,
		)
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return SymbolSummary{}, err
	}
	summary := SymbolSummary{
		StableKey:     symbolStableKey(snapshot, symbol),
		Name:          name,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Signature:     signature,
		Exported:      symbol.Exported,
		Repository:    location.RepositoryName,
		FilePath:      location.FilePath,
		StartLine:     symbol.StartLine,
		EndLine:       symbol.EndLine,
	}
	if format == ResponseFormatDetailed {
		summary.CanonicalIdentity = canonical
	}
	return summary, nil
}
