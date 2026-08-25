// Package goworkspace builds the synthetic go.work Kivgraph uses to load every
// registered Go module in a single go/packages universe.
//
// The file is written outside every repository, under the configured state
// directory. Kivgraph never writes a go.work inside an indexed repository.
package goworkspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// ErrRepositoryTarget reports a synthetic workspace path inside a repository.
var ErrRepositoryTarget = errors.New("synthetic go.work path is inside a registered repository")

// ErrNoModules reports that no registered repository declares a Go module.
var ErrNoModules = errors.New("no registered repository declares a Go module")

// ErrGoVersionUnsupported reports a registered module whose language version
// is newer than the one this build can type-check.
var ErrGoVersionUnsupported = errors.New("registered Go module requires a newer Go language version than this build supports")

// ConflictKind classifies a fact excluded from the synthetic workspace.
type ConflictKind string

const (
	// AmbiguousModule marks one module path declared by several repositories.
	AmbiguousModule ConflictKind = "AMBIGUOUS_MODULE_PROVIDER"
	// ReplaceConflict marks one replaced path with incompatible targets.
	ReplaceConflict ConflictKind = "MODULE_REPLACE_CONFLICT"
)

// Module is one Go module included in the synthetic workspace.
type Module struct {
	Repository   string
	ModulePath   string
	RootPath     string
	ManifestPath string
	GoVersion    string
	// PackagePatterns are the discovered package paths passed to go/packages.
	PackagePatterns []string
	// Reaches are the module paths this module requires or replaces. Only
	// the ones a registered repository provides matter, and they decide
	// which modules must share a workspace.
	Reaches []string
	// WorkspaceMembers are the module paths bound together by a go.work of
	// the indexed repository that lists this module, this one included.
	// That file is a fact of the repository, so its grouping is preserved
	// and its members always load through a workspace.
	WorkspaceMembers []string
}

// Replacement is a replace directive promoted to the synthetic workspace.
type Replacement struct {
	OldPath    string
	OldVersion string
	NewPath    string
	NewVersion string
}

// Conflict is a fact deliberately excluded because it cannot be decided.
type Conflict struct {
	Kind         ConflictKind
	Subject      string
	Repositories []string
	Details      []string
}

// Plan is the deterministic content of one synthetic workspace.
type Plan struct {
	GoVersion string
	Modules   []Module
	Replaces  []Replacement
	Conflicts []Conflict
}

// Options tunes how the plan is built.
type Options struct {
	// GoVersion overrides the version derived from the module manifests.
	GoVersion string
	// MaximumGoVersion is the highest language version the caller can type
	// check. Empty means the version of the toolchain that built this
	// binary, which is the only honest default: go/types is linked in, so
	// the workspace must never select a toolchain whose sources it cannot
	// read. Loading a newer module would fail deep inside the standard
	// library instead of naming the repository that requires it.
	MaximumGoVersion string
}

// LanguageVersion is the `major.minor` language version this build can type
// check, derived from the toolchain that compiled it.
func LanguageVersion() string {
	version := strings.TrimPrefix(runtime.Version(), "go")
	if index := strings.IndexAny(version, "-+ "); index >= 0 {
		version = version[:index]
	}
	if trimmed := semver.MajorMinor("v" + version); trimmed != "" {
		return strings.TrimPrefix(trimmed, "v")
	}
	return ""
}

// BuildPlan discovers every Go module of the registered repositories and
// composes the workspace content without writing anything.
//
// Ambiguous module paths and incompatible replacements are excluded and
// reported: go itself rejects a workspace with two directories providing the
// same module, and a replacement Kivgraph cannot decide must never be guessed.
func BuildPlan(ctx context.Context, repositories []workspace.Repository, options Options) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}

	byModulePath := make(map[string][]Module)
	replacements := make(map[replacementKey][]replacementSource)
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return Plan{}, fmt.Errorf("repository %q: name must not be empty", repository.Path)
		}
		discovery, err := workspace.DiscoverGo(ctx, repository)
		if err != nil {
			return Plan{}, fmt.Errorf("repository %q Go modules: %w", name, err)
		}
		workspaceReplaces := workspaceReplacementsByManifest(discovery.Workspaces)
		moduleByManifest := make(map[string]string, len(discovery.Modules))
		for _, module := range discovery.Modules {
			moduleByManifest[module.ManifestPath] = strings.TrimSpace(module.ModulePath)
		}
		for _, module := range discovery.Modules {
			modulePath := strings.TrimSpace(module.ModulePath)
			if modulePath == "" {
				return Plan{}, fmt.Errorf("repository %q module manifest %q has an empty module path", name, module.ManifestPath)
			}
			packagePatterns := PackagePatternsForModule(discovery.Packages, module)
			provider := workspace.GoModuleProvider{
				ModulePath:        modulePath,
				Repository:        name,
				ManifestPath:      module.ManifestPath,
				RootPath:          module.RootPath,
				GoVersion:         module.GoVersion,
				Replaces:          append([]workspace.GoReplacement(nil), module.Replaces...),
				WorkspaceReplaces: append([]workspace.GoReplacement(nil), workspaceReplaces[module.ManifestPath]...),
			}
			byModulePath[modulePath] = append(byModulePath[modulePath], Module{
				Repository:       name,
				ModulePath:       modulePath,
				RootPath:         module.RootPath,
				ManifestPath:     module.ManifestPath,
				GoVersion:        module.GoVersion,
				PackagePatterns:  packagePatterns,
				Reaches:          moduleReaches(module),
				WorkspaceMembers: workspaceMembers(discovery.Workspaces, module.ManifestPath, moduleByManifest),
			})
			collectReplacements(replacements, name, provider)
		}
	}

	if err := rejectUnsupportedGoVersions(byModulePath, options.MaximumGoVersion); err != nil {
		return Plan{}, err
	}

	plan := Plan{}
	for modulePath, candidates := range byModulePath {
		if len(candidates) > 1 {
			plan.Conflicts = append(plan.Conflicts, ambiguousModuleConflict(modulePath, candidates))
			continue
		}
		plan.Modules = append(plan.Modules, candidates[0])
	}
	if len(plan.Modules) == 0 {
		return Plan{}, ErrNoModules
	}
	sort.Slice(plan.Modules, func(left, right int) bool {
		if plan.Modules[left].ModulePath != plan.Modules[right].ModulePath {
			return plan.Modules[left].ModulePath < plan.Modules[right].ModulePath
		}
		return plan.Modules[left].RootPath < plan.Modules[right].RootPath
	})

	included := make(map[string]struct{}, len(plan.Modules))
	for _, module := range plan.Modules {
		included[module.ModulePath] = struct{}{}
	}
	resolveReplacements(&plan, replacements, included)

	version, err := workspaceGoVersion(plan.Modules, options.GoVersion)
	if err != nil {
		return Plan{}, err
	}
	plan.GoVersion = version

	sort.Slice(plan.Conflicts, func(left, right int) bool {
		if plan.Conflicts[left].Kind != plan.Conflicts[right].Kind {
			return plan.Conflicts[left].Kind < plan.Conflicts[right].Kind

		}
		return plan.Conflicts[left].Subject < plan.Conflicts[right].Subject
	})

	return plan, nil
}

// PackagePatternsForModule answers which import paths one module contributes,
// which is what a load has to name explicitly: a "./..." pattern silently
// skips a package whose files the build configuration all excludes, so it
// never reports the exclusion. Exported so an audit lists exactly what a pass
// would load instead of deriving a second answer.
func PackagePatternsForModule(packages []workspace.GoPackage, module workspace.GoModule) []string {
	patterns := make([]string, 0)
	for _, packageValue := range packages {
		if packageValue.ModulePath != module.ModulePath || !pathWithinModule(module.RootPath, packageValue.Directory) {
			continue
		}
		if strings.TrimSpace(packageValue.ImportPath) == "" {
			continue
		}
		patterns = append(patterns, packageValue.ImportPath)
	}
	sort.Strings(patterns)
	deduplicated := patterns[:0]
	for _, pattern := range patterns {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != pattern {
			deduplicated = append(deduplicated, pattern)
		}
	}
	return deduplicated
}

func pathWithinModule(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// moduleReaches lists every module path this manifest names. A requirement or
// a replacement pointing at a module another repository provides is the only
// reason two modules must resolve one shared build list.
func moduleReaches(module workspace.GoModule) []string {
	reaches := make([]string, 0, len(module.Requires)+len(module.Replaces))
	for _, requirement := range module.Requires {
		reaches = appendUnique(reaches, requirement)
	}
	for _, replacement := range module.Replaces {
		reaches = appendUnique(reaches, replacement.OldPath)
		if strings.TrimSpace(replacement.NewPath) != "" {
			reaches = appendUnique(reaches, replacement.NewPath)
		}
	}
	return reaches
}

// workspaceMembers maps the members of every repository go.work that lists
// this manifest onto their module paths, this manifest included. A go.work of
// the indexed repository is a fact about that repository, so the modules it
// binds keep resolving together and never fall back to module mode.
func workspaceMembers(
	workspaces []workspace.GoWorkspace,
	manifestPath string,
	moduleByManifest map[string]string,
) []string {
	members := make([]string, 0)
	for _, candidate := range workspaces {
		if !containsString(candidate.Modules, manifestPath) {
			continue
		}
		for _, member := range candidate.Modules {
			if modulePath, known := moduleByManifest[member]; known {
				members = appendUnique(members, modulePath)
			}
		}
	}
	return members
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}
func workspaceReplacementsByManifest(workspaces []workspace.GoWorkspace) map[string][]workspace.GoReplacement {
	result := make(map[string][]workspace.GoReplacement)
	for _, workspaceValue := range workspaces {
		for _, manifestPath := range workspaceValue.Modules {
			for _, replacement := range workspaceValue.Replaces {
				if containsWorkspaceReplacement(result[manifestPath], replacement) {
					continue
				}
				result[manifestPath] = append(result[manifestPath], replacement)
			}
		}
	}
	return result
}

func containsWorkspaceReplacement(replacements []workspace.GoReplacement, candidate workspace.GoReplacement) bool {
	for _, replacement := range replacements {
		if replacement == candidate {
			return true
		}
	}
	return false
}

type replacementKey struct {
	oldPath    string
	oldVersion string
}

type replacementSource struct {
	repository string
	module     string
	newPath    string
	newVersion string
}

func collectReplacements(sink map[replacementKey][]replacementSource, repository string, provider workspace.GoModuleProvider) {
	directives := make([]workspace.GoReplacement, 0, len(provider.Replaces)+len(provider.WorkspaceReplaces))
	directives = append(directives, provider.Replaces...)
	directives = append(directives, provider.WorkspaceReplaces...)
	for _, directive := range directives {
		newPath := directive.NewPath
		if directive.NewLocalPath != "" {
			newPath = directive.NewLocalPath
		}
		key := replacementKey{oldPath: directive.OldPath, oldVersion: directive.OldVersion}
		source := replacementSource{
			repository: repository,
			module:     provider.ModulePath,
			newPath:    newPath,
			newVersion: directive.NewVersion,
		}
		if containsReplacementSource(sink[key], source) {
			continue
		}
		sink[key] = append(sink[key], source)
	}
}

func containsReplacementSource(sources []replacementSource, candidate replacementSource) bool {
	for _, source := range sources {
		if source.newPath == candidate.newPath && source.newVersion == candidate.newVersion {
			return true
		}
	}
	return false
}

// resolveReplacements promotes the replacement of every module the workspace
// does not already provide by `use`.
//
// When the members disagree, go refuses to load the whole workspace unless the
// workspace itself overrides the replacement, so an override is emitted: the
// alternative is losing every fact of every repository. The choice is
// deterministic and the conflict is reported, so LUQUE-0810 can refuse every
// edge that would depend on the guessed target.
func resolveReplacements(plan *Plan, sources map[replacementKey][]replacementSource, included map[string]struct{}) {
	for key, candidates := range sources {
		if _, workspaceProvides := included[key.oldPath]; workspaceProvides {
			continue
		}
		chosen := candidates[0]
		if len(candidates) > 1 {
			plan.Conflicts = append(plan.Conflicts, replaceConflict(key, candidates))
			chosen = smallestReplacement(candidates)
		}
		plan.Replaces = append(plan.Replaces, Replacement{
			OldPath:    key.oldPath,
			OldVersion: key.oldVersion,
			NewPath:    chosen.newPath,
			NewVersion: chosen.newVersion,
		})
	}
	sort.Slice(plan.Replaces, func(left, right int) bool {
		if plan.Replaces[left].OldPath != plan.Replaces[right].OldPath {
			return plan.Replaces[left].OldPath < plan.Replaces[right].OldPath
		}
		return plan.Replaces[left].OldVersion < plan.Replaces[right].OldVersion
	})
}

// smallestReplacement picks the lexicographically first target so the emitted
// workspace is reproducible across machines and runs.
func smallestReplacement(candidates []replacementSource) replacementSource {
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.newPath < chosen.newPath ||
			(candidate.newPath == chosen.newPath && candidate.newVersion < chosen.newVersion) {
			chosen = candidate
		}
	}
	return chosen
}

func ambiguousModuleConflict(modulePath string, candidates []Module) Conflict {
	repositories := make([]string, 0, len(candidates))
	manifests := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		repositories = appendUnique(repositories, candidate.Repository)
		manifests = appendUnique(manifests, candidate.ManifestPath)
	}
	sort.Strings(repositories)
	sort.Strings(manifests)
	return Conflict{
		Kind:         AmbiguousModule,
		Subject:      modulePath,
		Repositories: repositories,
		Details:      manifests,
	}
}

func replaceConflict(key replacementKey, candidates []replacementSource) Conflict {
	repositories := make([]string, 0, len(candidates))
	details := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		repositories = appendUnique(repositories, candidate.repository)
		target := candidate.newPath
		if candidate.newVersion != "" {
			target += "@" + candidate.newVersion
		}
		details = appendUnique(details, candidate.module+" => "+target)
	}
	sort.Strings(repositories)
	sort.Strings(details)
	subject := key.oldPath
	if key.oldVersion != "" {
		subject += "@" + key.oldVersion
	}
	return Conflict{
		Kind:         ReplaceConflict,
		Subject:      subject,
		Repositories: repositories,
		Details:      details,
	}
}

// rejectUnsupportedGoVersions fails the plan when a registered module needs a
// newer language version than the caller can type-check.
//
// The workspace claims the highest version of its members, and the go command
// then selects a toolchain to match. A member above the cap would make every
// load of every repository fail inside the standard library of that toolchain,
// naming a file nobody registered. Failing here names the repository instead,
// and the whole index is refused rather than published without it.
func rejectUnsupportedGoVersions(byModulePath map[string][]Module, maximum string) error {
	supported := strings.TrimSpace(maximum)
	if supported == "" {
		supported = LanguageVersion()
	}
	if supported == "" {
		return nil
	}
	if !semver.IsValid("v" + supported) {
		return fmt.Errorf("invalid maximum go version %q", maximum)
	}
	ceiling := semver.MajorMinor("v" + supported)
	rejected := make([]string, 0)
	for _, candidates := range byModulePath {
		for _, module := range candidates {
			version := strings.TrimSpace(module.GoVersion)
			if version == "" || !semver.IsValid("v"+version) {
				continue
			}
			// The language version is major.minor: a patch release
			// never adds a language feature, so only the minor may
			// exceed what go/types understands.
			if semver.Compare(semver.MajorMinor("v"+version), ceiling) <= 0 {
				continue
			}
			rejected = append(rejected, fmt.Sprintf(
				"repository %q module %q requires go %s", module.Repository, module.ModulePath, version))
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	sort.Strings(rejected)
	return fmt.Errorf("%w (this build type-checks with go %s): %s; rebuild Kivgraph with that toolchain or drop \"go\" from the languages of that repository",
		ErrGoVersionUnsupported, supported, strings.Join(rejected, "; "))
}

// workspaceGoVersion returns the highest language version declared by the
// included modules: a workspace must not claim less than its members.
func workspaceGoVersion(modules []Module, override string) (string, error) {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		if !semver.IsValid("v" + trimmed) {
			return "", fmt.Errorf("invalid go version override %q", override)
		}
		return trimmed, nil
	}
	highest := ""
	for _, module := range modules {
		version := strings.TrimSpace(module.GoVersion)
		if version == "" {
			continue
		}
		if !semver.IsValid("v" + version) {
			return "", fmt.Errorf("module %q declares an invalid go version %q", module.ModulePath, version)
		}
		if highest == "" || semver.Compare("v"+version, "v"+highest) > 0 {
			highest = version
		}
	}
	if highest == "" {
		return "", fmt.Errorf("no included module declares a go version")
	}
	return highest, nil
}

// Render produces the canonical go.work bytes for this plan.
func (plan Plan) Render() ([]byte, error) {
	file := &modfile.WorkFile{Syntax: &modfile.FileSyntax{}}
	if err := file.AddGoStmt(plan.GoVersion); err != nil {
		return nil, fmt.Errorf("go directive: %w", err)
	}
	for _, module := range plan.Modules {
		if err := file.AddUse(filepath.ToSlash(module.RootPath), module.ModulePath); err != nil {
			return nil, fmt.Errorf("use %q: %w", module.RootPath, err)
		}
	}
	for _, replacement := range plan.Replaces {
		if err := file.AddReplace(replacement.OldPath, replacement.OldVersion, replacement.NewPath, replacement.NewVersion); err != nil {
			return nil, fmt.Errorf("replace %q: %w", replacement.OldPath, err)
		}
	}
	file.SortBlocks()
	file.Cleanup()
	return modfile.Format(file.Syntax), nil
}

// Partition splits the plan into the independent workspaces it really needs.
//
// A go.work resolves one build list for every module it uses, so unrelated
// repositories end up sharing a minimum version selection: a dependency bumped
// in one of them changes the versions selected for all the others, and a
// version no repository downloaded on its own breaks every load at once. Two
// modules only need the same workspace when one can reach the other: a
// requirement or a replacement naming a registered module, or a go.work of the
// indexed repository that already binds them together. Everything else is
// loaded exactly as its own toolchain would load it.
//
// The returned plans are ordered by their first module path, and a plan with a
// single module that reaches nothing keeps its own manifest as the only truth:
// callers load it in module mode, with no workspace at all.
func (plan Plan) Partition() []Plan {
	if len(plan.Modules) < 2 {
		return []Plan{plan}
	}
	provider := make(map[string]int, len(plan.Modules))
	for index, module := range plan.Modules {
		provider[module.ModulePath] = index
	}
	groups := newDisjointSet(len(plan.Modules))
	for index, module := range plan.Modules {
		for _, reachable := range module.Reaches {
			if other, provided := provider[reachable]; provided {
				groups.union(index, other)
			}
		}
		for _, member := range module.WorkspaceMembers {
			if other, provided := provider[member]; provided {
				groups.union(index, other)
			}
		}
	}

	byRoot := make(map[int][]Module)
	for index, module := range plan.Modules {
		root := groups.find(index)
		byRoot[root] = append(byRoot[root], module)
	}
	partitioned := make([]Plan, 0, len(byRoot))
	for _, modules := range byRoot {
		partitioned = append(partitioned, plan.subPlan(modules))
	}
	sort.Slice(partitioned, func(left, right int) bool {
		return partitioned[left].Modules[0].ModulePath < partitioned[right].Modules[0].ModulePath
	})
	return partitioned
}

// subPlan keeps the replacements that name a module of this group or a module
// none of the groups provides. A replacement aimed at another group would
// shadow a module this workspace never loads.
func (plan Plan) subPlan(modules []Module) Plan {
	provided := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		provided[module.ModulePath] = struct{}{}
	}
	replaces := make([]Replacement, 0, len(plan.Replaces))
	for _, replacement := range plan.Replaces {
		if _, mine := provided[replacement.OldPath]; mine {
			continue
		}
		replaces = append(replaces, replacement)
	}
	version, err := workspaceGoVersion(modules, "")
	if err != nil {
		// Every module of the plan already passed this check when the
		// plan was built, so a group of them cannot fail it.
		version = plan.GoVersion
	}
	return Plan{GoVersion: version, Modules: modules, Replaces: replaces}
}

// disjointSet groups module indexes without allocating a graph.
type disjointSet struct {
	parent []int
}

func newDisjointSet(size int) *disjointSet {
	parent := make([]int, size)
	for index := range parent {
		parent[index] = index
	}
	return &disjointSet{parent: parent}
}

func (set *disjointSet) find(index int) int {
	for set.parent[index] != index {
		set.parent[index] = set.parent[set.parent[index]]
		index = set.parent[index]
	}
	return index
}

func (set *disjointSet) union(left, right int) {
	leftRoot, rightRoot := set.find(left), set.find(right)
	if leftRoot == rightRoot {
		return
	}
	if leftRoot < rightRoot {
		set.parent[rightRoot] = leftRoot
		return
	}
	set.parent[leftRoot] = rightRoot
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// Result reports what a write did.
type Result struct {
	Path    string
	Bytes   int
	Changed bool
}

// Write renders the plan and installs it atomically at path.
//
// The path is rejected when it falls inside any registered repository, so a
// misconfiguration cannot make Kivgraph write a go.work into indexed code.
func Write(ctx context.Context, path string, plan Plan, repositories []workspace.Repository) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	target, err := resolveTarget(path, repositories)
	if err != nil {
		return Result{}, err
	}
	contents, err := plan.Render()
	if err != nil {
		return Result{}, err
	}
	if existing, err := os.ReadFile(target); err == nil && string(existing) == string(contents) {
		return Result{Path: target, Bytes: len(contents), Changed: false}, nil
	}
	if err := writeAtomic(target, contents); err != nil {
		return Result{}, err
	}
	return Result{Path: target, Bytes: len(contents), Changed: true}, nil
}

func resolveTarget(path string, repositories []workspace.Repository) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("synthetic go.work path must not be empty")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	absolute = filepath.Clean(absolute)
	for _, repository := range repositories {
		for _, root := range []string{repository.Path, repository.RealPath} {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			rootAbsolute, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			if pathWithin(filepath.Clean(rootAbsolute), absolute) {
				return "", fmt.Errorf("%w: %q is inside %q", ErrRepositoryTarget, absolute, rootAbsolute)
			}
		}
	}
	return absolute, nil
}

func writeAtomic(target string, contents []byte) error {
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".go.work-*")
	if err != nil {
		return fmt.Errorf("create temporary workspace: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary workspace: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary workspace: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary workspace: %w", err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fmt.Errorf("chmod temporary workspace: %w", err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("install workspace: %w", err)
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open %q: %w", directory, err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync %q: %w", directory, err)
	}
	return nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
