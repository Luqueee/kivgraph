package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/watcher"
)

// RepositorySummary is the stable public shape for a registered repository.
//
// The Indexed* fields describe the working tree the graph was built from and
// the Current* fields the tree on disk right now. Moved reports that the two
// disagree, which is the only warning a caller gets that a path or a line
// this server returns may no longer exist.
type RepositorySummary struct {
	Profile   string   `json:"profile,omitempty"`
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`

	IndexedCommit string `json:"indexed_commit,omitempty"`
	IndexedBranch string `json:"indexed_branch,omitempty"`
	IndexedDirty  bool   `json:"indexed_dirty,omitempty"`
	CurrentCommit string `json:"current_commit,omitempty"`
	CurrentBranch string `json:"current_branch,omitempty"`
	Moved         bool   `json:"moved"`
	MovedDetail   string `json:"moved_detail,omitempty"`
	// Derived marks a provider Kivgraph built from the machine rather than
	// from the registry: the standard library of the toolchain that indexed
	// this graph, whose release is in its name. Nothing checks it out and
	// nothing can move it.
	Derived bool `json:"derived,omitempty"`
}

// ListRepositoriesInput contains the optional page controls for
// list_repositories.
type ListRepositoriesInput struct {
	Profile []string `json:"profile,omitempty" jsonschema:"Profiles to list; omit or use * alone for all."`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"The next_cursor of the previous page. Every other argument must stay the same."`
	Limit   int      `json:"limit,omitempty" jsonschema:"Repositories in one page. Defaults to 50, maximum 500."`
}

const (
	DefaultRepositoryLimit  = 50
	MaximumRepositoryLimit  = 500
	repositoryQueryToolName = "list_repositories"
)

type listRepositoriesQuery struct {
	Tool string `json:"tool"`
}

// RegisterListRepositories adds the read-only repository listing tool.
func RegisterListRepositories(server *sdkmcp.Server) {
	RegisterListRepositoriesWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterListRepositoriesWithObserver adds list_repositories and optionally
// observes handler latency.
func RegisterListRepositoriesWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterListRepositoriesWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterListRepositoriesWithSnapshotStore registers list_repositories over
// the immutable snapshot currently published by snapshotStore.
func RegisterListRepositoriesWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterListRepositoriesWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterListRepositoriesWithObserverAndSnapshotStore registers
// list_repositories over a snapshot store and optionally observes latency.
func RegisterListRepositoriesWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments ListRepositoriesInput,
	) (*sdkmcp.CallToolResult, Response[[]RepositorySummary], error) {
		if snapshotStore == nil {
			return listRepositories(ctx, request, arguments, nil)
		}
		requested := arguments.Profile
		if len(requested) == 0 && snapshotStore.ProfileCount() > 1 {
			requested = []string{"*"}
		}
		selected, selectionErr := snapshotStore.ResolveProfiles(requested)
		if selectionErr != nil {
			return nil, Response[[]RepositorySummary]{}, WrapToolError(CodeInvalidArgument, selectionErr.Error(), selectionErr)
		}
		if len(selected) > 1 {
			return listRepositoriesAcrossProfiles(ctx, request, arguments, selected)
		}
		store, profile, count := selected[0].Store, selected[0].Name, snapshotStore.ProfileCount()
		result, response, err := listRepositories(ctx, request, arguments, store)
		scopeResponse(&response, profile, count)
		return result, response, err
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments ListRepositoriesInput,
		) (*sdkmcp.CallToolResult, Response[[]RepositorySummary], error) {
			start := time.Now()
			result, repositories, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, repositoryQueryToolName, request, start, repositories, err)
			return result, repositories, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        repositoryQueryToolName,
		Description: "The repositories the published graph covers, with the commit each was indexed at and which one is the derived provider.",
		Annotations: readOnlyClosedWorld(),
	}, handler)
}

func listRepositoriesAcrossProfiles(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	arguments ListRepositoriesInput,
	selected []hotsnapshot.ProfileStore,
) (*sdkmcp.CallToolResult, Response[[]RepositorySummary], error) {
	limit, err := normalizeRepositoryLimit(arguments.Limit)
	if err != nil {
		return nil, Response[[]RepositorySummary]{}, err
	}
	names := make([]string, 0, len(selected))
	profileSnapshots := make([]ProfileSnapshot, 0, len(selected))
	rows := make([]RepositorySummary, 0)
	skipped := make([]string, 0)
	for _, profile := range selected {
		names = append(names, profile.Name)
		snapshot := profile.Store.Load()
		if snapshot == nil {
			profileSnapshots = append(profileSnapshots, ProfileSnapshot{Name: profile.Name})
			skipped = append(skipped, profile.Name)
			continue
		}
		profileSnapshots = append(profileSnapshots, ProfileSnapshot{Name: profile.Name, SnapshotID: snapshot.Metadata().ID})
		pageArguments := arguments
		pageArguments.Profile = nil
		pageArguments.Cursor = ""
		pageArguments.Limit = MaximumRepositoryLimit
		for {
			_, response, err := listRepositories(ctx, request, pageArguments, profile.Store)
			if err != nil {
				return nil, Response[[]RepositorySummary]{}, err
			}
			for _, row := range response.Results {
				row.Profile = profile.Name
				rows = append(rows, row)
			}
			if response.NextCursor == nil {
				break
			}
			pageArguments.Cursor = *response.NextCursor
		}
	}
	queryHash, err := HashQuery(struct {
		Tool     string   `json:"tool"`
		Profiles []string `json:"profiles"`
	}{repositoryQueryToolName, names})
	if err != nil {
		return nil, Response[[]RepositorySummary]{}, err
	}
	setID := ProfileSetSnapshotID(profileSnapshots)
	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		if err := cursor.ValidateAgainst(setID, queryHash, SortingVersionStableKeyV1); err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		offset = cursor.Offset
	}
	if offset > len(rows) {
		return nil, Response[[]RepositorySummary]{}, NewToolError(CodeCursorInvalid, "cursor offset is beyond the merged result")
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	var nextCursor *string
	if end < len(rows) {
		cursor, err := NewCursor(setID, queryHash, end, SortingVersionStableKeyV1)
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		nextCursor = &encoded
	}
	response := Response[[]RepositorySummary]{
		Profiles:          profileSnapshots,
		CrossProfileEdges: "not_resolved",
		Total:             len(rows),
		Returned:          end - offset,
		Truncated:         end < len(rows),
		NextCursor:        nextCursor,
		Results:           rows[offset:end],
	}
	if len(skipped) > 0 {
		response.Guidance = strings.TrimSpace(response.Guidance + " Profiles without a published generation were skipped: " + strings.Join(skipped, ", ") + ".")
	}
	return nil, response, nil
}

func listRepositories(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments ListRepositoriesInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[[]RepositorySummary], error) {
	limit, err := normalizeRepositoryLimit(arguments.Limit)
	if err != nil {
		return nil, Response[[]RepositorySummary]{}, err
	}
	queryHash, err := HashQuery(listRepositoriesQuery{Tool: repositoryQueryToolName})
	if err != nil {
		return nil, Response[[]RepositorySummary]{}, err
	}

	// The legacy registration used by the empty server has no graph source.
	// Preserve its stable empty response; a configured store with no active
	// snapshot is different and must be reported as not ready.
	if snapshotStore == nil {
		return nil, emptyRepositoryResponse(), nil
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[[]RepositorySummary]{}, ErrIndexNotReady()
	}
	metadata := snapshot.Metadata()

	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionStableKeyV1); err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		offset = cursor.Offset
	}

	total := int(metadata.Counts.Repositories)
	if offset > total {
		offset = total
	}
	end := total
	if limit < total-offset {
		end = offset + limit
	}
	results := make([]RepositorySummary, 0, end-offset)
	for index := offset; index < end; index++ {
		record, found := snapshot.Repository(hotsnapshot.RepositoryID(index))
		if !found {
			return nil, Response[[]RepositorySummary]{},
				WrapToolError(CodeSnapshotUnavailable, "active snapshot repository index is inconsistent", fmt.Errorf("repository index %d is missing", index))
		}
		summary, err := repositorySummary(snapshot, record)
		if err != nil {
			return nil, Response[[]RepositorySummary]{},
				WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid repository metadata", err)
		}
		results = append(results, summary)
	}

	var nextCursor *string
	if end < total {
		cursor, err := NewCursor(metadata.ID, queryHash, end, SortingVersionStableKeyV1)
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[[]RepositorySummary]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[[]RepositorySummary]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         total,
		Returned:      len(results),
		Truncated:     nextCursor != nil,
		NextCursor:    nextCursor,
		Coverage:      Coverage{},
		Results:       results,
	}, nil
}

func normalizeRepositoryLimit(value int) (int, error) {
	if value == 0 {
		return DefaultRepositoryLimit, nil
	}
	if value < 1 || value > MaximumRepositoryLimit {
		return 0, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumRepositoryLimit))
	}
	return value, nil
}

// repositorySummary describes one repository of the snapshot, including
// whether its working tree still holds the commit the graph was built from.
//
// Answering that costs two small file reads per repository and is done
// inline: a caller that has to make a second call to learn the first one is
// stale will not make it, and an answer about code that is no longer there
// costs far more than the reads.
func repositorySummary(snapshot *hotsnapshot.GraphSnapshot, record hotsnapshot.RepositoryRecord) (RepositorySummary, error) {
	table := snapshot.Strings()
	key, keyOK := table.String(record.Key)
	name, nameOK := table.String(record.Name)
	path, pathOK := table.String(record.Path)
	languages, languagesOK := table.String(record.Languages)
	commit, commitOK := table.String(record.Commit)
	branch, branchOK := table.String(record.Branch)
	if !keyOK || !nameOK || !pathOK || !languagesOK || !commitOK || !branchOK {
		return RepositorySummary{}, fmt.Errorf(
			"repository metadata references invalid strings (key=%q key_ok=%t name_ok=%t path_ok=%t languages_ok=%t commit_ok=%t branch_ok=%t)",
			key, keyOK, nameOK, pathOK, languagesOK, commitOK, branchOK,
		)
	}
	summary := RepositorySummary{
		Name:          name,
		Path:          path,
		Languages:     splitRepositoryLanguages(languages),
		IndexedCommit: commit,
		IndexedBranch: branch,
		IndexedDirty:  record.Dirty,
	}
	if facts.IsSyntheticRepository(name) {
		// A derived provider has no working tree to compare against: it is the
		// standard library of the toolchain that indexed this graph, and the
		// release is in its name. Reporting it as a repository whose commit
		// could not be read would describe a freshness problem that does not
		// exist.
		summary.Derived = true
		summary.MovedDetail = "derived from the toolchain, not a registered repository: it has no commit to compare"
		return summary, nil
	}
	describeRepositoryMovement(&summary)
	return summary, nil
}

// describeRepositoryMovement compares the commit the graph was built from with
// the one the working tree holds now and fills the freshness fields of
// summary.
//
// A HEAD that cannot be read is never reported as agreement: the current
// fields stay empty, Moved stays false and MovedDetail carries the reason. An
// unknown answer must not read as a good one.
//
// Movement is decided by the commit alone. A branch renamed or recreated over
// the same commit leaves every path and every line exactly where the graph
// says they are, and reporting that as a move would train a caller to ignore
// the field.
func describeRepositoryMovement(summary *RepositorySummary) {
	if summary.IndexedCommit == "" {
		summary.MovedDetail = "the graph does not record the commit this repository was indexed at, so it cannot be compared with the working tree"
		return
	}
	head, err := watcher.ReadGitHead(summary.Path)
	if err != nil {
		summary.MovedDetail = fmt.Sprintf("the working tree could not be compared with the graph: %v", err)
		return
	}
	summary.CurrentCommit = head.Commit
	summary.CurrentBranch = head.Branch
	if head.Commit == summary.IndexedCommit {
		return
	}
	summary.Moved = true
	summary.MovedDetail = fmt.Sprintf(
		"indexed at commit %s on %s, the tree is now at %s on %s",
		shortCommit(summary.IndexedCommit), repositoryBranchName(summary.IndexedBranch),
		shortCommit(head.Commit), repositoryBranchName(head.Branch),
	)
}

// shortCommit abbreviates an object id for prose. The fields keep the full id;
// a sentence naming two of them in full is unreadable.
func shortCommit(commit string) string {
	const shortCommitLength = 7
	if len(commit) <= shortCommitLength {
		return commit
	}
	return commit[:shortCommitLength]
}

// repositoryBranchName names the position for prose. An empty branch is a
// detached HEAD, both when it was indexed and when it is read now.
func repositoryBranchName(branch string) string {
	if branch == "" {
		return "a detached HEAD"
	}
	return branch
}

func splitRepositoryLanguages(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	languages := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			languages = append(languages, part)
		}
	}
	return languages
}

func snapshotAgeMilliseconds(createdAt time.Time) int64 {
	age := time.Since(createdAt).Milliseconds()
	if age < 0 {
		return 0
	}
	return age
}

func emptyRepositoryResponse() Response[[]RepositorySummary] {
	return Response[[]RepositorySummary]{
		Total:    0,
		Returned: 0,
		Results:  []RepositorySummary{},
	}
}
