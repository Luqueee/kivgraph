package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parsedTypeScriptConfig is one tsconfig resolved through its own "extends"
// chain: effective compiler options and file lists, ready for source
// resolution.
type parsedTypeScriptConfig struct {
	// ConfigPath is the absolute, clean path to this tsconfig file.
	ConfigPath string
	// Directory is filepath.Dir(ConfigPath).
	Directory string
	// ExtendsChain lists the resolved parent config paths, nearest parent
	// first. It is empty when the config extends nothing.
	ExtendsChain []string
	// CompilerOptions are the effective options after applying the extends
	// chain, with path valued options rebased to absolute paths.
	CompilerOptions map[string]any
	// Files are the absolutized entries of "files"; nil when it was not
	// declared anywhere in the resolved chain.
	Files []string
	// Include are the absolutized patterns of "include"; nil when it was not
	// declared anywhere in the resolved chain.
	Include []string
	// Exclude are the absolutized patterns of "exclude"; nil when it was not
	// declared anywhere in the resolved chain.
	Exclude []string
	// HasFiles reports whether "files" was present, even if declared empty.
	HasFiles bool
	// HasInclude reports whether "include" was present, even if declared
	// empty.
	HasInclude bool
	// HasExclude reports whether "exclude" was present, even if declared
	// empty.
	HasExclude bool
}

// resolveTypeScriptConfig reads the tsconfig at configPath and resolves its
// "extends" chain into one effective, fully merged configuration.
//
// configPath must be absolute and must lie within repositoryRoot; a request
// with a relative path, or one that names a config outside the repository,
// is rejected before anything is read.
func resolveTypeScriptConfig(configPath, repositoryRoot string) (parsedTypeScriptConfig, error) {
	if !filepath.IsAbs(configPath) {
		return parsedTypeScriptConfig{}, fmt.Errorf("tsconfig path %q must be absolute", configPath)
	}
	cleanRepositoryRoot := filepath.Clean(repositoryRoot)
	cleanConfigPath := filepath.Clean(configPath)
	if !pathWithin(cleanRepositoryRoot, cleanConfigPath) {
		return parsedTypeScriptConfig{}, fmt.Errorf("tsconfig path %q escapes repository root %q", cleanConfigPath, cleanRepositoryRoot)
	}

	memo := make(map[string]resolvedTypeScriptAncestor)
	resolved, err := resolveTypeScriptConfigAncestor(cleanConfigPath, cleanRepositoryRoot, nil, memo)
	if err != nil {
		return parsedTypeScriptConfig{}, err
	}

	return parsedTypeScriptConfig{
		ConfigPath:      cleanConfigPath,
		Directory:       filepath.Dir(cleanConfigPath),
		ExtendsChain:    resolved.chain,
		CompilerOptions: resolved.compilerOptions,
		Files:           resolved.files,
		Include:         resolved.include,
		Exclude:         resolved.exclude,
		HasFiles:        resolved.hasFiles,
		HasInclude:      resolved.hasInclude,
		HasExclude:      resolved.hasExclude,
	}, nil
}

// cloneCompilerOptions returns a deep copy of options: nested maps and
// slices are cloned so the result shares no mutable state with the input,
// while scalars are copied by value. It returns nil for a nil input.
func cloneCompilerOptions(options map[string]any) map[string]any {
	if options == nil {
		return nil
	}
	clone := make(map[string]any, len(options))
	for key, value := range options {
		clone[key] = cloneCompilerOptionValue(value)
	}
	return clone
}

// cloneCompilerOptionValue deep clones one JSON-decoded value. A nested
// object (map[string]any) or array ([]any) is cloned recursively; every
// other value encoding/json can produce (string, float64, bool, nil) is
// already immutable and is returned as-is.
func cloneCompilerOptionValue(value any) any {
	switch typedValue := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typedValue))
		for key, nestedValue := range typedValue {
			clone[key] = cloneCompilerOptionValue(nestedValue)
		}
		return clone
	case []any:
		clone := make([]any, len(typedValue))
		for index, nestedValue := range typedValue {
			clone[index] = cloneCompilerOptionValue(nestedValue)
		}
		return clone
	default:
		return typedValue
	}
}

// resolvedTypeScriptAncestor is the fully merged effective state of one
// tsconfig, including everything inherited through its own "extends" chain.
type resolvedTypeScriptAncestor struct {
	compilerOptions map[string]any
	files           []string
	include         []string
	exclude         []string
	hasFiles        bool
	hasInclude      bool
	hasExclude      bool
	// chain lists this config's own ancestor paths, nearest parent first
	// and free of duplicates.
	chain []string
}

// resolveTypeScriptConfigAncestor resolves configPath and everything it
// extends, merging the whole chain into one effective result.
//
// ancestry holds every config path currently being resolved along the
// current path from the original request down to configPath's declaring
// child; it is how a genuine cycle is told apart from a diamond, where two
// branches legitimately share a common ancestor reached through different
// paths. memo caches the completed result of every config path already
// resolved during this call to resolveTypeScriptConfig, so a shared
// ancestor is loaded and merged only once.
func resolveTypeScriptConfigAncestor(
	configPath, repositoryRoot string,
	ancestry []string,
	memo map[string]resolvedTypeScriptAncestor,
) (resolvedTypeScriptAncestor, error) {
	if cached, ok := memo[configPath]; ok {
		return cached, nil
	}
	for _, ancestorPath := range ancestry {
		if ancestorPath == configPath {
			cycle := append(append([]string(nil), ancestry...), configPath)
			return resolvedTypeScriptAncestor{}, fmt.Errorf("extends cycle detected: %s", strings.Join(cycle, " -> "))
		}
	}

	document, err := loadTypeScriptConfigDocument(configPath, repositoryRoot)
	if err != nil {
		return resolvedTypeScriptAncestor{}, err
	}

	childAncestry := append(append([]string(nil), ancestry...), configPath)
	resolvedParents := make([]resolvedTypeScriptAncestor, len(document.parentPaths))
	for index, parentPath := range document.parentPaths {
		resolvedParent, err := resolveTypeScriptConfigAncestor(parentPath, repositoryRoot, childAncestry, memo)
		if err != nil {
			return resolvedTypeScriptAncestor{}, fmt.Errorf("tsconfig %q: resolve extends %q: %w", configPath, parentPath, err)
		}
		resolvedParents[index] = resolvedParent
	}

	// Parents merge left to right, so the rightmost entry of an "extends"
	// array wins over its siblings; the config's own declarations are
	// applied last and win over every parent, per TypeScript's documented
	// precedence. "files", "include" and "exclude" follow the same
	// left-to-right, child-wins precedence, but each winning declaration
	// replaces the accumulated value outright instead of merging into it.
	mergedCompilerOptions := make(map[string]any, len(document.compilerOptions))
	var mergedFiles, mergedInclude, mergedExclude []string
	var hasFiles, hasInclude, hasExclude bool
	for _, parent := range resolvedParents {
		for key, value := range parent.compilerOptions {
			mergedCompilerOptions[key] = value
		}
		if parent.hasFiles {
			mergedFiles, hasFiles = parent.files, true
		}
		if parent.hasInclude {
			mergedInclude, hasInclude = parent.include, true
		}
		if parent.hasExclude {
			mergedExclude, hasExclude = parent.exclude, true
		}
	}
	for key, value := range document.compilerOptions {
		mergedCompilerOptions[key] = value
	}
	if document.hasFiles {
		mergedFiles, hasFiles = document.files, true
	}
	if document.hasInclude {
		mergedInclude, hasInclude = document.include, true
	}
	if document.hasExclude {
		mergedExclude, hasExclude = document.exclude, true
	}

	// The chain lists ancestors nearest to configPath first. Within one
	// "extends" array the rightmost, highest precedence entry is nearest in
	// effect, so it is listed before its lower precedence siblings; each
	// entry's own ancestors follow it before the next sibling's.
	var chain []string
	for index := len(document.parentPaths) - 1; index >= 0; index-- {
		chain = appendUniqueTypeScriptPath(chain, document.parentPaths[index])
		for _, ancestorPath := range resolvedParents[index].chain {
			chain = appendUniqueTypeScriptPath(chain, ancestorPath)
		}
	}

	resolved := resolvedTypeScriptAncestor{
		compilerOptions: mergedCompilerOptions,
		files:           mergedFiles,
		include:         mergedInclude,
		exclude:         mergedExclude,
		hasFiles:        hasFiles,
		hasInclude:      hasInclude,
		hasExclude:      hasExclude,
		chain:           chain,
	}
	memo[configPath] = resolved
	return resolved, nil
}

// typeScriptConfigDocument is what one tsconfig file declares on its own,
// already rebased to absolute paths against its own directory. It does not
// yet reflect anything inherited through "extends".
type typeScriptConfigDocument struct {
	// parentPaths are the resolved absolute paths of this file's own
	// "extends" entries, left to right as declared.
	parentPaths     []string
	compilerOptions map[string]any
	files           []string
	include         []string
	exclude         []string
	hasFiles        bool
	hasInclude      bool
	hasExclude      bool
}

// rawTypeScriptConfigDocument mirrors the JSON shape of one tsconfig file.
// Files, Include and Exclude stay as raw JSON so their presence in the
// document can be told apart from their absence before they are decoded.
// References is intentionally not modeled here: it is never inherited
// through "extends" and this file never looks at it.
type rawTypeScriptConfigDocument struct {
	Extends         json.RawMessage `json:"extends"`
	CompilerOptions map[string]any  `json:"compilerOptions"`
	Files           json.RawMessage `json:"files"`
	Include         json.RawMessage `json:"include"`
	Exclude         json.RawMessage `json:"exclude"`
}

// loadTypeScriptConfigDocument reads and parses one tsconfig file. It
// resolves this file's own "extends" entries to absolute parent paths and
// rebases every path valued field it declares against its own directory,
// but it does not follow those parents; that is
// resolveTypeScriptConfigAncestor's job.
func loadTypeScriptConfigDocument(configPath, repositoryRoot string) (typeScriptConfigDocument, error) {
	data, err := readTypeScriptManifest(configPath)
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("read tsconfig %q: %w", configPath, err)
	}
	decoded, err := decodeJSONC(data)
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("decode tsconfig %q: %w", configPath, err)
	}
	var raw rawTypeScriptConfigDocument
	if err := json.Unmarshal(decoded, &raw); err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("parse tsconfig %q: %w", configPath, err)
	}

	directory := filepath.Dir(configPath)

	extendsEntries, err := decodeTypeScriptExtendsEntries(raw.Extends)
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("tsconfig %q: %w", configPath, err)
	}
	parentPaths := make([]string, 0, len(extendsEntries))
	for _, entry := range extendsEntries {
		parentPath, err := resolveTypeScriptExtendsEntry(entry, directory, repositoryRoot)
		if err != nil {
			return typeScriptConfigDocument{}, fmt.Errorf("tsconfig %q: %w", configPath, err)
		}
		parentPaths = append(parentPaths, parentPath)
	}

	files, hasFiles, err := decodeTypeScriptStringArrayField(raw.Files, "files")
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("tsconfig %q: %w", configPath, err)
	}
	include, hasInclude, err := decodeTypeScriptStringArrayField(raw.Include, "include")
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("tsconfig %q: %w", configPath, err)
	}
	exclude, hasExclude, err := decodeTypeScriptStringArrayField(raw.Exclude, "exclude")
	if err != nil {
		return typeScriptConfigDocument{}, fmt.Errorf("tsconfig %q: %w", configPath, err)
	}

	return typeScriptConfigDocument{
		parentPaths:     parentPaths,
		compilerOptions: rebaseTypeScriptCompilerOptionPaths(cloneCompilerOptions(raw.CompilerOptions), directory),
		files:           rebaseTypeScriptPathList(files, directory),
		include:         rebaseTypeScriptPathList(include, directory),
		exclude:         rebaseTypeScriptPathList(exclude, directory),
		hasFiles:        hasFiles,
		hasInclude:      hasInclude,
		hasExclude:      hasExclude,
	}, nil
}

// decodeTypeScriptExtendsEntries normalizes the "extends" field, which
// TypeScript accepts either as a single string or, since TypeScript 5.0, as
// an array of strings applied left to right.
func decodeTypeScriptExtendsEntries(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var entries []string
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, fmt.Errorf(`parse "extends" array: %w`, err)
		}
		return entries, nil
	}
	var entry string
	if err := json.Unmarshal(trimmed, &entry); err != nil {
		return nil, fmt.Errorf(`parse "extends": %w`, err)
	}
	if entry == "" {
		return nil, nil
	}
	return []string{entry}, nil
}

// decodeTypeScriptStringArrayField decodes one of "files", "include" or
// "exclude". The second result reports whether the field was present in the
// document at all, even when its value is an empty array or null.
func decodeTypeScriptStringArrayField(raw json.RawMessage, fieldName string) ([]string, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false, fmt.Errorf("%q must be an array of strings: %w", fieldName, err)
	}
	return values, true, nil
}

// resolveTypeScriptExtendsEntry resolves one "extends" entry declared by the
// config file in declaringDirectory to the absolute path of the parent
// tsconfig it names.
func resolveTypeScriptExtendsEntry(entry, declaringDirectory, repositoryRoot string) (string, error) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return "", fmt.Errorf(`"extends" entry must not be empty`)
	}

	if isPathLikeTypeScriptExtendsEntry(trimmed) {
		target := filepath.FromSlash(trimmed)
		if !filepath.IsAbs(target) {
			target = filepath.Join(declaringDirectory, target)
		}
		resolvedPath, err := resolveTypeScriptExtendsTarget(filepath.Clean(target))
		if err != nil {
			return "", fmt.Errorf(`resolve "extends" entry %q: %w`, entry, err)
		}
		if !pathWithin(repositoryRoot, resolvedPath) {
			return "", fmt.Errorf(`"extends" entry %q resolves outside repository root %q`, entry, repositoryRoot)
		}
		return resolvedPath, nil
	}

	// Anything else is a Node module specifier: walk up from the declaring
	// directory to repositoryRoot, inclusive, looking for a node_modules
	// directory that provides it.
	specifier := filepath.FromSlash(trimmed)
	for directory := declaringDirectory; ; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, "node_modules", specifier)
		if resolvedPath, ok := tryResolveTypeScriptExtendsTarget(candidate); ok {
			if !pathWithin(repositoryRoot, resolvedPath) {
				return "", fmt.Errorf(`"extends" entry %q resolves outside repository root %q`, entry, repositoryRoot)
			}
			return resolvedPath, nil
		}
		if directory == repositoryRoot || !isWithinRoot(repositoryRoot, directory) {
			break
		}
	}
	return "", fmt.Errorf(`cannot resolve "extends" entry %q from %q`, entry, declaringDirectory)
}

// isPathLikeTypeScriptExtendsEntry reports whether entry names a relative or
// absolute file, as opposed to a Node module specifier resolved through
// node_modules.
func isPathLikeTypeScriptExtendsEntry(entry string) bool {
	if strings.HasPrefix(entry, "./") || strings.HasPrefix(entry, "../") {
		return true
	}
	return filepath.IsAbs(filepath.FromSlash(entry))
}

// resolveTypeScriptExtendsTarget applies TypeScript's file resolution
// fallbacks to one candidate path and reports an error naming candidate when
// none of them apply.
func resolveTypeScriptExtendsTarget(candidate string) (string, error) {
	if resolvedPath, ok := tryResolveTypeScriptExtendsTarget(candidate); ok {
		return resolvedPath, nil
	}
	return "", fmt.Errorf("%q does not exist", candidate)
}

// tryResolveTypeScriptExtendsTarget resolves one candidate path: a directory
// resolves to the tsconfig.json inside it, and a candidate without a
// ".json" extension falls back to one with it appended.
func tryResolveTypeScriptExtendsTarget(candidate string) (string, bool) {
	if info, err := os.Stat(candidate); err == nil {
		if info.IsDir() {
			inner := filepath.Join(candidate, "tsconfig.json")
			if innerInfo, err := os.Stat(inner); err == nil && innerInfo.Mode().IsRegular() {
				return filepath.Clean(inner), true
			}
			return "", false
		}
		if info.Mode().IsRegular() {
			return filepath.Clean(candidate), true
		}
		return "", false
	}
	if strings.HasSuffix(candidate, ".json") {
		return "", false
	}
	withExtension := candidate + ".json"
	if info, err := os.Stat(withExtension); err == nil && info.Mode().IsRegular() {
		return filepath.Clean(withExtension), true
	}
	return "", false
}

// typeScriptScalarPathCompilerOptions are the compiler options whose value
// is a single path, relative to the tsconfig that declares them.
var typeScriptScalarPathCompilerOptions = []string{
	"outDir", "outFile", "rootDir", "baseUrl", "declarationDir", "tsBuildInfoFile",
}

// typeScriptArrayPathCompilerOptions are the compiler options whose value is
// an array of paths, each relative to the tsconfig that declares them.
var typeScriptArrayPathCompilerOptions = []string{"rootDirs", "typeRoots"}

// rebaseTypeScriptCompilerOptionPaths absolutizes the path valued compiler
// options in options against directory, the directory of the tsconfig that
// declared them. options is mutated in place and returned.
//
// "paths" is deliberately left untouched: TypeScript always resolves its
// entries relative to baseUrl, not to the declaring file, and baseUrl
// itself is already absolutized here, so rewriting "paths" too would rebase
// it a second time.
func rebaseTypeScriptCompilerOptionPaths(options map[string]any, directory string) map[string]any {
	if options == nil {
		return nil
	}
	for _, name := range typeScriptScalarPathCompilerOptions {
		value, ok := options[name].(string)
		if !ok || value == "" {
			continue
		}
		options[name] = absolutizeTypeScriptConfigPath(directory, value)
	}
	for _, name := range typeScriptArrayPathCompilerOptions {
		values, ok := options[name].([]any)
		if !ok {
			continue
		}
		rebasedValues := make([]any, len(values))
		for index, value := range values {
			text, ok := value.(string)
			if !ok {
				rebasedValues[index] = value
				continue
			}
			rebasedValues[index] = absolutizeTypeScriptConfigPath(directory, text)
		}
		options[name] = rebasedValues
	}
	return options
}

// rebaseTypeScriptPathList absolutizes every entry of a "files", "include"
// or "exclude" list against directory, the directory of the tsconfig that
// declared it. It returns nil when entries is nil, so a field that was
// never declared stays distinguishable from one absolutized to nothing.
func rebaseTypeScriptPathList(entries []string, directory string) []string {
	if entries == nil {
		return nil
	}
	rebasedEntries := make([]string, len(entries))
	for index, entry := range entries {
		rebasedEntries[index] = absolutizeTypeScriptConfigPath(directory, entry)
	}
	return rebasedEntries
}

// absolutizeTypeScriptConfigPath rebases one path or glob pattern declared
// in directory to an absolute path. filepath.Join, and the filepath.Clean
// it applies internally, only manipulate "." and ".." path segments and
// collapse redundant separators; neither interprets glob syntax, so "*",
// "?" and "**" survive untouched, e.g. Join("/repo/pkg", "src/**/*.ts")
// yields "/repo/pkg/src/**/*.ts".
func absolutizeTypeScriptConfigPath(directory, entry string) string {
	target := filepath.FromSlash(entry)
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(directory, target)
}
