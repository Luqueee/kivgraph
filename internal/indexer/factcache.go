package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/goworkspace"
)

// CacheMode selects what the fact cache does during a pass.
type CacheMode string

const (
	// CacheOff analyses every unit. No entry is read and none is written.
	CacheOff CacheMode = "off"
	// CacheOn serves a unit from its entry when every input it recorded
	// still has the same fingerprint.
	CacheOn CacheMode = "on"
	// CacheVerify analyses every unit and, when an entry would have been
	// served, compares it against what the analysis just produced. The
	// fresh facts are always the ones published: verification costs the
	// whole pass and buys the proof that the cache does not lie.
	CacheVerify CacheMode = "verify"
)

// ValidCacheMode reports whether mode is one this package implements.
func ValidCacheMode(mode CacheMode) bool {
	switch mode {
	case CacheOff, CacheOn, CacheVerify:
		return true
	default:
		return false
	}
}

// cacheEntryVersion is the format of a stored entry. An entry written by an
// older format is not read: the fields it lacks are inputs nobody validated.
const cacheEntryVersion = 1

// ErrCacheDiverged reports that a cached entry did not match the facts the
// analysis produced for the same unit. It aborts the pass on purpose: a cache
// that answers with facts nobody can reproduce is worse than no cache, because
// the graph it publishes looks exactly like a correct one.
var ErrCacheDiverged = errors.New("fact cache diverged from the analysis")

// inputKind names how an input is fingerprinted, so an entry describes how to
// re-check itself instead of relying on the reader to remember.
type inputKind string

const (
	// inputTree is every source file under a directory, by path and content.
	inputTree inputKind = "tree"
	// inputFile is one file's content; absent is a fingerprint of its own.
	inputFile inputKind = "file"
	// inputProvider is the package that answers a name, and its content.
	// A name nobody provides fingerprints as absent, so a provider that
	// appears later invalidates the entry that resolved without it.
	inputProvider inputKind = "provider"
	// inputRegistry is the map of module paths to the repositories that
	// provide them: what decides whether a reference leaves the repository,
	// and which repository its edges name. Its name says which registry,
	// never what the registry said: an input whose recorded name carries
	// its own value compares against itself and can never invalidate
	// anything.
	inputRegistry inputKind = "registry"

	// goRegistryInput is the only registry there is today.
	goRegistryInput = "go"
)

// cacheInput is one thing a unit read, and what it looked like.
type cacheInput struct {
	Kind        inputKind `json:"kind"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
}

// cacheEntry is the facts of one unit together with every input that produced
// them. Nothing here is a timestamp: an entry is valid exactly while the
// fingerprints it recorded still describe the world.
type cacheEntry struct {
	Version  int          `json:"version"`
	Unit     string       `json:"unit"`
	Analyzer string       `json:"analyzer"`
	Inputs   []cacheInput `json:"inputs"`

	Set             facts.Set `json:"set"`
	NotLoaded       bool      `json:"not_loaded"`
	LoadDiagnostics int       `json:"load_diagnostics"`
	Definitions     int       `json:"definitions"`
	References      int       `json:"references"`
	Unresolved      int       `json:"unresolved"`
	Symbols         int       `json:"symbols"`
	Detail          string    `json:"detail"`
}

func (entry cacheEntry) result() analysisResult {
	return analysisResult{
		set:             entry.Set,
		notLoaded:       entry.NotLoaded,
		loadDiagnostics: entry.LoadDiagnostics,
		definitions:     entry.Definitions,
		references:      entry.References,
		unresolved:      entry.Unresolved,
		symbols:         entry.Symbols,
		detail:          entry.Detail,
	}
}

// CacheReport counts what the cache did, for the pass to report.
type CacheReport struct {
	Mode     CacheMode
	Hits     int
	Misses   int
	Verified int
}

// factCache reads and writes one entry per analysis unit.
//
// It is safe for concurrent use: the units run at the same time, and two of
// them routinely depend on the same directory tree.
type factCache struct {
	directory string
	mode      CacheMode
	analyzer  string

	trees *fingerprintMemo
	// goRegistry is this pass's answer to "who provides which module".
	goRegistry string

	hits     atomic.Int64
	misses   atomic.Int64
	verified atomic.Int64
}

// newFactCache prepares the cache for a pass.
//
// A configured cache whose directory cannot be used fails the pass rather than
// disabling itself: running slower than asked, silently, is how a
// misconfiguration survives for months.
func newFactCache(options FullOptions) (*factCache, error) {
	mode := options.CacheMode
	if mode == "" {
		mode = CacheOff
	}
	if !ValidCacheMode(mode) {
		return nil, fmt.Errorf("fact cache: unknown mode %q, want off, on or verify", mode)
	}
	if mode == CacheOff {
		return &factCache{mode: CacheOff, trees: newFingerprintMemo()}, nil
	}
	directory := strings.TrimSpace(options.CacheDirectory)
	if directory == "" {
		return nil, fmt.Errorf("fact cache: mode %q needs a directory", mode)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("fact cache: prepare %q: %w", directory, err)
	}
	return &factCache{
		directory: directory,
		mode:      mode,
		analyzer:  analyzerFingerprint(options),
		trees:     newFingerprintMemo(),
	}, nil
}

func (cache *factCache) enabled() bool {
	return cache != nil && cache.mode != CacheOff && cache.directory != ""
}

func (cache *factCache) report() CacheReport {
	if cache == nil {
		return CacheReport{Mode: CacheOff}
	}
	return CacheReport{
		Mode:     cache.mode,
		Hits:     int(cache.hits.Load()),
		Misses:   int(cache.misses.Load()),
		Verified: int(cache.verified.Load()),
	}
}

// analyse answers one unit, from the cache when every input it recorded still
// matches, and from the analysis otherwise.
//
// In CacheVerify the analysis always runs and its facts are the ones returned;
// a stored entry that disagrees with them stops the pass.
func (cache *factCache) analyse(
	ctx context.Context,
	options FullOptions,
	unit analysisUnit,
	inputs analysisInputs,
) (analysisResult, error) {
	if !cache.enabled() {
		return analyseUnit(ctx, options, unit, inputs)
	}

	identity := unitIdentity(unit)
	entry, found := cache.load(identity)
	if found && cache.mode == CacheOn {
		cache.hits.Add(1)
		return entry.result(), nil
	}

	result, err := analyseUnit(ctx, options, unit, inputs)
	if err != nil {
		return analysisResult{}, err
	}
	if found && cache.mode == CacheVerify {
		if err := sameFacts(entry.result(), result); err != nil {
			return analysisResult{}, fmt.Errorf("%w: unit %s: %w", ErrCacheDiverged, identity, err)
		}
		cache.verified.Add(1)
		return result, nil
	}
	cache.misses.Add(1)

	// A module the loader could not read is never stored. Whether it can be
	// read depends on the module cache, which no fingerprint of the source
	// describes: storing the failure would make "download the dependencies
	// and index again" a no-op.
	if result.notLoaded {
		return result, nil
	}
	cache.store(identity, unit, options, inputs, result)
	return result, nil
}

// load reads the entry for a unit and validates every input it recorded. A
// single mismatch is a miss; so is anything unreadable.
func (cache *factCache) load(identity string) (cacheEntry, bool) {
	data, err := os.ReadFile(cache.path(identity))
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheEntry{}, false
	}
	if entry.Version != cacheEntryVersion || entry.Unit != identity || entry.Analyzer != cache.analyzer {
		return cacheEntry{}, false
	}
	for _, input := range entry.Inputs {
		if cache.fingerprint(input.Kind, input.Name) != input.Fingerprint {
			return cacheEntry{}, false
		}
	}
	// The facts are not validated here, and that is the same bar a fresh
	// analysis meets: one unit's set is a fragment whose cross-repository
	// edges point at symbols another unit defines. What gets validated is
	// the merged graph, before anything is published.
	return entry, true
}

// store writes the entry for a unit. A failure to write is not a failure of
// the pass: the graph is already correct, the next pass just pays again.
func (cache *factCache) store(
	identity string,
	unit analysisUnit,
	options FullOptions,
	inputs analysisInputs,
	result analysisResult,
) {
	entry := cacheEntry{
		Version:         cacheEntryVersion,
		Unit:            identity,
		Analyzer:        cache.analyzer,
		Inputs:          cache.describeInputs(unit, options, inputs, result),
		Set:             result.set,
		NotLoaded:       result.notLoaded,
		LoadDiagnostics: result.loadDiagnostics,
		Definitions:     result.definitions,
		References:      result.references,
		Unresolved:      result.unresolved,
		Symbols:         result.symbols,
		Detail:          result.detail,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := cache.path(identity)
	temporary, err := os.CreateTemp(cache.directory, "entry-*.tmp")
	if err != nil {
		return
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporary.Name())
		return
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		_ = os.Remove(temporary.Name())
	}
}

func (cache *factCache) path(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(cache.directory, hex.EncodeToString(sum[:])+".json")
}

// prune drops entries nothing has used for a while. A cache is not a record
// of anything, and two workspaces indexed from the same home share a
// directory: expiring by age keeps them from evicting each other.
func (cache *factCache) prune(maximumAge time.Duration) {
	if !cache.enabled() {
		return
	}
	entries, err := os.ReadDir(cache.directory)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-maximumAge)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.ModTime().After(deadline) {
			continue
		}
		_ = os.Remove(filepath.Join(cache.directory, entry.Name()))
	}
}

// unitIdentity names what the entry is about, never what it read.
func unitIdentity(unit analysisUnit) string {
	if unit.isGo {
		return "go\x00" + unit.repository.Name + "\x00" + unit.module.ModulePath
	}
	return "typescript\x00" + unit.repository.Name + "\x00" + unit.pkg.packageValue.Name
}

// describeInputs lists everything the unit's facts depend on.
//
// The unit's own sources are walked rather than read from the last run: a file
// added to a package is an input the previous run never saw, and a recorded
// list of paths would not notice it.
func (cache *factCache) describeInputs(
	unit analysisUnit,
	options FullOptions,
	inputs analysisInputs,
	result analysisResult,
) []cacheInput {
	described := make([]cacheInput, 0, 8)
	add := func(kind inputKind, name string) {
		described = append(described, cacheInput{
			Kind: kind, Name: name, Fingerprint: cache.fingerprint(kind, name),
		})
	}

	if unit.isGo {
		// Every module of the workspace group, not only this one: modules
		// share a synthetic workspace exactly when one reaches the other,
		// so a sibling's source is this module's type information.
		for _, module := range goWorkspaceGroup(unit, inputs) {
			add(inputTree, module.RootPath)
		}
		if unit.workFile != "" {
			add(inputFile, unit.workFile)
		}
		add(inputRegistry, goRegistryInput)
		return described
	}

	packageValue := unit.pkg.packageValue
	for _, root := range packageValue.SourceRoots {
		add(inputTree, root)
	}
	for _, path := range []string{packageValue.ManifestPath, packageValue.ProjectPath} {
		if strings.TrimSpace(path) != "" {
			add(inputFile, path)
		}
	}
	for _, lockfile := range lockfilePaths(unit.repository.RealPath) {
		add(inputFile, lockfile)
	}
	for _, name := range requestedPackages(result) {
		add(inputProvider, name)
	}
	return described
}

// fingerprint answers what an input looks like right now. Anything missing,
// unreadable or unknown fingerprints as absent, which never matches a
// fingerprint taken from something that was there.
func (cache *factCache) fingerprint(kind inputKind, name string) string {
	switch kind {
	case inputTree:
		return cache.trees.tree(name)
	case inputFile:
		return cache.trees.file(name)
	case inputProvider:
		return cache.trees.provider(name)
	case inputRegistry:
		if name != goRegistryInput {
			return "absent"
		}
		return cache.goRegistry
	default:
		return ""
	}
}

// goWorkspaceGroup answers the modules that share this unit's synthetic
// workspace, including the unit's own. A module loaded on its own is its own
// group.
func goWorkspaceGroup(unit analysisUnit, inputs analysisInputs) []goworkspace.Module {
	if unit.workFile == "" {
		return []goworkspace.Module{unit.module}
	}
	group := make([]goworkspace.Module, 0, 2)
	for _, module := range inputs.goModules {
		if inputs.workFiles[module.ModulePath] == unit.workFile {
			group = append(group, module)
		}
	}
	if len(group) == 0 {
		return []goworkspace.Module{unit.module}
	}
	sort.Slice(group, func(left, right int) bool { return group[left].RootPath < group[right].RootPath })
	return group
}

// withRegistry records this pass's module registry, so an entry taken while
// one repository provided a module is not served once another one does.
func (cache *factCache) withRegistry(inputs analysisInputs) {
	if cache == nil {
		return
	}
	sum := sha256.Sum256([]byte(goRegistryName(inputs)))
	cache.goRegistry = hex.EncodeToString(sum[:])
}

// goRegistryName renders which repository provides which module path. It is
// the answer to "does this reference leave the repository", so a repository
// registered later turns an unresolved reference into an edge and every Go
// entry taken without it has to be recomputed.
func goRegistryName(inputs analysisInputs) string {
	if inputs.moduleRegistry == nil {
		return ""
	}
	paths := inputs.moduleRegistry.ModulePaths()
	names := make([]string, 0, len(paths))
	for _, modulePath := range paths {
		for _, provider := range inputs.moduleRegistry.Providers(modulePath) {
			names = append(names, modulePath+"="+provider.Repository+"@"+provider.ManifestPath)
		}
	}
	return strings.Join(names, "\x00")
}

// requestedPackages names every package the unit asked about, resolved or
// not. An unresolved request matters as much as a resolved one: the package
// that answers it may exist by the next pass, and the entry taken without it
// would keep reporting the reference as unresolved forever.
func requestedPackages(result analysisResult) []string {
	seen := make(map[string]struct{}, len(result.requested))
	names := make([]string, 0, len(result.requested))
	for _, name := range result.requested {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		names = append(names, trimmed)
	}
	sort.Strings(names)
	return names
}

func lockfilePaths(root string) []string {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	return []string{
		filepath.Join(root, "pnpm-lock.yaml"),
		filepath.Join(root, "package-lock.json"),
		filepath.Join(root, "yarn.lock"),
	}
}

// analyzerFingerprint identifies what produces the facts, so an entry written
// by one build of Ladygraph is never served to another.
//
// The executable's own content is the identity, not its release string: a
// development build changes the normaliser without changing a version number,
// and an entry from before that change describes a graph this binary would
// not produce.
func analyzerFingerprint(options FullOptions) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "entry=%d\x00", cacheEntryVersion)
	fmt.Fprintf(hash, "tests=%t\x00network=%t\x00", options.IncludeTests, options.GoAllowNetwork)
	tags := append([]string(nil), options.GoBuildTags...)
	sort.Strings(tags)
	fmt.Fprintf(hash, "tags=%s\x00", strings.Join(tags, ","))
	fmt.Fprintf(hash, "worker=%s\x00", strings.TrimSpace(options.TypeScriptWorker))
	if executable, err := os.Executable(); err == nil {
		fmt.Fprintf(hash, "binary=%s\x00", fileFingerprint(executable))
	} else {
		// Unknown identity is not a licence to reuse anything.
		fmt.Fprintf(hash, "binary=unknown-%d\x00", time.Now().UnixNano())
	}
	fmt.Fprintf(hash, "goenv=%s\x00", goEnvironmentFingerprint())
	fmt.Fprintf(hash, "tsworker=%s\x00", typeScriptWorkerFingerprint(options))
	return hex.EncodeToString(hash.Sum(nil))
}

// goEnvironmentFingerprint identifies the go command this pass will run.
//
// go/types travels linked inside this binary, but the standard library it
// checks against is source under GOROOT, and which versions the build list
// selects is the go command's answer, not this binary's. A toolchain upgrade
// changes both without changing a byte of Ladygraph.
func goEnvironmentFingerprint() string {
	output, err := exec.Command("go", "env",
		"GOVERSION", "GOROOT", "GOFLAGS", "GOMODCACHE", "GOPATH", "GOPRIVATE").Output()
	if err != nil {
		// No toolchain identity means no entry is reusable: a pass with
		// no Go units is unaffected, and one with them must not guess.
		return fmt.Sprintf("unknown-%d", time.Now().UnixNano())
	}
	return strings.Join(strings.Fields(string(output)), "\x00")
}

// typeScriptWorkerFingerprint identifies the worker that produces TypeScript
// facts. Its command line is not enough: the same command runs whatever the
// last `pnpm build` left in dist.
func typeScriptWorkerFingerprint(options FullOptions) string {
	command, arguments, err := factsCommand(options, nil)
	if err != nil {
		return "unavailable"
	}
	resolved := command
	if path, err := exec.LookPath(command); err == nil {
		resolved = path
	}
	fingerprint := fileFingerprint(resolved)
	// node runs a script: the script is the analyser, not the runtime.
	for _, argument := range arguments {
		if strings.HasSuffix(argument, ".js") || strings.HasSuffix(argument, ".mjs") {
			fingerprint += "\x00" + fileFingerprint(argument)
		}
	}
	return fingerprint
}

// fingerprintMemo computes each directory tree and file once per pass. Units
// run concurrently and a provider package is an input of every consumer that
// names it.
type fingerprintMemo struct {
	mutex     sync.Mutex
	entries   map[string]*memoEntry
	providers map[string]string
}

type memoEntry struct {
	once  sync.Once
	value string
}

func newFingerprintMemo() *fingerprintMemo {
	return &fingerprintMemo{entries: make(map[string]*memoEntry)}
}

// withProviders records which package name is answered by which package tree,
// so a consumer's entry can depend on the provider rather than on every
// package in the workspace.
func (memo *fingerprintMemo) withProviders(packages []typeScriptPackageUnit) {
	providers := make(map[string]string, len(packages))
	for _, unit := range packages {
		providers[unit.packageValue.Name] = unit.packageValue.RootPath
	}
	memo.mutex.Lock()
	memo.providers = providers
	memo.mutex.Unlock()
}

func (memo *fingerprintMemo) provider(name string) string {
	memo.mutex.Lock()
	root, exists := memo.providers[name]
	memo.mutex.Unlock()
	if !exists {
		return "absent"
	}
	return memo.tree(root)
}

func (memo *fingerprintMemo) tree(root string) string {
	return memo.compute("tree\x00"+root, func() string { return treeFingerprint(root) })
}

func (memo *fingerprintMemo) file(path string) string {
	return memo.compute("file\x00"+path, func() string { return fileFingerprint(path) })
}

func (memo *fingerprintMemo) compute(key string, produce func() string) string {
	memo.mutex.Lock()
	entry, exists := memo.entries[key]
	if !exists {
		entry = &memoEntry{}
		memo.entries[key] = entry
	}
	memo.mutex.Unlock()
	entry.once.Do(func() { entry.value = produce() })
	return entry.value
}

// treeFingerprint hashes every source file under root by path and content.
//
// The walk deliberately over-approximates: a file the loader would exclude by
// build tag is hashed anyway, because deciding otherwise means reproducing the
// loader's own rules here, and being wrong in that direction serves facts
// nobody can reproduce. Being wrong the other way costs one analysis.
//
// node_modules is the exception. It is an input, but it is also hundreds of
// thousands of files that are a function of the lockfile, which is hashed
// instead: a hand-edited node_modules does not invalidate an entry.
func treeFingerprint(root string) string {
	if strings.TrimSpace(root) == "" {
		return "absent"
	}
	info, err := os.Stat(root)
	if err != nil {
		return "absent"
	}
	if !info.IsDir() {
		return fileFingerprint(root)
	}
	hash := sha256.New()
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if !isFingerprintedSource(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		fmt.Fprintf(hash, "%s\x00", filepath.ToSlash(relative))
		if _, err := io.Copy(hash, file); err != nil {
			return err
		}
		fmt.Fprint(hash, "\x00")
		return nil
	})
	if walkErr != nil {
		return "unreadable"
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func fileFingerprint(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "absent"
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "unreadable"
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "size=%s\x00", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(hash, file); err != nil {
		return "unreadable"
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// isFingerprintedSource is what a unit can read: the languages this indexer
// analyses, plus the manifests that decide how they are built.
func isFingerprintedSource(name string) bool {
	switch name {
	case "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "tsconfig.json":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

// sameFacts reports what differs between a stored entry and a fresh analysis.
// The comparison is over the encoded facts, which is what gets published, and
// it names the first difference rather than dumping two graphs.
func sameFacts(cached, fresh analysisResult) error {
	if cached.notLoaded != fresh.notLoaded {
		return fmt.Errorf("not-loaded is %t, analysis says %t", cached.notLoaded, fresh.notLoaded)
	}
	cachedEncoded, err := json.Marshal(cached.set)
	if err != nil {
		return fmt.Errorf("encode cached facts: %w", err)
	}
	freshEncoded, err := json.Marshal(fresh.set)
	if err != nil {
		return fmt.Errorf("encode analysed facts: %w", err)
	}
	if string(cachedEncoded) == string(freshEncoded) {
		return nil
	}
	return fmt.Errorf("facts differ: cached %s, analysed %s",
		factsShape(cached.set), factsShape(fresh.set))
}

func factsShape(set facts.Set) string {
	return fmt.Sprintf("%d symbols/%d edges/%d unresolved/%d evidence",
		len(set.Symbols), len(set.Edges), len(set.Unresolved), len(set.Evidence))
}
