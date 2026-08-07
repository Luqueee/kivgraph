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
	TypeScriptWorker  string
	WorkingDirectory  string

	// Progress, when set, is called synchronously as each unit of work
	// starts and finishes. It must not block: a slow callback slows the
	// index down.
	Progress func(ProgressEvent)
}

// FullReport records the work performed before the caller publishes the
// resulting facts. Counts are informational; an error is never hidden in a
// successful report.
type FullReport struct {
	GoRepositories         int
	GoModules              int
	GoLoads                int
	GoLoadErrors           int
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
	merged := facts.Set{}

	if len(goRepositories) != 0 {
		if strings.TrimSpace(options.SyntheticWorkFile) == "" {
			return facts.Set{}, report, errors.New("full index: synthetic Go work file is required")
		}
		plan, err := goworkspace.BuildPlan(ctx, goRepositories, goworkspace.Options{})
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("build synthetic Go workspace: %w", err)
		}
		if _, err := goworkspace.Write(ctx, options.SyntheticWorkFile, plan, goRepositories); err != nil {
			return facts.Set{}, report, fmt.Errorf("write synthetic Go workspace: %w", err)
		}
		report.SyntheticWorkFile = options.SyntheticWorkFile
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
					WorkFile:     options.SyntheticWorkFile,
					IncludeTests: options.IncludeTests,
				})
				report.GoLoads++
				if err != nil {
					return facts.Set{}, report, fmt.Errorf("load Go module %q for %q: %w", module.ModulePath, repository.Name, err)
				}
				if len(load.Errors) != 0 {
					report.GoLoadErrors += len(load.Errors)
					return facts.Set{}, report, fmt.Errorf("load Go module %q for %q reported diagnostics: %s", module.ModulePath, repository.Name, formatPackageErrors(load.Errors))
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

	for index, repository := range typeScriptRepositories {
		emitProgress(options.Progress, ProgressEvent{
			Phase: PhaseTypeScript, Repository: repository.Name,
			Started: true, Completed: index, Total: len(typeScriptRepositories),
		})
		payload, err := collectTypeScriptFacts(ctx, options, repository, typeScriptRepositories)
		if err != nil {
			return facts.Set{}, report, err
		}
		set, _, err := facts.NormalizeTypeScript(ctx, payload, repository.RealPath)
		if err != nil {
			return facts.Set{}, report, fmt.Errorf("normalise TypeScript facts for %q: %w", repository.Name, err)
		}
		mergeSets(&merged, set)
		report.TypeScriptSymbols += len(payload.Symbols)
		report.TypeScriptReferences += len(payload.References)
		report.TypeScriptUnresolved += len(payload.Unresolved)
		emitProgress(options.Progress, ProgressEvent{
			Phase: PhaseTypeScript, Repository: repository.Name,
			Detail:    fmt.Sprintf("symbols=%d references=%d", len(payload.Symbols), len(payload.References)),
			Completed: index + 1, Total: len(typeScriptRepositories),
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
		seen := make(map[facts.Language]struct{})
		for _, language := range languages[destination.Repositories[index].Key] {
			if _, exists := seen[language]; exists {
				continue
			}
			seen[language] = struct{}{}
			destination.Repositories[index].Languages = append(destination.Repositories[index].Languages, language)
		}
		sort.Slice(destination.Repositories[index].Languages, func(left, right int) bool {
			return destination.Repositories[index].Languages[left] < destination.Repositories[index].Languages[right]
		})
	}
}

func collectTypeScriptFacts(
	ctx context.Context,
	options FullOptions,
	repository workspace.Repository,
	providers []workspace.Repository,
) (facts.TypeScriptPayload, error) {
	output, err := os.CreateTemp("", "ladygraph-ts-facts-*.json")
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("create TypeScript facts output for %q: %w", repository.Name, err)
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return facts.TypeScriptPayload{}, fmt.Errorf("close TypeScript facts output for %q: %w", repository.Name, err)
	}
	defer os.Remove(outputPath)

	arguments := []string{"facts", repository.Name, repository.RealPath, outputPath}
	for _, provider := range providers {
		if provider.Name == repository.Name {
			continue
		}
		arguments = append(arguments, "--provider", provider.Name+"="+provider.RealPath)
	}
	command, commandArguments, err := factsCommand(options, arguments)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("prepare TypeScript facts command for %q: %w", repository.Name, err)
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
			return facts.TypeScriptPayload{}, fmt.Errorf("run TypeScript facts for %q: %w: %s", repository.Name, err, detail)
		}
		return facts.TypeScriptPayload{}, fmt.Errorf("run TypeScript facts for %q: %w", repository.Name, err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("read TypeScript facts for %q: %w", repository.Name, err)
	}
	payload, err := facts.DecodeTypeScriptPayload(data)
	if err != nil {
		return facts.TypeScriptPayload{}, fmt.Errorf("decode TypeScript facts for %q: %w", repository.Name, err)
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
