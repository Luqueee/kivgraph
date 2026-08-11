package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/rustloader"
	"github.com/Luqueee/ladygraph/internal/syntax"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// defaultRustWorkspaceLimit bounds concurrent analyzer processes when the
// caller states no budget. Each one holds a whole Cargo workspace and its
// sysroot in memory, so the ceiling is lower than the processor count.
const defaultRustWorkspaceLimit = 2

// rustWorkspaceUnit is one Cargo workspace and the crates it resolves.
type rustWorkspaceUnit struct {
	repository workspace.Repository
	workspace  workspace.CargoWorkspace
	crates     []workspace.CargoCrate
	// files is how many Rust sources the analyzer will read. It orders the
	// queue; it is never a fact about the graph.
	files int
}

// discoverRustWorkspaces finds the Cargo workspaces of every Rust repository
// and names the ones that declare none.
//
// A repository registered as Rust with no manifest contributes nothing, and a
// registry entry that contributes nothing looks like coverage.
func discoverRustWorkspaces(
	ctx context.Context,
	repositories []workspace.Repository,
) ([]rustWorkspaceUnit, []string, error) {
	units := make([]rustWorkspaceUnit, 0, len(repositories))
	withoutWorkspaces := make([]string, 0)
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		discovery, err := workspace.DiscoverCargo(ctx, repository)
		if err != nil {
			return nil, nil, fmt.Errorf("discover Cargo workspaces of %q: %w", repository.Name, err)
		}
		if len(discovery.Workspaces) == 0 {
			withoutWorkspaces = append(withoutWorkspaces, repository.Name)
			continue
		}
		cratesByWorkspace := make(map[string][]workspace.CargoCrate, len(discovery.Workspaces))
		for _, crate := range discovery.Crates {
			cratesByWorkspace[crate.WorkspacePath] = append(cratesByWorkspace[crate.WorkspacePath], crate)
		}
		for _, cargoWorkspace := range discovery.Workspaces {
			units = append(units, rustWorkspaceUnit{
				repository: repository,
				workspace:  cargoWorkspace,
				crates:     cratesByWorkspace[cargoWorkspace.ManifestPath],
				files:      countRustSources(cargoWorkspace.RootPath),
			})
		}
	}
	sort.SliceStable(units, func(left, right int) bool {
		if units[left].repository.Name != units[right].repository.Name {
			return units[left].repository.Name < units[right].repository.Name
		}
		return units[left].workspace.ManifestPath < units[right].workspace.ManifestPath
	})
	return units, withoutWorkspaces, nil
}

// countRustSources walks a workspace and counts the files the analyzer will
// read. It stats files and reads none.
func countRustSources(root string) int {
	if strings.TrimSpace(root) == "" {
		return 0
	}
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "target", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".rs") {
			count++
		}
		return nil
	})
	return count
}

// indexRustWorkspace turns one Cargo workspace into facts.
//
// The analyzer runs as its own process with its own output directory, so
// workspaces never share anything but the crate registry they are told about.
func indexRustWorkspace(
	ctx context.Context,
	options FullOptions,
	unit analysisUnit,
	registry *rustloader.CrateRegistry,
	parsers *syntax.ParserManager,
) (analysisResult, error) {
	rustUnit := unit.rust
	output, err := os.MkdirTemp("", "ladygraph-rust-*")
	if err != nil {
		return analysisResult{}, fmt.Errorf("create Rust analysis directory for %q: %w", rustUnit.repository.Name, err)
	}
	defer os.RemoveAll(output)

	result, runErr := rustloader.Run(ctx, rustloader.RunOptions{
		Workspace:         rustUnit.workspace.RootPath,
		OutputDirectory:   output,
		AnalyzerCommand:   options.RustAnalyzer,
		TargetDirectory:   options.RustTargetDirectory,
		Features:          options.RustFeatures,
		AllFeatures:       options.RustAllFeatures,
		NoDefaultFeatures: options.RustNoDefaultFeatures,
		Cfgs:              options.RustCfgs,
		BuildScripts:      options.RustBuildScripts,
		ProcMacros:        options.RustProcMacros,
		IncludeTests:      options.RustIncludeTests,
		AllowNetwork:      options.RustAllowNetwork,
		Sysroot:           options.RustSysroot,
		Threads:           rustAnalyzerThreads(options),
	})
	if runErr != nil {
		var classified *rustloader.RunError
		if errors.As(runErr, &classified) && classified.Kind != rustloader.RunErrorCanceled {
			// One workspace that does not load is not a pass that cannot
			// publish. Its facts are absent, and the graph says which
			// workspace and why instead of leaving a hole nobody can see.
			return workspaceNotLoadedFacts(rustUnit, classified), nil
		}
		return analysisResult{}, runErr
	}

	analysis, err := rustloader.Analyze(ctx, rustloader.AnalyzeOptions{
		Repository:   rustUnit.repository,
		Workspace:    rustUnit.workspace,
		Crates:       rustUnit.crates,
		Index:        result.Index,
		Registry:     registry,
		Parsers:      parsers,
		ProcMacros:   options.RustProcMacros,
		BuildScripts: options.RustBuildScripts,
		Diagnostics:  result.Diagnostics,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("analyse Rust workspace %q: %w", rustUnit.workspace.RootPath, err)
	}
	set, report, err := facts.NormalizeRust(ctx, facts.RustInput{
		Repository: rustUnit.repository,
		Analysis:   analysis,
	})
	if err != nil {
		return analysisResult{}, fmt.Errorf("normalise Rust workspace %q: %w", rustUnit.workspace.RootPath, err)
	}

	return analysisResult{
		set:         set,
		symbols:     len(set.Symbols),
		references:  len(analysis.References),
		unresolved:  len(set.Unresolved),
		detail:      rustUnitDetail(rustUnit),
		diagnostics: rustDiagnostics(rustUnit, analysis, report),
		requested:   requestedCrates(analysis),
	}, nil
}

// workspaceNotLoadedFacts declares a Cargo workspace the analyzer could not
// read.
//
// The repository record travels with the entry: a repository whose only
// workspace failed contributes nothing else, and an unresolved reference in a
// repository the set does not know is not a valid fact.
func workspaceNotLoadedFacts(unit rustWorkspaceUnit, failure *rustloader.RunError) analysisResult {
	repositoryKey := facts.RepositoryKey(unit.repository.Name)
	requested := unit.workspace.RootPath
	if len(unit.crates) != 0 {
		requested = unit.crates[0].Name
	}
	detail := failure.Detail
	if detail == "" && failure.Err != nil {
		detail = failure.Err.Error()
	}
	reason := rustloader.UnresolvedWorkspaceNotLoaded
	if failure.Kind == rustloader.RunErrorAnalyzerUnavailable {
		reason = rustloader.UnresolvedAnalyzerUnavailable
	}
	return analysisResult{
		set: facts.Set{
			Repositories: []facts.Repository{{
				Key:       repositoryKey,
				Name:      unit.repository.Name,
				RootPath:  unit.repository.RealPath,
				Languages: []facts.Language{facts.LanguageRust},
			}},
			Unresolved: []facts.UnresolvedReference{{
				RepositoryKey:    repositoryKey,
				Language:         facts.LanguageRust,
				RequestedPackage: requested,
				Reason:           string(reason),
				Detail:           detail,
			}},
		},
		notLoaded:   true,
		unresolved:  1,
		detail:      rustUnitDetail(unit),
		diagnostics: []string{fmt.Sprintf("%s: %s: %s", unit.repository.Name, failure.Kind, detail)},
	}
}

func rustUnitDetail(unit rustWorkspaceUnit) string {
	relative, err := filepath.Rel(unit.repository.RealPath, unit.workspace.RootPath)
	if err != nil || relative == "." || relative == "" {
		return unit.repository.Name
	}
	return filepath.ToSlash(relative)
}

// rustDiagnostics renders what the analysis reported without stopping the
// pass: the analyzer's own warnings and the facts normalisation could not keep.
func rustDiagnostics(unit rustWorkspaceUnit, analysis rustloader.Analysis, report facts.RustReport) []string {
	diagnostics := make([]string, 0, len(analysis.Diagnostics)+1)
	for _, diagnostic := range analysis.Diagnostics {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s: %s",
			unit.repository.Name, rustUnitDetail(unit), diagnostic))
	}
	if report.DefinitionsWithoutCrate != 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s: %d symbols name a crate no manifest declares",
			unit.repository.Name, rustUnitDetail(unit), report.DefinitionsWithoutCrate))
	}
	if analysis.ReferencesWithoutSource != 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s: %d uses have no enclosing declaration",
			unit.repository.Name, rustUnitDetail(unit), analysis.ReferencesWithoutSource))
	}
	return diagnostics
}

// requestedCrates names every crate the workspace asked about, resolved or
// not: both answers change when the repository that provides it appears,
// disappears or changes version.
func requestedCrates(analysis rustloader.Analysis) []string {
	seen := make(map[string]struct{}, len(analysis.Dependencies))
	names := make([]string, 0, len(analysis.Dependencies))
	add := func(name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		names = append(names, trimmed)
	}
	for _, dependency := range analysis.Dependencies {
		add(dependency.TargetCrate.Name)
	}
	for _, entry := range analysis.Unresolved {
		add(entry.RequestedCrate)
	}
	sort.Strings(names)
	return names
}

func rustWorkspaceLimit(options FullOptions) int {
	if options.RustMaximumWorkspaces > 0 {
		return options.RustMaximumWorkspaces
	}
	limit := runtime.NumCPU() / 4
	if limit < 1 {
		limit = 1
	}
	if limit > defaultRustWorkspaceLimit {
		limit = defaultRustWorkspaceLimit
	}
	return limit
}

// rustAnalyzerThreads splits the machine between the analyzer processes this
// pass runs at once, so two workspaces do not each prime caches with every
// core.
func rustAnalyzerThreads(options FullOptions) int {
	workers := rustWorkspaceLimit(options)
	threads := runtime.NumCPU() / workers
	if threads < 1 {
		threads = 1
	}
	return threads
}

// repositoriesForRust selects the repositories registered under a Rust
// language name, including the alias `rs` the registry vocabulary accepts.
func repositoriesForRust(repositories []workspace.Repository) []workspace.Repository {
	result := make([]workspace.Repository, 0)
	for _, repository := range repositories {
		for _, candidate := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(candidate)) {
			case "rust", "rs":
				result = append(result, repository)
				goto nextRepository
			}
		}
	nextRepository:
	}
	return result
}

// prepareRust discovers the Cargo workspaces of the pass and the shared state
// their analysis needs: the crate registry that attributes a crate to its
// provider, and one Tree-sitter pool for every workspace.
func prepareRust(
	ctx context.Context,
	options FullOptions,
	repositories []workspace.Repository,
	report *FullReport,
) ([]rustWorkspaceUnit, *rustloader.CrateRegistry, *syntax.ParserManager, error) {
	if len(repositories) == 0 {
		return nil, nil, nil, nil
	}
	if strings.TrimSpace(options.RustAnalyzer) == "" {
		return nil, nil, nil, errors.New("full index: the Rust analyzer command is required")
	}
	if strings.TrimSpace(options.RustTargetDirectory) == "" {
		return nil, nil, nil, errors.New("full index: the Rust target directory is required")
	}
	units, withoutWorkspaces, err := discoverRustWorkspaces(ctx, repositories)
	if err != nil {
		return nil, nil, nil, err
	}
	report.RustWithoutWorkspaces = withoutWorkspaces

	registry, err := rustloader.NewCrateRegistry(ctx, repositories)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build Rust crate registry: %w", err)
	}
	parsers, err := syntax.NewParserManager(rustWorkspaceLimit(options))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Rust parsers: %w", err)
	}
	return units, registry, parsers, nil
}

func rustAnalysisUnits(units []rustWorkspaceUnit) []analysisUnit {
	analysisUnits := make([]analysisUnit, 0, len(units))
	for _, unit := range units {
		analysisUnits = append(analysisUnits, analysisUnit{
			repository: unit.repository, rust: unit, isRust: true,
		})
	}
	return analysisUnits
}

// rustAnalysisFingerprint identifies everything that decides what the Rust
// index contains: the analyzer that produced it and the build configuration it
// was given. A feature turned on changes which code exists, so an entry taken
// without it describes a graph this pass would not produce.
func rustAnalysisFingerprint(options FullOptions) string {
	hash := sha256.New()
	command := strings.Fields(strings.TrimSpace(options.RustAnalyzer))
	fmt.Fprintf(hash, "command=%s\x00", strings.Join(command, " "))
	if len(command) != 0 {
		if resolved, _, err := rustloader.ResolveAnalyzer(command[0]); err == nil {
			fmt.Fprintf(hash, "analyzer=%s\x00", fileFingerprint(resolved))
			if version, err := exec.Command(resolved, "--version").Output(); err == nil {
				fmt.Fprintf(hash, "version=%s\x00", strings.TrimSpace(string(version)))
			}
		}
	}
	features := append([]string(nil), options.RustFeatures...)
	sort.Strings(features)
	cfgs := append([]string(nil), options.RustCfgs...)
	sort.Strings(cfgs)
	fmt.Fprintf(hash, "features=%s\x00all=%t\x00nodefault=%t\x00",
		strings.Join(features, ","), options.RustAllFeatures, options.RustNoDefaultFeatures)
	fmt.Fprintf(hash, "cfgs=%s\x00scripts=%t\x00macros=%t\x00tests=%t\x00network=%t\x00sysroot=%s\x00",
		strings.Join(cfgs, ","), options.RustBuildScripts, options.RustProcMacros,
		options.RustIncludeTests, options.RustAllowNetwork, strings.TrimSpace(options.RustSysroot))
	if cargo, err := exec.LookPath("cargo"); err == nil {
		if version, err := exec.Command(cargo, "--version").Output(); err == nil {
			fmt.Fprintf(hash, "cargo=%s\x00", strings.TrimSpace(string(version)))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}
