package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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
//
// Symbols are grouped by file rather than carrying a path each. On a directory
// outline the path was the second largest field on every row, and it is the one
// piece of a row that a whole group of rows shares.
type FileOutline struct {
	Repository string        `json:"repository"`
	Path       string        `json:"path"`
	Packages   []string      `json:"packages,omitempty"`
	Languages  []string      `json:"languages,omitempty"`
	Files      []OutlineFile `json:"files"`
}

// OutlineFile is one file and the declarations this page carries for it.
type OutlineFile struct {
	Path    string          `json:"path"`
	Symbols []OutlineSymbol `json:"symbols"`
}

// OutlineSymbol is one declaration.
//
// QualifiedName is set only when it differs from Name, which for a top-level
// declaration in most languages it does not.
//
// The stable key is not here. An outline of a 155-declaration file spent half
// its tokens on base32 keys, and every row already names the symbol well enough
// to address it: the repository, the file of its group and its qualified name
// are what `get_symbol` and `find_references` now accept. The key returns under
// `response_format: "detailed"`, together with the fully qualified signature.
type OutlineSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	Exported  bool   `json:"exported"`
	StartLine uint32 `json:"start_line"`
	EndLine   uint32 `json:"end_line"`

	QualifiedName     string `json:"qualified_name,omitempty"`
	StableKey         string `json:"stable_key,omitempty"`
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
	addQueryTool(server, &sdkmcp.Tool{
		Name:        fileOutlineToolName,
		Description: "Declarations under a path, grouped by file, with kind, signature and range. Use it for a package; one small file is cheaper to read.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
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

	outline := FileOutline{Repository: repositoryName, Path: path, Files: []OutlineFile{}}
	packages := make(map[string]struct{})
	languages := make(map[string]struct{})
	// Groups follow the order the page first mentions each file, so two calls
	// over the same page produce byte-identical responses.
	groups := make(map[string]int, len(files))
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
		row, location, err := outlineSymbol(snapshot, symbol, format)
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
		index, exists := groups[location.FilePath]
		if !exists {
			index = len(outline.Files)
			groups[location.FilePath] = index
			outline.Files = append(outline.Files, OutlineFile{Path: location.FilePath})
		}
		outline.Files[index].Symbols = append(outline.Files[index].Symbols, row)
		kept++
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
		Name:      name,
		Kind:      kind,
		Signature: localSignature(signature, location.PackageName),
		Exported:  symbol.Exported,
		StartLine: symbol.StartLine,
		EndLine:   symbol.EndLine,
	}
	if qualifiedName != name {
		row.QualifiedName = qualifiedName
	}
	if format == ResponseFormatDetailed {
		row.StableKey = string(symbol.StableKey)
		row.Signature = signature
		row.CanonicalIdentity = canonical
	}
	return row, location, nil
}

// localSignature drops the package path from the types a symbol's own package
// declares, which is how the source that declares it reads.
//
// `func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) ...Set` inside
// `internal/facts` spends most of its tokens spelling out where the reader
// already is. Types from elsewhere keep their path, because there the package
// is the information. The full signature returns under `detailed`.
func localSignature(signature, packageName string) string {
	if signature == "" || packageName == "" {
		return signature
	}
	return strings.ReplaceAll(signature, packageName+".", "")
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
