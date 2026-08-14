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
	CrossRepoConsumerExactSymbol = "exact_symbol"
	CrossRepoConsumerPackage     = "package"
	CrossRepoConsumerCandidate   = "candidate"
	CrossRepoConsumerUnresolved  = "unresolved"

	DefaultCrossRepoConsumerLimit      = 50
	MaximumCrossRepoConsumerLimit      = hotsnapshot.MaxExactResults
	SortingVersionCrossRepoConsumersV1 = "consumers-v1"
	findCrossRepoConsumersToolName     = "find_cross_repo_consumers"
)

// FindCrossRepoConsumersInput identifies one target symbol and asks for all
// known consumers in other repositories. Unresolved results are deliberately
// returned as a separate category: they describe a request and evidence, not
// an exact symbol identity.
type FindCrossRepoConsumersInput struct {
	StableKey      string `json:"stable_key,omitempty"`
	QualifiedName  string `json:"qualified_name,omitempty"`
	Repository     string `json:"repository,omitempty"`
	Path           string `json:"path,omitempty"`
	Repo           string `json:"repo,omitempty"`
	Language       string `json:"language,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// CrossRepoConsumerSummary is the common wire shape for exact symbol,
// package-level, candidate, and unresolved consumer records. Empty fields are
// intentional when the source fact has no symbol or file identity.
type CrossRepoConsumerSummary struct {
	Category string `json:"category"`

	// The consumer, named and located the way every other row of this surface
	// names one: enough to open it, and enough to address it in the next call.
	Name          string `json:"name,omitempty"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	EdgeKind      string `json:"edge_kind,omitempty"`
	Repository    string `json:"repository"`
	PackageName   string `json:"package_name,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
	StartLine     uint32 `json:"start_line,omitempty"`
	EndLine       uint32 `json:"end_line,omitempty"`

	Confidence   string `json:"confidence"`
	Provenance   string `json:"provenance,omitempty"`
	EvidenceKind string `json:"evidence_kind,omitempty"`

	// Derived identifiers, returned only for the detailed format. Measured on a
	// four-row answer they were 81 % of it.
	ConsumerSymbolKey     string `json:"consumer_symbol_key,omitempty"`
	ConsumerRepositoryKey string `json:"consumer_repository_key,omitempty"`
	ConsumerPackageKey    string `json:"consumer_package_key,omitempty"`
	ConsumerFileKey       string `json:"consumer_file_key,omitempty"`
	EvidenceKey           string `json:"evidence_key,omitempty"`

	UnresolvedKey    string `json:"unresolved_key,omitempty"`
	RequestedPackage string `json:"requested_package,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Detail           string `json:"detail,omitempty"`
	StartColumn      uint32 `json:"start_column,omitempty"`
	StartOffset      uint32 `json:"start_offset,omitempty"`
}

// CrossRepoSubject is the symbol the query asked about, stated once. It is the
// argument, not a result, and it was repeated on every row as five target_*
// fields.
type CrossRepoSubject struct {
	QualifiedName string `json:"qualified_name"`
	Repository    string `json:"repository"`
	PackageName   string `json:"package_name"`
	ModulePath    string `json:"module_path,omitempty"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
	EndLine       uint32 `json:"end_line"`

	StableKey     string `json:"stable_key,omitempty"`
	RepositoryKey string `json:"repository_key,omitempty"`
	PackageKey    string `json:"package_key,omitempty"`
}

// CrossRepoConsumers is one page of consumers around a subject.
type CrossRepoConsumers struct {
	Subject   CrossRepoSubject           `json:"subject"`
	Consumers []CrossRepoConsumerSummary `json:"consumers"`
}

type findCrossRepoConsumersOptions struct {
	Selector symbolSelector
	Repo     string
	Language string
	Limit    int
	Format   string
}

type findCrossRepoConsumersQuery struct {
	Tool          string `json:"tool"`
	StableKey     string `json:"stable_key,omitempty"`
	QualifiedName string `json:"qualified_name,omitempty"`
	Repository    string `json:"repository,omitempty"`
	Path          string `json:"path,omitempty"`
	Repo          string `json:"repo,omitempty"`
	Language      string `json:"language,omitempty"`
}

type consumerLocation struct {
	SymbolKey      string
	SymbolName     string
	QualifiedName  string
	SymbolKind     string
	StartLine      uint32
	EndLine        uint32
	RepositoryName string
	RepositoryKey  string
	PackageKey     string
	PackageName    string
	FileKey        string
	FilePath       string
	Languages      []string
}

type targetLocation struct {
	consumerLocation
	PackageID  hotsnapshot.PackageID
	ModulePath string
}

// RegisterFindCrossRepoConsumers adds the read-only consumer lookup without a
// graph source. Calls require a snapshot-backed registration to return data.
func RegisterFindCrossRepoConsumers(server *sdkmcp.Server) {
	RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterFindCrossRepoConsumersWithObserver adds the tool and observes its
// handler latency when observer is non-nil.
func RegisterFindCrossRepoConsumersWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterFindCrossRepoConsumersWithSnapshotStore registers the tool over the
// immutable snapshot currently published by snapshotStore.
func RegisterFindCrossRepoConsumersWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore registers the
// tool over an immutable snapshot and optionally observes latency.
func RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments FindCrossRepoConsumersInput,
	) (*sdkmcp.CallToolResult, Response[CrossRepoConsumers], error) {
		return findCrossRepoConsumers(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindCrossRepoConsumersInput,
		) (*sdkmcp.CallToolResult, Response[CrossRepoConsumers], error) {
			start := time.Now()
			result, response, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findCrossRepoConsumersToolName, start, response, err)
			return result, response, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        findCrossRepoConsumersToolName,
		Description: "Consumers of a symbol in other repositories, exact uses kept apart from package-level dependencies. A language server stops at its workspace.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func findCrossRepoConsumers(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindCrossRepoConsumersInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[CrossRepoConsumers], error) {
	options, err := normalizeFindCrossRepoConsumersInput(arguments)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	queryHash, err := HashQuery(findCrossRepoConsumersQuery{
		Tool: findCrossRepoConsumersToolName, StableKey: options.Selector.StableKey,
		QualifiedName: options.Selector.QualifiedName, Repository: options.Selector.Repository,
		Path: options.Selector.Path, Repo: options.Repo, Language: options.Language,
	})
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[CrossRepoConsumers]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[CrossRepoConsumers]{}, ErrIndexNotReady()
	}
	targetID, err := resolveSymbolSelector(snapshot, options.Selector)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	target, err := crossRepoTargetLocation(snapshot, targetID)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid target metadata", err)
	}

	metadata := snapshot.Metadata()
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[CrossRepoConsumers]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionCrossRepoConsumersV1); err != nil {
			return nil, Response[CrossRepoConsumers]{}, err
		}
		offset = cursor.Offset
	}

	results, coverage, err := collectCrossRepoConsumers(snapshot, targetID, target, options)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid consumer metadata", err)
	}
	total := len(results)
	if offset > total {
		offset = total
	}
	end := offset + options.Limit
	if end > total {
		end = total
	}
	page := append([]CrossRepoConsumerSummary(nil), results[offset:end]...)
	hasMore := end < total
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, end, SortingVersionCrossRepoConsumersV1)
		if err != nil {
			return nil, Response[CrossRepoConsumers]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[CrossRepoConsumers]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[CrossRepoConsumers]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(page), Truncated: hasMore, NextCursor: nextCursor,
		Coverage: coverage,
		Guidance: crossRepoGuidance(total, len(page), hasMore),
		Results:  CrossRepoConsumers{Subject: crossRepoSubject(target, options.Format), Consumers: page},
	}, nil
}

func normalizeFindCrossRepoConsumersInput(arguments FindCrossRepoConsumersInput) (findCrossRepoConsumersOptions, error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
	if err != nil {
		return findCrossRepoConsumersOptions{}, err
	}
	repo, err := normalizeReferenceFilter(arguments.Repo, "repo")
	if err != nil {
		return findCrossRepoConsumersOptions{}, err
	}
	language, err := normalizeReferenceFilter(arguments.Language, "language")
	if err != nil {
		return findCrossRepoConsumersOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultCrossRepoConsumerLimit
	}
	if limit < 1 || limit > MaximumCrossRepoConsumerLimit {
		return findCrossRepoConsumersOptions{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumCrossRepoConsumerLimit))
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return findCrossRepoConsumersOptions{}, err
	}
	return findCrossRepoConsumersOptions{Selector: selector, Repo: repo, Language: language, Limit: limit, Format: format}, nil
}

func collectCrossRepoConsumers(
	snapshot *hotsnapshot.GraphSnapshot,
	targetID hotsnapshot.SymbolID,
	target targetLocation,
	options findCrossRepoConsumersOptions,
) ([]CrossRepoConsumerSummary, Coverage, error) {
	results := make([]CrossRepoConsumerSummary, 0)
	coverage := Coverage{}

	for _, edge := range snapshot.Incoming(targetID) {
		decoded, relevant, err := decodeReferenceEdge(edge)
		if err != nil {
			return nil, Coverage{}, err
		}
		if !relevant {
			continue
		}
		source, err := crossRepoSymbolLocation(snapshot, edge.Target)
		if err != nil {
			return nil, Coverage{}, err
		}
		if source.RepositoryKey == target.RepositoryKey || !consumerLocationMatches(source, options) {
			continue
		}
		category := CrossRepoConsumerCandidate
		if decoded.Confidence.Exact() {
			category = CrossRepoConsumerExactSymbol
		}
		addReferenceCoverage(&coverage, decoded.Confidence)
		summary, err := crossRepoSymbolSummary(snapshot, source, target, edge, decoded, category, options.Format)
		if err != nil {
			return nil, Coverage{}, err
		}
		results = append(results, summary)
	}

	for _, dependency := range snapshot.PackageDependencies(target.PackageID) {
		kind, err := facts.EdgeKindFromCode(dependency.Kind)
		if err != nil {
			return nil, Coverage{}, err
		}
		confidence, err := facts.ConfidenceFromCode(dependency.Confidence)
		if err != nil {
			return nil, Coverage{}, err
		}
		provenance, err := facts.ProvenanceFromCode(dependency.Provenance)
		if err != nil {
			return nil, Coverage{}, err
		}
		source, err := crossRepoPackageLocation(snapshot, dependency.Source)
		if err != nil {
			return nil, Coverage{}, err
		}
		if source.RepositoryKey == target.RepositoryKey || !consumerLocationMatches(source, options) {
			continue
		}
		// A PACKAGE_DEPENDS_ON edge is evidence about the package, not about
		// the symbol this query names: it never counts as an exact consumer.
		coverage.PackageLevel++
		row := CrossRepoConsumerSummary{
			Category:    CrossRepoConsumerPackage,
			Repository:  source.RepositoryName,
			PackageName: source.PackageName,
			EdgeKind:    string(kind), Confidence: string(confidence), Provenance: string(provenance),
		}
		if options.Format == ResponseFormatDetailed {
			row.ConsumerRepositoryKey = source.RepositoryKey
			row.ConsumerPackageKey = source.PackageKey
			row.EvidenceKey = crossRepoPackageEvidenceKey(snapshot, dependency.Evidence)
		}
		results = append(results, row)
	}

	for _, reference := range snapshot.UnresolvedReferences() {
		if !crossRepoUnresolvedMatchesTarget(snapshot, reference, target) {
			continue
		}
		source, err := crossRepoUnresolvedLocation(snapshot, reference)
		if err != nil {
			return nil, Coverage{}, err
		}
		if source.RepositoryKey == target.RepositoryKey || !consumerLocationMatches(source, options) {
			continue
		}
		confidence := facts.Unresolved
		addReferenceCoverage(&coverage, confidence)
		table := snapshot.Strings()
		key, keyOK := table.String(reference.Key)
		requestedPackage, packageOK := table.String(reference.RequestedPackage)
		requestedSymbol, symbolOK := table.String(reference.RequestedSymbol)
		reason, reasonOK := table.String(reference.Reason)
		detail, detailOK := table.String(reference.Detail)
		if !keyOK || !packageOK || !symbolOK || !reasonOK || !detailOK {
			return nil, Coverage{}, fmt.Errorf("unresolved reference contains invalid strings")
		}
		row := CrossRepoConsumerSummary{
			Category:    CrossRepoConsumerUnresolved,
			Name:        source.SymbolName,
			Repository:  source.RepositoryName,
			PackageName: source.PackageName,
			FilePath:    source.FilePath,
			Confidence:  string(confidence), RequestedPackage: requestedPackage,
			RequestedSymbol: requestedSymbol, Reason: reason, Detail: detail,
			StartLine: reference.StartLine, StartColumn: reference.StartColumn, StartOffset: reference.StartOffset,
		}
		if options.Format == ResponseFormatDetailed {
			row.ConsumerSymbolKey = source.SymbolKey
			row.ConsumerRepositoryKey = source.RepositoryKey
			row.ConsumerPackageKey = source.PackageKey
			row.ConsumerFileKey = source.FileKey
			row.UnresolvedKey = key
		}
		results = append(results, row)
	}

	sort.SliceStable(results, func(i, j int) bool { return crossRepoConsumerLess(results[i], results[j]) })
	return results, coverage, nil
}

func crossRepoSymbolSummary(
	snapshot *hotsnapshot.GraphSnapshot,
	source consumerLocation,
	target targetLocation,
	edge hotsnapshot.PackedEdge,
	decoded decodedReferenceEdge,
	category string,
	format string,
) (CrossRepoConsumerSummary, error) {
	evidence, found := snapshot.Evidence(edge.Evidence)
	if !found {
		return CrossRepoConsumerSummary{}, fmt.Errorf("edge evidence index %d is missing", edge.Evidence)
	}
	evidenceKind, found := snapshot.Strings().String(evidence.Kind)
	if !found {
		return CrossRepoConsumerSummary{}, fmt.Errorf("edge evidence index %d has an invalid kind", edge.Evidence)
	}
	summary := CrossRepoConsumerSummary{
		Category:      category,
		Name:          source.SymbolName,
		QualifiedName: source.QualifiedName,
		Kind:          source.SymbolKind,
		EdgeKind:      string(decoded.Kind),
		Repository:    source.RepositoryName,
		PackageName:   source.PackageName,
		FilePath:      source.FilePath,
		StartLine:     source.StartLine,
		EndLine:       source.EndLine,
		Confidence:    string(decoded.Confidence),
		Provenance:    string(decoded.Provenance),
		EvidenceKind:  evidenceKind,
	}
	if format == ResponseFormatDetailed {
		summary.ConsumerSymbolKey = source.SymbolKey
		summary.ConsumerRepositoryKey = source.RepositoryKey
		summary.ConsumerPackageKey = source.PackageKey
		summary.ConsumerFileKey = source.FileKey
		summary.EvidenceKey = crossRepoEvidenceKey(snapshot, edge.Evidence)
	}
	return summary, nil
}

func crossRepoTargetLocation(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.SymbolID) (targetLocation, error) {
	location, err := crossRepoSymbolLocation(snapshot, id)
	if err != nil {
		return targetLocation{}, err
	}
	symbol, found := snapshot.Symbol(id)
	if !found {
		return targetLocation{}, fmt.Errorf("symbol index %d is missing", id)
	}
	file, found := snapshot.File(symbol.File)
	if !found {
		return targetLocation{}, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	pkg, found := snapshot.Package(file.Package)
	if !found {
		return targetLocation{}, fmt.Errorf("symbol %q references missing package %d", symbol.StableKey, file.Package)
	}
	modulePath, ok := snapshot.Strings().String(pkg.ModulePath)
	if !ok {
		return targetLocation{}, fmt.Errorf("package %d has an invalid module path", file.Package)
	}
	return targetLocation{consumerLocation: location, PackageID: file.Package, ModulePath: modulePath}, nil
}

func crossRepoSymbolLocation(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.SymbolID) (consumerLocation, error) {
	symbol, file, repository, languages, err := symbolReferenceLocation(snapshot, id)
	if err != nil {
		return consumerLocation{}, err
	}
	fileRecord, found := snapshot.File(symbol.File)
	if !found {
		return consumerLocation{}, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	pkg, found := snapshot.Package(fileRecord.Package)
	if !found {
		return consumerLocation{}, fmt.Errorf("symbol %q references missing package %d", symbol.StableKey, fileRecord.Package)
	}
	table := snapshot.Strings()
	packageKey, packageOK := table.String(pkg.Key)
	packageName, packageNameOK := table.String(pkg.Name)
	symbolName, symbolNameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	symbolKind, kindOK := table.String(symbol.Kind)
	if !packageOK || !packageNameOK || !symbolNameOK || !qualifiedNameOK || !kindOK {
		return consumerLocation{}, fmt.Errorf("symbol %q references invalid package or symbol strings", symbol.StableKey)
	}
	return consumerLocation{
		SymbolKey: string(symbol.StableKey), SymbolName: symbolName, QualifiedName: qualifiedName,
		SymbolKind: symbolKind, StartLine: symbol.StartLine, EndLine: symbol.EndLine,
		RepositoryName: repository.name, RepositoryKey: repository.key,
		PackageKey: packageKey, PackageName: packageName,
		FileKey: file.key, FilePath: file.path, Languages: languages,
	}, nil
}

func crossRepoPackageLocation(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.PackageID) (consumerLocation, error) {
	pkg, found := snapshot.Package(id)
	if !found {
		return consumerLocation{}, fmt.Errorf("package index %d is missing", id)
	}
	repository, found := snapshot.Repository(pkg.Repository)
	if !found {
		return consumerLocation{}, fmt.Errorf("package %d references missing repository %d", id, pkg.Repository)
	}
	table := snapshot.Strings()
	repositoryKey, repositoryOK := table.String(repository.Key)
	packageKey, packageOK := table.String(pkg.Key)
	packageName, packageNameOK := table.String(pkg.Name)
	if !repositoryOK || !packageOK || !packageNameOK {
		return consumerLocation{}, fmt.Errorf("package %d has invalid identity strings", id)
	}
	languages, err := packageLanguages(snapshot, pkg, repository)
	if err != nil {
		return consumerLocation{}, err
	}
	return consumerLocation{RepositoryKey: repositoryKey, PackageKey: packageKey, PackageName: packageName, Languages: languages}, nil
}

func crossRepoUnresolvedLocation(snapshot *hotsnapshot.GraphSnapshot, reference hotsnapshot.UnresolvedReferenceRecord) (consumerLocation, error) {
	table := snapshot.Strings()
	repository, found := snapshot.Repository(reference.Repository)
	if !found {
		return consumerLocation{}, fmt.Errorf("unresolved reference references missing repository %d", reference.Repository)
	}
	repositoryKey, found := table.String(repository.Key)
	if !found {
		return consumerLocation{}, fmt.Errorf("unresolved reference has invalid repository key")
	}
	if reference.Source != hotsnapshot.InvalidSymbolID {
		location, err := crossRepoSymbolLocation(snapshot, reference.Source)
		if err != nil {
			return consumerLocation{}, err
		}
		return location, nil
	}
	location := consumerLocation{RepositoryKey: repositoryKey}
	if reference.File != hotsnapshot.InvalidFileID {
		file, found := snapshot.File(reference.File)
		if !found {
			return consumerLocation{}, fmt.Errorf("unresolved reference references missing file %d", reference.File)
		}
		fileKey, fileKeyOK := table.String(file.Key)
		filePath, filePathOK := table.String(file.Path)
		pkg, packageOK := snapshot.Package(file.Package)
		if !fileKeyOK || !filePathOK || !packageOK {
			return consumerLocation{}, fmt.Errorf("unresolved reference has invalid file/package metadata")
		}
		packageKey, packageKeyOK := table.String(pkg.Key)
		packageName, packageNameOK := table.String(pkg.Name)
		if !packageKeyOK || !packageNameOK {
			return consumerLocation{}, fmt.Errorf("unresolved reference has invalid package metadata")
		}
		languages, err := packageLanguages(snapshot, pkg, repository)
		if err != nil {
			return consumerLocation{}, err
		}
		location.PackageKey, location.PackageName = packageKey, packageName
		location.FileKey, location.FilePath, location.Languages = fileKey, filePath, languages
		return location, nil
	}
	language, languageOK := table.String(reference.Language)
	if !languageOK {
		return consumerLocation{}, fmt.Errorf("unresolved reference has invalid language")
	}
	location.Languages = nonEmptyStrings(language)
	return location, nil
}

func packageLanguages(snapshot *hotsnapshot.GraphSnapshot, pkg hotsnapshot.PackageRecord, repository hotsnapshot.RepositoryRecord) ([]string, error) {
	table := snapshot.Strings()
	if language, ok := table.String(pkg.Language); ok && language != "" {
		return []string{language}, nil
	}
	languages, ok := table.String(repository.Languages)
	if !ok {
		return nil, fmt.Errorf("repository %d has invalid languages", pkg.Repository)
	}
	return splitRepositoryLanguages(languages), nil
}

func crossRepoUnresolvedMatchesTarget(snapshot *hotsnapshot.GraphSnapshot, reference hotsnapshot.UnresolvedReferenceRecord, target targetLocation) bool {
	table := snapshot.Strings()
	requestedPackage, packageOK := table.String(reference.RequestedPackage)
	requestedSymbol, symbolOK := table.String(reference.RequestedSymbol)
	if !packageOK || !symbolOK || requestedPackage == "" {
		return false
	}
	if requestedPackage != target.PackageKey && requestedPackage != target.PackageName && requestedPackage != target.ModulePath {
		return false
	}
	// A failure that named no symbol is a fact about the import of a whole
	// package -- an unreadable module, an absent provider. Attributing it to
	// every symbol that package exports would answer a question nobody
	// asked; `get_unresolved_references` serves it by requested_package.
	if requestedSymbol == "" {
		return false
	}
	return requestedSymbol == target.SymbolKey || requestedSymbol == target.SymbolName || requestedSymbol == target.QualifiedName ||
		strings.HasSuffix(requestedSymbol, "."+target.SymbolName) || strings.HasSuffix(requestedSymbol, "::"+target.SymbolName)
}

// consumerLocationMatches applies the caller's narrowing. The repository filter
// takes a name, which is what every row of this surface carries and what the
// triple selector accepts.
func consumerLocationMatches(location consumerLocation, options findCrossRepoConsumersOptions) bool {
	if options.Repo != "" && location.RepositoryName != options.Repo {
		return false
	}
	return options.Language == "" || containsString(location.Languages, options.Language)
}

func crossRepoEvidenceKey(snapshot *hotsnapshot.GraphSnapshot, evidence hotsnapshot.EvidenceID) string {
	record, found := snapshot.Evidence(evidence)
	if !found {
		return ""
	}
	key, _ := snapshot.Strings().String(record.Key)
	return key
}

func crossRepoPackageEvidenceKey(snapshot *hotsnapshot.GraphSnapshot, evidence hotsnapshot.InternedString) string {
	key, _ := snapshot.Strings().String(evidence)
	return key
}

func crossRepoConsumerLess(left, right CrossRepoConsumerSummary) bool {
	categoryRank := func(value string) int {
		switch value {
		case CrossRepoConsumerExactSymbol:
			return 0
		case CrossRepoConsumerPackage:
			return 1
		case CrossRepoConsumerCandidate:
			return 2
		default:
			return 3
		}
	}
	if categoryRank(left.Category) != categoryRank(right.Category) {
		return categoryRank(left.Category) < categoryRank(right.Category)
	}
	keys := [7]string{
		left.ConsumerRepositoryKey, left.ConsumerPackageKey, left.ConsumerSymbolKey,
		left.ConsumerFileKey, left.Kind, left.EvidenceKey, left.UnresolvedKey,
	}
	rightKeys := [7]string{
		right.ConsumerRepositoryKey, right.ConsumerPackageKey, right.ConsumerSymbolKey,
		right.ConsumerFileKey, right.Kind, right.EvidenceKey, right.UnresolvedKey,
	}
	for index := range keys {
		if keys[index] != rightKeys[index] {
			return keys[index] < rightKeys[index]
		}
	}
	// The subject is the same symbol on every row of a response, so what breaks
	// a remaining tie is the consumer's own position.
	return left.StartLine < right.StartLine
}

func nonEmptyStrings(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

// crossRepoSubject states the queried symbol once. It was five `target_*` fields
// on every row, and it is the argument of the call.
func crossRepoSubject(target targetLocation, format string) CrossRepoSubject {
	subject := CrossRepoSubject{
		QualifiedName: target.QualifiedName,
		Repository:    target.RepositoryName,
		PackageName:   target.PackageName,
		ModulePath:    target.ModulePath,
		FilePath:      target.FilePath,
		StartLine:     target.StartLine,
		EndLine:       target.EndLine,
	}
	if format == ResponseFormatDetailed {
		subject.StableKey = target.SymbolKey
		subject.RepositoryKey = target.RepositoryKey
		subject.PackageKey = target.PackageKey
	}
	return subject
}

// crossRepoGuidance reads an empty answer for what it is. This tool is the one
// with no native competitor, so "nobody outside uses it" is a finding rather than
// a miss -- and the two ways it can be wrong are worth naming.
func crossRepoGuidance(total, returned int, truncated bool) string {
	switch {
	case total == 0:
		return "no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository"
	case truncated:
		return truncatedGuidance(returned, total, "repo or language")
	default:
		return ""
	}
}
