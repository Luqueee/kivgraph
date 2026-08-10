package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/goloader"
	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/workspace"
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
	GoAllowNetwork   bool
	TypeScriptWorker string
	WorkingDirectory string

	// Progress, when set, is called synchronously as each unit of work
	// starts and finishes. It must not block: a slow callback slows the
	// index down.
	Progress func(ProgressEvent)
}

// FullReport records the work performed before the caller publishes the
// resulting facts. Counts are informational; an error is never hidden in a
// successful report.
type FullReport struct {
	GoRepositories int
	GoModules      int
	GoLoads        int
	GoLoadErrors   int
	// GoLoadDiagnostics counts the diagnostics that did not block the pass:
	// a directory with no file to select, and the advisory the loader
	// attaches to it.
	GoLoadDiagnostics int
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
	SyntheticWorkFile      string
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
	typeScriptPackages, err := discoverTypeScriptPackages(ctx, typeScriptRepositories)
	if err != nil {
		return facts.Set{}, report, err
	}
	merged := facts.Set{}

	if len(goRepositories) != 0 {
		if strings.TrimSpace(options.SyntheticWorkFile) == "" {
			return facts.Set{}, report, errors.New("full index: synthetic Go work file is required")
		}
		plan, err := goworkspace.BuildPlan(ctx, goRepositories, goworkspace.Options{})
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("build synthetic Go workspace: %w", err)
		}
		workFiles, workspaces, err := writeWorkspaces(ctx, options.SyntheticWorkFile, plan, goRepositories)
		if err != nil {
			return facts.Set{}, report, err
		}
		report.SyntheticWorkFile = options.SyntheticWorkFile
		report.GoWorkspaces = workspaces

		moduleRegistry, err := goloader.NewModuleRegistry(ctx, goRepositories)
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("build Go module registry: %w", err)
		}
		modulesByRepository := modulesByRepository(plan.Modules)
		conflictingModules := conflictSubjects(plan.Conflicts)
		goModules := 0
		for _, repository := range goRepositories {
			goModules += len(modulesByRepository[repository.Name])
		}
		for _, repository := range goRepositories {
			for _, module := range modulesByRepository[repository.Name] {
				if err := ctx.Err(); err != nil {
					return facts.Set{}, report, err
				}
				emitProgress(options.Progress, ProgressEvent{
					Phase: PhaseGo, Repository: repository.Name, Detail: module.ModulePath,
					Started: true, Completed: report.GoModules, Total: goModules,
				})
				load, err := goloader.Load(ctx, goloader.Options{
					Directory:    module.RootPath,
					WorkFile:     workFiles[module.ModulePath],
					Patterns:     append([]string(nil), module.PackagePatterns...),
					IncludeTests: options.IncludeTests,
					BuildTags:    append([]string(nil), options.GoBuildTags...),
					AllowNetwork: options.GoAllowNetwork,
				})
				report.GoLoads++
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("load Go module %q for %q: %w", module.ModulePath, repository.Name, err)
				}
				blocking := load.BlockingErrors()
				report.GoLoadDiagnostics += len(load.Errors) - len(blocking)
				if len(blocking) != 0 {
					report.GoLoadErrors += len(blocking)
					return facts.Set{}, report, fmt.Errorf("load Go module %q for %q reported diagnostics: %s%s",
						module.ModulePath, repository.Name, formatPackageErrors(blocking), toolchainHint(blocking))
				}
				definitions, err := goloader.ExtractDefinitions(ctx, load, goloader.DefinitionOptions{Repository: repository.Name})
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("extract Go definitions for %q: %w", repository.Name, err)
				}
				keyed, err := goloader.AssignStableKeys(ctx, definitions)
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("assign Go stable keys for %q: %w", repository.Name, err)
				}
				uses, err := goloader.ExtractUses(ctx, load, goloader.UseOptions{Repository: repository.Name})
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("extract Go uses for %q: %w", repository.Name, err)
				}
				packageDependencies, err := goloader.ResolvePackageDependencies(ctx, uses)
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("resolve Go package dependencies for %q: %w", repository.Name, err)
				}
				references, err := goloader.ClassifyReferences(ctx, load, uses)
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("classify Go references for %q: %w", repository.Name, err)
				}
				cross, err := goloader.ResolveCrossRepository(ctx, uses, moduleRegistry, goloader.CrossRepositoryOptions{
					ConsumerRepository: repository.Name,
					ConflictingModules: conflictingModules,
				})
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("resolve Go cross-repository references for %q: %w", repository.Name, err)
				}
				typeRelations, err := goloader.ResolveTypeRelations(ctx, load, goloader.TypeRelationOptions{Repository: repository.Name})
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("resolve Go type relations for %q: %w", repository.Name, err)
				}
				unresolved, err := goloader.ClassifyUnresolved(ctx, load, cross, goloader.UnresolvedOptions{
					Repository:         repository.Name,
					WorkspaceConflicts: plan.Conflicts,
				})
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("classify Go unresolved references for %q: %w", repository.Name, err)
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
					return facts.Set{}, report, fmt.Errorf("normalise Go facts for %q: %w", repository.Name, err)
				}
				mergeSets(&merged, set)
				report.GoModules++
				report.GoDefinitions += len(keyed)
				report.GoReferences += len(references)
				report.GoUnresolved += len(unresolved)
				emitProgress(options.Progress, ProgressEvent{
					Phase: PhaseGo, Repository: repository.Name, Detail: module.ModulePath,
					Completed: report.GoModules, Total: goModules,
				})
			}
		}
	}

	for index, packageUnit := range typeScriptPackages {
		repository := packageUnit.repository
		packageValue := packageUnit.packageValue
		emitProgress(options.Progress, ProgressEvent{
			Phase: PhaseTypeScript, Repository: repository.Name,
			Detail: packageValue.Name, Started: true, Completed: index,
			Total: len(typeScriptPackages),
		})
		payload, err := collectTypeScriptFacts(ctx, options, packageUnit, typeScriptPackages)
		if err != nil {
			return facts.Set{}, report, err
		}
		set, _, err := facts.NormalizeTypeScript(ctx, payload, repository.RealPath)
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("normalise TypeScript facts for %q package %q: %w",
				repository.Name, packageValue.Name, err)
		}
		mergeSets(&merged, set)
		report.TypeScriptSymbols += len(payload.Symbols)
		report.TypeScriptReferences += len(payload.References)
		report.TypeScriptUnresolved += len(payload.Unresolved)
		emitProgress(options.Progress, ProgressEvent{
			Phase: PhaseTypeScript, Repository: repository.Name,
			Detail: fmt.Sprintf("package=%s symbols=%d references=%d",
				packageValue.Name, len(payload.Symbols), len(payload.References)),
			Completed: index + 1, Total: len(typeScriptPackages),
		})
	}

	emitProgress(options.Progress, ProgressEvent{Phase: PhaseMerge, Started: true})
	merged.Sort()
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

func validateLanguages(repositories []workspace.Repository) error {
	for _, repository := range repositories {
		for _, language := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(language)) {
			case "go", "typescript", "javascript", "ts", "js":
			default:
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
}

func discoverTypeScriptPackages(
	ctx context.Context,
	repositories []workspace.Repository,
) ([]typeScriptPackageUnit, error) {
	packages := make([]typeScriptPackageUnit, 0)
	for _, repository := range repositories {
		registry, err := workspace.NewTypeScriptPackageRegistry(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("discover TypeScript packages for %q: %w", repository.Name, err)
		}
		for _, packageValue := range registry.List() {
			if strings.TrimSpace(packageValue.ProjectPath) == "" {
				continue
			}
			packages = append(packages, typeScriptPackageUnit{
				repository:   repository,
				packageValue: packageValue,
			})
		}
	}
	if len(packages) == 0 && len(repositories) != 0 {
		return nil, fmt.Errorf("TypeScript repositories have no named package with a project")
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
	return packages, nil
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

func mergeSets(destination *facts.Set, source facts.Set) {
	languages := make(map[string][]facts.Language)
	for _, repository := range destination.Repositories {
		languages[repository.Key] = append([]facts.Language(nil), repository.Languages...)
	}
	for _, repository := range source.Repositories {
		languages[repository.Key] = append(languages[repository.Key], repository.Languages...)
	}
	destination.Merge(source)
	for index := range destination.Repositories {
		// Merge already carried the languages of both sides into the
		// record, so the deduplicated union replaces them. Appending it
		// would keep every language once per merged fact set.
		seen := make(map[facts.Language]struct{})
		union := make([]facts.Language, 0, len(languages[destination.Repositories[index].Key]))
		for _, language := range languages[destination.Repositories[index].Key] {
			if _, exists := seen[language]; exists {
				continue
			}
			seen[language] = struct{}{}
			union = append(union, language)
		}
		sort.Slice(union, func(left, right int) bool { return union[left] < union[right] })
		destination.Repositories[index].Languages = union
	}
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
