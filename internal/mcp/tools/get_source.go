package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	getSourceToolName = "get_source"

	// DefaultSourceContextLines is zero because the graph knows where a
	// declaration begins and ends. Context is for what surrounds it, which the
	// caller asks for when it wants it.
	DefaultSourceContextLines = 0
	// MaximumSourceContextLines bounds the surrounding lines a caller can ask
	// for. Above this a caller wants the file, and the host reads files.
	MaximumSourceContextLines = 100
	// MaximumSourceSymbols bounds how many bodies one call assembles.
	MaximumSourceSymbols = 20
	// MaximumSourceBytes is the ceiling on the bytes one response carries. It is
	// declared when it trims: a response that quietly stops halfway is worse
	// than one that says it stopped.
	MaximumSourceBytes = 262_144
)

// GetSourceInput asks for the bodies of one or more symbols.
//
// Every symbol is named the way the rest of this surface returns them: a stable
// key, or the repository, path and qualified name of a row the caller already
// read.
type GetSourceInput struct {
	Profile        []string        `json:"profile,omitempty" jsonschema:"Profiles; default if omitted."`
	Symbols        []SourceRequest `json:"symbols" jsonschema:"Up to 20 selectors."`
	ContextLines   int             `json:"context_lines,omitempty" jsonschema:"Full body included. Omit unless you need surrounding lines (0–100)."`
	ResponseFormat string          `json:"response_format,omitempty" jsonschema:"concise or detailed (with keys)."`
}

// SourceRequest names one symbol.
type SourceRequest struct {
	StableKey     string  `json:"stable_key,omitempty" jsonschema:"Durable symbol key."`
	QualifiedName string  `json:"qualified_name,omitempty" jsonschema:"Qualified declaration name."`
	Repository    string  `json:"repository,omitempty" jsonschema:"Declaring repository."`
	Path          string  `json:"path,omitempty" jsonschema:"Repository-relative file."`
	Line          *uint32 `json:"line,omitempty" jsonschema:"Reference line; requires snapshot_id from the same response."`
	SnapshotID    *uint64 `json:"snapshot_id,omitempty" jsonschema:"With line: copy the reference snapshot_id."`
}

// SourceBody is the code of one symbol, or the reason there is none.
//
// Freshness travels with the bytes. When the file still hashes to what the
// generation recorded, Fresh is true and the range is the graph's. When it does
// not, the file is the authority -- it is what the agent will edit -- so the
// declaration is re-anchored by name, Shifted says by how many lines, and the
// caller can see that the graph's own numbers moved. See ADR 0040.
type SourceBody struct {
	Profiles       ProfileNames `json:"profile,omitempty"`
	Repository     string       `json:"repository"`
	Path           string       `json:"path"`
	QualifiedName  string       `json:"qualified_name"`
	Kind           string       `json:"kind"`
	StartLine      uint32       `json:"start_line"`
	EndLine        uint32       `json:"end_line"`
	ShownStartLine uint32       `json:"shown_start_line,omitempty"`
	ShownEndLine   uint32       `json:"shown_end_line,omitempty"`
	Fresh          bool         `json:"fresh"`
	Code           string       `json:"code,omitempty"`

	// Shifted is the signed line offset between the range the generation
	// recorded and the range served. It is absent when nothing moved.
	Shifted int `json:"shifted,omitempty"`
	// Unavailable says why this row carries no code. One stale file does not
	// invalidate the other rows of the same answer.
	Unavailable string `json:"unavailable,omitempty"`

	StableKey string `json:"stable_key,omitempty"`
}

// SourceResult is the assembled answer.
type SourceResult struct {
	ContextLines int          `json:"context_lines"`
	Bodies       []SourceBody `json:"bodies"`
	// Trimmed is set when the byte ceiling stopped the response short, naming
	// how many bodies were dropped.
	Trimmed int `json:"trimmed,omitempty"`
}

// RegisterGetSource adds the read-only source tool without a graph source.
func RegisterGetSource(server *sdkmcp.Server) {
	RegisterGetSourceWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetSourceWithSnapshotStore registers get_source over the immutable
// snapshot currently published by snapshotStore.
func RegisterGetSourceWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetSourceWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetSourceWithObserverAndSnapshotStore registers get_source over a
// snapshot store and optionally observes latency.
func RegisterGetSourceWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetSourceInput,
	) (*sdkmcp.CallToolResult, Response[SourceResult], error) {
		stableKey := ""
		for _, symbol := range arguments.Symbols {
			if symbol.StableKey != "" {
				stableKey = symbol.StableKey
				break
			}
		}
		selected, count, err := resolveProfileSelection(snapshotStore, arguments.Profile, stableKey)
		if err != nil {
			return nil, Response[SourceResult]{}, err
		}
		if count > 1 && (len(arguments.Profile) != 1 || arguments.Profile[0] == "*") {
			for _, symbol := range arguments.Symbols {
				if symbol.Line != nil || symbol.SnapshotID != nil {
					return nil, Response[SourceResult]{}, NewToolError(CodeInvalidArgument, "a source position requires exactly one named profile")
				}
			}
		}
		if len(selected) > 1 {
			return getSourceAcrossProfiles(ctx, request, arguments, selected)
		}
		store, profile := selected[0].Store, selected[0].Name
		result, response, err := getSource(ctx, request, arguments, store)
		scopeResponse(&response, profile, count)
		return result, response, err
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetSourceInput,
		) (*sdkmcp.CallToolResult, Response[SourceResult], error) {
			start := time.Now()
			result, bodies, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, getSourceToolName, request, start, bodies, err)
			return result, bodies, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        getSourceToolName,
		Description: "Read symbol bodies or enclosing functions at reference lines, in one batch.",
		Annotations: readOnlyClosedWorld(),
		Meta:        alwaysLoadMeta(),
	}, handler)
}

func getSourceAcrossProfiles(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	arguments GetSourceInput,
	selected []hotsnapshot.ProfileStore,
) (*sdkmcp.CallToolResult, Response[SourceResult], error) {
	selectors, contextLines, format, err := normalizeGetSourceInput(arguments)
	if err != nil {
		return nil, Response[SourceResult]{}, err
	}
	profiles := make([]ProfileSnapshot, 0, len(selected))
	rows := make([]SourceBody, 0, len(arguments.Symbols)*len(selected))
	variants := make(map[string]int)
	coverage := Coverage{}
	// A symbol one profile does not hold is expected: profiles index different
	// repositories. One that no profile holds is the caller's mistake, and
	// leaving it out of the answer would read as a body that was served.
	missing := make([]error, len(selectors))
	resolved := make([]bool, len(selectors))
	for _, profile := range selected {
		snapshot := profile.Store.Load()
		if snapshot == nil {
			return nil, Response[SourceResult]{}, ErrIndexNotReady()
		}
		profiles = append(profiles, ProfileSnapshot{Name: profile.Name, SnapshotID: snapshot.Metadata().ID})
		cache := map[string]*sourceFile{}
		for index, selector := range selectors {
			symbolID, resolveErr := resolveSymbolSelector(snapshot, selector)
			if resolveErr != nil {
				if code := ErrorCode(resolveErr); code == CodeSymbolNotFound || code == CodeRepositoryNotFound {
					missing[index] = resolveErr
					continue
				}
				return nil, Response[SourceResult]{}, resolveErr
			}
			resolved[index] = true
			symbol, found := snapshot.Symbol(symbolID)
			if !found {
				return nil, Response[SourceResult]{}, WrapToolError(CodeSnapshotUnavailable,
					"active snapshot symbol index is inconsistent", fmt.Errorf("symbol index %d is missing", symbolID))
			}
			row, bodyErr := sourceBody(snapshot, symbol, contextLines, ResponseFormatDetailed, cache)
			if bodyErr != nil {
				return nil, Response[SourceResult]{}, bodyErr
			}
			stableKey := row.StableKey
			row.Profiles = ""
			if format != ResponseFormatDetailed {
				row.StableKey = ""
			}
			payload, marshalErr := json.Marshal(row)
			if marshalErr != nil {
				return nil, Response[SourceResult]{}, WrapToolError(CodeSnapshotUnavailable, "encode source payload for profile merge", marshalErr)
			}
			key := stableKey + "\x00" + string(payload)
			if position, found := variants[key]; found {
				rows[position].Profiles = rows[position].Profiles.append(profile.Name)
				continue
			}
			row.Profiles = profileNames(profile.Name)
			variants[key] = len(rows)
			rows = append(rows, row)
			if row.Unavailable == "" {
				coverage.Exact++
			}
		}
	}
	for index, found := range resolved {
		if !found && missing[index] != nil {
			return nil, Response[SourceResult]{}, missing[index]
		}
	}
	budget := MaximumSourceBytes
	kept := 0
	for kept < len(rows) && len(rows[kept].Code) <= budget {
		budget -= len(rows[kept].Code)
		kept++
	}
	trimmed := len(rows) - kept
	rows = rows[:kept]
	coverage.Exact = 0
	for _, row := range rows {
		if row.Unavailable == "" {
			coverage.Exact++
		}
	}
	result := SourceResult{ContextLines: contextLines, Bodies: rows, Trimmed: trimmed}
	rendered := renderSourceProfiles(profiles, result)
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: rendered}}}, Response[SourceResult]{
		Profiles: profiles, CrossProfileEdges: "not_resolved",
		Total: len(rows) + trimmed, Returned: len(rows), Truncated: trimmed > 0,
		Coverage: coverage, Results: result,
	}, nil
}

func getSource(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetSourceInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[SourceResult], error) {
	selectors, contextLines, format, err := normalizeGetSourceInput(arguments)
	if err != nil {
		return nil, Response[SourceResult]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[SourceResult]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[SourceResult]{}, ErrIndexNotReady()
	}

	result := SourceResult{ContextLines: contextLines, Bodies: make([]SourceBody, 0, len(selectors))}
	coverage := Coverage{}
	budget := MaximumSourceBytes
	// Files are read once even when several requested symbols share one, which
	// on a reference answer is the common case.
	cache := map[string]*sourceFile{}
	var positions positionFiles
	for index, selector := range selectors {
		var symbolID hotsnapshot.SymbolID
		var resolveErr error
		if position := arguments.Symbols[index]; position.Line != nil {
			symbolID, resolveErr = positions.resolve(ctx, snapshot, position, selector.Path, cache)
		} else {
			symbolID, resolveErr = resolveSymbolSelector(snapshot, selector)
		}
		if resolveErr != nil {
			return nil, Response[SourceResult]{}, resolveErr
		}
		symbol, found := snapshot.Symbol(symbolID)
		if !found {
			return nil, Response[SourceResult]{}, WrapToolError(
				CodeSnapshotUnavailable,
				"active snapshot symbol index is inconsistent",
				fmt.Errorf("symbol index %d is missing", symbolID),
			)
		}
		body, err := sourceBody(snapshot, symbol, contextLines, format, cache)
		if err != nil {
			return nil, Response[SourceResult]{}, err
		}
		if len(body.Code) > budget {
			result.Trimmed = len(selectors) - index
			break
		}
		budget -= len(body.Code)
		if body.Unavailable == "" {
			coverage.Exact++
		}
		result.Bodies = append(result.Bodies, body)
	}

	metadata := snapshot.Metadata()
	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	// This is the one tool that answers in prose, and it is a measurement rather
	// than a preference. A body inside a JSON string pays for every newline and
	// every tab twice: measured on one 26-line declaration, 302 tokens of source
	// become 374 as a JSON string and 430 as a full row -- the same 427 the host's
	// own range read costs. Serving code through the envelope buys nothing, so the
	// code travels as code and the envelope's counters travel in the header line.
	rendered := renderSourceText(metadata.ID, result)
	return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: rendered}},
		}, Response[SourceResult]{
			SnapshotID:    &snapshotID,
			SnapshotAgeMS: &snapshotAgeMS,
			Total:         len(selectors),
			Returned:      len(result.Bodies),
			Truncated:     result.Trimmed > 0,
			Coverage:      coverage,
			Results:       result,
		}, nil
}

// sourceFile is one file read once: its lines and whether the bytes still hash
// to what the generation recorded.
type sourceFile struct {
	lines []string
	fresh bool
	err   error
}

func sourceBody(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	contextLines int,
	format string,
	cache map[string]*sourceFile,
) (SourceBody, error) {
	table := snapshot.Strings()
	qualifiedName, qualifiedOK := table.String(symbol.QualifiedName)
	name, nameOK := table.String(symbol.Name)
	kind, kindOK := table.String(symbol.Kind)
	if !qualifiedOK || !nameOK || !kindOK {
		return SourceBody{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid symbol metadata",
			fmt.Errorf("symbol %q has invalid display strings", symbolStableKey(snapshot, symbol)))
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return SourceBody{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid symbol metadata", err)
	}
	file, fileOK := snapshot.File(symbol.File)
	if !fileOK {
		return SourceBody{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot file index is inconsistent",
			fmt.Errorf("file index %d is missing", symbol.File))
	}

	body := SourceBody{
		Repository:    location.RepositoryName,
		Path:          location.FilePath,
		QualifiedName: qualifiedName,
		Kind:          kind,
		StartLine:     symbol.StartLine,
		EndLine:       symbol.EndLine,
	}
	if format == ResponseFormatDetailed {
		body.StableKey = symbolStableKey(snapshot, symbol)
	}
	if symbol.StartLine == 0 || symbol.EndLine < symbol.StartLine {
		body.Unavailable = "the generation records no line range for this symbol"
		return body, nil
	}

	// A relative path names a file only inside its repository: two repositories
	// both have a main.go.
	cacheKey := location.RepositoryPath + "\x00" + location.FilePath
	loaded := cache[cacheKey]
	if loaded == nil {
		loaded = loadSourceFile(location.RepositoryPath, location.FilePath, file.ContentDigest)
		cache[cacheKey] = loaded
	}
	if loaded.err != nil {
		body.Unavailable = loaded.err.Error()
		return body, nil
	}
	body.Fresh = loaded.fresh

	start, end := int(symbol.StartLine), int(symbol.EndLine)
	if !loaded.fresh {
		// The file is the authority: it is what the agent will edit. Re-anchor
		// the declaration by name, and say by how much it moved. This asserts no
		// graph fact -- it answers "these lines" with the lines the declaration
		// now occupies. See ADR 0040.
		anchored, shifted, anchorErr := reanchorDeclaration(loaded.lines, name, start, end)
		if anchorErr != nil {
			body.Unavailable = anchorErr.Error()
			return body, nil
		}
		start, end = anchored, anchored+(end-start)
		body.StartLine, body.EndLine = uint32(start), uint32(end)
		body.Shifted = shifted
	}
	last := len(loaded.lines)
	if last > end && loaded.lines[last-1] == "" {
		last-- // Split leaves a sentinel after a terminal newline, not a source line.
	}
	from, to := max(1, start-contextLines), min(last, end+contextLines)
	body.Code = strings.Join(loaded.lines[from-1:to], "\n")
	if from != start || to != end {
		body.ShownStartLine, body.ShownEndLine = uint32(from), uint32(to)
	}
	return body, nil
}

// loadSourceFile reads one file under a repository and reports whether it still
// is the file the generation analysed.
//
// Nothing outside the repository is read, and no component of the path may be a
// symbolic link: the same policy the workspace layer applies when indexing, from
// the same function, so the two cannot drift apart.
func loadSourceFile(repositoryPath, relativePath string, indexed [sha256.Size]byte) *sourceFile {
	if repositoryPath == "" {
		return &sourceFile{err: fmt.Errorf("the generation records no path for this repository")}
	}
	cleaned := filepath.Clean(relativePath)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return &sourceFile{err: fmt.Errorf("path %q escapes its repository", relativePath)}
	}
	absolute := filepath.Join(repositoryPath, cleaned)
	if symlink, err := workspace.FirstSymlink(absolute); err != nil {
		return &sourceFile{err: fmt.Errorf("inspect %q: %w", relativePath, err)}
	} else if symlink != "" {
		return &sourceFile{err: fmt.Errorf("path %q contains symlink component %q", relativePath, symlink)}
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return &sourceFile{err: fmt.Errorf("read %q: %w", relativePath, err)}
	}
	digest := sha256.Sum256(data)
	fresh := indexed != [sha256.Size]byte{} && digest == indexed
	return &sourceFile{lines: strings.Split(string(data), "\n"), fresh: fresh}
}

// reanchorDeclaration finds the line that declares name closest to where the
// generation last saw it.
//
// Closest, not first: a file usually gains or loses lines above a declaration,
// so the nearest candidate is the one that moved rather than a different symbol
// with the same name elsewhere. Two candidates equally close is an ambiguity and
// says so; choosing would be the nominal coincidence this project forbids.
func reanchorDeclaration(lines []string, name string, start, end int) (int, int, error) {
	if name == "" {
		return 0, 0, fmt.Errorf("the file changed and the generation records no name to re-anchor")
	}
	best, bestDistance, ties := 0, 0, 0
	for index, line := range lines {
		if !declaresName(line, name) {
			continue
		}
		candidate := index + 1
		distance := candidate - start
		if distance < 0 {
			distance = -distance
		}
		switch {
		case best == 0 || distance < bestDistance:
			best, bestDistance, ties = candidate, distance, 1
		case distance == bestDistance:
			ties++
		}
	}
	if best == 0 {
		return 0, 0, fmt.Errorf("the file changed and no declaration of %q remains in it", name)
	}
	if ties > 1 {
		return 0, 0, fmt.Errorf("the file changed and %q now appears %d times equally far from its recorded position", name, ties)
	}
	if best+(end-start) > len(lines) {
		return 0, 0, fmt.Errorf("the file changed and %q no longer spans %d lines", name, end-start+1)
	}
	return best, best - start, nil
}

// declaresName reports whether line declares name, as opposed to mentioning it.
// The test is deliberately coarse -- this is byte delivery, not resolution -- but
// it does require the name to stand alone, so `Merge` never matches `MergeAll`.
func declaresName(line, name string) bool {
	index := strings.Index(line, name)
	for index >= 0 {
		before := byte(' ')
		if index > 0 {
			before = line[index-1]
		}
		after := byte(' ')
		if index+len(name) < len(line) {
			after = line[index+len(name)]
		}
		if !isIdentifierByte(before) && !isIdentifierByte(after) {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "*") {
				return true
			}
		}
		next := strings.Index(line[index+1:], name)
		if next < 0 {
			return false
		}
		index += next + 1
	}
	return false
}

func isIdentifierByte(value byte) bool {
	switch {
	case value >= 'a' && value <= 'z', value >= 'A' && value <= 'Z', value >= '0' && value <= '9':
		return true
	case value == '_':
		return true
	default:
		return false
	}
}

func normalizeGetSourceInput(arguments GetSourceInput) ([]symbolSelector, int, string, error) {
	if len(arguments.Symbols) == 0 {
		return nil, 0, "", NewToolError(CodeInvalidArgument, "symbols must name at least one symbol")
	}
	if len(arguments.Symbols) > MaximumSourceSymbols {
		return nil, 0, "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"symbols must name at most %d symbols", MaximumSourceSymbols,
		))
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, 0, "", err
	}
	contextLines := arguments.ContextLines
	if contextLines < 0 || contextLines > MaximumSourceContextLines {
		return nil, 0, "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"context_lines must be between 0 and %d", MaximumSourceContextLines,
		))
	}
	selectors := make([]symbolSelector, 0, len(arguments.Symbols))
	for _, request := range arguments.Symbols {
		if request.Line != nil || request.SnapshotID != nil {
			if request.Line == nil || *request.Line == 0 || request.SnapshotID == nil || request.Repository == "" || request.Path == "" || request.StableKey != "" || request.QualifiedName != "" {
				return nil, 0, "", NewToolError(CodeInvalidArgument, "position requires repository, path, positive line and snapshot_id instead of stable_key or qualified_name")
			}
			if _, err := normalizeOutlineArgument(request.Repository, "repository"); err != nil {
				return nil, 0, "", err
			}
			path, err := normalizeOutlinePath(request.Path)
			if err != nil {
				return nil, 0, "", err
			}
			selectors = append(selectors, symbolSelector{Repository: request.Repository, Path: path})
			continue
		}
		selector, err := normalizeSymbolSelector(request.StableKey, request.Repository, request.Path, request.QualifiedName)
		if err != nil {
			return nil, 0, "", err
		}
		selectors = append(selectors, selector)
	}
	return selectors, contextLines, format, nil
}

// renderSourceText writes the answer the way source is read: one header line per
// body and then the bytes, unescaped and unnumbered.
//
// `@` opens a body and `!` a row that has none. A header names the repository,
// the path, the range served and the declaration, and says when the range is not
// the one the generation recorded.
func renderSourceText(snapshotID uint64, result SourceResult) string {
	builder := &strings.Builder{}
	fmt.Fprintf(builder, "snapshot %d  %d bodies  context %d", snapshotID, len(result.Bodies), result.ContextLines)
	if result.Trimmed > 0 {
		fmt.Fprintf(builder, "  trimmed %d at the %d byte ceiling", result.Trimmed, MaximumSourceBytes)
	}
	builder.WriteByte('\n')
	for _, body := range result.Bodies {
		if body.Unavailable != "" {
			fmt.Fprintf(builder, "! %s %s %s — %s\n", body.Repository, body.Path, body.QualifiedName, body.Unavailable)
			continue
		}
		fmt.Fprintf(builder, "@ %s %s:%d-%d %s %s",
			body.Repository, body.Path, body.StartLine, body.EndLine, body.Kind, body.QualifiedName)
		writeSourceSuffix(builder, body)
	}
	return builder.String()
}

func writeSourceSuffix(builder *strings.Builder, body SourceBody) {
	if body.ShownStartLine != 0 {
		fmt.Fprintf(builder, " [shown %d-%d]", body.ShownStartLine, body.ShownEndLine)
	}
	if !body.Fresh {
		fmt.Fprintf(builder, " [file changed, re-anchored %+d]", body.Shifted)
	}
	if body.StableKey != "" {
		fmt.Fprintf(builder, " %s", body.StableKey)
	}
	builder.WriteByte('\n')
	builder.WriteString(body.Code)
	builder.WriteByte('\n')
}

func renderSourceProfiles(profiles []ProfileSnapshot, result SourceResult) string {
	builder := &strings.Builder{}
	builder.WriteString("profiles")
	for _, profile := range profiles {
		fmt.Fprintf(builder, " %s=%d", profile.Name, profile.SnapshotID)
	}
	fmt.Fprintf(builder, "  %d bodies  context %d", len(result.Bodies), result.ContextLines)
	if result.Trimmed > 0 {
		fmt.Fprintf(builder, "  trimmed %d at the %d byte ceiling", result.Trimmed, MaximumSourceBytes)
	}
	builder.WriteByte('\n')
	for _, body := range result.Bodies {
		profileLabel := strings.ReplaceAll(string(body.Profiles), "\x00", ",")
		if body.Unavailable != "" {
			fmt.Fprintf(builder, "! [%s] %s %s %s — %s\n", profileLabel, body.Repository, body.Path, body.QualifiedName, body.Unavailable)
			continue
		}
		fmt.Fprintf(builder, "@ [%s] %s %s:%d-%d %s %s",
			profileLabel, body.Repository, body.Path, body.StartLine, body.EndLine, body.Kind, body.QualifiedName)
		writeSourceSuffix(builder, body)
	}
	return builder.String()
}
