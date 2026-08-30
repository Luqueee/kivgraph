package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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
//
// `view` is the granularity of the answer, never a different answer: `compact`
// -the default- states once what every consumer shares and groups the package
// dependencies of a repository into one entry, and `full` repeats every field
// on every row. `files` is rejected: this answer is a set of repositories.
type FindCrossRepoConsumersInput struct {
	Profile        []string `json:"profile,omitempty" jsonschema:"Profiles to query; omit for the default, or use * alone for all."`
	StableKey      string   `json:"stable_key,omitempty" jsonschema:"The target symbol durable key, as a detailed result returns it. The triple works instead."`
	QualifiedName  string   `json:"qualified_name,omitempty" jsonschema:"The target symbol fully qualified name, as every row of this surface carries it."`
	Repository     string   `json:"repository,omitempty" jsonschema:"The repository that declares the target symbol, the provider side of the question."`
	Path           string   `json:"path,omitempty" jsonschema:"The repository-relative file that declares the target symbol."`
	Repo           string   `json:"repo,omitempty" jsonschema:"Keep only consumers found in this repository."`
	Language       string   `json:"language,omitempty" jsonschema:"Keep only consumers written in this language."`
	Limit          int      `json:"limit,omitempty" jsonschema:"Consumers in one page. Defaults to 50."`
	Cursor         string   `json:"cursor,omitempty" jsonschema:"The next_cursor of the previous page. Every other argument must stay the same."`
	ResponseFormat string   `json:"response_format,omitempty" jsonschema:"concise (the default) omits the derived identifiers; detailed returns them."`
	View           string   `json:"view,omitempty" jsonschema:"Granularity, never a different answer: compact (the default) groups the package dependencies of a repository, full repeats every field. files is rejected."`
}

// CrossRepoConsumerSummary is the common wire shape for exact symbol,
// package-level, candidate, and unresolved consumer records. Empty fields are
// intentional when the source fact has no symbol or file identity.
type CrossRepoConsumerSummary struct {
	Profiles ProfileNames `json:"profile,omitempty"`
	Category string       `json:"category"`

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
	// View selects how the page is written. It is the argument that produced
	// the shape and never travels in it.
	View string `json:"-"`
}

// CompactCrossRepoSubject is the queried symbol with its location written as
// the `repository:path:line` triple every tool accepts, and `end_line` only
// when the declaration spans more than the line already stated.
type CompactCrossRepoSubject struct {
	QualifiedName string `json:"qualified_name"`
	At            string `json:"at"`
	PackageName   string `json:"pkg"`
	ModulePath    string `json:"module_path,omitempty"`
	EndLine       uint32 `json:"end_line,omitempty"`

	StableKey     string `json:"stable_key,omitempty"`
	RepositoryKey string `json:"repository_key,omitempty"`
	PackageKey    string `json:"package_key,omitempty"`
}

// CompactCrossRepoConsumers is one page with the columns every consumer shares
// lifted into the header, and the rest grouped by whatever tuple they still
// share instead of repeating it per row; see compact. Consumers and Groups
// are mutually exclusive: Groups appears only when the page itself could not
// agree on category, edge_kind, confidence, provenance, evidence_kind or
// reason.
type CompactCrossRepoConsumers struct {
	Subject          CompactCrossRepoSubject    `json:"subject"`
	Category         string                     `json:"category,omitempty"`
	EdgeKind         string                     `json:"edge_kind,omitempty"`
	Confidence       string                     `json:"confidence,omitempty"`
	Provenance       string                     `json:"provenance,omitempty"`
	EvidenceKind     string                     `json:"evidence_kind,omitempty"`
	Reason           string                     `json:"reason,omitempty"`
	RequestedPackage string                     `json:"requested_package,omitempty"`
	RequestedSymbol  string                     `json:"requested_symbol,omitempty"`
	Consumers        []CompactCrossRepoConsumer `json:"consumers,omitempty"`
	Groups           []CompactCrossRepoGroup    `json:"groups,omitempty"`
}

// CompactCrossRepoGroup is every consumer that shares one exact tuple of
// category, edge_kind, confidence, provenance, evidence_kind and reason the
// page could not hoist. Absent means this group's rows hold the page's
// hoisted value too.
//
// Detail is not part of that tuple: it is prose, sometimes a template shared
// by every unresolved row of one reason and sometimes not, so it is worth a
// second hoist attempt once the bucket is fixed rather than a grouping key of
// its own -- the same way ReachedFrom works for get_blast_radius.
type CompactCrossRepoGroup struct {
	Category     string `json:"category,omitempty"`
	EdgeKind     string `json:"edge_kind,omitempty"`
	Confidence   string `json:"confidence,omitempty"`
	Provenance   string `json:"provenance,omitempty"`
	EvidenceKind string `json:"evidence_kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Detail       string `json:"detail,omitempty"`

	Consumers []CompactCrossRepoConsumer `json:"consumers"`
}

// CompactCrossRepoConsumer is one consumer, or one repository's package-level
// dependency on the provider. `at` is `path:line` inside `repo` and is absent
// for a package dependency, which proves the repository depends on the
// provider and never that a file uses the symbol. A field the header or the
// row's own group already states is absent here, `reason` and `detail`
// included: a shared reason is common enough to hoist to a group, and a
// shared `detail` inside that group is a template, not per-row prose.
type CompactCrossRepoConsumer struct {
	Profiles ProfileNames `json:"profile,omitempty"`
	Category string       `json:"category,omitempty"`
	Repo     string       `json:"repo"`
	// PackageName is the bare name while one package of the repository holds
	// the fact, and the list of names when several do.
	PackageName any    `json:"pkg,omitempty"`
	At          string `json:"at,omitempty"`
	EndLine     uint32 `json:"end_line,omitempty"`

	QualifiedName string `json:"qualified_name,omitempty"`
	Name          string `json:"name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	EdgeKind      string `json:"edge_kind,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	Provenance    string `json:"provenance,omitempty"`
	EvidenceKind  string `json:"evidence_kind,omitempty"`

	RequestedPackage string `json:"requested_package,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Detail           string `json:"detail,omitempty"`
	Column           uint32 `json:"col,omitempty"`
	Offset           uint32 `json:"offset,omitempty"`

	ConsumerSymbolKey     string `json:"consumer_symbol_key,omitempty"`
	ConsumerRepositoryKey string `json:"consumer_repository_key,omitempty"`
	ConsumerPackageKey    string `json:"consumer_package_key,omitempty"`
	ConsumerFileKey       string `json:"consumer_file_key,omitempty"`
	EvidenceKey           string `json:"evidence_key,omitempty"`
	UnresolvedKey         string `json:"unresolved_key,omitempty"`
}

// MarshalJSON writes the compact page unless the call asked for the full one.
// Both views report the same consumers with the same confidence and the same
// provenance: `coverage` keeps `exact` and `package_level` apart in either, so
// a package dependency is never read as a use of the symbol.
func (page CrossRepoConsumers) MarshalJSON() ([]byte, error) {
	if page.View == ViewFull || page.View == "" {
		type fullPage struct {
			Subject   CrossRepoSubject           `json:"subject"`
			Consumers []CrossRepoConsumerSummary `json:"consumers"`
		}
		return json.Marshal(fullPage{Subject: page.Subject, Consumers: page.Consumers})
	}
	return json.Marshal(page.compact())
}

type findCrossRepoConsumersOptions struct {
	Selector symbolSelector
	Repo     string
	Language string
	Limit    int
	Format   string
	View     string
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
		if snapshotStore != nil {
			if profileErr := RequireStableKeyProfile(snapshotStore.ProfileCount(), arguments.StableKey, arguments.Profile); profileErr != nil {
				return nil, Response[CrossRepoConsumers]{}, profileErr
			}
			selected, selectionErr := snapshotStore.ResolveProfiles(arguments.Profile)
			if selectionErr != nil {
				return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeInvalidArgument, selectionErr.Error(), selectionErr)
			}
			if len(selected) > 1 {
				return findCrossRepoConsumersAcrossProfiles(ctx, request, arguments, selected)
			}
		}
		store, profile, count, err := resolveSingleProfile(snapshotStore, arguments.Profile, arguments.StableKey)
		if err != nil {
			return nil, Response[CrossRepoConsumers]{}, err
		}
		result, response, err := findCrossRepoConsumers(ctx, request, arguments, store)
		scopeResponse(&response, profile, count)
		return result, response, err
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
		Description: "Consumers of a symbol in other repositories, exact uses kept apart from package-level dependencies that prove no use. A language server stops at its own workspace and cannot answer this.",
		Annotations: readOnlyClosedWorld(),
	}, handler)
}

func findCrossRepoConsumersAcrossProfiles(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindCrossRepoConsumersInput,
	selected []hotsnapshot.ProfileStore,
) (*sdkmcp.CallToolResult, Response[CrossRepoConsumers], error) {
	options, err := normalizeFindCrossRepoConsumersInput(arguments)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	names := make([]string, 0, len(selected))
	for _, profile := range selected {
		names = append(names, profile.Name)
	}
	queryHash, err := HashQuery(struct {
		Tool     string                      `json:"tool"`
		Profiles []string                    `json:"profiles"`
		Query    findCrossRepoConsumersQuery `json:"query"`
	}{findCrossRepoConsumersToolName, names, findCrossRepoConsumersQuery{
		Tool: findCrossRepoConsumersToolName, StableKey: options.Selector.StableKey,
		QualifiedName: options.Selector.QualifiedName, Repository: options.Selector.Repository,
		Path: options.Selector.Path, Repo: options.Repo, Language: options.Language,
	}})
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	profiles := make([]ProfileSnapshot, 0, len(selected))
	rows := make([]CrossRepoConsumerSummary, 0)
	variants := make(map[string]int)
	coverage := Coverage{}
	mergedCompleteness := Completeness{Verdict: VerdictComplete}
	var subject CrossRepoSubject
	foundSubject := false
	for _, profile := range selected {
		snapshot := profile.Store.Load()
		if snapshot == nil {
			return nil, Response[CrossRepoConsumers]{}, ErrIndexNotReady()
		}
		profileCompleteness := Completeness{Verdict: VerdictComplete}
		profileOptions := options
		profileOptions.Format = ResponseFormatDetailed
		targetID, resolveErr := resolveSymbolSelector(snapshot, profileOptions.Selector)
		if resolveErr == nil {
			target, locationErr := crossRepoTargetLocation(snapshot, targetID)
			if locationErr != nil {
				return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid target metadata", locationErr)
			}
			foundSubject = true
			if subject.QualifiedName == "" {
				subject = crossRepoSubject(target, options.Format)
			}
			profileRows, profileCoverage, collectErr := collectCrossRepoConsumers(snapshot, targetID, target, profileOptions)
			if collectErr != nil {
				return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid consumer metadata", collectErr)
			}
			addCoverage(&coverage, profileCoverage)
			completeness, _, completenessErr := completenessFor(snapshot, target.SymbolName, hotsnapshot.InvalidRepositoryID)
			if completenessErr != nil {
				return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid unresolved metadata", completenessErr)
			}
			profileCompleteness = completeness
			mergeCompleteness(&mergedCompleteness, &completeness)
			for _, row := range profileRows {
				identity := strings.Join([]string{row.ConsumerSymbolKey, row.ConsumerPackageKey, row.UnresolvedKey}, "\x00")
				row.Profiles = ""
				if options.Format != ResponseFormatDetailed {
					row.ConsumerSymbolKey, row.ConsumerRepositoryKey, row.ConsumerPackageKey = "", "", ""
					row.ConsumerFileKey, row.EvidenceKey, row.UnresolvedKey = "", "", ""
				}
				payload, marshalErr := json.Marshal(row)
				if marshalErr != nil {
					return nil, Response[CrossRepoConsumers]{}, WrapToolError(CodeSnapshotUnavailable, "encode consumer payload for profile merge", marshalErr)
				}
				key := identity + "\x00" + string(payload)
				if position, duplicate := variants[key]; duplicate {
					rows[position].Profiles = rows[position].Profiles.append(profile.Name)
					continue
				}
				row.Profiles = profileNames(profile.Name)
				variants[key] = len(rows)
				rows = append(rows, row)
			}
		} else if code := ErrorCode(resolveErr); code != CodeSymbolNotFound && code != CodeRepositoryNotFound {
			return nil, Response[CrossRepoConsumers]{}, resolveErr
		}
		completeness := profileCompleteness
		profiles = append(profiles, ProfileSnapshot{Name: profile.Name, SnapshotID: snapshot.Metadata().ID, Completeness: &completeness})
	}
	if !foundSubject {
		return nil, Response[CrossRepoConsumers]{}, NewToolError(CodeSymbolNotFound, "symbol was not found in the selected profiles")
	}
	offset, end, next, err := profilePageBounds(profiles, queryHash, SortingVersionCrossRepoConsumersV1, arguments.Cursor, options.Limit, len(rows))
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, err
	}
	return nil, Response[CrossRepoConsumers]{
		Profiles: profiles, CrossProfileEdges: "not_resolved",
		Total: len(rows), Returned: end - offset, Truncated: end < len(rows), NextCursor: next,
		Coverage: coverage, Completeness: &mergedCompleteness,
		Guidance: crossRepoGuidance(len(rows), end-offset, end < len(rows), mergedCompleteness.Verdict),
		Results:  CrossRepoConsumers{Subject: subject, Consumers: rows[offset:end], View: options.View},
		View:     options.View,
	}, nil
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

	// This tool is the one with no native competitor, and its empty answer is
	// sold as a finding: nobody outside uses this. A finding needs the check
	// behind it. The scope half is deliberately global -- InvalidRepositoryID --
	// because a package the index could not read in *any* repository is exactly
	// what would hide a consumer, and the target's own repository is the one
	// place a consumer cannot be.
	//
	// Its count does not go into coverage: the cross-repository failures are
	// already listed as UNRESOLVED rows of this answer and counted there, and
	// adding them twice would inflate the only number a caller can audit.
	completeness, _, err := completenessFor(snapshot, target.SymbolName, hotsnapshot.InvalidRepositoryID)
	if err != nil {
		return nil, Response[CrossRepoConsumers]{}, WrapToolError(
			CodeSnapshotUnavailable, "active snapshot contains invalid unresolved metadata", err)
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[CrossRepoConsumers]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(page), Truncated: hasMore, NextCursor: nextCursor,
		Coverage:     coverage,
		Completeness: &completeness,
		Guidance:     crossRepoGuidance(total, len(page), hasMore, completeness.Verdict),
		View:         options.View,
		Results: CrossRepoConsumers{
			Subject: crossRepoSubject(target, options.Format), Consumers: page, View: options.View,
		},
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
	// The answer is a set of repositories, so `files` has nothing to return.
	view, err := normalizeView(arguments.View, false)
	if err != nil {
		return findCrossRepoConsumersOptions{}, err
	}
	return findCrossRepoConsumersOptions{
		Selector: selector, Repo: repo, Language: language,
		Limit: limit, Format: format, View: view,
	}, nil
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
	stableKey := symbolStableKey(snapshot, symbol)
	file, found := snapshot.File(symbol.File)
	if !found {
		return targetLocation{}, fmt.Errorf("symbol %q references missing file %d", stableKey, symbol.File)
	}
	pkg, found := snapshot.Package(file.Package)
	if !found {
		return targetLocation{}, fmt.Errorf("symbol %q references missing package %d", stableKey, file.Package)
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
	stableKey := symbolStableKey(snapshot, symbol)
	fileRecord, found := snapshot.File(symbol.File)
	if !found {
		return consumerLocation{}, fmt.Errorf("symbol %q references missing file %d", stableKey, symbol.File)
	}
	pkg, found := snapshot.Package(fileRecord.Package)
	if !found {
		return consumerLocation{}, fmt.Errorf("symbol %q references missing package %d", stableKey, fileRecord.Package)
	}
	table := snapshot.Strings()
	packageKey, packageOK := table.String(pkg.Key)
	packageName, packageNameOK := table.String(pkg.Name)
	symbolName, symbolNameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	symbolKind, kindOK := table.String(symbol.Kind)
	if !packageOK || !packageNameOK || !symbolNameOK || !qualifiedNameOK || !kindOK {
		return consumerLocation{}, fmt.Errorf("symbol %q references invalid package or symbol strings", stableKey)
	}
	return consumerLocation{
		SymbolKey: stableKey, SymbolName: symbolName, QualifiedName: qualifiedName,
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
	repositoryName, repositoryNameOK := table.String(repository.Name)
	packageKey, packageOK := table.String(pkg.Key)
	packageName, packageNameOK := table.String(pkg.Name)
	if !repositoryOK || !repositoryNameOK || !packageOK || !packageNameOK {
		return consumerLocation{}, fmt.Errorf("package %d has invalid identity strings", id)
	}
	languages, err := packageLanguages(snapshot, pkg, repository)
	if err != nil {
		return consumerLocation{}, err
	}
	return consumerLocation{
		RepositoryName: repositoryName, RepositoryKey: repositoryKey,
		PackageKey: packageKey, PackageName: packageName, Languages: languages,
	}, nil
}

func crossRepoUnresolvedLocation(snapshot *hotsnapshot.GraphSnapshot, reference hotsnapshot.UnresolvedReferenceRecord) (consumerLocation, error) {
	table := snapshot.Strings()
	repository, found := snapshot.Repository(reference.Repository)
	if !found {
		return consumerLocation{}, fmt.Errorf("unresolved reference references missing repository %d", reference.Repository)
	}
	repositoryKey, keyOK := table.String(repository.Key)
	repositoryName, nameOK := table.String(repository.Name)
	if !keyOK || !nameOK {
		return consumerLocation{}, fmt.Errorf("unresolved reference has invalid repository identity")
	}
	if reference.Source != hotsnapshot.InvalidSymbolID {
		location, err := crossRepoSymbolLocation(snapshot, reference.Source)
		if err != nil {
			return consumerLocation{}, err
		}
		return location, nil
	}
	location := consumerLocation{RepositoryName: repositoryName, RepositoryKey: repositoryKey}
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

// crossRepoHoisted is every column already accounted for above one consumer
// row, whether by the page header or by its group: a row repeats a field only
// when its own value differs from this. One named struct instead of nine
// positional strings is deliberate -- passing the wrong one in the wrong slot
// is exactly the defect that first made a compact page more expensive than
// not compacting at all; see ADR 0046.
type crossRepoHoisted struct {
	Category, EdgeKind, Confidence, Provenance, EvidenceKind, Reason, Detail string
	RequestedPackage, RequestedSymbol                                        string
}

// compact lifts into the header what every consumer of the page repeats, then
// groups what is left by the exact tuple of category, edge_kind, confidence,
// provenance, evidence_kind and reason each consumer still shares -- stated
// once per group instead of once per row. Within a group, a repository still
// gets one entry for its package dependencies, and `detail` -- the absolute
// path or the sentence a resolution failure carries, a template repeated
// verbatim by every row of one reason and not a row's own prose -- gets a
// second, group-scoped hoist attempt the same way ReachedFrom does for
// get_blast_radius.
//
// The rows are the same facts throughout: `total`, `returned` and `coverage`
// still count every dependency, so a repository that depends on the provider
// from two packages is one entry and two package-level facts, grouped or not.
func (page CrossRepoConsumers) compact() CompactCrossRepoConsumers {
	rows := len(page.Consumers)
	shared := func(field func(CrossRepoConsumerSummary) string) string {
		return hoistString(rows, func(index int) string { return field(page.Consumers[index]) })
	}
	requested := func(field func(CrossRepoConsumerSummary) string) string {
		single := ""
		for _, row := range page.Consumers {
			value := field(row)
			if value == "" {
				continue
			}
			if single != "" && single != value {
				return ""
			}
			single = value
		}
		return single
	}
	compact := CompactCrossRepoConsumers{
		Subject:      compactCrossRepoSubject(page.Subject),
		Category:     shared(func(row CrossRepoConsumerSummary) string { return row.Category }),
		EdgeKind:     shared(func(row CrossRepoConsumerSummary) string { return row.EdgeKind }),
		Confidence:   shared(func(row CrossRepoConsumerSummary) string { return row.Confidence }),
		Provenance:   shared(func(row CrossRepoConsumerSummary) string { return row.Provenance }),
		EvidenceKind: shared(func(row CrossRepoConsumerSummary) string { return row.EvidenceKind }),
		Reason:       shared(func(row CrossRepoConsumerSummary) string { return row.Reason }),
		// The request is a property of the call, not of the consumer: every
		// unresolved row that reached this page asked for this symbol in this
		// package. Only the rows of other categories carry none, so the two
		// fields rise as soon as the rows that have them agree -- and a
		// resolution failure spelled the import differently keeps its own.
		RequestedPackage: requested(func(row CrossRepoConsumerSummary) string { return row.RequestedPackage }),
		RequestedSymbol:  requested(func(row CrossRepoConsumerSummary) string { return row.RequestedSymbol }),
	}
	pageHoisted := crossRepoHoisted{
		Category: compact.Category, EdgeKind: compact.EdgeKind, Confidence: compact.Confidence,
		Provenance: compact.Provenance, EvidenceKind: compact.EvidenceKind, Reason: compact.Reason,
		RequestedPackage: compact.RequestedPackage, RequestedSymbol: compact.RequestedSymbol,
	}
	flat := compactCrossRepoConsumerRows(page.Consumers, pageHoisted)
	if compact.Category != "" && compact.EdgeKind != "" && compact.Confidence != "" &&
		compact.Provenance != "" && compact.EvidenceKind != "" && compact.Reason != "" {
		// Every grouping column is already on the page: nothing left to group
		// by, so this is the whole answer, not a candidate.
		compact.Consumers = flat
		return compact
	}

	residual := func(row CrossRepoConsumerSummary) []string {
		return []string{
			blankWhenHoisted(row.Category, compact.Category),
			blankWhenHoisted(row.EdgeKind, compact.EdgeKind),
			blankWhenHoisted(row.Confidence, compact.Confidence),
			blankWhenHoisted(row.Provenance, compact.Provenance),
			blankWhenHoisted(row.EvidenceKind, compact.EvidenceKind),
			blankWhenHoisted(row.Reason, compact.Reason),
		}
	}
	buckets := groupByResidual(page.Consumers, residual)
	if len(buckets) <= 1 {
		compact.Consumers = flat
		return compact
	}

	groups := make([]CompactCrossRepoGroup, 0, len(buckets))
	for _, bucket := range buckets {
		first := bucket[0]
		group := CompactCrossRepoGroup{}
		bucketHoisted := pageHoisted
		if bucketHoisted.Category == "" {
			bucketHoisted.Category = first.Category
			group.Category = first.Category
		}
		if bucketHoisted.EdgeKind == "" {
			bucketHoisted.EdgeKind = first.EdgeKind
			group.EdgeKind = first.EdgeKind
		}
		if bucketHoisted.Confidence == "" {
			bucketHoisted.Confidence = first.Confidence
			group.Confidence = first.Confidence
		}
		if bucketHoisted.Provenance == "" {
			bucketHoisted.Provenance = first.Provenance
			group.Provenance = first.Provenance
		}
		if bucketHoisted.EvidenceKind == "" {
			bucketHoisted.EvidenceKind = first.EvidenceKind
			group.EvidenceKind = first.EvidenceKind
		}
		if bucketHoisted.Reason == "" {
			bucketHoisted.Reason = first.Reason
			group.Reason = first.Reason
		}
		// Detail is not a grouping key -- its cardinality has nothing to do
		// with the tuple above -- but every row of this bucket already shares
		// that tuple, so it costs one more hoistString to check whether they
		// also share the sentence.
		bucketHoisted.Detail = hoistString(len(bucket), func(index int) string { return bucket[index].Detail })
		group.Detail = bucketHoisted.Detail
		group.Consumers = compactCrossRepoConsumerRows(bucket, bucketHoisted)
		groups = append(groups, group)
	}
	// Grouping only wins when a tuple repeats enough to pay for its own
	// header; a page where every consumer disagrees is cheaper flat.
	// Marshaling both candidates costs nothing on a page this small, and it is
	// the only way to guarantee grouping never costs more than not grouping.
	if flatBytes, err := json.Marshal(flat); err == nil {
		if groupedBytes, err := json.Marshal(groups); err == nil && len(groupedBytes) >= len(flatBytes) {
			compact.Consumers = flat
			return compact
		}
	}
	compact.Groups = groups
	return compact
}

// compactCrossRepoConsumerRows writes one page or one group's consumers, merging a
// repository's several package dependencies into the one entry
// crossRepoPackageGroup already keys them by.
func compactCrossRepoConsumerRows(rows []CrossRepoConsumerSummary, hoisted crossRepoHoisted) []CompactCrossRepoConsumer {
	consumers := make([]CompactCrossRepoConsumer, 0, len(rows))
	grouped := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.Category == CrossRepoConsumerPackage {
			key := crossRepoPackageGroup(row)
			if index, found := grouped[key]; found {
				existing := consumers[index].PackageName
				consumers[index].PackageName = crossRepoWithPackage(existing, row.PackageName)
				continue
			}
			grouped[key] = len(consumers)
		}
		consumers = append(consumers, crossRepoConsumerEntry(row, hoisted))
	}
	return consumers
}

// crossRepoConsumerEntry writes one row without the fields already
// accounted for above it, whether on the page or on its group.
func crossRepoConsumerEntry(row CrossRepoConsumerSummary, hoisted crossRepoHoisted) CompactCrossRepoConsumer {
	consumer := CompactCrossRepoConsumer{
		Profiles:              row.Profiles,
		Category:              crossRepoPerRow(hoisted.Category, row.Category),
		Repo:                  row.Repository,
		QualifiedName:         row.QualifiedName,
		Kind:                  row.Kind,
		EdgeKind:              crossRepoPerRow(hoisted.EdgeKind, row.EdgeKind),
		Confidence:            crossRepoPerRow(hoisted.Confidence, row.Confidence),
		Provenance:            crossRepoPerRow(hoisted.Provenance, row.Provenance),
		EvidenceKind:          crossRepoPerRow(hoisted.EvidenceKind, row.EvidenceKind),
		RequestedPackage:      crossRepoPerRow(hoisted.RequestedPackage, row.RequestedPackage),
		RequestedSymbol:       crossRepoPerRow(hoisted.RequestedSymbol, row.RequestedSymbol),
		Reason:                crossRepoPerRow(hoisted.Reason, row.Reason),
		Detail:                crossRepoPerRow(hoisted.Detail, row.Detail),
		Column:                row.StartColumn,
		Offset:                row.StartOffset,
		ConsumerSymbolKey:     row.ConsumerSymbolKey,
		ConsumerRepositoryKey: row.ConsumerRepositoryKey,
		ConsumerPackageKey:    row.ConsumerPackageKey,
		ConsumerFileKey:       row.ConsumerFileKey,
		EvidenceKey:           row.EvidenceKey,
		UnresolvedKey:         row.UnresolvedKey,
	}
	if row.PackageName != "" {
		consumer.PackageName = row.PackageName
	}
	// `name` is the last segment of `qualified_name` on every row that carries
	// both, and the only identity an unresolved row has when it carries no
	// qualified name.
	if row.Name != crossRepoNameTail(row.QualifiedName) {
		consumer.Name = row.Name
	}
	if row.FilePath != "" {
		consumer.At = row.FilePath + ":" + strconv.FormatUint(uint64(row.StartLine), 10)
	}
	if row.EndLine != 0 && row.EndLine != row.StartLine {
		consumer.EndLine = row.EndLine
	}
	return consumer
}

func compactCrossRepoSubject(subject CrossRepoSubject) CompactCrossRepoSubject {
	compact := CompactCrossRepoSubject{
		QualifiedName: subject.QualifiedName,
		At:            locationLabel(subject.Repository, subject.FilePath, subject.StartLine),
		PackageName:   subject.PackageName,
		ModulePath:    subject.ModulePath,
		StableKey:     subject.StableKey,
		RepositoryKey: subject.RepositoryKey,
		PackageKey:    subject.PackageKey,
	}
	if subject.EndLine != subject.StartLine {
		compact.EndLine = subject.EndLine
	}
	return compact
}

// crossRepoPerRow drops a value the header already states. Repeating it is what
// this view exists to remove: over `workspace` those columns were a fifth of a page.
func crossRepoPerRow(hoisted, value string) string {
	if hoisted == value {
		return ""
	}
	return value
}

// crossRepoPackageGroup is a package dependency without its package name. Two
// dependencies that agree on it differ only in which package of the repository
// declared them, and the question asked is about the repository. The evidence
// and package keys are part of it, so a detailed answer -- which returns them
// per package -- never merges two rows into one that could not carry both.
func crossRepoPackageGroup(row CrossRepoConsumerSummary) string {
	return strings.Join([]string{
		string(row.Profiles), row.Repository, row.EdgeKind, row.Confidence, row.Provenance,
		row.ConsumerRepositoryKey, row.ConsumerPackageKey, row.EvidenceKey,
	}, "\x00")
}

// crossRepoWithPackage keeps `pkg` a bare name while the repository has one
// package holding the fact, and a list once it has more.
func crossRepoWithPackage(current any, name string) any {
	switch existing := current.(type) {
	case string:
		if existing == name {
			return existing
		}
		return []string{existing, name}
	case []string:
		for _, value := range existing {
			if value == name {
				return existing
			}
		}
		return append(existing, name)
	default:
		return name
	}
}

// crossRepoNameTail is the last segment of a qualified name, which is what the
// `name` field of a row repeats when it has one.
func crossRepoNameTail(qualifiedName string) string {
	if index := strings.LastIndexAny(qualifiedName, ".:/#"); index >= 0 {
		return qualifiedName[index+1:]
	}
	return qualifiedName
}

// crossRepoGuidance reads an empty answer for what it is. This tool is the one
// with no native competitor, so "nobody outside uses it" is a finding rather
// than a miss -- which is exactly why it may only be said when the graph can
// support it.
func crossRepoGuidance(total, returned int, truncated bool, verdict string) string {
	switch {
	case total == 0 && verdict == VerdictLowerBound:
		return "no repository in the published graph resolves a use of this symbol, but the index recorded places it could not read: read completeness.blind_spots and invisible_scopes before reporting that nothing outside uses it"
	case total == 0:
		return "no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository"
	case truncated:
		return truncatedGuidance(returned, total, "repo or language")
	default:
		return ""
	}
}
