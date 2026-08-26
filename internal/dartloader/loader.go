package dartloader

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const PayloadVersion = 1

// Options controls one authoritative Dart analysis pass. The defaults preserve
// the historical loader behaviour, while the additional switches make package
// configuration and bounded analysis explicit to callers.
type Options struct {
	Command             string
	SDKPath             string
	Repository          workspace.Repository
	IncludeGenerated    bool
	IncludeTests        bool
	IncludeExternal     bool
	IncludeSDK          bool
	PackageConfig       string
	WaitForAnalysis     bool
	MaximumAnalysisTime time.Duration
	Providers           []workspace.Repository
}

type position struct{ Line, Character int }
type lspRange struct{ Start, End position }
type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}
type documentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Detail         string           `json:"detail"`
	ContainerName  string           `json:"containerName"`
	Location       location         `json:"location"`
	Range          lspRange         `json:"range"`
	SelectionRange lspRange         `json:"selectionRange"`
	Children       []documentSymbol `json:"children"`
}

// Run indexes a Dart project through the SDK's LSP Analysis Server. The
// server supplies declarations and resolved references; Kivgraph only uses
// source text to classify an already-resolved occurrence as a call.
func Run(ctx context.Context, command, sdkPath string, repository workspace.Repository, includeGenerated, includeTests bool) (facts.SemanticPayload, error) {
	return RunWithOptions(ctx, Options{Command: command, SDKPath: sdkPath, Repository: repository, IncludeGenerated: includeGenerated, IncludeTests: includeTests})
}

// RunWithRepositories is Run with the registered repository roots available
// for provider identity mapping. The loader still publishes only the consumer
// repository's files; provider files are read only to identify an analyzer
// target that crosses a registered repository boundary.
func RunWithRepositories(ctx context.Context, command, sdkPath string, repository workspace.Repository, includeGenerated, includeTests bool, providers []workspace.Repository) (facts.SemanticPayload, error) {
	return RunWithOptions(ctx, Options{Command: command, SDKPath: sdkPath, Repository: repository, IncludeGenerated: includeGenerated, IncludeTests: includeTests, Providers: providers})
}

// RunWithOptions is the configurable Dart entry point used by the full
// indexer. The Analysis Server owns semantic resolution; this package only
// translates its results into the versioned Kivgraph payload.
func RunWithOptions(ctx context.Context, options Options) (facts.SemanticPayload, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.MaximumAnalysisTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.MaximumAnalysisTime)
		defer cancel()
	}
	repository := options.Repository
	root := repository.RealPath
	if root == "" {
		root = repository.Path
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return facts.SemanticPayload{}, err
	}
	files, err := dartFilesForRepository(root, repository.Roots, repository.Exclusions, options.IncludeGenerated, options.IncludeTests)
	if err != nil {
		return facts.SemanticPayload{}, err
	}
	client, err := start(ctx, options.Command, options.SDKPath, root)
	if err != nil {
		return facts.SemanticPayload{}, err
	}
	defer client.close()
	if err := client.initialize(ctx, root); err != nil {
		return facts.SemanticPayload{}, err
	}

	contents := make(map[string][]byte, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return facts.SemanticPayload{}, fmt.Errorf("read Dart file %q: %w", path, err)
		}
		contents[path] = data
		client.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": fileURI(path), "languageId": "dart", "version": 1, "text": string(data)}})
	}

	payload := facts.SemanticPayload{Version: PayloadVersion, Authoritative: true, Repository: repository.Name, Language: facts.LanguageDart, Package: facts.SemanticPackage{Name: dartPackageName(root), RootPath: root, ManifestPath: filepath.Join(root, "pubspec.yaml")}}
	for _, path := range files {
		payload.Files = append(payload.Files, facts.SemanticFile{Path: relative(root, path), Generated: isGenerated(path), LibraryName: dartLibraryName(contents[path])})
	}
	symbolsByURI := make(map[string][]documentSymbol)
	for _, path := range files {
		result, err := client.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}})
		if err != nil {
			return facts.SemanticPayload{}, fmt.Errorf("Dart symbols %q: %w", path, err)
		}
		var rows []documentSymbol
		if len(result) != 0 && string(result) != "null" {
			if err := json.Unmarshal(result, &rows); err != nil {
				return facts.SemanticPayload{}, fmt.Errorf("decode Dart symbols %q: %w", path, err)
			}
		}
		symbolsByURI[fileURI(path)] = rows
	}

	byID := make(map[string]facts.SemanticSymbol)
	byURI := make(map[string][]facts.SemanticSymbol)
	selectionOffsets := make(map[string]int)
	moduleIDs := make(map[string]string, len(files))
	for _, path := range files {
		rel := relative(root, path)
		data := contents[path]
		end := positionAt(data, len(data))
		id := "module\x00" + rel
		moduleIDs[rel] = id
		moduleName := strings.TrimSuffix(strings.ReplaceAll(rel, "/", "."), ".dart")
		byID[id] = facts.SemanticSymbol{ID: id, File: rel, Name: filepath.Base(rel), QualifiedName: moduleName, Kind: "module", Exported: true, Signature: "module " + moduleName, StartLine: 1, StartColumn: 0, Start: 0, EndLine: end.Line + 1, EndColumn: end.Character, End: len(data)}
		byURI[fileURI(path)] = append(byURI[fileURI(path)], byID[id])
		selectionOffsets[id] = 0
	}
	for _, path := range files {
		uri := fileURI(path)
		flattenSymbols(uri, symbolsByURI[uri], "", root, contents, &payload, byID, &byURI, selectionOffsets)
	}
	for _, symbol := range byID {
		payload.Symbols = append(payload.Symbols, symbol)
	}
	sort.Slice(payload.Symbols, func(i, j int) bool { return payload.Symbols[i].ID < payload.Symbols[j].ID })

	packageRoots := dartPackageRoots(root, options.PackageConfig, options.IncludeExternal)
	if err := appendNavigationReferences(ctx, client, options.Command, options.SDKPath, root, files, contents, byURI, payload.Symbols, selectionOffsets, options.Providers, packageRoots, options.WaitForAnalysis, &payload); err != nil {
		return facts.SemanticPayload{}, err
	}

	directivePattern := regexp.MustCompile(`(?ms)^\s*(import|export|part)\s+([^;]+);`)
	uriPattern := regexp.MustCompile(`['"]([^'"]+)['"]`)
	prefixPattern := regexp.MustCompile(`\bas\s+([A-Za-z_]\w*)`)
	for _, path := range files {
		data := contents[path]
		for _, match := range directivePattern.FindAllSubmatchIndex(data, -1) {
			body := string(data[match[4]:match[5]])
			uris := uriPattern.FindAllStringSubmatch(body, -1)
			if len(uris) == 0 {
				continue
			}
			directive := string(data[match[2]:match[3]])
			requested := uris[0][1]
			alternatives := make([]string, 0, len(uris)-1)
			for _, alternative := range uris[1:] {
				alternatives = append(alternatives, alternative[1])
			}
			requestedPackage := requested
			if strings.HasPrefix(requested, "package:") && !strings.HasPrefix(requested, "package:"+payload.Package.Name+"/") {
				requestedPackage = strings.TrimPrefix(requested, "package:")
				if slash := strings.IndexByte(requestedPackage, '/'); slash >= 0 {
					requestedPackage = requestedPackage[:slash]
				}
			}
			// The keyword starts the directive; `match[0]` includes the
			// indentation the pattern skipped, and `match[1]` is just past the
			// semicolon. Evidence has to span the directive, not the whitespace
			// before it.
			startPoint := positionAt(data, match[2])
			endPoint := positionAt(data, match[1])
			line := startPoint.Line + 1
			targetID := resolveDartImport(root, path, requested, moduleIDs, payload.Package.Name)
			if directive == "part" {
				partPath := dartRelativeFile(root, path, requested)
				libraryPath := relative(root, path)
				partFile := partPath
				if strings.HasPrefix(strings.TrimSpace(body), "of ") {
					libraryPath = partPath
					partFile = relative(root, path)
				}
				if targetID != "" && partPath != "" {
					payload.Parts = append(payload.Parts, facts.SemanticPart{
						File: relative(root, path), LibraryFile: libraryPath, PartFile: partFile,
						StartLine: line, StartColumn: startPoint.Character, Start: match[2],
						EndLine: endPoint.Line + 1, EndColumn: endPoint.Character, End: match[1],
						Detail: requested,
					})
				}
				continue
			}
			kind := string(facts.ImportsSymbol)
			if directive == "export" {
				// A Dart export directive forwards another library's public
				// surface; it is therefore a re-export even when the source
				// uses the shorthand `export 'foo.dart';`.
				kind = string(facts.Reexports)
			}
			prefix := ""
			if match := prefixPattern.FindStringSubmatch(body); len(match) == 2 {
				prefix = match[1]
			}
			payload.Imports = append(payload.Imports, facts.SemanticImport{
				File: relative(root, path), Kind: kind,
				RequestedPackage: requestedPackage, RequestedSymbol: requested,
				Alternatives: alternatives, Prefix: prefix,
				Deferred: strings.Contains(body, "deferred"), TargetID: targetID,
				StartLine: line, StartColumn: startPoint.Character, Start: match[2],
				EndLine: endPoint.Line + 1, EndColumn: endPoint.Character, End: match[1],
				Detail: strings.TrimSpace(body),
			})
		}
	}
	return payload, nil
}

type client struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  <-chan rpcMessage
	mu   sync.Mutex
	next int
}
type rpcMessage struct {
	ID     json.RawMessage
	Method string
	Result json.RawMessage
	Error  json.RawMessage
}

func start(ctx context.Context, command, sdkPath, root string) (*client, error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		command = sdkPath
		fields = strings.Fields(command)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("Dart analyzer command is empty")
	}
	executable, err := exec.LookPath(fields[0])
	if err != nil {
		return nil, fmt.Errorf("Dart analyzer %q is unavailable: %w", fields[0], err)
	}
	args := append([]string{}, fields[1:]...)
	if filepath.Base(executable) == "dart" {
		args = append(args, "language-server", "--protocol=lsp", "--client-id=kivgraph", "--client-version=dev")
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	messages := make(chan rpcMessage, 32)
	go readMessages(stdout, messages)
	return &client{cmd: cmd, in: in, out: messages, next: 1}, nil
}

// SDKRoot resolves the Dart SDK directory from the configured dart command.
// It is exported for the full indexer, which can register the SDK as an
// explicit provider only when include_sdk is enabled.
func SDKRoot(command string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "", fmt.Errorf("Dart SDK command is empty")
	}
	executable, err := exec.LookPath(fields[0])
	if err != nil {
		return "", fmt.Errorf("Dart SDK %q is unavailable: %w", fields[0], err)
	}
	if filepath.Base(executable) != "dart" && filepath.Base(executable) != "dart.exe" {
		return "", fmt.Errorf("Dart SDK command %q is not the dart executable", executable)
	}
	candidates := []string{
		filepath.Join(filepath.Dir(executable), ".."),
		filepath.Join(filepath.Dir(executable), "cache", "dart-sdk"),
	}
	for _, candidate := range candidates {
		root, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, statErr := os.Stat(filepath.Join(root, "lib")); statErr == nil && info.IsDir() {
			return root, nil
		}
	}
	return "", fmt.Errorf("Dart SDK executable %q has no discoverable lib directory", executable)
}

type navigationTarget struct {
	Kind      string `json:"kind"`
	FileIndex int    `json:"fileIndex"`
	Offset    int    `json:"offset"`
	Length    int    `json:"length"`
}

type navigationRegion struct {
	Offset  int   `json:"offset"`
	Length  int   `json:"length"`
	Targets []int `json:"targets"`
}

type navigationResult struct {
	Files   []string           `json:"files"`
	Targets []navigationTarget `json:"targets"`
	Regions []navigationRegion `json:"regions"`
}

type analyzerLocation struct {
	File   string `json:"file"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type analyzerElement struct {
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Location       *analyzerLocation `json:"location"`
	Parameters     string            `json:"parameters"`
	ReturnType     string            `json:"returnType"`
	TypeParameters string            `json:"typeParameters"`
	AliasedType    string            `json:"aliasedType"`
	ExtendedType   string            `json:"extendedType"`
}

type analyzerOverriddenMember struct {
	Element   analyzerElement `json:"element"`
	ClassName string          `json:"className"`
}

type analyzerOverride struct {
	Offset           int                        `json:"offset"`
	Length           int                        `json:"length"`
	SuperclassMember *analyzerOverriddenMember  `json:"superclassMember"`
	InterfaceMembers []analyzerOverriddenMember `json:"interfaceMembers"`
}

type analyzerOutline struct {
	Offset     int               `json:"offset"`
	Length     int               `json:"length"`
	CodeOffset int               `json:"codeOffset"`
	CodeLength int               `json:"codeLength"`
	Element    analyzerElement   `json:"element"`
	Children   []analyzerOutline `json:"children"`
}

type analyzerMessage struct {
	ID     string          `json:"id"`
	Event  string          `json:"event"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type analyzerClient struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	out      <-chan analyzerMessage
	mu       sync.Mutex
	eventsMu sync.Mutex
	events   []analyzerMessage
	next     int
}

func startAnalyzer(ctx context.Context, command, sdkPath, root string) (*analyzerClient, error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		fields = strings.Fields(sdkPath)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("Dart analyzer command is empty")
	}
	executable, err := exec.LookPath(fields[0])
	if err != nil {
		return nil, fmt.Errorf("Dart analyzer %q is unavailable: %w", fields[0], err)
	}
	args := append([]string{}, fields[1:]...)
	if filepath.Base(executable) == "dart" {
		args = append(args, "language-server", "--protocol=analyzer", "--client-id=kivgraph", "--client-version=dev")
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	messages := make(chan analyzerMessage, 64)
	go readAnalyzerMessages(stdout, messages)
	return &analyzerClient{cmd: cmd, in: in, out: messages, next: 1}, nil
}

func (c *analyzerClient) nextID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := strconv.Itoa(c.next)
	c.next++
	return id
}

func (c *analyzerClient) sendRequest(id, method string, params any) error {
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "%s\n", data)
	return err
}

func (c *analyzerClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID()
	if err := c.sendRequest(id, method, params); err != nil {
		return nil, err
	}
	results, err := c.collect(ctx, map[string]struct{}{id: {}})
	if err != nil {
		return nil, err
	}
	return results[id], nil
}

func (c *analyzerClient) collect(ctx context.Context, requests map[string]struct{}) (map[string]json.RawMessage, error) {
	results := make(map[string]json.RawMessage, len(requests))
	for len(results) < len(requests) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case message, ok := <-c.out:
			if !ok {
				return nil, fmt.Errorf("Dart analysis server closed")
			}
			if message.Event != "" {
				c.eventsMu.Lock()
				c.events = append(c.events, message)
				c.eventsMu.Unlock()
				continue
			}
			if _, wanted := requests[message.ID]; !wanted {
				continue
			}
			if len(message.Error) != 0 && string(message.Error) != "null" {
				return nil, fmt.Errorf("Dart analyzer request failed: %s", message.Error)
			}
			results[message.ID] = message.Result
		}
	}
	return results, nil
}

func (c *analyzerClient) eventCount() int {
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	return len(c.events)
}

func (c *analyzerClient) eventsSince(index int) []analyzerMessage {
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	if index < 0 || index >= len(c.events) {
		return append([]analyzerMessage(nil), c.events...)
	}
	return append([]analyzerMessage(nil), c.events[index:]...)
}

func (c *analyzerClient) drainEvents(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case message, ok := <-c.out:
			if !ok {
				return nil
			}
			if message.Event == "" {
				continue
			}
			c.eventsMu.Lock()
			c.events = append(c.events, message)
			c.eventsMu.Unlock()
		}
	}
}

func (c *analyzerClient) close() {
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

func readAnalyzerMessages(reader io.Reader, output chan<- analyzerMessage) {
	defer close(output)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var message analyzerMessage
		if json.Unmarshal(scanner.Bytes(), &message) == nil && (message.ID != "" || message.Event != "") {
			output <- message
		}
	}
}

func appendNavigationReferences(ctx context.Context, lsp *client, command, sdkPath, root string, files []string, contents map[string][]byte, byURI map[string][]facts.SemanticSymbol, symbols []facts.SemanticSymbol, selectionOffsets map[string]int, providers []workspace.Repository, packageRoots []string, waitForAnalysis bool, payload *facts.SemanticPayload) error {
	server, err := startAnalyzer(ctx, command, sdkPath, root)
	if err != nil {
		return err
	}
	defer server.close()
	if _, err := server.call(ctx, "server.getVersion", map[string]any{}); err != nil {
		return err
	}
	eventStart := server.eventCount()
	if _, err := server.call(ctx, "analysis.setSubscriptions", map[string]any{
		"subscriptions": map[string]any{
			"OUTLINE":     files,
			"NAVIGATION":  files,
			"OCCURRENCES": files,
			"OVERRIDES":   files,
			"IMPLEMENTED": files,
		},
	}); err != nil {
		return err
	}
	analysisRoots := map[string]any{"included": []string{root}, "excluded": []string{filepath.Join(root, ".dart_tool"), filepath.Join(root, "build")}}
	if len(packageRoots) > 0 {
		analysisRoots["packageRoots"] = packageRoots
	}
	if _, err := server.call(ctx, "analysis.setAnalysisRoots", analysisRoots); err != nil {
		return err
	}
	if _, err := server.call(ctx, "analysis.setPriorityFiles", map[string]any{"files": files}); err != nil {
		return err
	}

	byLocation := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		path := filepath.Join(root, filepath.FromSlash(symbol.File))
		byLocation[locationKey(path, selectionOffsets[symbol.ID])] = symbol.ID
	}
	requests := make(map[string]struct{}, len(files))
	requestPaths := make(map[string]string, len(files))
	for _, path := range files {
		id := server.nextID()
		requests[id] = struct{}{}
		requestPaths[id] = path
		if err := server.sendRequest(id, "analysis.getNavigation", map[string]any{"file": path, "offset": 0, "length": len(contents[path])}); err != nil {
			return err
		}
	}
	results, err := server.collect(ctx, requests)
	if err != nil {
		return err
	}
	if waitForAnalysis {
		if err := server.drainEvents(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
	events := server.eventsSince(eventStart)
	appendDartAnalyzerOutlines(events, root, contents, byURI, selectionOffsets, payload)
	appendExtensionTypeRepresentations(root, contents, byURI, selectionOffsets, payload)
	symbols = payload.Symbols
	byLocation = make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		path := filepath.Join(root, filepath.FromSlash(symbol.File))
		byLocation[locationKey(path, selectionOffsets[symbol.ID])] = symbol.ID
	}
	appendDartAnalyzerOverrides(events, root, contents, byURI, symbols, selectionOffsets, payload)
	seen := make(map[string]struct{})
	for id, result := range results {
		sourcePath := requestPaths[id]
		var navigation navigationResult
		if len(result) == 0 || string(result) == "null" || json.Unmarshal(result, &navigation) != nil {
			continue
		}
		for _, region := range navigation.Regions {
			sourceData := contents[sourcePath]
			sourceOffset := analyzerOffset(sourceData, region.Offset)
			source := enclosing(byURI[fileURI(sourcePath)], positionAt(sourceData, sourceOffset))
			if source.ID == "" {
				continue
			}
			for _, targetIndex := range region.Targets {
				if targetIndex < 0 || targetIndex >= len(navigation.Targets) {
					continue
				}
				target := navigation.Targets[targetIndex]
				if target.FileIndex < 0 || target.FileIndex >= len(navigation.Files) {
					continue
				}
				targetPath := navigation.Files[target.FileIndex]
				if strings.HasPrefix(targetPath, "file:") {
					if parsedPath, parseErr := uriPath(targetPath); parseErr == nil {
						targetPath = parsedPath
					}
				}
				targetPath = filepath.Clean(targetPath)
				targetData := contents[targetPath]
				targetOffset := target.Offset
				if len(targetData) > 0 {
					targetOffset = analyzerOffset(targetData, target.Offset)
				}
				targetID := byLocation[locationKey(targetPath, targetOffset)]
				var externalTarget *facts.SemanticTarget
				if targetID == "" && !pathWithin(root, targetPath) {
					provider, ok := providerForPath(targetPath, providers)
					if ok {
						externalTarget = externalDartTarget(ctx, lsp, targetPath, targetOffset, provider)
					}
				}
				if targetID == "" && externalTarget == nil {
					startPosition := positionAt(sourceData, sourceOffset)
					unresolvedKey := fmt.Sprintf("%s\x00%s\x00%d", source.ID, targetPath, sourceOffset)
					if _, exists := seen[unresolvedKey]; !exists {
						seen[unresolvedKey] = struct{}{}
						payload.Unresolved = append(payload.Unresolved, facts.SemanticUnresolved{
							File: relative(root, sourcePath), SourceID: source.ID,
							RequestedPackage: targetPath, RequestedSymbol: target.Kind,
							Reason: "DART_TARGET_NOT_INDEXED", Detail: "Analysis Server target was outside the published provider set",
							StartLine: startPosition.Line + 1, StartColumn: startPosition.Character, Start: sourceOffset,
						})
					}
					continue
				}
				// Identity, not position: a declaration does not reference
				// itself. The occurrence of `Vehicle` in `class Vehicle` and the
				// name on a `library` directive both resolve to the symbol that
				// encloses them, at a different offset -- so comparing offsets
				// let four self loops through as EXACT edges.
				if targetID != "" && targetID == source.ID {
					continue
				}
				endOffset := analyzerOffset(sourceData, region.Offset+region.Length)
				kind := dartReferenceKind(sourceData, sourceOffset, endOffset-sourceOffset, target.Kind)
				key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", source.ID, targetID, sourceOffset, endOffset-sourceOffset)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				startOffset := analyzerOffset(sourceData, region.Offset)
				startPosition := positionAt(sourceData, startOffset)
				endPosition := positionAt(sourceData, endOffset)
				payload.References = append(payload.References, facts.SemanticReference{File: relative(root, sourcePath), SourceID: source.ID, TargetID: targetID, Target: externalTarget, Kind: kind, StartLine: startPosition.Line + 1, StartColumn: startPosition.Character, Start: startOffset, EndLine: endPosition.Line + 1, EndColumn: endPosition.Character, End: endOffset, Text: sliceLine(sourceData, startPosition)})
			}
		}
	}
	return nil
}

// appendDartAnalyzerOutlines uses the analyzer protocol's semantic outline as
// a second declaration source. The LSP outline is intentionally retained as
// the fast path, while this event fills gaps such as top-level declarations,
// getters/setters, type aliases, mixins and extension members that different
// SDK/LSP versions expose inconsistently.
func appendDartAnalyzerOutlines(events []analyzerMessage, root string, contents map[string][]byte, byURI map[string][]facts.SemanticSymbol, selectionOffsets map[string]int, payload *facts.SemanticPayload) {
	existing := make(map[string]struct{}, len(payload.Symbols))
	for _, symbol := range payload.Symbols {
		path := filepath.Join(root, filepath.FromSlash(symbol.File))
		existing[locationKey(path, selectionOffsets[symbol.ID])] = struct{}{}
	}
	for _, event := range events {
		if event.Event != "analysis.outline" {
			continue
		}
		var params struct {
			File    string          `json:"file"`
			Outline analyzerOutline `json:"outline"`
		}
		if json.Unmarshal(event.Params, &params) != nil || strings.TrimSpace(params.File) == "" {
			continue
		}
		path := filepath.Clean(params.File)
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, ok := contents[path]
		if !ok {
			continue
		}
		relativePath := relative(root, path)
		uri := fileURI(path)
		var visit func(analyzerOutline, string)
		visit = func(row analyzerOutline, prefix string) {
			name := strings.TrimSpace(row.Element.Name)
			// The Analysis Server names the compilation unit `<unit>`, and marks
			// other synthetic elements the same way. It is not a declaration:
			// taking it as one published a second copy of every symbol in the
			// file under a `<unit>.` prefix, and a reference then joined the two
			// copies of one declaration as an EXACT edge.
			if name == "" || strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">") {
				for _, child := range row.Children {
					visit(child, prefix)
				}
				return
			}
			// Analysis Server outlines also contain constructor invocation
			// expressions (for example repeated Flutter SizedBox widgets).
			// They are not declarations, and their outline-qualified names are
			// intentionally identical when they occur more than once in the
			// same build method. Publishing them would therefore create several
			// DEFINES edges for one stable symbol identity. Constructors that are
			// actual declarations are still supplied by the LSP document outline.
			if isDartAnalyzerConstructorInvocation(row, root) {
				return
			}
			qualified := name
			if prefix != "" {
				qualified = prefix + "." + name
			}
			selection := analyzerOffset(data, row.Offset)
			if row.Element.Location != nil {
				selection = analyzerOffset(data, row.Element.Location.Offset)
			} else if index := declarationNameOffset(data, analyzerOffset(data, row.Offset), analyzerOffset(data, row.Offset+row.Length), name); index >= 0 {
				// The identifier, not the start of the declaration: an outline row
				// with no element location otherwise keyed on the `class` keyword
				// while the LSP row keyed on the name, so one declaration was
				// published twice and a reference joined the two copies.
				selection = index
			}
			kind := analyzerDartKind(row.Element.Kind)
			if _, found := existing[locationKey(path, selection)]; !found {
				startOffset := analyzerOffset(data, row.Offset)
				endOffset := analyzerOffset(data, row.Offset+row.Length)
				if row.Length <= 0 {
					startOffset = selection
					endOffset = min(len(data), selection+len(name))
				}
				start := positionAt(data, startOffset)
				end := positionAt(data, endOffset)
				signature := analyzerSignature(row.Element)
				if signature == "" {
					signature = declarationSignature(data, start)
				}
				id := fmt.Sprintf("%s\x00%s\x00%s\x00%d:%d", relativePath, qualified, kind, start.Line, start.Character)
				symbol := facts.SemanticSymbol{
					ID: id, File: relativePath, Name: name, QualifiedName: qualified,
					Kind: kind, Exported: !strings.HasPrefix(name, "_"), Signature: signature,
					StartLine: start.Line + 1, StartColumn: start.Character, Start: startOffset,
					EndLine: end.Line + 1, EndColumn: end.Character, End: endOffset,
				}
				payload.Symbols = append(payload.Symbols, symbol)
				byURI[uri] = append(byURI[uri], symbol)
				selectionOffsets[id] = selection
				existing[locationKey(path, selection)] = struct{}{}
			}
			for _, child := range row.Children {
				visit(child, qualified)
			}
		}
		visit(params.Outline, "")
	}
	sort.Slice(payload.Symbols, func(i, j int) bool { return payload.Symbols[i].ID < payload.Symbols[j].ID })
	// One identity, one row. Both declaration sources can observe the same
	// declaration and agree on its identity, and the duplicate then inflates
	// every count taken from the payload without adding a fact.
	unique := payload.Symbols[:0]
	for index, symbol := range payload.Symbols {
		if index > 0 && symbol.ID == payload.Symbols[index-1].ID {
			continue
		}
		unique = append(unique, symbol)
	}
	payload.Symbols = unique
}

// extensionTypeHeader matches the representation declaration of an extension
// type: `extension type UserId(int value)`, with the optional `const` and an
// optional named constructor. The type may itself be generic or nullable, so
// the field name is the last identifier before the closing parenthesis.
var extensionTypeHeader = regexp.MustCompile(`extension\s+type\s+(?:const\s+)?[A-Za-z_$][A-Za-z0-9_$]*(?:<[^>]*>)?(?:\.[A-Za-z_$][A-Za-z0-9_$]*)?\s*\(\s*[^()]*?([A-Za-z_$][A-Za-z0-9_$]*)\s*\)`)

// appendExtensionTypeRepresentations publishes the representation field of an
// extension type. Neither declaration source lists it, so every use of it
// resolved to a target that is not in the graph -- `String asText() =>
// value.toString()` lost its only relation -- even though the field is a
// declaration any holder can name as `id.value`.
func appendExtensionTypeRepresentations(root string, contents map[string][]byte, byURI map[string][]facts.SemanticSymbol, selectionOffsets map[string]int, payload *facts.SemanticPayload) {
	declared := make(map[string]struct{}, len(payload.Symbols))
	for _, symbol := range payload.Symbols {
		declared[symbol.File+"\x00"+symbol.QualifiedName] = struct{}{}
	}
	added := make([]facts.SemanticSymbol, 0, 4)
	for _, symbol := range payload.Symbols {
		if symbol.Kind != "extension" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(symbol.File))
		data := contents[path]
		if symbol.Start < 0 || symbol.End > len(data) || symbol.Start >= symbol.End {
			continue
		}
		header := data[symbol.Start:symbol.End]
		if brace := bytes.IndexByte(header, '{'); brace >= 0 {
			header = header[:brace]
		}
		match := extensionTypeHeader.FindSubmatchIndex(header)
		if match == nil {
			continue
		}
		name := string(header[match[2]:match[3]])
		qualified := symbol.QualifiedName + "." + name
		if _, found := declared[symbol.File+"\x00"+qualified]; found {
			continue
		}
		start := symbol.Start + match[2]
		end := symbol.Start + match[3]
		startPosition := positionAt(data, start)
		endPosition := positionAt(data, end)
		id := fmt.Sprintf("%s\x00%s\x00%s\x00%d:%d", symbol.File, qualified, "field", startPosition.Line, startPosition.Character)
		field := facts.SemanticSymbol{
			ID: id, File: symbol.File, Name: name, QualifiedName: qualified,
			Kind: "field", Exported: !strings.HasPrefix(name, "_"),
			Signature: "field " + qualified,
			StartLine: startPosition.Line + 1, StartColumn: startPosition.Character, Start: start,
			EndLine: endPosition.Line + 1, EndColumn: endPosition.Character, End: end,
		}
		added = append(added, field)
		declared[symbol.File+"\x00"+qualified] = struct{}{}
		selectionOffsets[id] = start
		byURI[fileURI(path)] = append(byURI[fileURI(path)], field)
	}
	if len(added) == 0 {
		return
	}
	payload.Symbols = append(payload.Symbols, added...)
	sort.Slice(payload.Symbols, func(i, j int) bool { return payload.Symbols[i].ID < payload.Symbols[j].ID })
}

// declarationNameOffset finds where a declaration's identifier begins inside
// its own span, so an outline row without an element location keys on the same
// offset the LSP outline used. It returns -1 when the name is not there, and
// requires a whole-word match so `Value` inside `PartValue` is not taken.
func declarationNameOffset(data []byte, start, end int, name string) int {
	if name == "" || start < 0 || end > len(data) || start >= end {
		return -1
	}
	window := string(data[start:end])
	for offset := 0; ; {
		index := strings.Index(window[offset:], name)
		if index < 0 {
			return -1
		}
		index += offset
		before := index == 0 || !isIdentifierPart(window[index-1])
		after := index+len(name) >= len(window) || !isIdentifierPart(window[index+len(name)])
		if before && after {
			return start + index
		}
		offset = index + len(name)
	}
}

func isDartAnalyzerConstructorInvocation(row analyzerOutline, root string) bool {
	kind := strings.ToUpper(strings.TrimSpace(row.Element.Kind))
	if kind == "CONSTRUCTOR_INVOCATION" {
		return true
	}
	if kind != "CONSTRUCTOR" {
		return false
	}
	if row.Element.Location == nil || strings.TrimSpace(row.Element.Location.File) == "" {
		return true
	}
	path := filepath.Clean(row.Element.Location.File)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return !pathWithin(root, path)
}

func analyzerSignature(element analyzerElement) string {
	name := strings.TrimSpace(element.Name)
	parameters := strings.TrimSpace(element.Parameters)
	returnType := strings.TrimSpace(element.ReturnType)
	typeParameters := strings.TrimSpace(element.TypeParameters)
	aliasedType := strings.TrimSpace(element.AliasedType)
	extendedType := strings.TrimSpace(element.ExtendedType)
	if name == "" {
		return ""
	}
	if parameters != "" {
		returnType = strings.TrimSpace(returnType)
		if returnType != "" {
			return returnType + " " + name + typeParameters + parameters
		}
		return name + typeParameters + parameters
	}
	if aliasedType != "" {
		return "typedef " + name + " = " + aliasedType
	}
	if extendedType != "" {
		return name + " extends " + extendedType
	}
	return name + typeParameters
}

func analyzerDartKind(kind string) string {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "CLASS", "CLASS_TYPE_ALIAS":
		return "class"
	case "CONSTRUCTOR", "CONSTRUCTOR_INVOCATION":
		return "constructor"
	case "ENUM":
		return "enum"
	case "ENUM_CONSTANT":
		return "enum_member"
	case "EXTENSION":
		return "extension"
	case "EXTENSION_TYPE":
		return "extension_type"
	case "FIELD":
		return "field"
	case "FUNCTION", "FUNCTION_INVOCATION":
		return "function"
	case "FUNCTION_TYPE_ALIAS", "TYPE_ALIAS":
		return "type_alias"
	case "GETTER":
		return "getter"
	case "SETTER":
		return "setter"
	case "LIBRARY", "COMPILATION_UNIT":
		return "module"
	case "LOCAL_VARIABLE":
		return "local_variable"
	case "METHOD":
		return "method"
	case "MIXIN":
		return "mixin"
	case "PARAMETER":
		return "parameter"
	case "PREFIX":
		return "prefix"
	case "TOP_LEVEL_VARIABLE":
		return "variable"
	case "TYPE_PARAMETER":
		return "type_parameter"
	default:
		return "symbol"
	}
}

func appendDartAnalyzerOverrides(events []analyzerMessage, root string, contents map[string][]byte, byURI map[string][]facts.SemanticSymbol, symbols []facts.SemanticSymbol, selectionOffsets map[string]int, payload *facts.SemanticPayload) {
	byLocation := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		path := filepath.Join(root, filepath.FromSlash(symbol.File))
		byLocation[locationKey(path, selectionOffsets[symbol.ID])] = symbol.ID
	}
	seen := make(map[string]struct{})
	for _, event := range events {
		if event.Event != "analysis.overrides" {
			continue
		}
		var params struct {
			File      string             `json:"file"`
			Overrides []analyzerOverride `json:"overrides"`
		}
		if json.Unmarshal(event.Params, &params) != nil {
			continue
		}
		sourcePath := filepath.Clean(params.File)
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(root, sourcePath)
		}
		data, ok := contents[sourcePath]
		if !ok {
			continue
		}
		for _, override := range params.Overrides {
			sourceOffset := analyzerOffset(data, override.Offset)
			source := enclosing(byURI[fileURI(sourcePath)], positionAt(data, sourceOffset))
			if source.ID == "" {
				continue
			}
			members := make([]analyzerOverriddenMember, 0, 1+len(override.InterfaceMembers))
			if override.SuperclassMember != nil {
				members = append(members, *override.SuperclassMember)
			}
			members = append(members, override.InterfaceMembers...)
			for _, member := range members {
				if member.Element.Location == nil {
					continue
				}
				targetPath := filepath.Clean(member.Element.Location.File)
				if !filepath.IsAbs(targetPath) {
					targetPath = filepath.Join(root, targetPath)
				}
				targetOffset := member.Element.Location.Offset
				if targetData := contents[targetPath]; len(targetData) > 0 {
					targetOffset = analyzerOffset(targetData, targetOffset)
				}
				targetID := byLocation[locationKey(targetPath, targetOffset)]
				if targetID == "" || targetID == source.ID {
					continue
				}
				key := fmt.Sprintf("%s\x00%s\x00%d", source.ID, targetID, sourceOffset)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				startOffset := analyzerOffset(data, override.Offset)
				endOffset := analyzerOffset(data, override.Offset+override.Length)
				start := positionAt(data, startOffset)
				end := positionAt(data, endOffset)
				payload.References = append(payload.References, facts.SemanticReference{
					File: relative(root, sourcePath), SourceID: source.ID, TargetID: targetID,
					Kind: "OVERRIDES", StartLine: start.Line + 1, StartColumn: start.Character,
					Start: startOffset, EndLine: end.Line + 1, EndColumn: end.Character,
					End: endOffset, Text: sliceLine(data, start),
				})
			}
		}
	}
}

// initialize announces hierarchical document symbols because flattenSymbols
// needs the DocumentSymbol shape: a SymbolInformation carries a range that
// covers only the identifier, so no declaration ever contains a reference made
// inside its body and enclosing() answers with the file's module instead. That
// published `EXTENDS models.dart -> Vehicle` for `class ElectricVehicle extends
// Vehicle` -- an EXACT edge naming the wrong source.
func (c *client) initialize(ctx context.Context, root string) error {
	_, err := c.call(ctx, "initialize", map[string]any{"processId": nil, "rootUri": fileURI(root), "workspaceFolders": []any{map[string]any{"uri": fileURI(root), "name": filepath.Base(root)}}, "capabilities": map[string]any{"workspace": map[string]any{"workspaceFolders": true}, "textDocument": map[string]any{"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true}}}})
	if err != nil {
		return err
	}
	c.notify("initialized", map[string]any{})
	return nil
}

func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.next
	c.next++
	c.mu.Unlock()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case message, ok := <-c.out:
			if !ok {
				return nil, fmt.Errorf("Dart analysis server closed")
			}
			if string(message.ID) != strconv.Itoa(id) {
				continue
			}
			if len(message.Error) != 0 && string(message.Error) != "null" {
				return nil, fmt.Errorf("Dart LSP %s failed: %s", method, message.Error)
			}
			return message.Result, nil
		}
	}
}

func (c *client) notify(method string, params any) {
	_ = c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (c *client) send(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}
func (c *client) close() {
	_ = c.send(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": "shutdown", "params": nil})
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

func readMessages(reader io.Reader, output chan<- rpcMessage) {
	defer close(output)
	buffered := bufio.NewReader(reader)
	for {
		length := 0
		for {
			line, err := buffered.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				length, _ = strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			}
		}
		if length <= 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(buffered, body); err != nil {
			return
		}
		var message rpcMessage
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			message = rpcMessage{ID: envelope.ID, Method: envelope.Method, Result: envelope.Result, Error: envelope.Error}
			output <- message
		}
	}
}

func flattenSymbols(uri string, rows []documentSymbol, prefix, root string, contents map[string][]byte, payload *facts.SemanticPayload, byID map[string]facts.SemanticSymbol, byURI *map[string][]facts.SemanticSymbol, selectionOffsets map[string]int) {
	path, err := uriPath(uri)
	if err != nil {
		return
	}
	relativePath := relative(root, path)
	for _, row := range rows {
		full := row.Range
		if full.End.Line == 0 && full.Start.Line == 0 {
			full = row.Location.Range
		}
		selection := row.SelectionRange
		if selection.End.Line == 0 && selection.Start.Line == 0 {
			selection = full
		}
		qualified := row.Name
		if prefix != "" {
			qualified = prefix + "." + row.Name
		} else if row.ContainerName != "" {
			qualified = row.ContainerName + "." + row.Name
		}
		kind := dartKind(row.Kind)
		id := fmt.Sprintf("%s\x00%s\x00%s\x00%d:%d", relativePath, qualified, kind, selection.Start.Line, selection.Start.Character)
		data := contents[path]
		signature := row.Detail
		if strings.TrimSpace(signature) == "" {
			signature = declarationSignature(data, selection.Start)
		}
		symbol := facts.SemanticSymbol{ID: id, File: relativePath, Name: row.Name, QualifiedName: qualified, Kind: kind, Exported: !strings.HasPrefix(row.Name, "_"), Signature: signature, StartLine: full.Start.Line + 1, StartColumn: full.Start.Character, Start: offsetAt(data, full.Start), EndLine: full.End.Line + 1, EndColumn: full.End.Character, End: offsetAt(data, full.End)}
		byID[id] = symbol
		selectionOffsets[id] = offsetAt(data, selection.Start)
		(*byURI)[uri] = append((*byURI)[uri], symbol)
		flattenSymbols(uri, row.Children, qualified, root, contents, payload, byID, byURI, selectionOffsets)
	}
}

func enclosing(symbols []facts.SemanticSymbol, point position) facts.SemanticSymbol {
	var best facts.SemanticSymbol
	var fallback facts.SemanticSymbol
	for _, symbol := range symbols {
		if beforePosition(point, position{Line: symbol.StartLine - 1, Character: symbol.StartColumn}) {
			continue
		}
		if fallback.ID == "" || afterPosition(position{Line: symbol.StartLine - 1, Character: symbol.StartColumn}, position{Line: fallback.StartLine - 1, Character: fallback.StartColumn}) {
			fallback = symbol
		}
		if point.Line+1 < symbol.StartLine || point.Line+1 > symbol.EndLine {
			continue
		}
		if point.Line+1 == symbol.StartLine && point.Character < symbol.StartColumn {
			continue
		}
		if point.Line+1 == symbol.EndLine && point.Character > symbol.EndColumn {
			continue
		}
		if best.ID == "" || (symbol.EndLine-symbol.StartLine) < (best.EndLine-best.StartLine) {
			best = symbol
		}
	}
	if best.ID == "" {
		return fallback
	}
	return best
}

func beforePosition(left, right position) bool {
	return left.Line < right.Line || (left.Line == right.Line && left.Character < right.Character)
}

func afterPosition(left, right position) bool {
	return right.Line < left.Line || (right.Line == left.Line && right.Character < left.Character)
}
func dartKind(kind int) string {
	switch kind {
	case 1:
		return "file"
	case 2, 4:
		return "module"
	case 3:
		// The Dart LSP reports an extension type as a Namespace. Calling that a
		// module let a declaration compete with its own file for the file's
		// module identity, and the last one by identity won: a `part` directive
		// then published PART_OF pointing at the extension type instead of the
		// library. Reproduced with a library under `src/`, where the name orders
		// after `module`.
		return "extension"
	case 5:
		return "class"
	case 6:
		return "method"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 14:
		return "constant"
	case 15:
		return "string"
	case 16:
		return "number"
	case 17:
		return "boolean"
	case 18:
		return "array"
	case 19:
		return "object"
	case 20:
		return "key"
	case 21:
		return "null"
	case 8:
		return "field"
	case 7:
		return "property"
	case 22:
		return "enum_member"
	case 23:
		return "struct"
	case 24:
		return "event"
	case 25:
		return "operator"
	case 26:
		return "type_parameter"
	default:
		return "symbol"
	}
}
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func uriPath(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(path), nil
}
func relative(root, path string) string {
	value, _ := filepath.Rel(root, path)
	return filepath.ToSlash(value)
}
func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func providerForPath(path string, providers []workspace.Repository) (workspace.Repository, bool) {
	path, err := filepath.Abs(path)
	if err != nil {
		return workspace.Repository{}, false
	}
	var selected workspace.Repository
	longest := -1
	for _, provider := range providers {
		root := provider.RealPath
		if root == "" {
			root = provider.Path
		}
		root, err = filepath.Abs(root)
		if err != nil || !pathWithin(root, path) {
			continue
		}
		if len(root) > longest {
			selected = provider
			longest = len(root)
		}
	}
	return selected, longest >= 0
}

func externalDartTarget(ctx context.Context, server *client, path string, offset int, provider workspace.Repository) *facts.SemanticTarget {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	result, err := server.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}})
	if err != nil || len(result) == 0 || string(result) == "null" {
		return nil
	}
	var rows []documentSymbol
	if json.Unmarshal(result, &rows) != nil {
		return nil
	}
	root := provider.RealPath
	if root == "" {
		root = provider.Path
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil
	}
	contents := map[string][]byte{path: data}
	uri := fileURI(path)
	byURI := map[string][]facts.SemanticSymbol{}
	byID := map[string]facts.SemanticSymbol{}
	selectionOffsets := map[string]int{}
	var temporary facts.SemanticPayload
	flattenSymbols(uri, rows, "", root, contents, &temporary, byID, &byURI, selectionOffsets)
	symbol := enclosing(byURI[uri], positionAt(data, offset))
	if symbol.ID == "" {
		return nil
	}
	return &facts.SemanticTarget{Repository: provider.Name, Package: dartPackageName(root), File: relative(root, path), QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Signature: symbol.Signature, Source: "PROVIDER_SOURCE"}
}

func sliceLine(data []byte, point position) string {
	lines := bytes.Split(data, []byte("\n"))
	if point.Line < 0 || point.Line >= len(lines) {
		return ""
	}
	return string(lines[point.Line])
}

func declarationSignature(data []byte, point position) string {
	line := strings.TrimSpace(sliceLine(data, point))
	for _, delimiter := range []string{"{", "=>", ";"} {
		if index := strings.Index(line, delimiter); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
	}
	if line == "" {
		return "dart declaration"
	}
	return line
}

func offsetAt(data []byte, point position) int {
	line := 0
	offset := 0
	for offset < len(data) && line < point.Line {
		_, width := utf8.DecodeRune(data[offset:])
		if width == 0 {
			width = 1
		}
		if data[offset] == '\n' {
			line++
		}
		offset += width
	}
	return min(offset+utf16ByteOffset(data[offset:], point.Character), len(data))
}
func callAt(data []byte, point position) bool {
	lines := bytes.Split(data, []byte("\n"))
	if point.Line < 0 || point.Line >= len(lines) {
		return false
	}
	line := lines[point.Line]
	column := utf16ByteOffset(line, point.Character)
	rest := line[min(column, len(line)):]
	return bytes.HasPrefix(bytes.TrimSpace(rest), []byte("("))
}
func callAtOffset(data []byte, offset int) bool {
	return callAt(data, positionAt(data, offset))
}

// dartReferenceKind keeps the analyzer-resolved target and only classifies the
// syntactic relation around the already-resolved occurrence. This is the same
// evidence rule used by the Go and Rust adapters: syntax chooses the edge kind,
// never the identity of its target.
func dartReferenceKind(data []byte, offset, length int, targetKind string) string {
	point := positionAt(data, offset)
	line := sliceLine(data, point)
	byteCharacter := utf16ByteOffset([]byte(line), point.Character)
	prefix := strings.TrimSpace(line[:byteCharacter])
	bestIndex := -1
	bestKind := ""
	for _, relation := range []struct {
		keyword string
		kind    string
	}{
		{keyword: "implements", kind: "IMPLEMENTS"},
		{keyword: "extends", kind: "EXTENDS"},
		{keyword: "with", kind: "EMBEDS"},
	} {
		if index := strings.LastIndex(prefix, relation.keyword); index >= 0 {
			before := index == 0 || !isIdentifierPart(prefix[index-1])
			after := index+len(relation.keyword) == len(prefix) || !isIdentifierPart(prefix[index+len(relation.keyword)])
			if before && after && index > bestIndex {
				bestIndex = index
				bestKind = relation.kind
			}
		}
	}
	if bestKind != "" {
		return bestKind
	}
	if callAtOffset(data, offset+length) {
		return "CALLS_DIRECT"
	}
	prefix = strings.TrimSpace(line[:point.Character])
	// A member access means the occurrence is not what the expression yields:
	// `=> value.toString()` returns the string, and `value` is only read.
	memberAccess := strings.HasPrefix(strings.TrimSpace(line[byteCharacter+min(length, len(line)-byteCharacter):]), ".")
	// An arrow body is a return: `Runner build() => handler` hands back the
	// function exactly as `return handler` does, and only the keyword was
	// recognised.
	if !memberAccess && (prefix == "return" || strings.HasPrefix(prefix, "return ") || strings.Contains(prefix, " return ") || strings.HasSuffix(prefix, "=>")) {
		if isFunctionLikeDartTarget(targetKind) {
			return "RETURNS_FUNCTION"
		}
	}
	if isFunctionLikeDartTarget(targetKind) {
		suffix := strings.TrimSpace(line[min(byteCharacter+length, len(line)):])
		// An operand of a comparison is neither assigned nor passed: the value
		// that travels on is the boolean, and the occurrence is only read.
		// `final same = (other == handler)` assigns the comparison, and
		// `register(other == handler)` passes it.
		if comparedInDartPrefix(prefix) {
			return "REFERENCES"
		}
		if assignsInPrefix(prefix) {
			return "ASSIGNS_FUNCTION"
		}
		if strings.HasPrefix(suffix, ",") || strings.HasPrefix(suffix, ")") || strings.HasPrefix(suffix, "]") {
			if opensDartArgument(prefix) {
				return "PASSES_AS_CALLBACK"
			}
		}
	}
	rest := line[byteCharacter+min(length, len(line)-byteCharacter):]
	trailing := strings.TrimSpace(rest)
	if strings.HasPrefix(trailing, ".") {
		return "REFERENCES"
	}
	if isTypeLikeDartTarget(targetKind) {
		return "TYPE_USES"
	}
	// The Analysis Server answers UNKNOWN for an enum, a mixin and an
	// extension type used as a type, so the kind cannot classify the relation
	// and the position has to. `VehicleKind kind` is a declaration: the
	// occurrence is a type annotation and the name that follows is what it
	// annotates. The reference itself is already resolved; this only decides
	// which relation it is, which is what every other branch here does too.
	if targetKind == "" || strings.EqualFold(strings.TrimSpace(targetKind), "UNKNOWN") {
		if annotatesDeclaration(rest) {
			return "TYPE_USES"
		}
	}
	return "REFERENCES"
}

// assignsInPrefix reports whether the text before an occurrence ends in a plain
// assignment. An arrow body (`=>`), a comparison and a compound operator all
// carry an `=` that is not one: `int get value => _value` was published as
// ASSIGNS_FUNCTION for a getter read.
func assignsInPrefix(prefix string) bool {
	for index := len(prefix) - 1; index >= 0; index-- {
		if prefix[index] != '=' {
			continue
		}
		if index+1 < len(prefix) && (prefix[index+1] == '=' || prefix[index+1] == '>') {
			continue
		}
		if index > 0 && strings.IndexByte("!<>=+-*/%&|^~", prefix[index-1]) >= 0 {
			continue
		}
		return true
	}
	return false
}

// comparedInDartPrefix reports whether the text immediately before an
// occurrence is a comparison operator, which makes the occurrence an operand.
// What travels on is the boolean, so the function is only read: neither
// assigned nor passed, whatever the brackets around it look like.
func comparedInDartPrefix(prefix string) bool {
	trimmed := strings.TrimRight(prefix, " \t")
	if trimmed == "" {
		return false
	}
	for _, operator := range []string{"==", "!=", ">=", "<="} {
		if strings.HasSuffix(trimmed, operator) {
			return true
		}
	}
	// A bare `<` or `>` is a comparison only when it is not the tail of an
	// arrow, a shift or a compound operator. Generic arguments cannot reach
	// here: this branch only runs for a function-like target.
	last := trimmed[len(trimmed)-1]
	if last != '<' && last != '>' {
		return false
	}
	if len(trimmed) == 1 {
		return true
	}
	return strings.IndexByte("=<>-", trimmed[len(trimmed)-2]) < 0
}

// dartControlKeywords open a parenthesis that is not an argument list. `assert`
// is deliberately absent: it takes arguments like any call.
var dartControlKeywords = map[string]struct{}{
	"if": {}, "while": {}, "for": {}, "switch": {}, "catch": {}, "do": {},
}

// opensDartArgument reports whether the innermost unclosed bracket of prefix
// opens an argument list or a collection literal, rather than grouping an
// expression or carrying a control-flow subject.
//
// The old test was `prefix contains a bracket`, which every `if (...)` and
// every parenthesised expression satisfies. An argument list is opened by a
// callee, so what decides is the identifier immediately before the bracket:
// a control keyword or nothing at all is not one.
func opensDartArgument(prefix string) bool {
	depth := 0
	for index := len(prefix) - 1; index >= 0; index-- {
		switch prefix[index] {
		case ')', ']', '}':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			return calleePrecedesDartBracket(prefix[:index])
		case '[', '{':
			if depth > 0 {
				depth--
				continue
			}
			// A collection literal holding a function is passing it on, and a
			// subscript is reading through one; both keep the old reading.
			return true
		}
	}
	return false
}

// calleePrecedesDartBracket reports whether text ends in a name that can be
// called. A member access counts -- `registry.add(` is a call -- and a control
// keyword does not.
func calleePrecedesDartBracket(text string) bool {
	trimmed := strings.TrimRight(text, " \t")
	end := len(trimmed)
	for end > 0 && isIdentifierPart(trimmed[end-1]) {
		end--
	}
	name := trimmed[end:]
	if name == "" {
		return false
	}
	_, isControl := dartControlKeywords[name]
	return !isControl
}

// annotatesDeclaration reports whether what follows an occurrence is the name
// it annotates, after any generic arguments and nullability marker.
func annotatesDeclaration(trailing string) bool {
	if depth := 0; strings.HasPrefix(trailing, "<") {
		for index := 0; index < len(trailing); index++ {
			switch trailing[index] {
			case '<':
				depth++
			case '>':
				depth--
				if depth == 0 {
					trailing = trailing[index+1:]
					index = len(trailing)
				}
			}
		}
	}
	trailing = strings.TrimPrefix(trailing, "?")
	if trailing == "" || !isSpace(trailing[0]) {
		return false
	}
	trailing = strings.TrimLeft(trailing, " \t")
	return trailing != "" && (trailing[0] == '_' || trailing[0] >= 'a' && trailing[0] <= 'z' || trailing[0] >= 'A' && trailing[0] <= 'Z')
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func isFunctionLikeDartTarget(kind string) bool {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	return strings.Contains(kind, "FUNCTION") || strings.Contains(kind, "METHOD") || strings.Contains(kind, "GETTER") || strings.Contains(kind, "SETTER") || strings.Contains(kind, "CONSTRUCTOR")
}

func isTypeLikeDartTarget(kind string) bool {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	return strings.Contains(kind, "CLASS") || strings.Contains(kind, "ENUM") || strings.Contains(kind, "MIXIN") || strings.Contains(kind, "TYPE_ALIAS") || strings.Contains(kind, "EXTENSION_TYPE")
}

func isIdentifierPart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
func positionAt(data []byte, offset int) position {
	offset = min(max(offset, 0), len(data))
	line, column := 0, 0
	for index := 0; index < offset; {
		value, width := utf8.DecodeRune(data[index:offset])
		if width == 0 {
			width = 1
		}
		if value == '\n' {
			line++
			column = 0
		} else {
			column += utf16Width(value)
		}
		index += width
	}
	return position{Line: line, Character: column}
}

// analyzerOffset converts the Dart Analysis Server's UTF-16 code-unit offset
// into the byte offset used by Go strings and the canonical payload.
func analyzerOffset(data []byte, codeUnits int) int {
	return utf16ByteOffset(data, codeUnits)
}

func utf16ByteOffset(data []byte, codeUnits int) int {
	if codeUnits <= 0 {
		return 0
	}
	units := 0
	for index := 0; index < len(data); {
		value, width := utf8.DecodeRune(data[index:])
		if width == 0 {
			width = 1
		}
		widthInUTF16 := utf16Width(value)
		if units+widthInUTF16 > codeUnits {
			return index
		}
		units += widthInUTF16
		index += width
		if units == codeUnits {
			return index
		}
	}
	return len(data)
}

func utf16Width(value rune) int {
	if value > 0xffff {
		return 2
	}
	return 1
}
func locationKey(path string, offset int) string {
	return filepath.Clean(path) + "\x00" + strconv.Itoa(offset)
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func isGenerated(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".g.dart") || strings.HasSuffix(base, ".freezed.dart") || strings.HasSuffix(base, ".gr.dart") || strings.HasSuffix(base, ".config.dart") || strings.HasSuffix(base, ".mocks.dart") {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		if part == "gen" || part == "generated" {
			return true
		}
	}
	return false
}

func resolveDartImport(root, sourcePath, requested string, modules map[string]string, packageName string) string {
	var relativePath string
	switch {
	case strings.HasPrefix(requested, "dart:"):
		return ""
	case strings.HasPrefix(requested, "package:"):
		prefix := "package:" + packageName + "/"
		if !strings.HasPrefix(requested, prefix) {
			return ""
		}
		relativePath = filepath.ToSlash(filepath.Join("lib", strings.TrimPrefix(requested, prefix)))
	default:
		relativePath = relative(root, filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(requested)))
	}
	relativePath = filepath.ToSlash(filepath.Clean(relativePath))
	return modules[relativePath]
}

func dartRelativeFile(root, sourcePath, requested string) string {
	if strings.HasPrefix(requested, "dart:") || strings.HasPrefix(requested, "package:") {
		return ""
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(requested)))
	if !pathWithin(root, path) {
		return ""
	}
	return relative(root, path)
}

func dartFiles(root string, includeGenerated, includeTests bool) ([]string, error) {
	return dartFilesForRepository(root, nil, nil, includeGenerated, includeTests)
}

func dartFilesForRepository(root string, roots, exclusions []string, includeGenerated, includeTests bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".dart_tool", "build", ".idea":
				return filepath.SkipDir
			}
			if pathExcluded(root, path, exclusions) || !pathInRoots(root, path, roots) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".dart" || pathExcluded(root, path, exclusions) || !pathInRoots(root, path, roots) || (!includeGenerated && isGenerated(path)) || (!includeTests && isTestPath(root, path)) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func pathInRoots(root, path string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	rel := filepath.ToSlash(relative(root, path))
	for _, configured := range roots {
		configured = strings.Trim(strings.TrimSpace(filepath.ToSlash(configured)), "/")
		if configured == "" || configured == "." || rel == configured || strings.HasPrefix(rel, configured+"/") {
			return true
		}
	}
	return false
}

func pathExcluded(root, path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	rel := filepath.ToSlash(relative(root, path))
	for _, raw := range patterns {
		pattern := strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/")
		if pattern == "" {
			continue
		}
		if rel == pattern || strings.HasPrefix(rel, pattern+"/") {
			return true
		}
		if strings.HasPrefix(pattern, "**/") {
			pattern = strings.TrimPrefix(pattern, "**/")
		}
		if matched, _ := filepath.Match(pattern, rel); matched {
			return true
		}
		parts := strings.Split(rel, "/")
		for index := range parts {
			if matched, _ := filepath.Match(pattern, strings.Join(parts[index:], "/")); matched {
				return true
			}
		}
	}
	return false
}
func isTestPath(root, path string) bool {
	rel := filepath.ToSlash(relative(root, path))
	return rel == "test" || strings.HasPrefix(rel, "test/") || rel == "integration_test" || strings.HasPrefix(rel, "integration_test/")
}
func dartPackageName(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	if err != nil {
		return filepath.Base(root)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(fields) == 2 && fields[0] == "name" {
			return strings.TrimSpace(fields[1])
		}
	}
	return filepath.Base(root)
}

func dartLibraryName(data []byte) string {
	match := regexp.MustCompile(`(?m)^\s*library\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)\s*;`).FindSubmatch(data)
	if len(match) != 2 {
		return ""
	}
	return string(match[1])
}

// dartPackageRoots reads pub's package configuration when external package
// analysis is enabled. The Analysis Server can then resolve package: URIs
// against the same package roots as `dart analyze`, while Kivgraph still
// publishes only the registered repository's files.
func dartPackageRoots(root, configured string, includeExternal bool) []string {
	if !includeExternal {
		return nil
	}
	seen := map[string]struct{}{filepath.Clean(root): {}}
	entries := dartPackageEntries(root, configured)
	roots := make([]string, 0, len(entries))
	for _, pkg := range entries {
		path := pkg.Root
		if _, exists := seen[path]; exists {
			continue
		}
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			continue
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	return roots
}

type dartPackageEntry struct {
	Name string
	Root string
}

func dartPackageEntries(root, configured string) []dartPackageEntry {
	configPath := strings.TrimSpace(configured)
	if configPath == "" || configPath == "auto" {
		configPath = filepath.Join(root, ".dart_tool", "package_config.json")
	} else if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(root, configPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var config struct {
		Packages []struct {
			Name    string `json:"name"`
			RootURI string `json:"rootUri"`
		} `json:"packages"`
	}
	if json.Unmarshal(data, &config) != nil {
		return nil
	}
	configDir := filepath.Dir(configPath)
	entries := make([]dartPackageEntry, 0, len(config.Packages))
	seen := make(map[string]struct{}, len(config.Packages))
	for _, pkg := range config.Packages {
		uri := strings.TrimSpace(pkg.RootURI)
		if uri == "" {
			continue
		}
		path := uri
		if strings.HasPrefix(uri, "file:") {
			parsed, parseErr := uriPath(uri)
			if parseErr != nil {
				continue
			}
			path = parsed
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, filepath.FromSlash(path))
		}
		path, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			continue
		}
		seen[path] = struct{}{}
		entries = append(entries, dartPackageEntry{Name: strings.TrimSpace(pkg.Name), Root: path})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Root < entries[j].Root })
	return entries
}

// ExternalPackageRepositories turns the package roots resolved by Pub into
// explicit providers. They are only returned when the caller opts in; the
// main repository remains the only default unit and package-cache source is
// never silently added to a graph.
func ExternalPackageRepositories(root, configured string) []workspace.Repository {
	entries := dartPackageEntries(root, configured)
	providers := make([]workspace.Repository, 0, len(entries))
	for _, entry := range entries {
		if entry.Root == root {
			continue
		}
		digest := sha256.Sum256([]byte(entry.Root))
		name := fmt.Sprintf("dart-package:%s:%x", entry.Name, digest[:4])
		providers = append(providers, workspace.Repository{Name: name, Path: entry.Root, RealPath: entry.Root, Languages: []string{"dart"}, Roots: []string{"lib"}})
	}
	return providers
}
