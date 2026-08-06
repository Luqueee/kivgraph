package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// RepositorySummary is the stable public shape for a registered repository.
type RepositorySummary struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
}

// ListRepositoriesInput contains the optional page controls for
// list_repositories.
type ListRepositoriesInput struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
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
		return listRepositories(ctx, request, arguments, snapshotStore)
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
			observe(observer, callObserver, repositoryQueryToolName, start, repositories, err)
			return result, repositories, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        repositoryQueryToolName,
		Description: "Lists repositories registered with Ladygraph.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
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
		return nil, Response[[]RepositorySummary]{}, NewToolError(CodeIndexNotReady, "no HotSnapshot is published")
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

func repositorySummary(snapshot *hotsnapshot.GraphSnapshot, record hotsnapshot.RepositoryRecord) (RepositorySummary, error) {
	table := snapshot.Strings()
	key, keyOK := table.String(record.Key)
	name, nameOK := table.String(record.Name)
	path, pathOK := table.String(record.Path)
	languages, languagesOK := table.String(record.Languages)
	if !keyOK || !nameOK || !pathOK || !languagesOK {
		return RepositorySummary{}, fmt.Errorf(
			"repository metadata references invalid strings (key=%q key_ok=%t name_ok=%t path_ok=%t languages_ok=%t)",
			key, keyOK, nameOK, pathOK, languagesOK,
		)
	}
	return RepositorySummary{
		Name:      name,
		Path:      path,
		Languages: splitRepositoryLanguages(languages),
	}, nil
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
