package indexing

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/filelock"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// FullOptions configures a complete index and canonical graph publication.
// The operation reads every configured repository and publishes a new
// generation only after the rebuild gates pass.
type FullOptions struct {
	Profile      string
	Repositories []workspace.Repository
	// Composition is the effective worktree selection that produced
	// Repositories. When present, it is persisted with the generation instead
	// of being reconstructed from live topology configuration by readers.
	Composition       *topology.ProfileComposition
	SyntheticWorkFile string
	IncludeTests      bool
	GoOS              string
	GoARCH            string
	GoCGOEnabled      *bool
	// GoBuildTags are the build constraints every Go load satisfies.
	GoBuildTags []string
	// GoAllowNetwork lets the Go loads reach a module proxy.
	GoAllowNetwork bool
	// GoMaximumLoads bounds concurrent Go loads.
	GoMaximumLoads int
	// TypeScriptMaximumWorkers bounds concurrent TypeScript workers.
	TypeScriptMaximumWorkers int
	TypeScriptWorker         string
	// TypeScriptIncludeUnclaimedSources indexes the TypeScript files no
	// project of a repository claims.
	TypeScriptIncludeUnclaimedSources bool
	// Rust* configure the external analyzer: the command, where its build
	// artifacts land, and the build configuration it is given.
	RustAnalyzer          string
	RustTargetDirectory   string
	RustMaximumWorkspaces int
	RustFeatures          []string
	RustAllFeatures       bool
	RustNoDefaultFeatures bool
	RustCfgs              []string
	RustBuildScripts      bool
	RustProcMacros        bool
	RustIncludeTests      bool
	RustAllowNetwork      bool
	RustSysroot           string
	// RustIndexSysroot asks for the standard library in the graph.
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
	JavaTargetDirectory     string
	JavaMaximumIndexTime    time.Duration
	CSharpIndexerCommand    string
	CSharpProject           string
	CSharpMaximumWorkers    int
	CSharpIncludeTests      bool
	CSharpIncludeGenerated  bool
	CSharpSkipRestore       bool
	CSharpTargetDirectory   string
	CSharpMaximumIndexTime  time.Duration
	WorkingDirectory        string
	// CacheMode and CacheDirectory configure the fact cache: whether a
	// unit may be served from the facts a previous pass stored, and where
	// those entries live.
	CacheMode      indexer.CacheMode
	CacheDirectory string
	// SharedTargetsLockPath serializes profiles and processes that write the
	// installation-level Rust, Java and C# analyzer target directories.
	SharedTargetsLockPath string
	Root                  string
	ResolverVersion       string
	Store                 generation.Config
	Metrics               *metrics.Registry
	// Invalidation records the source manifest only after the complete rebuild
	// has passed and the generation has been published. A nil manager keeps the
	// lower-level indexing API useful for callers that do not own installation
	// state.
	Invalidation *invalidation.Manager

	Progress        func(indexer.ProgressEvent)
	RebuildProgress func(rebuild.StageName)
}

// OptionsFromConfig maps a loaded configuration onto a full index request.
//
// Every caller that indexes -- the CLI and the MCP tool -- asks for the same
// pass over the same configuration, and the only difference between them is
// who reports progress and where the work runs. Building the request twice is
// how one of them came to index no Rust at all: a field added to the
// configuration reached one call site and not the other. The caller fills in
// Repositories, WorkingDirectory, ResolverVersion and the progress sinks,
// which are the only things the configuration does not decide.
func OptionsFromConfig(configuration config.Config) FullOptions {
	return FullOptions{
		Profile:                           configuration.Profiles.Default,
		SyntheticWorkFile:                 configuration.Go.SyntheticWorkFile,
		IncludeTests:                      configuration.Go.IncludeTests,
		GoOS:                              configuration.Go.GOOS,
		GoARCH:                            configuration.Go.GOARCH,
		GoCGOEnabled:                      configuration.Go.CGOEnabled,
		GoBuildTags:                       configuration.Go.BuildTags,
		GoAllowNetwork:                    configuration.Go.AllowNetwork,
		GoMaximumLoads:                    configuration.Go.MaximumLoads,
		TypeScriptMaximumWorkers:          configuration.TypeScript.MaximumWorkers,
		TypeScriptWorker:                  configuration.TypeScript.WorkerCommand,
		TypeScriptIncludeUnclaimedSources: configuration.TypeScript.IncludeUnclaimedSources,
		RustAnalyzer:                      configuration.Rust.AnalyzerCommand,
		RustTargetDirectory:               configuration.Rust.TargetDirectory,
		RustMaximumWorkspaces:             configuration.Rust.MaximumWorkspaces,
		RustFeatures:                      configuration.Rust.Features,
		RustAllFeatures:                   configuration.Rust.AllFeatures,
		RustNoDefaultFeatures:             configuration.Rust.NoDefaultFeatures,
		RustCfgs:                          configuration.Rust.Cfgs,
		RustBuildScripts:                  configuration.Rust.BuildScripts,
		RustProcMacros:                    configuration.Rust.ProcMacros,
		RustIncludeTests:                  configuration.Rust.IncludeTests,
		RustAllowNetwork:                  configuration.Rust.AllowNetwork,
		RustSysroot:                       configuration.Rust.Sysroot,
		RustIndexSysroot:                  configuration.Rust.IndexSysroot,
		PythonIndexer:                     configuration.Python.IndexerCommand,
		PythonAnalyzer:                    configuration.Python.AnalyzerCommand,
		PythonAnalyzerMode:                configuration.Python.AnalyzerMode,
		PythonPath:                        configuration.Python.PythonPath,
		PythonMaximumWorkers:              configuration.Python.MaximumWorkers,
		PythonIncludeTests:                configuration.Python.IncludeTests,
		PythonIncludeGenerated:            configuration.Python.IncludeGenerated,
		PythonIncludeExternal:             configuration.Python.IncludeExternal,
		DartAnalyzer:                      configuration.Dart.AnalyzerCommand,
		DartSDKPath:                       configuration.Dart.SDKPath,
		DartMaximumWorkers:                configuration.Dart.MaximumWorkers,
		DartIncludeTests:                  configuration.Dart.IncludeTests,
		DartIncludeGenerated:              configuration.Dart.IncludeGenerated,
		DartIncludeExternal:               configuration.Dart.IncludeExternal,
		DartIncludeSDK:                    configuration.Dart.IncludeSDK,
		DartPackageConfig:                 configuration.Dart.PackageConfig,
		DartWaitForAnalysis:               configuration.Dart.WaitForAnalysis,
		DartMaximumAnalysisTime:           time.Duration(configuration.Dart.MaximumAnalysisTime),
		JavaIndexerCommand:                configuration.Java.IndexerCommand,
		JavaBuildTool:                     configuration.Java.BuildTool,
		JavaMaximumWorkers:                configuration.Java.MaximumWorkers,
		JavaIncludeTests:                  configuration.Java.IncludeTests,
		JavaIncludeGenerated:              configuration.Java.IncludeGenerated,
		JavaTargetDirectory:               configuration.Java.TargetDirectory,
		JavaMaximumIndexTime:              time.Duration(configuration.Java.MaximumIndexTime),
		CSharpIndexerCommand:              configuration.CSharp.IndexerCommand,
		CSharpProject:                     configuration.CSharp.Project,
		CSharpMaximumWorkers:              configuration.CSharp.MaximumWorkers,
		CSharpIncludeTests:                configuration.CSharp.IncludeTests,
		CSharpIncludeGenerated:            configuration.CSharp.IncludeGenerated,
		CSharpSkipRestore:                 configuration.CSharp.SkipRestore,
		CSharpTargetDirectory:             configuration.CSharp.TargetDirectory,
		CSharpMaximumIndexTime:            time.Duration(configuration.CSharp.MaximumIndexTime),
		CacheMode:                         indexer.CacheMode(configuration.Indexing.FactCache),
		CacheDirectory:                    configuration.Indexing.FactCachePath,
		Root:                              filepath.Dir(configuration.Storage.DatabasePath),
		Store:                             generation.DefaultConfig(),
	}
}

func (options FullOptions) indexerOptions() indexer.FullOptions {
	return indexer.FullOptions{
		Profile:                           options.Profile,
		ResolverVersion:                   options.ResolverVersion,
		Repositories:                      options.Repositories,
		SyntheticWorkFile:                 options.SyntheticWorkFile,
		IncludeTests:                      options.IncludeTests,
		GoOS:                              options.GoOS,
		GoARCH:                            options.GoARCH,
		GoCGOEnabled:                      options.GoCGOEnabled,
		GoBuildTags:                       options.GoBuildTags,
		GoAllowNetwork:                    options.GoAllowNetwork,
		GoMaximumLoads:                    options.GoMaximumLoads,
		TypeScriptMaximumWorkers:          options.TypeScriptMaximumWorkers,
		TypeScriptWorker:                  options.TypeScriptWorker,
		TypeScriptIncludeUnclaimedSources: options.TypeScriptIncludeUnclaimedSources,
		RustAnalyzer:                      options.RustAnalyzer,
		RustTargetDirectory:               options.RustTargetDirectory,
		RustMaximumWorkspaces:             options.RustMaximumWorkspaces,
		RustFeatures:                      options.RustFeatures,
		RustAllFeatures:                   options.RustAllFeatures,
		RustNoDefaultFeatures:             options.RustNoDefaultFeatures,
		RustCfgs:                          options.RustCfgs,
		RustBuildScripts:                  options.RustBuildScripts,
		RustProcMacros:                    options.RustProcMacros,
		RustIncludeTests:                  options.RustIncludeTests,
		RustAllowNetwork:                  options.RustAllowNetwork,
		RustSysroot:                       options.RustSysroot,
		RustIndexSysroot:                  options.RustIndexSysroot,
		PythonIndexer:                     options.PythonIndexer,
		PythonAnalyzer:                    options.PythonAnalyzer,
		PythonAnalyzerMode:                options.PythonAnalyzerMode,
		PythonPath:                        options.PythonPath,
		PythonMaximumWorkers:              options.PythonMaximumWorkers,
		PythonIncludeTests:                options.PythonIncludeTests,
		PythonIncludeGenerated:            options.PythonIncludeGenerated,
		PythonIncludeExternal:             options.PythonIncludeExternal,
		DartAnalyzer:                      options.DartAnalyzer,
		DartSDKPath:                       options.DartSDKPath,
		DartMaximumWorkers:                options.DartMaximumWorkers,
		DartIncludeTests:                  options.DartIncludeTests,
		DartIncludeGenerated:              options.DartIncludeGenerated,
		DartIncludeExternal:               options.DartIncludeExternal,
		DartIncludeSDK:                    options.DartIncludeSDK,
		DartPackageConfig:                 options.DartPackageConfig,
		DartWaitForAnalysis:               options.DartWaitForAnalysis,
		DartMaximumAnalysisTime:           options.DartMaximumAnalysisTime,
		JavaIndexerCommand:                options.JavaIndexerCommand,
		JavaBuildTool:                     options.JavaBuildTool,
		JavaMaximumWorkers:                options.JavaMaximumWorkers,
		JavaIncludeTests:                  options.JavaIncludeTests,
		JavaIncludeGenerated:              options.JavaIncludeGenerated,
		JavaTargetDirectory:               options.JavaTargetDirectory,
		JavaMaximumIndexTime:              options.JavaMaximumIndexTime,
		CSharpIndexerCommand:              options.CSharpIndexerCommand,
		CSharpProject:                     options.CSharpProject,
		CSharpMaximumWorkers:              options.CSharpMaximumWorkers,
		CSharpIncludeTests:                options.CSharpIncludeTests,
		CSharpIncludeGenerated:            options.CSharpIncludeGenerated,
		CSharpSkipRestore:                 options.CSharpSkipRestore,
		CSharpTargetDirectory:             options.CSharpTargetDirectory,
		CSharpMaximumIndexTime:            options.CSharpMaximumIndexTime,
		WorkingDirectory:                  options.WorkingDirectory,
		CacheMode:                         options.CacheMode,
		CacheDirectory:                    options.CacheDirectory,
		Progress:                          options.Progress,
	}
}

// ObserveSources resolves the providers a full pass will read and captures
// their current mutable state. Watchers use the same route as RunFull so a
// stale registry or analyzer fingerprint cannot make invalidation disagree
// with the manifest that a subsequent rebuild publishes.
func ObserveSources(ctx context.Context, options FullOptions) (sourceobservation.Manifest, []workspace.Repository, error) {
	indexOptions := options.indexerOptions()
	effectiveRepositories, err := indexer.ResolveRepositories(indexOptions)
	if err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("resolve effective index sources: %w", err)
	}
	analyzerFingerprint, err := indexer.AnalyzerFingerprint(indexOptions)
	if err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("observe index analyzer configuration: %w", err)
	}
	manifest, observedRepositories, err := sourceobservation.CaptureWithRepositories(
		ctx,
		options.Profile,
		options.ResolverVersion,
		analyzerFingerprint,
		effectiveRepositories,
	)
	if err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("observe index sources: %w", err)
	}
	var composition topology.ProfileComposition
	if options.Composition != nil {
		composition = *options.Composition
	} else {
		composition, err = observedTopologyComposition(manifest, observedRepositories)
		if err != nil {
			return sourceobservation.Manifest{}, nil, fmt.Errorf("derive effective topology composition: %w", err)
		}
	}
	persistedComposition, err := sourceobservation.NewTopologyComposition(composition)
	if err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("observe effective topology composition: %w", err)
	}
	if err := validateObservedComposition(composition, manifest, observedRepositories); err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("observe effective topology composition: %w", err)
	}
	manifest.Composition = &persistedComposition
	if err := manifest.Validate(); err != nil {
		return sourceobservation.Manifest{}, nil, fmt.Errorf("validate effective topology composition: %w", err)
	}
	return manifest, observedRepositories, nil
}

// observedTopologyComposition gives legacy registries the same immutable
// generation record as topology-backed profiles. It selects only direct source
// repositories; analyzer-derived providers remain additional observed inputs
// and cannot be profile worktrees.
func observedTopologyComposition(
	manifest sourceobservation.Manifest,
	observed []workspace.Repository,
) (topology.ProfileComposition, error) {
	profile, err := topology.NewProfileID(manifest.Profile)
	if err != nil {
		return topology.ProfileComposition{}, fmt.Errorf("source observation profile: %w", err)
	}
	sources := make(map[string]sourceobservation.Source, len(manifest.Sources))
	for _, source := range manifest.Sources {
		sources[source.Repository] = source
	}
	composition := topology.ProfileComposition{
		Profile: topology.Profile{ID: profile},
	}
	selected := make(map[string]struct{}, len(observed))
	for _, repository := range observed {
		name := strings.TrimSpace(repository.Name)
		source, found := sources[name]
		if !found {
			return topology.ProfileComposition{}, fmt.Errorf("observed source repository %q is missing from source observations", repository.Name)
		}
		if source.Derived != repository.Derived {
			return topology.ProfileComposition{}, fmt.Errorf("observed source repository %q has a different derived state", source.Repository)
		}
		if source.Derived {
			continue
		}
		if _, exists := selected[source.Repository]; exists {
			return topology.ProfileComposition{}, fmt.Errorf("observed source repository %q is duplicated", source.Repository)
		}
		repositoryID, err := topology.NewLogicalRepositoryID(source.Repository)
		if err != nil {
			return topology.ProfileComposition{}, fmt.Errorf("observed source repository %q: %w", source.Repository, err)
		}
		worktree := topology.Worktree{
			ID: source.Observation.Worktree, Repository: repositoryID, Path: repository.Path,
		}
		composition.Profile.Worktrees = append(composition.Profile.Worktrees, topology.WorktreeSelection{
			Repository: repositoryID, Worktree: worktree.ID,
		})
		composition.Repositories = append(composition.Repositories, topology.LogicalRepository{
			ID: repositoryID, Name: source.Repository,
		})
		composition.Worktrees = append(composition.Worktrees, worktree)
		selected[source.Repository] = struct{}{}
	}
	for _, source := range manifest.Sources {
		if source.Derived {
			continue
		}
		if _, found := selected[source.Repository]; !found {
			return topology.ProfileComposition{}, fmt.Errorf("source repository %q was not observed", source.Repository)
		}
	}
	return composition, nil
}

func validateObservedComposition(
	composition topology.ProfileComposition,
	manifest sourceobservation.Manifest,
	observed []workspace.Repository,
) error {
	repositories := make(map[string]workspace.Repository, len(observed))
	for _, repository := range observed {
		name := strings.TrimSpace(repository.Name)
		if _, exists := repositories[name]; exists {
			return fmt.Errorf("observed source repository %q is duplicated", name)
		}
		repositories[name] = repository
	}
	sources := make(map[string]sourceobservation.Source, len(manifest.Sources))
	for _, source := range manifest.Sources {
		sources[source.Repository] = source
	}
	worktrees := make(map[topology.LogicalRepositoryID]topology.Worktree, len(composition.Worktrees))
	for _, worktree := range composition.Worktrees {
		if _, exists := worktrees[worktree.Repository]; exists {
			return fmt.Errorf("topology repository %q selects multiple worktrees", worktree.Repository)
		}
		worktrees[worktree.Repository] = worktree
	}
	for _, selected := range composition.Repositories {
		name := string(selected.ID)
		repository, exists := repositories[name]
		if !exists {
			return fmt.Errorf("topology repository %q was not observed", name)
		}
		if repository.Derived {
			return fmt.Errorf("topology repository %q selects a derived source", name)
		}
		worktree, exists := worktrees[selected.ID]
		if !exists {
			return fmt.Errorf("topology repository %q selects no worktree", name)
		}
		observedWorktree := repository.Worktree
		if observedWorktree == "" {
			source, found := sources[name]
			if !found {
				return fmt.Errorf("topology repository %q has no source observation", name)
			}
			observedWorktree = source.Observation.Worktree
		}
		if observedWorktree != worktree.ID {
			return fmt.Errorf("topology repository %q selects worktree %q, observed %q", name, worktree.ID, observedWorktree)
		}
		if filepath.Clean(repository.Path) != filepath.Clean(worktree.Path) {
			return fmt.Errorf("topology repository %q selects path %q, observed %q", name, worktree.Path, repository.Path)
		}
	}
	return nil
}

// Counts is the number of authoritative facts produced by one full pass.
type Counts struct {
	Repositories int `json:"repositories"`
	Packages     int `json:"packages"`
	Files        int `json:"files"`
	Symbols      int `json:"symbols"`
	Evidence     int `json:"evidence"`
	Edges        int `json:"edges"`
	Unresolved   int `json:"unresolved"`
}

// FullResult contains the reports needed by CLI and MCP callers without
// exposing the temporary facts set after the rebuild has completed.
type FullResult struct {
	Counts        Counts
	IndexReport   indexer.FullReport
	RebuildReport rebuild.Report
	// RecordingError means the graph was published but derived freshness or
	// invalidation state could not be persisted. It must not turn a valid
	// generation into a failed rebuild.
	RecordingError error
}

// RunFull indexes all repositories, validates the facts, and publishes one
// canonical generation. It does not modify the repository registry; callers
// that manage a candidate registry commit it separately.
func RunFull(ctx context.Context, options FullOptions) (result FullResult, resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Root == "" {
		return FullResult{}, fmt.Errorf("full index: root is required")
	}
	if options.ResolverVersion == "" {
		return FullResult{}, fmt.Errorf("full index: resolver version is required")
	}
	if options.SharedTargetsLockPath != "" {
		lock, acquired, err := filelock.Acquire(options.SharedTargetsLockPath)
		if err != nil {
			return FullResult{}, fmt.Errorf("full index: acquire shared analyzer targets lock: %w", err)
		}
		if !acquired {
			return FullResult{}, fmt.Errorf("full index: shared analyzer targets are busy; another profile is indexing")
		}
		defer func() {
			if err := lock.Release(); err != nil {
				resultErr = errors.Join(resultErr,
					fmt.Errorf("full index: release shared analyzer targets lock: %w", err))
			}
		}()
	}

	indexOptions := options.indexerOptions()
	manifest, observedRepositories, err := ObserveSources(ctx, options)
	if err != nil {
		return FullResult{}, err
	}
	before, err := freshness.Capture(ctx, observedRepositories)
	if err != nil {
		return FullResult{}, fmt.Errorf("capture source inventory: %w", err)
	}

	factSet, indexReport, err := indexer.FullWithRepositories(ctx, indexOptions, observedRepositories)
	result = FullResult{IndexReport: indexReport}
	if err != nil {
		return result, fmt.Errorf("index repositories: %w", err)
	}
	defer indexReport.DiscardCache()
	result.Counts = countsFromFacts(factSet)
	after, err := freshness.Capture(ctx, observedRepositories)
	if err != nil {
		return result, fmt.Errorf("verify source inventory: %w", err)
	}
	if before != after {
		return result, fmt.Errorf("source inventory changed during indexing; no fresh generation published")
	}

	layout, err := rebuild.Roles(ctx, rebuild.LayoutOptions{
		Root:  filepath.Clean(options.Root),
		Store: options.Store,
	})
	if err != nil {
		return result, fmt.Errorf("resolve next generation: %w", err)
	}
	snapshotID, err := strconv.ParseInt(layout.NextID, 10, 64)
	if err != nil {
		return result, fmt.Errorf("parse next generation %q: %w", layout.NextID, err)
	}

	rebuildReport, err := rebuild.Run(ctx, rebuild.Options{
		Root:            filepath.Clean(options.Root),
		GenerationID:    layout.NextID,
		Facts:           factSet,
		ResolverVersion: options.ResolverVersion,
		SnapshotID:      snapshotID,
		Store:           options.Store,
		Metrics:         options.Metrics,
		Progress:        options.RebuildProgress,
		SourceManifest:  &manifest,
		VerifySources: func(verifyCtx context.Context) error {
			current, _, observeErr := ObserveSources(verifyCtx, options)
			if observeErr != nil {
				return observeErr
			}
			return sourceobservation.Compare(manifest, current)
		},
	})
	result.RebuildReport = rebuildReport
	if err != nil {
		return result, fmt.Errorf("rebuild graph: %w", err)
	}
	if !rebuildReport.Passed {
		return result, fmt.Errorf("rebuild graph did not pass its gates")
	}
	indexReport.CommitCache()
	if err := freshness.Save(ctx, options.Root, uint64(snapshotID), after); err != nil {
		result.RecordingError = fmt.Errorf("record published generation freshness: %w", err)
	}
	if options.Invalidation != nil {
		if err := options.Invalidation.RecordPublished(ctx, invalidation.ProfileRecord{
			Profile:    options.Profile,
			Generation: rebuildReport.GenerationID,
			Manifest:   manifest,
		}); err != nil {
			result.RecordingError = errors.Join(result.RecordingError,
				fmt.Errorf("record published source state: %w", err))
		}
	}
	return result, nil
}

func countsFromFacts(set facts.Set) Counts {
	return Counts{
		Repositories: len(set.Repositories),
		Packages:     len(set.Packages),
		Files:        len(set.Files),
		Symbols:      len(set.Symbols),
		Evidence:     len(set.Evidence),
		Edges:        len(set.Edges),
		Unresolved:   len(set.Unresolved),
	}
}
