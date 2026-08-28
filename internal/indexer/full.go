// Package indexer runs the full analysis pass that turns registered
// repositories into semantic facts, and owns the fact cache that pass may
// reuse -- a cache that must agree with the analysis or be reported diverged.
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

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/dartloader"
	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/goloader"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/syntax"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// defaultGoLoadLimit and defaultTypeScriptWorkerLimit bound concurrent
// analysis when the caller states no budget of its own.
const (
	defaultGoLoadLimit           = 8
	defaultTypeScriptWorkerLimit = 3
	defaultPythonWorkerLimit     = 3
	defaultDartWorkerLimit       = 2
	// defaultJavaWorkerLimit is one: scip-java drives the project's own build
	// tool, and two Maven or Gradle daemons on one machine compete for the
	// same local repository and the same heap.
	defaultJavaWorkerLimit = 1
	// defaultCSharpWorkerLimit is one for the reason Java's is: scip-dotnet
	// runs `dotnet restore`, and two restores share one package cache.
	defaultCSharpWorkerLimit = 1
)

// ProgressPhase names the unit of work a progress event belongs to.
type ProgressPhase string

const (
	// PhaseGo is one Go module of one repository.
	PhaseGo ProgressPhase = "go"
	// PhaseTypeScript is one TypeScript repository.
	PhaseTypeScript ProgressPhase = "typescript"
	// PhaseRust is one Cargo workspace.
	PhaseRust ProgressPhase = "rust"
	// PhasePython is one Python repository.
	PhasePython ProgressPhase = "python"
	// PhaseDart is one Dart package repository.
	PhaseDart ProgressPhase = "dart"
	// PhaseJava is one Java repository.
	PhaseJava ProgressPhase = "java"
	// PhaseCSharp is one C# repository.
	PhaseCSharp ProgressPhase = "csharp"
	// PhaseSemantic names a semantic unit whose language has no phase of its
	// own. Nothing reaches it today; it exists so an unnamed language reports
	// progress under a truthful label instead of another language's.
	PhaseSemantic ProgressPhase = "semantic"
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
	GoOS              string
	GoARCH            string
	GoCGOEnabled      *bool
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
	// TypeScriptIncludeUnclaimedSources indexes the TypeScript files no
	// project of a repository claims, through the engine's inferred project.
	// It is off by default: those files are real code with real callers, and
	// the compiler options they are checked under are Kivgraph's choice
	// rather than a declaration of the project that would have owned them.
	TypeScriptIncludeUnclaimedSources bool
	// RustAnalyzer is the command line of the external Rust indexer.
	RustAnalyzer string
	// RustTargetDirectory is where cargo writes the artifacts of the Rust
	// analysis. It lives outside every indexed repository, because
	// rust-analyzer always runs build scripts.
	RustTargetDirectory string
	// RustMaximumWorkspaces bounds concurrent analyzer processes. Zero uses
	// the documented default: each one holds a whole workspace in memory.
	RustMaximumWorkspaces int
	RustFeatures          []string
	RustAllFeatures       bool
	RustNoDefaultFeatures bool
	RustCfgs              []string
	RustBuildScripts      bool
	RustProcMacros        bool
	// RustIncludeTests sets `cfg(test)` for the crates of a workspace.
	RustIncludeTests bool
	// RustAllowNetwork lets cargo reach a registry while a workspace loads.
	RustAllowNetwork bool
	RustSysroot      string
	// RustIndexSysroot puts the standard library of the toolchain in the
	// graph as a synthetic provider. It is off by default: it multiplies the
	// symbol count by an order of magnitude, and its absence leaves the
	// graph exactly as it was, declaring what it lost.
	RustIndexSysroot        bool
	PythonIndexer           string
	PythonAnalyzer          string
	PythonAnalyzerMode      string
	PythonPath              string
	PythonMaximumWorkers    int
	PythonIncludeTests      bool
	PythonIncludeGenerated  bool
	PythonIncludeExternal   bool
	DartAnalyzer            string
	DartSDKPath             string
	DartMaximumWorkers      int
	DartIncludeTests        bool
	DartIncludeGenerated    bool
	DartIncludeExternal     bool
	DartIncludeSDK          bool
	DartPackageConfig       string
	DartWaitForAnalysis     bool
	DartMaximumAnalysisTime time.Duration
	JavaIndexerCommand      string
	JavaBuildTool           string
	JavaMaximumWorkers      int
	JavaIncludeTests        bool
	JavaIncludeGenerated    bool
	// JavaTargetDirectory is where scip-java writes its SemanticDB output and
	// its index. It lives outside every indexed repository for the reason
	// RustTargetDirectory does: the analyzer's output is not the repository's
	// source, and an index that leaves artefacts behind has modified what it
	// came to read.
	JavaTargetDirectory    string
	JavaMaximumIndexTime   time.Duration
	CSharpIndexerCommand   string
	CSharpProject          string
	CSharpMaximumWorkers   int
	CSharpIncludeTests     bool
	CSharpIncludeGenerated bool
	CSharpSkipRestore      bool
	CSharpTargetDirectory  string
	CSharpMaximumIndexTime time.Duration
	WorkingDirectory       string
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
	// TypeScript that declare no named package with a project. A manifest
	// alone is not one: a repository of loose .mjs files beside a
	// package.json has no project for a program to be built from. They
	// contribute nothing, and a registry entry that contributes nothing
	// looks like coverage.
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
	// GoModulesNotRead names each of those modules and why, one line each.
	//
	// It is not GoDiagnostics: that field is what the loader said about a
	// module it nevertheless read, and this is the class that produced no
	// facts at all. Keeping them apart is what lets a reader see
	// "not_loaded=4 diagnostics=1" and still learn what the four were --
	// the count travels today and the reason was reachable only by
	// querying the published graph for its MODULE_NOT_LOADED rows, which
	// no command exposes. A hollow graph nobody can explain is how an
	// index passes and answers about nothing.
	GoModulesNotRead []string
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
	// TypeScriptUnclaimedSources counts the source files no project of a
	// repository claims and that this pass indexed anyway, through the
	// inferred project. It is zero unless
	// TypeScriptIncludeUnclaimedSources is on.
	TypeScriptUnclaimedSources int
	// TypeScriptUnclaimedWithoutPackage names the unclaimed files that no
	// package unit encloses. Nothing can index them -- a file needs a
	// package to belong to and a project to be analysed beside -- so the
	// pass names them instead of dropping them in silence.
	TypeScriptUnclaimedWithoutPackage []string
	RustRepositories                  int
	RustWorkspaces                    int
	RustCrates                        int
	RustSymbols                       int
	RustReferences                    int
	RustUnresolved                    int
	// RustWorkspacesNotLoaded counts the workspaces the analyzer could not
	// read. Their facts are absent and declared; the pass publishes the rest.
	RustWorkspacesNotLoaded int
	// RustDiagnostics carries what the analyzer reported without blocking
	// the pass, which is where a degraded load says so.
	RustDiagnostics []string
	// RustWithoutWorkspaces names the repositories registered as Rust that
	// declare no Cargo manifest. They contribute nothing, and a registry
	// entry that contributes nothing looks like coverage.
	RustWithoutWorkspaces []string
	PythonRepositories    int
	PythonSymbols         int
	PythonReferences      int
	PythonUnresolved      int
	// PythonRepositoriesNotLoaded and DartRepositoriesNotLoaded count the
	// repositories whose analyzer is not installed on this machine. Their
	// facts are absent and the pass says so, rather than one absent toolchain
	// deciding whether every other repository gets a graph.
	PythonRepositoriesNotLoaded int
	DartRepositories            int
	DartSymbols                 int
	DartReferences              int
	DartUnresolved              int
	DartRepositoriesNotLoaded   int
	JavaRepositories            int
	JavaSymbols                 int
	JavaReferences              int
	JavaUnresolved              int
	JavaRepositoriesNotLoaded   int
	CSharpRepositories          int
	CSharpSymbols               int
	CSharpReferences            int
	CSharpUnresolved            int
	CSharpRepositoriesNotLoaded int
	// EdgesWithoutProvider counts the edges the merge dropped because the
	// repository that provides the target does not publish its declaration.
	// Each one is declared as an unresolved reference with the position that
	// was observed.
	EdgesWithoutProvider int
	// RustSysroot names the synthetic repository the standard library was
	// published under, empty when it is not in this graph.
	RustSysroot string
	// RustSysrootReason says why the standard library is absent. It is set
	// whenever RustSysroot is empty, because a reader cannot tell a machine
	// without a toolchain from a configuration that asked for nothing.
	RustSysrootReason string
	SyntheticWorkFile string
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
	configuredDartRepositories := repositoriesForLanguage(repositories, "dart")
	if options.DartIncludeExternal {
		for _, repository := range configuredDartRepositories {
			root := repository.RealPath
			if root == "" {
				root = repository.Path
			}
			providers := dartloader.ExternalPackageRepositories(root, options.DartPackageConfig)
			for _, provider := range providers {
				if _, exists := repositoryByName(repositories, provider.Name); exists {
					continue
				}
				repositories = append(repositories, provider)
				options.Repositories = append(options.Repositories, provider)
			}
		}
	}

	goRepositories := repositoriesForLanguage(repositories, "go")
	typeScriptRepositories := repositoriesForTypeScript(repositories)
	rustRepositories := repositoriesForRust(repositories)
	pythonRepositories := repositoriesForPython(repositories)
	dartRepositories := repositoriesForLanguage(repositories, "dart")
	javaRepositories := repositoriesForLanguage(repositories, "java")
	cSharpRepositories := repositoriesForLanguage(repositories, "csharp")
	cSharpRepositories = append(cSharpRepositories, repositoriesForLanguage(repositories, "cs")...)
	cSharpRepositories = dedupeRepositories(cSharpRepositories)
	if options.DartIncludeSDK {
		sdkRoot, sdkErr := dartloader.SDKRoot(options.DartSDKPath)
		if sdkErr != nil {
			return facts.Set{}, FullReport{}, fmt.Errorf("discover Dart SDK provider: %w", sdkErr)
		}
		const sdkRepositoryName = "dart-sdk"
		if _, exists := repositoryByName(repositories, sdkRepositoryName); !exists {
			sdkRepository := workspace.Repository{Name: sdkRepositoryName, Path: sdkRoot, RealPath: sdkRoot, Languages: []string{"dart"}, Roots: []string{"lib"}}
			repositories = append(repositories, sdkRepository)
			options.Repositories = append(options.Repositories, sdkRepository)
			dartRepositories = append(dartRepositories, sdkRepository)
		}
	}
	report := FullReport{
		GoRepositories:         len(goRepositories),
		TypeScriptRepositories: len(typeScriptRepositories),
		RustRepositories:       len(rustRepositories),
		PythonRepositories:     len(pythonRepositories),
		DartRepositories:       len(dartRepositories),
		JavaRepositories:       len(javaRepositories),
		CSharpRepositories:     len(cSharpRepositories),
	}
	typeScriptDiscovered, err := discoverTypeScriptPackages(
		ctx, typeScriptRepositories, options.TypeScriptIncludeUnclaimedSources)
	if err != nil {
		return facts.Set{}, report, err
	}
	typeScriptPackages := typeScriptDiscovered.packages
	report.TypeScriptWithoutPackages = typeScriptDiscovered.withoutPackages
	report.TypeScriptUnclaimedWithoutPackage = typeScriptDiscovered.unclaimedWithoutPackage
	for _, unit := range typeScriptPackages {
		report.TypeScriptUnclaimedSources += len(unit.unclaimed)
	}
	// Every unit's facts are merged in one pass at the end of the pass, not
	// one at a time: a pairwise merge pays for the whole accumulated graph
	// on every step.
	sets := make([]facts.Set, 0, len(typeScriptDiscovered.conflicts))
	for _, entry := range typeScriptDiscovered.conflicts {
		sets = append(sets, ambiguousPackageFacts(entry))
		report.TypeScriptAmbiguous++
	}

	rustUnits, rustCrateRegistry, rustParsers, err := prepareRust(ctx, options, rustRepositories, &report)
	if err != nil {
		return facts.Set{}, report, err
	}
	if rustParsers != nil {
		defer rustParsers.Close()
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
					workFile: workFiles[module.ModulePath], kind: unitGo,
				})
			}
		}
	}

	units := append(goUnits, typeScriptUnits(typeScriptPackages)...)
	units = append(units, rustAnalysisUnits(rustUnits)...)
	units = append(units, semanticUnits(pythonRepositories, facts.LanguagePython)...)
	units = append(units, semanticUnits(dartRepositories, facts.LanguageDart)...)
	units = append(units, semanticUnits(javaRepositories, facts.LanguageJava)...)
	units = append(units, semanticUnits(cSharpRepositories, facts.LanguageCSharp)...)
	results, cacheReport, err := analyse(ctx, options, units, analysisInputs{
		moduleRegistry:     moduleRegistry,
		conflictingModules: conflictingModules,
		planConflicts:      planConflicts,
		typeScriptPackages: typeScriptPackages,
		goModules:          goModules,
		workFiles:          workFiles,
		crateRegistry:      rustCrateRegistry,
		parsers:            rustParsers,
	})
	if err != nil {
		return facts.Set{}, report, err
	}
	report.Cache = cacheReport

	// The merge follows the order of the units, never the order they
	// finished, so the published graph does not depend on how the work was
	// scheduled.
	composed := make(map[string]composedTarget)
	for index, unit := range units {
		result := results[index]
		sets = append(sets, result.set)
		for key, target := range result.composed {
			composed[key] = target
		}
		switch unit.kind {
		case unitGo:
			report.GoLoads++
			report.GoModules++
			if result.notLoaded {
				report.GoModulesNotLoaded++
				if result.notRead != "" {
					report.GoModulesNotRead = append(report.GoModulesNotRead, result.notRead)
				}
			}
			report.GoLoadDiagnostics += result.loadDiagnostics
			report.GoDiagnostics = append(report.GoDiagnostics, result.diagnostics...)
			report.GoDefinitions += result.definitions
			report.GoReferences += result.references
			report.GoUnresolved += result.unresolved
		case unitRust:
			report.RustWorkspaces++
			report.RustCrates += len(unit.rust.crates)
			if result.notLoaded {
				report.RustWorkspacesNotLoaded++
			}
			report.RustDiagnostics = append(report.RustDiagnostics, result.diagnostics...)
			report.RustSymbols += result.symbols
			report.RustReferences += result.references
			report.RustUnresolved += result.unresolved
		case unitSemantic:
			report.addSemantic(unit.language, result)
		default:
			report.TypeScriptSymbols += result.symbols
			report.TypeScriptReferences += result.references
			report.TypeScriptUnresolved += result.unresolved
		}
	}

	emitProgress(options.Progress, ProgressEvent{Phase: PhaseMerge, Started: true})
	merged := mergeSets(sets, options.Repositories)
	merged, withoutProvider := closeCrossRepositoryEdges(merged, composed)
	merged = resolveSemanticPackageDependencies(merged)
	report.EdgesWithoutProvider = withoutProvider
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

func repositoryByName(repositories []workspace.Repository, name string) (workspace.Repository, bool) {
	for _, repository := range repositories {
		if repository.Name == name {
			return repository, true
		}
	}
	return workspace.Repository{}, false
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

func repositoriesForPython(repositories []workspace.Repository) []workspace.Repository {
	result := make([]workspace.Repository, 0)
	for _, repository := range repositories {
		for _, candidate := range repository.Languages {
			if strings.EqualFold(strings.TrimSpace(candidate), "python") || strings.EqualFold(strings.TrimSpace(candidate), "py") {
				result = append(result, repository)
				break
			}
		}
	}
	return result
}

type typeScriptPackageUnit struct {
	repository   workspace.Repository
	packageValue workspace.TypeScriptPackage
	// files is how many source files the worker will read. It orders the
	// queue; it is never a fact about the graph.
	files int
	// unclaimed are the absolute source files this package encloses that no
	// project of the repository claims, sorted. It is empty unless
	// FullOptions.TypeScriptIncludeUnclaimedSources is on.
	unclaimed []string
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

// typeScriptDiscovery is what one discovery pass over the TypeScript
// repositories found.
type typeScriptDiscovery struct {
	packages  []typeScriptPackageUnit
	conflicts []typeScriptConflict
	// withoutPackages names the repositories that declare no named package
	// with an applicable project.
	withoutPackages []string
	// unclaimedWithoutPackage names the unclaimed files that no package unit
	// encloses. Nothing can index them, so they are named rather than
	// dropped in silence.
	unclaimedWithoutPackage []string
}

// discoverTypeScriptPackages finds the packages of every TypeScript
// repository, and names the ones that declare none.
//
// A repository registered as TypeScript whose tree holds no named package
// with a project contributes nothing: the pipeline discovers packages, and a
// directory of loose .mjs files is not one. That used to be silent, so a
// registry entry suggested coverage the graph never had.
//
// includeUnclaimed additionally asks for the source files no project of a
// repository claims. They are found once per repository and then attributed
// to the package unit that encloses each one, because a file belongs to a
// package and is analysed beside a project, and the enclosing package is the
// only one whose project can see it at all.
func discoverTypeScriptPackages(
	ctx context.Context,
	repositories []workspace.Repository,
	includeUnclaimed bool,
) (typeScriptDiscovery, error) {
	result := typeScriptDiscovery{
		packages:                make([]typeScriptPackageUnit, 0),
		conflicts:               make([]typeScriptConflict, 0),
		withoutPackages:         make([]string, 0),
		unclaimedWithoutPackage: make([]string, 0),
	}
	for _, repository := range repositories {
		discovered := 0
		registry, err := workspace.NewTypeScriptPackageRegistry(ctx, repository)
		if err != nil {
			return typeScriptDiscovery{}, fmt.Errorf("discover TypeScript packages for %q: %w", repository.Name, err)
		}
		first := len(result.packages)
		for _, packageValue := range registry.List() {
			if strings.TrimSpace(packageValue.ProjectPath) == "" {
				continue
			}
			result.packages = append(result.packages, typeScriptPackageUnit{
				repository:   repository,
				packageValue: packageValue,
				files:        countSourceFiles(packageValue.SourceRoots),
			})
			discovered++
		}
		if includeUnclaimed {
			orphans, err := attributeUnclaimedSources(ctx, repository, result.packages[first:])
			if err != nil {
				return typeScriptDiscovery{}, err
			}
			result.unclaimedWithoutPackage = append(result.unclaimedWithoutPackage, orphans...)
		}
		for _, conflict := range registry.Conflicts() {
			result.conflicts = append(result.conflicts, typeScriptConflict{
				repository: repository,
				conflict:   conflict,
			})
			discovered++
		}
		if discovered == 0 {
			result.withoutPackages = append(result.withoutPackages, repository.Name)
		}
	}
	if len(result.packages) == 0 && len(result.conflicts) == 0 && len(repositories) != 0 {
		return typeScriptDiscovery{}, fmt.Errorf("TypeScript repositories have no named package with a project")
	}
	packages := result.packages
	sort.Slice(packages, func(left, right int) bool {
		if packages[left].repository.Name != packages[right].repository.Name {
			return packages[left].repository.Name < packages[right].repository.Name
		}
		if packages[left].packageValue.Name != packages[right].packageValue.Name {
			return packages[left].packageValue.Name < packages[right].packageValue.Name
		}
		return packages[left].packageValue.ManifestPath < packages[right].packageValue.ManifestPath
	})
	conflicts := result.conflicts
	sort.Slice(conflicts, func(left, right int) bool {
		if conflicts[left].repository.Name != conflicts[right].repository.Name {
			return conflicts[left].repository.Name < conflicts[right].repository.Name
		}
		return conflicts[left].conflict.Name < conflicts[right].conflict.Name
	})
	sort.Strings(result.unclaimedWithoutPackage)
	return result, nil
}

// attributeUnclaimedSources gives every source file no project of the
// repository claims to the unit whose package encloses it, and returns the
// files no unit encloses.
//
// The enclosing package is the one with the longest root path containing the
// file: in a monorepo the root manifest encloses every package, so the
// deepest match is the only attribution that puts a file in the package a
// reader would name. A file no unit encloses cannot be indexed at all -- the
// payload of a unit declares its package, and there is none -- so it is
// returned to be reported.
func attributeUnclaimedSources(
	ctx context.Context,
	repository workspace.Repository,
	units []typeScriptPackageUnit,
) ([]string, error) {
	discovery, err := workspace.DiscoverTypeScript(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("discover TypeScript projects for %q: %w", repository.Name, err)
	}
	unclaimed, err := workspace.UnclaimedTypeScriptSources(ctx, repository, discovery)
	if err != nil {
		return nil, fmt.Errorf("resolve unclaimed TypeScript sources for %q: %w", repository.Name, err)
	}
	orphans := make([]string, 0)
	for _, source := range unclaimed {
		owner := -1
		for index := range units {
			root := units[index].packageValue.RootPath
			if !pathWithin(root, source) {
				continue
			}
			if owner == -1 || len(root) > len(units[owner].packageValue.RootPath) {
				owner = index
			}
		}
		if owner == -1 {
			orphans = append(orphans, source)
			continue
		}
		units[owner].unclaimed = append(units[owner].unclaimed, source)
	}
	return orphans, nil
}

// pathWithin reports whether root contains candidate. It is the containment
// rule `internal/workspace` and `internal/watcher` already apply, kept local
// like theirs.
func pathWithin(root, candidate string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
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
			" (this build type-checks with go %s; rebuild Kivgraph with a toolchain at least as new as the sources it must read)",
			goworkspace.LanguageVersion())
	}
	return ""
}

// mergeSets merges every unit's facts into one set and stamps the two things
// no single unit can know.
//
// A repository appears in as many sets as it has units, and MergeAll keeps the
// first record of each key, so the languages of the later ones are collected
// here and replace it: a Go repository that also holds TypeScript must not
// lose either language because one of its two units happened to be merged
// first.
//
// Provenance is written here for a stronger reason. Where the tree stood is
// not a fact any analysis produces, and recording it per unit put it inside
// the cached fact set: a served entry replayed the commit of the pass that
// wrote it, so the graph reported a repository as moved while its every
// symbol was current. First-wins made it worse than stale -- with one unit
// hit and another missed, the published commit depended on which -- and
// `fact_cache: verify` compares whole sets, so a commit that changed no file
// aborted the pass as a divergence.
func mergeSets(sets []facts.Set, repositories []workspace.Repository) facts.Set {
	provenance := make(map[string]workspace.Repository, len(repositories))
	for _, repository := range repositories {
		provenance[facts.RepositoryKey(repository.Name)] = repository
	}
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
		if source, known := provenance[merged.Repositories[index].Key]; known {
			merged.Repositories[index].Commit = source.Commit
			merged.Repositories[index].Branch = source.Branch
			merged.Repositories[index].Dirty = source.Dirty
		}
	}
	return merged
}

// ProviderDefinitionNotIndexed is the reason of a use whose provider is in the
// graph and does not publish the declaration the use resolves to. The standard
// library reaches this through macros: `impl Add for u32` is generated by
// `add_impl!`, so it exists in no source range and no index defines it.
const ProviderDefinitionNotIndexed = "PROVIDER_DEFINITION_NOT_INDEXED"

// closeCrossRepositoryEdges drops the edges whose target no repository of this
// pass published, and declares each one instead.
//
// A unit accepts a target attributed to another repository on trust: it cannot
// see the provider's facts, and the provider is indexed in the same pass, so
// the merge is where the two ends meet. Trust is not always repaid. A crate of
// the standard library declares most of its arithmetic through macros --
// `impl Add for u32` is generated by `add_impl!` -- so the analyzer resolves a
// use to a symbol whose declaration exists in no source range and therefore in
// no index. Publishing that edge dangles the graph and aborts the pass; hiding
// it would remove the only trace that the use was observed.
//
// The dropped edge becomes an unresolved reference with its evidence, which is
// what the observation actually supports: this repository uses that symbol, and
// the repository that provides it does not publish the declaration.
func closeCrossRepositoryEdges(merged facts.Set, composed map[string]composedTarget) (facts.Set, int) {
	symbols := make(map[string]facts.Symbol, len(merged.Symbols))
	for _, symbol := range merged.Symbols {
		symbols[symbol.Key] = symbol
	}
	placed := make(map[string]struct{}, len(merged.Packages)+len(merged.Files))
	for _, entry := range merged.Packages {
		placed[entry.Key] = struct{}{}
	}
	for _, entry := range merged.Files {
		placed[entry.Key] = struct{}{}
	}
	evidence := make(map[string]facts.Evidence, len(merged.Evidence))
	for _, entry := range merged.Evidence {
		evidence[entry.Key] = entry
	}
	kept := merged.Edges[:0]
	dropped := 0
	for _, edge := range merged.Edges {
		if _, exists := symbols[edge.TargetKey]; exists {
			kept = append(kept, edge)
			continue
		}
		if _, exists := placed[edge.TargetKey]; exists {
			kept = append(kept, edge)
			continue
		}
		source, sourceKnown := symbols[edge.SourceKey]
		target, described := composed[edge.TargetKey]
		if !sourceKnown || !described {
			// Nothing here can say what was observed: an end that is not a
			// symbol of this graph, or a key no unit reported composing.
			// Validation names it rather than this dropping it unexplained.
			kept = append(kept, edge)
			continue
		}
		dropped++
		merged.Unresolved = append(merged.Unresolved,
			unresolvedFromEdge(edge, source, target, evidence[edge.EvidenceKey]))
	}
	merged.Edges = kept
	if dropped != 0 {
		// One pass over the single set deduplicates the appended entries on
		// the same identity Merge uses and restores the canonical order:
		// several edges can share one missing target at one position.
		merged = facts.MergeAll([]facts.Set{merged})
	}
	return merged, dropped
}

// composedTarget describes a target whose key a unit built from a provider's
// identity without reading its declaration.
type composedTarget struct {
	Repository string
	Package    string
	Symbol     string
}

// unresolvedFromEdge describes a use whose provider does not publish its target.
//
// A registered provider gets one entry per use, with the position that was
// observed: whoever reads it wants to go there. A derived provider gets one per
// symbol, with no position at all, and the reason is measured. The standard
// library declares every operator on every primitive through macros, so on one
// real repository this produced 4.165 entries over 205 symbols -- `u64::add`,
// `f64::div`, `usize::PartialEq::eq` -- and buried the 791 gaps that were about
// the repository's own dependencies. A class that is a property of the provider
// is declared like one, which is what `MACRO_EXPANSION_DISABLED` already does.
func unresolvedFromEdge(
	edge facts.Edge,
	source facts.Symbol,
	target composedTarget,
	observed facts.Evidence,
) facts.UnresolvedReference {
	entry := facts.UnresolvedReference{
		RepositoryKey:    source.RepositoryKey,
		Language:         source.Language,
		RequestedPackage: target.Package,
		RequestedSymbol:  target.Symbol,
		Reason:           ProviderDefinitionNotIndexed,
	}
	if facts.IsSyntheticRepository(target.Repository) {
		entry.Detail = fmt.Sprintf("%s of %s is generated by a macro and exists in no indexed source range, so every use of it in this repository is absent from the graph",
			target.Symbol, target.Package)
		return entry
	}
	entry.FileKey = source.FileKey
	entry.SourceSymbolKey = source.Key
	entry.Detail = fmt.Sprintf("%s resolves to %s of %s, which %s does not publish: the declaration exists in no indexed source range",
		edge.Kind, target.Symbol, target.Package, target.Repository)
	if observed.Key != "" {
		entry.FileKey = observed.FileKey
		entry.Start = observed.Start
	}
	return entry
}

func collectTypeScriptFacts(
	ctx context.Context,
	options FullOptions,
	consumer typeScriptPackageUnit,
	providers []typeScriptPackageUnit,
) (facts.TypeScriptPayload, error) {
	repository := consumer.repository
	packageValue := consumer.packageValue
	output, err := os.CreateTemp("", "kivgraph-ts-facts-*.json")
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
	// One argument per file, absolute and inside the repository root: the
	// worker rejects anything else. The list is empty unless
	// FullOptions.TypeScriptIncludeUnclaimedSources is on, so a pass with the
	// key off invokes the worker with byte-identical arguments.
	for _, source := range consumer.unclaimed {
		arguments = append(arguments, "--unclaimed", source)
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

// unitKind is what a unit is, and it is one field rather than a bool per
// language on purpose.
//
// Every dispatch over a unit used to be a `switch { case unit.isGo: ...
// default: }` whose default was TypeScript, spread over nine sites in two
// files. A unit built without its flag was not a compile error and not a
// runtime error: it was analysed as a TypeScript package. Adding the third
// semantic language meant finding all nine by hand, and the language that
// found eight of them would have published a graph that looked right.
//
// The zero value is deliberately not a language. It is the one value
// analyseUnit refuses, so the failure that used to be a misroute is now a
// stopped pass that names the unit.
type unitKind uint8

const (
	unitUnspecified unitKind = iota
	unitGo
	unitTypeScript
	unitRust
	// unitSemantic is every language that arrives through
	// facts.SemanticPayload. They differ by `language`, never by kind: the
	// pass schedules, caches and merges them identically, and the only code
	// that asks which one it is, is the loader switch in indexSemantic.
	unitSemantic
)

// analysisUnit is one independent piece of work: a Go module, a TypeScript
// package, a Cargo workspace or a semantic repository. Nothing in a unit reads
// another unit's state, which is what lets them run at the same time.
type analysisUnit struct {
	repository workspace.Repository
	module     goworkspace.Module
	pkg        typeScriptPackageUnit
	rust       rustWorkspaceUnit
	workFile   string
	kind       unitKind
	language   facts.Language
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
	switch unit.kind {
	case unitGo:
		return len(unit.module.PackagePatterns)
	case unitRust:
		return unit.rust.files
	case unitSemantic:
		return countSemanticFiles(unit.repository, unit.language)
	default:
		return len(unit.pkg.packageValue.SourceRoots) + unit.pkg.files
	}
}

func (unit analysisUnit) detail() string {
	switch unit.kind {
	case unitGo:
		return unit.module.ModulePath
	case unitRust:
		return rustUnitDetail(unit.rust)
	case unitSemantic:
		return string(unit.language)
	default:
		return unit.pkg.packageValue.Name
	}
}

// addSemantic folds one semantic unit's result into the counters of its
// language.
//
// It is one switch, here, rather than a case per language in the merge loop.
// The counters are still flat fields because `index --full --json` publishes
// them by name and that shape is a compatibility surface; what moved is the
// number of places that have to learn a language, from five to one.
func (report *FullReport) addSemantic(language facts.Language, result analysisResult) {
	var symbols, references, unresolved, notLoaded *int
	switch language {
	case facts.LanguagePython:
		symbols, references, unresolved, notLoaded =
			&report.PythonSymbols, &report.PythonReferences,
			&report.PythonUnresolved, &report.PythonRepositoriesNotLoaded
	case facts.LanguageDart:
		symbols, references, unresolved, notLoaded =
			&report.DartSymbols, &report.DartReferences,
			&report.DartUnresolved, &report.DartRepositoriesNotLoaded
	case facts.LanguageJava:
		symbols, references, unresolved, notLoaded =
			&report.JavaSymbols, &report.JavaReferences,
			&report.JavaUnresolved, &report.JavaRepositoriesNotLoaded
	case facts.LanguageCSharp:
		symbols, references, unresolved, notLoaded =
			&report.CSharpSymbols, &report.CSharpReferences,
			&report.CSharpUnresolved, &report.CSharpRepositoriesNotLoaded
	default:
		// A semantic unit whose language has no counters would be indexed
		// and then reported as nothing, which reads as a language with no
		// code. indexSemantic refuses the same language, so this is
		// unreachable rather than tolerant.
		return
	}
	*symbols += result.symbols
	*references += result.references
	*unresolved += result.unresolved
	if result.notLoaded {
		*notLoaded++
	}
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
	// composed describes the targets this unit attributed to another
	// repository without reading their declarations. The merge is the only
	// place that can tell whether the provider published them, and this is
	// what lets it say which symbol was missing rather than only how many.
	composed map[string]composedTarget
	// diagnostics are what the loader said without blocking the pass.
	diagnostics []string
	// notRead explains a unit that produced no facts, for the reader of
	// the report. Empty for every unit that loaded.
	notRead string
}

// moduleNotLoadedFacts declares a Go module the loader could not read.
//
// The repository record travels with the entry for the same reason the
// ambiguous-package one does: a repository whose only module failed
// contributes nothing else, and an unresolved reference in a repository the
// set does not know is not a valid fact. The detail keeps the diagnostics the
// go command produced, so the answer to "why is this repository empty" is in
// the graph rather than in a log nobody kept.

// moduleWithoutGoPackages records a go.mod that names a module and holds no Go.
//
// It gets its own path rather than moduleNotLoadedFacts because it is not the
// same thing. That one is for a module that should load and cannot -- most
// often dependencies nobody has downloaded -- and it publishes an unresolved
// reference so the graph says what is missing. This one has nothing missing:
// Hugo names its themes with a go.mod, and so do several other tools, and
// there is no state of the world in which one of those yields a Go package.
// Filing it as MODULE_NOT_LOADED would leave a reference in the graph that no
// `go mod download` could ever clear, and somebody would keep trying.
//
// Counted rather than silent. It is one of the modules this pass did not read,
// which is exactly what GoModulesNotLoaded is for, and a directory quietly
// skipped is how a graph ends up missing something nobody can name.
//
// Before this, one such directory failed the whole index: five repositories
// went unindexed because a Hugo site sat inside one of them -- and it was in
// that repository's exclusions, which never reached this far.
func moduleWithoutGoPackages(unit analysisUnit) analysisResult {
	return analysisResult{
		notLoaded: true,
		detail:    "no Go packages: " + unit.module.ModulePath + " declares a module and holds none",
		notRead: fmt.Sprintf("%s: %s: NO_GO_PACKAGES: declares a module and holds no Go file",
			unit.repository.Name, unit.module.ModulePath),
	}
}

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
		notRead: fmt.Sprintf("%s: %s: MODULE_NOT_LOADED: %s",
			unit.repository.Name, unit.module.ModulePath, firstLine(detail)),
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
		GOOS:         options.GoOS,
		GOARCH:       options.GoARCH,
		CGOEnabled:   options.GoCGOEnabled,
		BuildTags:    append([]string(nil), options.GoBuildTags...),
		AllowNetwork: options.GoAllowNetwork,
	})
	if err != nil {
		if errors.Is(err, goloader.ErrNoPackages) {
			return moduleWithoutGoPackages(unit), nil
		}
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
	set, _, err := facts.NormalizeTypeScript(ctx, payload, repository)
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

	// Working inside a checkout means running the worker that was just built
	// there. An installed shim from an earlier release sits on the PATH under
	// the same default name and would silently win, so `pnpm build` would
	// change nothing an index run could observe -- a wrong measurement that
	// looks exactly like a correct one. An explicitly configured command
	// still decides.
	//
	// The checkout entry point takes the arguments of the `facts` subcommand,
	// which the shim would have consumed itself. A caller that passes none is
	// asking for the command, not for a run.
	workerRoot := filepath.Join(options.WorkingDirectory, "ts-worker")
	checkoutEntry := filepath.Join(workerRoot, "dist", "facts-cli.js")
	if info, statErr := os.Stat(checkoutEntry); command == defaultTypeScriptWorkerCommand &&
		statErr == nil && info.Mode().IsRegular() {
		if len(arguments) == 0 {
			return "node", []string{checkoutEntry}, nil
		}
		return "node", append([]string{checkoutEntry}, arguments[1:]...), nil
	}
	if _, lookupErr := exec.LookPath(command); lookupErr == nil {
		return command, append(commandArguments, arguments...), nil
	} else if command != defaultTypeScriptWorkerCommand {
		return "", nil, fmt.Errorf("worker command %q is not executable: %w", command, lookupErr)
	}

	// A bundle carries the worker beside the executable. Resolving it there
	// keeps an installation working when its `bin` is not on the PATH, which
	// is how anyone runs it by absolute path the first time.
	if sibling, found := siblingExecutable(defaultTypeScriptWorkerCommand); found {
		return sibling, append(commandArguments, arguments...), nil
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		return "", nil, fmt.Errorf("default worker is unavailable and pnpm is not executable: %w", err)
	}
	return "pnpm", append([]string{"--dir", workerRoot}, arguments...), nil
}

// defaultTypeScriptWorkerCommand is the name a bundle installs and the
// configuration defaults to.
const defaultTypeScriptWorkerCommand = "kivgraph-ts-worker"

// siblingExecutable answers an executable installed next to the running
// binary, which is what a bundle is.
func siblingExecutable(name string) (string, bool) {
	selfPath, err := os.Executable()
	if err != nil {
		return "", false
	}
	// Every name this platform would run, not just the one it would compile
	// to: the bundle's worker shim is a script, and a search for the compiled
	// form would report a complete bundle as missing it.
	for _, name := range executable.Candidates(name) {
		candidate := filepath.Join(filepath.Dir(selfPath), name)
		info, statErr := os.Stat(candidate)
		if statErr != nil || !executable.IsProgram(info) {
			continue
		}
		return candidate, true
	}
	return "", false
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
	// crateRegistry attributes a Rust crate to the repository that provides
	// it, and parsers is the shared Tree-sitter pool the Rust analysis reads
	// visibility and call shapes with.
	crateRegistry *rustloader.CrateRegistry
	parsers       *syntax.ParserManager
}

func typeScriptUnits(packages []typeScriptPackageUnit) []analysisUnit {
	units := make([]analysisUnit, 0, len(packages))
	for _, packageUnit := range packages {
		units = append(units, analysisUnit{
			repository: packageUnit.repository, pkg: packageUnit, kind: unitTypeScript,
		})
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
	cache.withRegistry(inputs, options.Repositories)
	var goQueue, typeScriptQueue, rustQueue []int
	semanticQueues := map[facts.Language][]int{}
	for index, unit := range units {
		switch unit.kind {
		case unitGo:
			goQueue = append(goQueue, index)
		case unitRust:
			rustQueue = append(rustQueue, index)
		case unitSemantic:
			semanticQueues[unit.language] = append(semanticQueues[unit.language], index)
		default:
			typeScriptQueue = append(typeScriptQueue, index)
		}
	}
	byWeight := func(queue []int) {
		sort.SliceStable(queue, func(left, right int) bool {
			return units[queue[left]].weight() > units[queue[right]].weight()
		})
	}
	byWeight(goQueue)
	byWeight(rustQueue)
	byWeight(typeScriptQueue)
	for _, queue := range semanticQueues {
		byWeight(queue)
	}

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
	run(rustQueue, rustWorkspaceLimit(options), PhaseRust)
	run(typeScriptQueue, typeScriptWorkerLimit(options), PhaseTypeScript)
	// Each semantic language drains its own queue under its own budget: the
	// analyzers are separate processes with separate costs, and one of them
	// saturating the machine is what a per-language limit exists to stop.
	// The order is fixed rather than the map's, so a pass schedules the same
	// way twice.
	for _, language := range semanticSchedule {
		queue := semanticQueues[language]
		if len(queue) == 0 {
			continue
		}
		limit, phase := semanticBudget(options, language)
		run(queue, limit, phase)
	}

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
	switch unit.kind {
	case unitGo:
		return indexGoModule(ctx, options, unit,
			inputs.moduleRegistry, inputs.conflictingModules, inputs.planConflicts)
	case unitRust:
		return indexRustWorkspace(ctx, options, unit, inputs.crateRegistry, inputs.parsers)
	case unitSemantic:
		return indexSemantic(ctx, options, unit)
	case unitTypeScript:
		return indexTypeScriptPackage(ctx, options, unit, inputs.typeScriptPackages)
	default:
		// The default used to be TypeScript, so a unit built without its
		// kind was analysed as a TypeScript package and published facts
		// nobody asked for. A pass that cannot say what a unit is stops.
		return analysisResult{}, fmt.Errorf(
			"analysis unit for repository %q has no kind", unit.repository.Name)
	}
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

func pythonWorkerLimit(options FullOptions) int {
	if options.PythonMaximumWorkers > 0 {
		return options.PythonMaximumWorkers
	}
	return defaultPythonWorkerLimit
}

func dartWorkerLimit(options FullOptions) int {
	if options.DartMaximumWorkers > 0 {
		return options.DartMaximumWorkers
	}
	return defaultDartWorkerLimit
}

func javaWorkerLimit(options FullOptions) int {
	if options.JavaMaximumWorkers > 0 {
		return options.JavaMaximumWorkers
	}
	return defaultJavaWorkerLimit
}

func cSharpWorkerLimit(options FullOptions) int {
	if options.CSharpMaximumWorkers > 0 {
		return options.CSharpMaximumWorkers
	}
	return defaultCSharpWorkerLimit
}

// dedupeRepositories keeps the first occurrence of each repository.
//
// A language with more than one accepted spelling is selected once per
// spelling, and a repository that declares both `csharp` and `cs` would
// otherwise be indexed twice: two units, one cache identity, and a merge that
// sees every symbol declared in two places.
func dedupeRepositories(repositories []workspace.Repository) []workspace.Repository {
	seen := make(map[string]struct{}, len(repositories))
	result := make([]workspace.Repository, 0, len(repositories))
	for _, repository := range repositories {
		if _, exists := seen[repository.Name]; exists {
			continue
		}
		seen[repository.Name] = struct{}{}
		result = append(result, repository)
	}
	return result
}

// semanticSchedule fixes the order the semantic languages are dispatched in.
// A map range would reorder the queues between runs, and the pass is required
// to schedule the same corpus the same way twice.
var semanticSchedule = []facts.Language{
	facts.LanguagePython, facts.LanguageDart, facts.LanguageJava,
	facts.LanguageCSharp,
}

// semanticBudget is the worker limit and progress phase of one semantic
// language.
func semanticBudget(options FullOptions, language facts.Language) (int, ProgressPhase) {
	switch language {
	case facts.LanguagePython:
		return pythonWorkerLimit(options), PhasePython
	case facts.LanguageDart:
		return dartWorkerLimit(options), PhaseDart
	case facts.LanguageJava:
		return javaWorkerLimit(options), PhaseJava
	case facts.LanguageCSharp:
		return cSharpWorkerLimit(options), PhaseCSharp
	default:
		return 1, PhaseSemantic
	}
}
