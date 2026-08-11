package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Luqueee/ladygraph/internal/config"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/goloader"
	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// defaultGoLoadLimit and defaultTypeScriptWorkerLimit bound concurrent
// analysis when the caller states no budget of its own.
const (
	defaultGoLoadLimit           = 8
	defaultTypeScriptWorkerLimit = 3
)

// ProgressPhase names the unit of work a progress event belongs to.
type ProgressPhase string

const (
	// PhaseGo is one Go module of one repository.
	PhaseGo ProgressPhase = "go"
	// PhaseTypeScript is one TypeScript repository.
	PhaseTypeScript ProgressPhase = "typescript"
	// PhaseMerge is the final sort and validation of the merged fact set.
	PhaseMerge ProgressPhase = "merge"
)

// ProgressEvent reports that one unit of indexing work started or finished.
// Completed counts finished units of the phase and is 0 on a start event;
// Total is 0 when the phase has no countable units.
type ProgressEvent struct {
	Phase      ProgressPhase
	Repository string
	Detail     string
	Started    bool
	Completed  int
	Total      int
}

// FullOptions configures one clean semantic indexing pass. The pass never
// writes inside a registered repository: Go's synthetic workspace and the
// temporary TypeScript facts payloads live outside the sources.
type FullOptions struct {
	Repositories      []workspace.Repository
	SyntheticWorkFile string
	IncludeTests      bool
	// GoBuildTags are the build constraints the Go loads satisfy. A package
	// guarded by a tag that is absent here declares no file to read and is
	// reported as unresolved instead of indexed.
	GoBuildTags []string
	// GoAllowNetwork lets the Go loads reach a module proxy. Indexing is
	// hermetic by default.
	GoAllowNetwork bool
	// GoMaximumLoads bounds concurrent Go loads. Zero uses the processor
	// count, capped, because every load holds a complete type universe.
	GoMaximumLoads int
	// TypeScriptMaximumWorkers bounds concurrent TypeScript workers. Zero
	// uses the documented default.
	TypeScriptMaximumWorkers int
	TypeScriptWorker         string
	WorkingDirectory         string
	// CacheMode selects whether a unit may be served from its stored
	// facts. Empty is CacheOff.
	CacheMode CacheMode
	// CacheDirectory holds one entry per analysis unit. It lives outside
	// every indexed repository, like the rest of the state.
	CacheDirectory string

	// Progress, when set, is called synchronously as each unit of work
	// starts and finishes. It must not block: a slow callback slows the
	// index down.
	Progress func(ProgressEvent)
}

// FullReport records the work performed before the caller publishes the
// resulting facts. Counts are informational; an error is never hidden in a
// successful report.
type FullReport struct {
	// Cache reports what the fact cache did, so a pass says how much of
	// itself it skipped and on whose authority.
	Cache CacheReport
	// TypeScriptWithoutPackages names the repositories registered as
	// TypeScript that declare no package. They contribute nothing, and a
	// registry entry that contributes nothing looks like coverage.
	TypeScriptWithoutPackages []string
	// GoDiagnostics carries what the Go loader reported without blocking
	// the pass. The count alone said something happened and nothing said
	// what, and a diagnostic nobody can read is a diagnostic nobody has.
	GoDiagnostics []string

	GoRepositories int
	GoModules      int
	GoLoads        int
	GoLoadErrors   int
	// GoLoadDiagnostics counts the diagnostics that did not block the pass:
	// a directory with no file to select, and the advisory the loader
	// attaches to it.
	GoLoadDiagnostics int
	// GoModulesNotLoaded counts the modules the loader could not read. Their
	// facts are absent and declared; the pass still publishes the rest.
	GoModulesNotLoaded int
	// GoWorkspaces counts the synthetic go.work files this pass installed.
	// A module that reaches no other registered module loads without one.
	GoWorkspaces           int
	GoDefinitions          int
	GoReferences           int
	GoUnresolved           int
	TypeScriptRepositories int
	TypeScriptSymbols      int
	TypeScriptReferences   int
	TypeScriptUnresolved   int
	// TypeScriptAmbiguous counts the package names several manifests of one
	// repository declare. No manifest provides them.
	TypeScriptAmbiguous int
	SyntheticWorkFile   string
}

// Full loads every configured repository and normalises the authoritative Go
// and TypeScript facts into one validated set. A loader diagnostic aborts the
// pass instead of publishing a graph that is known to be incomplete.
func Full(ctx context.Context, options FullOptions) (facts.Set, FullReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return facts.Set{}, FullReport{}, err
	}

	repositories := append([]workspace.Repository(nil), options.Repositories...)
	sort.SliceStable(repositories, func(left, right int) bool {
		return repositories[left].Name < repositories[right].Name
	})
	if options.WorkingDirectory == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return facts.Set{}, FullReport{}, fmt.Errorf("resolve indexing working directory: %w", err)
		}
		options.WorkingDirectory = workingDirectory
	}
	if err := validateLanguages(repositories); err != nil {
		return facts.Set{}, FullReport{}, err
	}

	goRepositories := repositoriesForLanguage(repositories, "go")
	typeScriptRepositories := repositoriesForTypeScript(repositories)
	report := FullReport{
		GoRepositories:         len(goRepositories),
		TypeScriptRepositories: len(typeScriptRepositories),
	}
	typeScriptPackages, typeScriptConflicts, withoutPackages, err := discoverTypeScriptPackages(ctx, typeScriptRepositories)
	if err != nil {
		return facts.Set{}, report, err
	}
	report.TypeScriptWithoutPackages = withoutPackages
	// Every unit's facts are merged in one pass at the end of the pass, not
	// one at a time: a pairwise merge pays for the whole accumulated graph
	// on every step.
	sets := make([]facts.Set, 0, len(typeScriptConflicts))
	for _, entry := range typeScriptConflicts {
		sets = append(sets, ambiguousPackageFacts(entry))
		report.TypeScriptAmbiguous++
	}

	var (
		goUnits            []analysisUnit
		goModules          []goworkspace.Module
		workFiles          map[string]string
		moduleRegistry     *goloader.ModuleRegistry
		conflictingModules []string
		planConflicts      []goworkspace.Conflict
	)
	if len(goRepositories) != 0 {
		if strings.TrimSpace(options.SyntheticWorkFile) == "" {
			return facts.Set{}, report, errors.New("full index: synthetic Go work file is required")
		}
		plan, err := goworkspace.BuildPlan(ctx, goRepositories, goworkspace.Options{})
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("build synthetic Go workspace: %w", err)
		}
		files, workspaces, err := writeWorkspaces(ctx, options.SyntheticWorkFile, plan, goRepositories)
		if err != nil {
			return facts.Set{}, report, err
		}
		workFiles = files
		goModules = plan.Modules
		report.SyntheticWorkFile = options.SyntheticWorkFile
		report.GoWorkspaces = workspaces

		moduleRegistry, err = goloader.NewModuleRegistry(ctx, goRepositories)
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("build Go module registry: %w", err)
		}
		modulesByRepository := modulesByRepository(plan.Modules)
		conflictingModules = conflictSubjects(plan.Conflicts)
		planConflicts = plan.Conflicts
		goUnits = make([]analysisUnit, 0, len(plan.Modules))
		for _, repository := range goRepositories {
			for _, module := range modulesByRepository[repository.Name] {
				goUnits = append(goUnits, analysisUnit{
					repository: repository, module: module,
					workFile: workFiles[module.ModulePath], isGo: true,
				})
			}
		}
	}

	units := append(goUnits, typeScriptUnits(typeScriptPackages)...)
	results, cacheReport, err := analyse(ctx, options, units, analysisInputs{
		moduleRegistry:     moduleRegistry,
		conflictingModules: conflictingModules,
		planConflicts:      planConflicts,
		typeScriptPackages: typeScriptPackages,
		goModules:          goModules,
		workFiles:          workFiles,
	})
	if err != nil {
		return facts.Set{}, report, err
	}
	report.Cache = cacheReport

	// The merge follows the order of the units, never the order they
	// finished, so the published graph does not depend on how the work was
	// scheduled.
	for index, unit := range units {
		result := results[index]
		sets = append(sets, result.set)
		if unit.isGo {
			report.GoLoads++
			report.GoModules++
			if result.notLoaded {
				report.GoModulesNotLoaded++
			}
			report.GoLoadDiagnostics += result.loadDiagnostics
			report.GoDiagnostics = append(report.GoDiagnostics, result.diagnostics...)
			report.GoDefinitions += result.definitions
			report.GoReferences += result.references
			report.GoUnresolved += result.unresolved
			continue
		}
		report.TypeScriptSymbols += result.symbols
		report.TypeScriptReferences += result.references
		report.TypeScriptUnresolved += result.unresolved
	}

	emitProgress(options.Progress, ProgressEvent{Phase: PhaseMerge, Started: true})
	merged := mergeSets(sets)
	if err := merged.Validate(); err != nil {
		return facts.Set{}, report, fmt.Errorf("validate full indexed facts: %w", err)
	}
	emitProgress(options.Progress, ProgressEvent{
		Phase:  PhaseMerge,
		Detail: fmt.Sprintf("symbols=%d edges=%d unresolved=%d", len(merged.Symbols), len(merged.Edges), len(merged.Unresolved)),
	})
	return merged, report, nil
}

// emitProgress reports one event when the caller asked for progress.
func emitProgress(report func(ProgressEvent), event ProgressEvent) {
	if report == nil {
		return
	}
	report(event)
}

// validateLanguages rejects a repository declaring a language this indexer
// cannot analyse. The vocabulary is config.SupportedLanguages, the same one
// the registry validates against when the value is written: a second list
// here would let `init` accept what the pass refuses.
func validateLanguages(repositories []workspace.Repository) error {
	for _, repository := range repositories {
		for _, language := range repository.Languages {
			if !config.SupportedLanguage(language) {
				return fmt.Errorf("repository %q: unsupported language %q", repository.Name, language)
			}
		}
	}
	return nil
}

func repositoriesForLanguage(repositories []workspace.Repository, language string) []workspace.Repository {
	result := make([]workspace.Repository, 0)
	for _, repository := range repositories {
		for _, candidate := range repository.Languages {
			if strings.EqualFold(strings.TrimSpace(candidate), language) {
				result = append(result, repository)
				break
			}
		}
	}
	return result
}

func repositoriesForTypeScript(repositories []workspace.Repository) []workspace.Repository {
	result := make([]workspace.Repository, 0)
	for _, repository := range repositories {
		for _, candidate := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(candidate)) {
			case "typescript", "javascript", "ts", "js":
				result = append(result, repository)
				goto nextRepository
			}
		}
	nextRepository:
	}
	return result
}

type typeScriptPackageUnit struct {
	repository   workspace.Repository
	packageValue workspace.TypeScriptPackage
	// files is how many source files the worker will read. It orders the
	// queue; it is never a fact about the graph.
	files int
}

// countSourceFiles walks the roots a package declares. The walk stats files
// and reads none, and it skips the directories no analysis reads, so it costs
// a fraction of the analysis it schedules.
func countSourceFiles(roots []string) int {
	total := 0
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", ".git", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
				total++
			}
			return nil
		})
	}
	return total
}

// discoverTypeScriptPackages finds the packages of every TypeScript
// repository, and names the ones that declare none.
//
// A repository registered as TypeScript whose tree holds no named package
// with a project contributes nothing: the pipeline discovers packages, and a
// directory of loose .mjs files is not one. That used to be silent, so a
// registry entry suggested coverage the graph never had.
func discoverTypeScriptPackages(
	ctx context.Context,
	repositories []workspace.Repository,
) ([]typeScriptPackageUnit, []typeScriptConflict, []string, error) {
	packages := make([]typeScriptPackageUnit, 0)
	conflicts := make([]typeScriptConflict, 0)
	withoutPackages := make([]string, 0)
	for _, repository := range repositories {
		discovered := 0
		registry, err := workspace.NewTypeScriptPackageRegistry(ctx, repository)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("discover TypeScript packages for %q: %w", repository.Name, err)
		}
		for _, packageValue := range registry.List() {
			if strings.TrimSpace(packageValue.ProjectPath) == "" {
				continue
			}
			packages = append(packages, typeScriptPackageUnit{
				repository:   repository,
				packageValue: packageValue,
				files:        countSourceFiles(packageValue.SourceRoots),
			})
			discovered++
		}
		for _, conflict := range registry.Conflicts() {
			conflicts = append(conflicts, typeScriptConflict{
				repository: repository,
				conflict:   conflict,
			})
			discovered++
		}
		if discovered == 0 {
			withoutPackages = append(withoutPackages, repository.Name)
		}
	}
	if len(packages) == 0 && len(conflicts) == 0 && len(repositories) != 0 {
		return nil, nil, nil, fmt.Errorf("TypeScript repositories have no named package with a project")
	}
	sort.Slice(packages, func(left, right int) bool {
		if packages[left].repository.Name != packages[right].repository.Name {
			return packages[left].repository.Name < packages[right].repository.Name
		}
		if packages[left].packageValue.Name != packages[right].packageValue.Name {
			return packages[left].packageValue.Name < packages[right].packageValue.Name
		}
		return packages[left].packageValue.ManifestPath < packages[right].packageValue.ManifestPath
	})
	sort.Slice(conflicts, func(left, right int) bool {
		if conflicts[left].repository.Name != conflicts[right].repository.Name {
			return conflicts[left].repository.Name < conflicts[right].repository.Name
		}
		return conflicts[left].conflict.Name < conflicts[right].conflict.Name
	})
	return packages, conflicts, withoutPackages, nil
}

// typeScriptConflict is one ambiguous package name and the repository that
// declares it more than once.
type typeScriptConflict struct {
	repository workspace.Repository
	conflict   workspace.TypeScriptPackageConflict
}

// ambiguousPackageFacts declares a package name no manifest can claim.
//
// The repository record travels with the entry: a repository whose only
// packages are ambiguous contributes nothing else, and an unresolved
// reference in a repository the set does not know is not a valid fact.
func ambiguousPackageFacts(entry typeScriptConflict) facts.Set {
	repositoryKey := facts.RepositoryKey(entry.repository.Name)
	return facts.Set{
		Repositories: []facts.Repository{{
			Key:       repositoryKey,
			Name:      entry.repository.Name,
			RootPath:  entry.repository.RealPath,
			Languages: []facts.Language{facts.LanguageTypeScript},
		}},
		Unresolved: []facts.UnresolvedReference{{
			RepositoryKey:    repositoryKey,
			Language:         facts.LanguageTypeScript,
			RequestedPackage: entry.conflict.Name,
			Reason:           "AMBIGUOUS_PACKAGE_PROVIDER",
			Detail:           "declared by " + strings.Join(entry.conflict.Manifests, " and "),
		}},
	}
}

func modulesByRepository(modules []goworkspace.Module) map[string][]goworkspace.Module {
	result := make(map[string][]goworkspace.Module)
	for _, module := range modules {
		result[module.Repository] = append(result[module.Repository], module)
	}
	for repository := range result {
		sort.Slice(result[repository], func(left, right int) bool {
			if result[repository][left].ModulePath != result[repository][right].ModulePath {
				return result[repository][left].ModulePath < result[repository][right].ModulePath
			}
			return result[repository][left].RootPath < result[repository][right].RootPath
		})
	}
	return result
}

func conflictSubjects(conflicts []goworkspace.Conflict) []string {
	result := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		if strings.TrimSpace(conflict.Subject) != "" {
			result = append(result, conflict.Subject)
		}
	}
	sort.Strings(result)
	return result
}

func formatPackageErrors(errors []goloader.PackageError) string {
	parts := make([]string, 0, len(errors))
	for _, failure := range errors {
		parts = append(parts, fmt.Sprintf("%s %s: %s", failure.PackagePath, failure.Position, failure.Message))
	}
	return strings.Join(parts, "; ")
}

// writeWorkspaces installs one synthetic go.work per independent group of
// modules and answers the file each module must load with.
//
// A module that reaches no other registered module needs no workspace: its own
// manifest already resolves it, exactly as its own toolchain would. One shared
// workspace would resolve a single build list for every repository at once, so
// a dependency bumped in one of them changes the versions selected for all the
// others -- and a version no repository downloaded on its own breaks every
// load together.
func writeWorkspaces(
	ctx context.Context,
	base string,
	plan goworkspace.Plan,
	repositories []workspace.Repository,
) (map[string]string, int, error) {
	files := make(map[string]string, len(plan.Modules))
	used := make(map[string]struct{})
	written := 0
	for _, group := range plan.Partition() {
		if len(group.Modules) == 1 && len(group.Modules[0].WorkspaceMembers) == 0 {
			continue
		}
		target := base
		if written > 0 {
			target = siblingWorkFile(base, written)
		}
		if _, err := goworkspace.Write(ctx, target, group, repositories); err != nil {
			return nil, 0, fmt.Errorf("write synthetic Go workspace %q: %w", target, err)
		}
		for _, module := range group.Modules {
			files[module.ModulePath] = target
		}
		used[target] = struct{}{}
		written++
	}
	if err := removeUnusedWorkFiles(base, used); err != nil {
		return nil, 0, err
	}
	return files, written, nil
}

func siblingWorkFile(base string, index int) string {
	extension := filepath.Ext(base)
	return strings.TrimSuffix(base, extension) + "." + strconv.Itoa(index) + extension
}

// removeUnusedWorkFiles drops the synthetic files this run did not need, so a
// workspace left by another set of repositories cannot be mistaken for the one
// in force. The go command keeps its hashes in a sibling `.sum`, which is
// meaningless once its workspace is gone.
func removeUnusedWorkFiles(base string, used map[string]struct{}) error {
	extension := filepath.Ext(base)
	candidates, err := filepath.Glob(strings.TrimSuffix(base, extension) + ".*" + extension)
	if err != nil {
		return fmt.Errorf("scan synthetic Go workspaces: %w", err)
	}
	for _, candidate := range append(candidates, base) {
		if _, keep := used[candidate]; keep {
			continue
		}
		for _, target := range []string{candidate, candidate + ".sum"} {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove unused synthetic Go workspace %q: %w", target, err)
			}
		}
	}
	return nil
}

// toolchainHint explains a diagnostic that no repository can act on: go/types
// is linked into this binary, so a dependency written for a newer language
// version is unreadable no matter what the repository declares.
func toolchainHint(errors []goloader.PackageError) string {
	for _, failure := range errors {
		if !strings.Contains(failure.Message, "file requires newer Go version") {
			continue
		}
		return fmt.Sprintf(
			" (this build type-checks with go %s; rebuild Ladygraph with a toolchain at least as new as the sources it must read)",
			goworkspace.LanguageVersion())
	}
	return ""
}

// mergeSets merges every unit's facts into one set. A repository appears in
// as many sets as it has units, and MergeAll keeps the first record of each
// key, so the languages of the later ones are collected here and replace it:
// a Go repository that also holds TypeScript must not lose either language
// because one of its two units happened to be merged first.
func mergeSets(sets []facts.Set) facts.Set {
	languages := make(map[string][]facts.Language)
	for _, set := range sets {
		for _, repository := range set.Repositories {
			languages[repository.Key] = append(languages[repository.Key], repository.Languages...)
		}
	}
	merged := facts.MergeAll(sets)
	for index := range merged.Repositories {
		seen := make(map[facts.Language]struct{})
		union := make([]facts.Language, 0, len(languages[merged.Repositories[index].Key]))
		for _, language := range languages[merged.Repositories[index].Key] {
			if _, exists := seen[language]; exists {
				continue
			}
			seen[language] = struct{}{}
			union = append(union, language)
		}
		sort.Slice(union, func(left, right int) bool { return union[left] < union[right] })
		merged.Repositories[index].Languages = union
	}
	return merged
}

func collectTypeScriptFacts(
	ctx context.Context,
	options FullOptions,
	consumer typeScriptPackageUnit,
	providers []typeScriptPackageUnit,
) (facts.TypeScriptPayload, error) {
	repository := consumer.repository
	packageValue := consumer.packageValue
	output, err := os.CreateTemp("", "ladygraph-ts-facts-*.json")
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("create TypeScript facts output for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return facts.TypeScriptPayload{}, fmt.Errorf("close TypeScript facts output for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	defer os.Remove(outputPath)

	arguments := []string{
		"facts", repository.Name, repository.RealPath, outputPath,
		"--project", packageValue.ProjectPath,
	}
	for _, provider := range providers {
		if provider.repository.Name == repository.Name {
			continue
		}
		arguments = append(arguments,
			"--provider", provider.repository.Name+"="+provider.repository.RealPath,
			"--provider-project", provider.repository.Name+"="+provider.packageValue.ProjectPath,
		)
	}
	command, commandArguments, err := factsCommand(options, arguments)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("prepare TypeScript facts command for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	commandContext := exec.CommandContext(ctx, command, commandArguments...)
	commandContext.Dir = options.WorkingDirectory
	var stdout, stderr bytes.Buffer
	commandContext.Stdout = &stdout
	commandContext.Stderr = &stderr
	if err := commandContext.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail != "" {
			return facts.TypeScriptPayload{}, fmt.Errorf("run TypeScript facts for %q package %q: %w: %s",
				repository.Name, packageValue.Name, err, detail)
		}
		return facts.TypeScriptPayload{}, fmt.Errorf("run TypeScript facts for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("read TypeScript facts for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	payload, err := facts.DecodeTypeScriptPayload(data)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("decode TypeScript facts for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	return payload, nil
}

// analysisUnit is one independent piece of work: a Go module or a TypeScript
// package. Nothing in a unit reads another unit's state, which is what lets
// them run at the same time.
type analysisUnit struct {
	repository workspace.Repository
	module     goworkspace.Module
	pkg        typeScriptPackageUnit
	workFile   string
	isGo       bool
}

// weight estimates how long a unit will take, so the long poles start first.
//
// The estimate is the number of source files the unit will read. It is a
// proxy, not a measurement -- a thousand trivial files are cheaper than a
// hundred generic ones -- but the scheduling question is only which unit must
// not be left for last, and for that a proxy is enough: a pass ends when its
// slowest unit ends, and starting that one late adds its whole duration to
// the tail.
func (unit analysisUnit) weight() int {
	if unit.isGo {
		return len(unit.module.PackagePatterns)
	}
	return len(unit.pkg.packageValue.SourceRoots) + unit.pkg.files
}

func (unit analysisUnit) detail() string {
	if unit.isGo {
		return unit.module.ModulePath
	}
	return unit.pkg.packageValue.Name
}

// analysisResult is what one unit contributes to the pass.
type analysisResult struct {
	set facts.Set
	// notLoaded marks a module whose facts are absent because the loader
	// could not read it. The pass continues and the graph declares it.
	notLoaded       bool
	loadDiagnostics int
	definitions     int
	references      int
	unresolved      int
	symbols         int
	detail          string
	// requested names every package the unit asked about, resolved or
	// not. It is what the fact cache depends on besides the sources.
	requested []string
	// diagnostics are what the loader said without blocking the pass.
	diagnostics []string
}

// moduleNotLoadedFacts declares a Go module the loader could not read.
//
// The repository record travels with the entry for the same reason the
// ambiguous-package one does: a repository whose only module failed
// contributes nothing else, and an unresolved reference in a repository the
// set does not know is not a valid fact. The detail keeps the diagnostics the
// go command produced, so the answer to "why is this repository empty" is in
// the graph rather than in a log nobody kept.
func moduleNotLoadedFacts(unit analysisUnit, blocking []goloader.PackageError) analysisResult {
	repositoryKey := facts.RepositoryKey(unit.repository.Name)
	detail := formatPackageErrors(blocking) + toolchainHint(blocking)
	return analysisResult{
		set: facts.Set{
			Repositories: []facts.Repository{{
				Key:       repositoryKey,
				Name:      unit.repository.Name,
				RootPath:  unit.repository.RealPath,
				Languages: []facts.Language{facts.LanguageGo},
			}},
			Unresolved: []facts.UnresolvedReference{{
				RepositoryKey:    repositoryKey,
				Language:         facts.LanguageGo,
				RequestedPackage: unit.module.ModulePath,
				Reason:           "MODULE_NOT_LOADED",
				Detail:           detail,
			}},
		},
		unresolved: 1,
		notLoaded:  true,
		detail:     "not loaded: " + firstLine(detail),
	}
}

func firstLine(value string) string {
	if index := strings.IndexAny(value, ";\n"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}

// indexGoModule turns one Go module into facts. Every step already takes its
// own inputs; the module registry and the workspace conflicts are read-only
// for the whole pass.
func indexGoModule(
	ctx context.Context,
	options FullOptions,
	unit analysisUnit,
	moduleRegistry *goloader.ModuleRegistry,
	conflictingModules []string,
	planConflicts []goworkspace.Conflict,
) (analysisResult, error) {
	repository, module := unit.repository, unit.module
	load, err := goloader.Load(ctx, goloader.Options{
		Directory:    module.RootPath,
		WorkFile:     unit.workFile,
		Patterns:     append([]string(nil), module.PackagePatterns...),
		IncludeTests: options.IncludeTests,
		BuildTags:    append([]string(nil), options.GoBuildTags...),
		AllowNetwork: options.GoAllowNetwork,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("load Go module %q for %q: %w", module.ModulePath, repository.Name, err)
	}
	blocking := load.BlockingErrors()
	if len(blocking) != 0 {
		// One module that does not load is not 32 repositories that
		// cannot be indexed. Its facts are untrustworthy, so none are
		// published for it, and the graph says which module and why
		// instead of leaving a hole nobody can see. A repository whose
		// dependencies were never downloaded is the common case, and it
		// must not decide whether everything else gets a graph.
		return moduleNotLoadedFacts(unit, blocking), nil
	}
	definitions, err := goloader.ExtractDefinitions(ctx, load, goloader.DefinitionOptions{Repository: repository.Name})
	if err != nil {
		return analysisResult{}, fmt.Errorf("extract Go definitions for %q: %w", repository.Name, err)
	}
	keyed, err := goloader.AssignStableKeys(ctx, definitions)
	if err != nil {
		return analysisResult{}, fmt.Errorf("assign Go stable keys for %q: %w", repository.Name, err)
	}
	uses, err := goloader.ExtractUses(ctx, load, goloader.UseOptions{Repository: repository.Name})
	if err != nil {
		return analysisResult{}, fmt.Errorf("extract Go uses for %q: %w", repository.Name, err)
	}
	packageDependencies, err := goloader.ResolvePackageDependencies(ctx, uses)
	if err != nil {
		return analysisResult{}, fmt.Errorf("resolve Go package dependencies for %q: %w", repository.Name, err)
	}
	references, err := goloader.ClassifyReferences(ctx, load, uses)
	if err != nil {
		return analysisResult{}, fmt.Errorf("classify Go references for %q: %w", repository.Name, err)
	}
	cross, err := goloader.ResolveCrossRepository(ctx, uses, moduleRegistry, goloader.CrossRepositoryOptions{
		ConsumerRepository: repository.Name,
		ConflictingModules: conflictingModules,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("resolve Go cross-repository references for %q: %w", repository.Name, err)
	}
	typeRelations, err := goloader.ResolveTypeRelations(ctx, load, goloader.TypeRelationOptions{Repository: repository.Name})
	if err != nil {
		return analysisResult{}, fmt.Errorf("resolve Go type relations for %q: %w", repository.Name, err)
	}
	unresolved, err := goloader.ClassifyUnresolved(ctx, load, cross, goloader.UnresolvedOptions{
		Repository:         repository.Name,
		WorkspaceConflicts: planConflicts,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("classify Go unresolved references for %q: %w", repository.Name, err)
	}
	set, _, err := facts.NormalizeGo(ctx, facts.GoInput{
		Repository:          repository,
		Definitions:         keyed,
		References:          references,
		CrossRepository:     cross,
		PackageDependencies: packageDependencies,
		TypeRelations:       typeRelations,
		Unresolved:          unresolved,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("normalise Go facts for %q: %w", repository.Name, err)
	}
	return analysisResult{
		set:             set,
		loadDiagnostics: len(load.Errors) - len(blocking),
		diagnostics:     nonBlockingDiagnostics(load.Errors, blocking, module.ModulePath),
		definitions:     len(keyed),
		references:      len(references),
		unresolved:      len(unresolved),
	}, nil
}

// nonBlockingDiagnostics renders what the loader said about a module it
// nevertheless read.
//
// Some of these become UNRESOLVED entries in the graph -- a package that
// failed to type check, one whose build constraints selected no file -- and
// the rest, the go command's own resolution and configuration complaints, are
// facts about the pass rather than about a symbol. Those used to be counted
// and dropped, which left "diagnostics=3" and no way to learn what the three
// were.
func nonBlockingDiagnostics(errors, blocking []goloader.PackageError, modulePath string) []string {
	if len(errors) == len(blocking) {
		return nil
	}
	excluded := make(map[string]struct{}, len(blocking))
	for _, failure := range blocking {
		excluded[failure.PackagePath+"\x00"+failure.Position+"\x00"+failure.Message] = struct{}{}
	}
	diagnostics := make([]string, 0, len(errors)-len(blocking))
	for _, failure := range errors {
		if _, isBlocking := excluded[failure.PackagePath+"\x00"+failure.Position+"\x00"+failure.Message]; isBlocking {
			continue
		}
		where := failure.PackagePath
		if where == "" {
			where = modulePath
		}
		if failure.Position != "" {
			where += " " + failure.Position
		}
		diagnostics = append(diagnostics, fmt.Sprintf("%s [%s] %s", where, failure.Kind, failure.Message))
	}
	return diagnostics
}

// indexTypeScriptPackage turns one TypeScript package into facts. The worker
// runs as its own process with its own output file, so packages never share
// anything but the providers they are told about.
func indexTypeScriptPackage(
	ctx context.Context,
	options FullOptions,
	unit analysisUnit,
	providers []typeScriptPackageUnit,
) (analysisResult, error) {
	repository, packageValue := unit.repository, unit.pkg.packageValue
	payload, err := collectTypeScriptFacts(ctx, options, unit.pkg, providers)
	if err != nil {
		return analysisResult{}, err
	}
	set, _, err := facts.NormalizeTypeScript(ctx, payload, repository.RealPath)
	if err != nil {
		return analysisResult{}, fmt.Errorf("normalise TypeScript facts for %q package %q: %w",
			repository.Name, packageValue.Name, err)
	}
	return analysisResult{
		set:        set,
		symbols:    len(payload.Symbols),
		references: len(payload.References),
		unresolved: len(payload.Unresolved),
		requested:  requestedTypeScriptPackages(payload),
		detail: fmt.Sprintf("package=%s symbols=%d references=%d",
			packageValue.Name, len(payload.Symbols), len(payload.References)),
	}, nil
}

// requestedTypeScriptPackages names every package the worker asked about.
// A resolved import and a failed one are the same kind of dependency: both
// answers change when the package that would provide the name appears,
// disappears or changes what it exports.
func requestedTypeScriptPackages(payload facts.TypeScriptPayload) []string {
	names := make([]string, 0, len(payload.Imports)+len(payload.Unresolved)+len(payload.Dependencies))
	for _, entry := range payload.Imports {
		names = append(names, entry.RequestedPackage)
	}
	for _, entry := range payload.Unresolved {
		names = append(names, entry.RequestedPackage)
	}
	for _, entry := range payload.Dependencies {
		names = append(names, entry.Package)
	}
	return names
}
func factsCommand(options FullOptions, arguments []string) (string, []string, error) {
	commandLine := strings.TrimSpace(options.TypeScriptWorker)
	if commandLine == "" {
		return "", nil, errors.New("TypeScript worker command is empty")
	}
	parts := strings.Fields(commandLine)
	if len(parts) == 0 {
		return "", nil, errors.New("TypeScript worker command is empty")
	}
	command := parts[0]
	commandArguments := append([]string(nil), parts[1:]...)
	if _, lookupErr := exec.LookPath(command); lookupErr == nil {
		return command, append(commandArguments, arguments...), nil
	} else if command != "ladygraph-ts-worker" {
		return "", nil, fmt.Errorf("worker command %q is not executable: %w", command, lookupErr)
	}

	workerRoot := filepath.Join(options.WorkingDirectory, "ts-worker")
	factsEntry := filepath.Join(workerRoot, "dist", "facts-cli.js")
	if info, statErr := os.Stat(factsEntry); statErr == nil && info.Mode().IsRegular() {
		return "node", append([]string{factsEntry}, arguments[1:]...), nil
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		return "", nil, fmt.Errorf("default worker is unavailable and pnpm is not executable: %w", err)
	}
	return "pnpm", append([]string{"--dir", workerRoot}, arguments...), nil
}

// DecodeFactsJSON is kept small and explicit for callers that persist a facts
// set between indexing and rebuilding.
func DecodeFactsJSON(data []byte) (facts.Set, error) {
	var set facts.Set
	if err := json.Unmarshal(data, &set); err != nil {
		return facts.Set{}, fmt.Errorf("decode facts JSON: %w", err)
	}
	if err := set.Validate(); err != nil {
		return facts.Set{}, err
	}
	return set, nil
}

// analysisInputs are the read-only facts every unit shares.
type analysisInputs struct {
	moduleRegistry     *goloader.ModuleRegistry
	conflictingModules []string
	planConflicts      []goworkspace.Conflict
	typeScriptPackages []typeScriptPackageUnit
	// goModules and workFiles say which modules share a synthetic
	// workspace: a module's type information includes its group's source.
	goModules []goworkspace.Module
	workFiles map[string]string
}

func typeScriptUnits(packages []typeScriptPackageUnit) []analysisUnit {
	units := make([]analysisUnit, 0, len(packages))
	for _, packageUnit := range packages {
		units = append(units, analysisUnit{repository: packageUnit.repository, pkg: packageUnit})
	}
	return units
}

// analyse runs every unit concurrently and answers their results in the order
// of the units.
//
// The units are independent by construction: a Go load builds its own type
// universe and a TypeScript package is a separate worker process with its own
// output file. Running them one after another left a machine with many cores
// idle for most of an index, and the analysis is the larger half of the pass.
//
// Each kind drains its own queue, longest unit first, with as many workers as
// its budget allows and never more than it has units. Dispatch order matters
// because a pass ends when its slowest unit ends: starting the slowest one
// last adds its whole duration to the tail. The first failure cancels the
// rest -- a pass that cannot publish should stop paying for work nobody will
// use.
func analyse(
	ctx context.Context,
	options FullOptions,
	units []analysisUnit,
	inputs analysisInputs,
) ([]analysisResult, CacheReport, error) {
	results := make([]analysisResult, len(units))
	if len(units) == 0 {
		return results, CacheReport{Mode: CacheOff}, nil
	}
	cache, err := newFactCache(options)
	if err != nil {
		return nil, CacheReport{Mode: CacheOff}, err
	}
	cache.trees.withProviders(inputs.typeScriptPackages)
	cache.withRegistry(inputs)
	var goQueue, typeScriptQueue []int
	for index, unit := range units {
		if unit.isGo {
			goQueue = append(goQueue, index)
			continue
		}
		typeScriptQueue = append(typeScriptQueue, index)
	}
	byWeight := func(queue []int) {
		sort.SliceStable(queue, func(left, right int) bool {
			return units[queue[left]].weight() > units[queue[right]].weight()
		})
	}
	byWeight(goQueue)
	byWeight(typeScriptQueue)

	group, groupCtx := errgroup.WithContext(ctx)
	report := serializedProgress(options.Progress)
	run := func(queue []int, limit int, phase ProgressPhase) {
		if len(queue) == 0 {
			return
		}
		pending := make(chan int, len(queue))
		for _, index := range queue {
			pending <- index
		}
		close(pending)
		var done atomic.Int64
		total := len(queue)
		for range min(limit, len(queue)) {
			group.Go(func() error {
				for index := range pending {
					if err := groupCtx.Err(); err != nil {
						return err
					}
					unit := units[index]
					report(ProgressEvent{
						Phase: phase, Repository: unit.repository.Name, Detail: unit.detail(),
						Started: true, Completed: int(done.Load()), Total: total,
					})
					result, err := cache.analyse(groupCtx, options, unit, inputs)
					if err != nil {
						return err
					}
					results[index] = result
					detail := result.detail
					if detail == "" {
						detail = unit.detail()
					}
					report(ProgressEvent{
						Phase: phase, Repository: unit.repository.Name, Detail: detail,
						Completed: int(done.Add(1)), Total: total,
					})
				}
				return nil
			})
		}
	}
	run(goQueue, goLoadLimit(options), PhaseGo)
	run(typeScriptQueue, typeScriptWorkerLimit(options), PhaseTypeScript)

	if err := group.Wait(); err != nil {
		return nil, cache.report(), err
	}
	// Entries are only useful while something still asks for them, and two
	// workspaces indexed from the same home share this directory.
	cache.prune(30 * 24 * time.Hour)
	return results, cache.report(), nil
}

func analyseUnit(
	ctx context.Context,
	options FullOptions,
	unit analysisUnit,
	inputs analysisInputs,
) (analysisResult, error) {
	if unit.isGo {
		return indexGoModule(ctx, options, unit,
			inputs.moduleRegistry, inputs.conflictingModules, inputs.planConflicts)
	}
	return indexTypeScriptPackage(ctx, options, unit, inputs.typeScriptPackages)
}

// serializedProgress makes one callback safe to call from every unit. The
// contract says a progress callback must not block; it never said it would be
// called from one goroutine.
func serializedProgress(report func(ProgressEvent)) func(ProgressEvent) {
	if report == nil {
		return func(ProgressEvent) {}
	}
	var mutex sync.Mutex
	return func(event ProgressEvent) {
		mutex.Lock()
		defer mutex.Unlock()
		report(event)
	}
}

// goLoadLimit bounds concurrent Go loads. Each one holds a complete type
// universe, so the ceiling exists to keep a large registry from trading the
// whole machine's memory for the last few seconds of an index.
func goLoadLimit(options FullOptions) int {
	if options.GoMaximumLoads > 0 {
		return options.GoMaximumLoads
	}
	limit := runtime.GOMAXPROCS(0)
	if limit > defaultGoLoadLimit {
		return defaultGoLoadLimit
	}
	if limit < 1 {
		return 1
	}
	return limit
}

func typeScriptWorkerLimit(options FullOptions) int {
	if options.TypeScriptMaximumWorkers > 0 {
		return options.TypeScriptMaximumWorkers
	}
	return defaultTypeScriptWorkerLimit
}
