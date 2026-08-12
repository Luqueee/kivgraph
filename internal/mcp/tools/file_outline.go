package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

const (
	DefaultOutlineLimit = 200
	MaximumOutlineLimit = hotsnapshot.MaxExactResults
	fileOutlineToolName = "get_file_outline"
)

// GetFileOutlineInput asks for the declarations under one path.
//
// Path is a repository-relative file, or a directory whose files are all
// wanted. It is the only entry point into the graph that starts from
// something an agent already holds: everything else needs a stable key, and
// the only way to get one is to guess a symbol name.
type GetFileOutlineInput struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Kind       string `json:"kind,omitempty"`
	// IncludeMembers adds struct fields, properties and enum members. They
	// are off by default because they are not declarations a reader chooses
	// between: they are the shape of the type above them, and on a real file
	// they are half the payload.
	IncludeMembers bool   `json:"include_members,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
}

// outlineMemberKinds are the symbol kinds that belong to the declaration
// above them rather than standing on their own.
var outlineMemberKinds = map[string]struct{}{
	"field":       {},
	"property":    {},
	"enum_member": {},
	"variant":     {},
}

// FileOutline is the skeleton of everything declared under a path: what is
// there, of what kind, with what signature and on which lines. It is what a
// reader needs before deciding which body to open.
type FileOutline struct {
	Repository string          `json:"repository"`
	Path       string          `json:"path"`
	Files      int             `json:"files"`
	Symbols    []OutlineSymbol `json:"symbols"`
	Packages   []string        `json:"packages,omitempty"`
	Languages  []string        `json:"languages,omitempty"`
}

// OutlineSymbol is one declaration.
//
// FilePath is set only when the outline spans more than one file. Repeating
// the path the caller just asked for, on every row, is the largest single
// waste a one-file outline can carry.
//
// QualifiedName is set only when it differs from Name, which for a top-level
// declaration in most languages it does not.
type OutlineSymbol struct {
	StableKey string `json:"stable_key"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	Exported  bool   `json:"exported"`
	StartLine uint32 `json:"start_line"`
	EndLine   uint32 `json:"end_line"`

	QualifiedName     string `json:"qualified_name,omitempty"`
	FilePath          string `json:"file_path,omitempty"`
	CanonicalIdentity string `json:"canonical_identity,omitempty"`
}

type fileOutlineQuery struct {
	Tool           string `json:"tool"`
	Repository     string `json:"repository"`
	Path           string `json:"path"`
	Kind           string `json:"kind,omitempty"`
	IncludeMembers bool   `json:"include_members,omitempty"`
}

// RegisterGetFileOutline adds the read-only outline tool without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterGetFileOutline(server *sdkmcp.Server) {
	RegisterGetFileOutlineWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetFileOutlineWithSnapshotStore registers get_file_outline over the
// immutable snapshot currently published by snapshotStore.
func RegisterGetFileOutlineWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetFileOutlineWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetFileOutlineWithObserverAndSnapshotStore registers
// get_file_outline over a snapshot store and optionally observes latency.
func RegisterGetFileOutlineWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetFileOutlineInput,
	) (*sdkmcp.CallToolResult, Response[FileOutline], error) {
		return getFileOutline(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetFileOutlineInput,
		) (*sdkmcp.CallToolResult, Response[FileOutline], error) {
			start := time.Now()
			result, outline, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, fileOutlineToolName, start, outline, err)
			return result, outline, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: fileOutlineToolName,
		Description: "Lists the declarations of one file or of a directory, with their kind, signature and line range. " +
			"Use it to read the shape of code without opening it, and to get the stable keys the other tools need.",
		OutputSchema: ConciseOutputSchema(),
		Annotations:  &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func getFileOutline(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetFileOutlineInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[FileOutline], error) {
	repositoryName, err := normalizeOutlineArgument(arguments.Repository, "repository")
	if err != nil {
		return nil, Response[FileOutline]{}, err
	}
	path, err := normalizeOutlinePath(arguments.Path)
	if err != nil {
		return nil, Response[FileOutline]{}, err
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[FileOutline]{}, err
	}
	limit, err := normalizeOutlineLimit(arguments.Limit)
	if err != nil {
		return nil, Response[FileOutline]{}, err
	}
	queryHash, err := HashQuery(fileOutlineQuery{
		Tool: fileOutlineToolName, Repository: repositoryName, Path: path,
		Kind: arguments.Kind, IncludeMembers: arguments.IncludeMembers,
	})
	if err != nil {
		return nil, Response[FileOutline]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[FileOutline]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[FileOutline]{}, ErrIndexNotReady()
	}
	metadata := snapshot.Metadata()

	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[FileOutline]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionStableKeyV1); err != nil {
			return nil, Response[FileOutline]{}, err
		}
		offset = cursor.Offset
	}

	repositoryID, found := snapshot.RepositoryByName(repositoryName)
	if !found {
		return nil, Response[FileOutline]{}, NewToolError(CodeRepositoryNotFound, fmt.Sprintf(
			"repository %q is not in the published graph", repositoryName,
		))
	}
	// A path the graph does not know is an error naming both halves, never an
	// empty page: an empty page reads as "nothing is declared here", which is
	// a different and much more misleading answer.
	files := snapshot.FilesUnder(repositoryID, path)
	if len(files) == 0 {
		return nil, Response[FileOutline]{}, NewToolError(CodeSymbolNotFound, fmt.Sprintf(
			"repository %q has no indexed file at or under %q", repositoryName, path,
		))
	}

	page, err := snapshot.SearchSymbolsInFiles(files, offset, limit)
	if err != nil {
		return nil, Response[FileOutline]{}, WrapToolError(CodeInvalidArgument, "outline pagination is invalid", err)
	}

	outline := FileOutline{Repository: repositoryName, Path: path, Files: len(files)}
	packages := make(map[string]struct{})
	languages := make(map[string]struct{})
	kept := 0
	for _, id := range page.IDs {
		symbol, found := snapshot.Symbol(id)
		if !found {
			return nil, Response[FileOutline]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot symbol index is inconsistent",
				fmt.Errorf("symbol index %d is missing", id),
			)
		}
		row, location, err := outlineSymbol(snapshot, symbol, format, len(files) > 1)
		if err != nil {
			return nil, Response[FileOutline]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot contains invalid symbol metadata",
				err,
			)
		}
		if arguments.Kind != "" && row.Kind != arguments.Kind {
			continue
		}
		if _, member := outlineMemberKinds[row.Kind]; member && !arguments.IncludeMembers {
			continue
		}
		packages[location.PackageName] = struct{}{}
		outline.Symbols = append(outline.Symbols, row)
		kept++
	}
	if outline.Symbols == nil {
		outline.Symbols = []OutlineSymbol{}
	}
	for _, file := range files {
		record, found := snapshot.File(file)
		if !found {
			continue
		}
		if language, ok := snapshot.Strings().String(record.Language); ok && language != "" {
			languages[language] = struct{}{}
		}
	}
	outline.Packages = sortedKeys(packages)
	outline.Languages = sortedKeys(languages)

	var nextCursor *string
	if page.HasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, page.Offset+len(page.IDs), SortingVersionStableKeyV1)
		if err != nil {
			return nil, Response[FileOutline]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[FileOutline]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[FileOutline]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         page.Total,
		Returned:      kept,
		Truncated:     page.HasMore,
		NextCursor:    nextCursor,
		Coverage:      Coverage{Exact: kept},
		Results:       outline,
	}, nil
}

func outlineSymbol(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	format string,
	withPath bool,
) (OutlineSymbol, symbolLocation, error) {
	table := snapshot.Strings()
	canonical, canonicalOK := table.String(symbol.CanonicalIdentity)
	name, nameOK := table.String(symbol.Name)
	qualifiedName, qualifiedNameOK := table.String(symbol.QualifiedName)
	kind, kindOK := table.String(symbol.Kind)
	signature, signatureOK := table.String(symbol.Signature)
	if !canonicalOK || !nameOK || !qualifiedNameOK || !kindOK || !signatureOK {
		return OutlineSymbol{}, symbolLocation{}, fmt.Errorf(
			"symbol %q references invalid strings (canonical_ok=%t name_ok=%t qualified_name_ok=%t kind_ok=%t signature_ok=%t)",
			symbol.StableKey, canonicalOK, nameOK, qualifiedNameOK, kindOK, signatureOK,
		)
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return OutlineSymbol{}, symbolLocation{}, err
	}
	row := OutlineSymbol{
		StableKey: string(symbol.StableKey),
		Name:      name,
		Kind:      kind,
		Signature: signature,
		Exported:  symbol.Exported,
		StartLine: symbol.StartLine,
		EndLine:   symbol.EndLine,
	}
	if qualifiedName != name {
		row.QualifiedName = qualifiedName
	}
	if withPath {
		row.FilePath = location.FilePath
	}
	if format == ResponseFormatDetailed {
		row.CanonicalIdentity = canonical
	}
	return row, location, nil
}

func normalizeOutlineArgument(value, field string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"%s must be a non-empty value without surrounding whitespace", field,
		))
	}
	return value, nil
}

// normalizeOutlinePath rejects what the snapshot cannot hold: paths are stored
// repository-relative and forward-slashed, so an absolute path or a traversal
// is a mistake worth naming rather than a query that quietly finds nothing.
func normalizeOutlinePath(value string) (string, error) {
	path, err := normalizeOutlineArgument(value, "path")
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(path, "/") {
		return "", NewToolError(CodeInvalidArgument, "path must be repository-relative, not absolute")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", NewToolError(CodeInvalidArgument, "path must not contain a .. segment")
		}
	}
	return strings.TrimSuffix(path, "/"), nil
}

// sortedKeys returns the set's members in a stable order: two calls on the
// same snapshot must produce byte-identical responses, and map iteration
// does not.
func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func normalizeOutlineLimit(value int) (int, error) {
	if value == 0 {
		return DefaultOutlineLimit, nil
	}
	if value < 1 || value > MaximumOutlineLimit {
		return 0, NewToolError(CodeInvalidArgument, fmt.Sprintf("limit must be between 1 and %d", MaximumOutlineLimit))
	}
	return value, nil
}
