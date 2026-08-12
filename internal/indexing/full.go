package indexing

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/Luqueee/ladygraph/internal/config"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/indexer"
	"github.com/Luqueee/ladygraph/internal/metrics"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// FullOptions configures a complete index and canonical graph publication.
// The operation reads every configured repository and publishes a new
// generation only after the rebuild gates pass.
type FullOptions struct {
	Repositories      []workspace.Repository
	SyntheticWorkFile string
	IncludeTests      bool
	// GoBuildTags are the build constraints every Go load satisfies.
	GoBuildTags []string
	// GoAllowNetwork lets the Go loads reach a module proxy.
	GoAllowNetwork bool
	// GoMaximumLoads bounds concurrent Go loads.
	GoMaximumLoads int
	// TypeScriptMaximumWorkers bounds concurrent TypeScript workers.
	TypeScriptMaximumWorkers int
	TypeScriptWorker         string
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
	WorkingDirectory      string
	// CacheMode and CacheDirectory configure the fact cache: whether a
	// unit may be served from the facts a previous pass stored, and where
	// those entries live.
	CacheMode       indexer.CacheMode
	CacheDirectory  string
	Root            string
	ResolverVersion string
	Store           generation.Config
	Metrics         *metrics.Registry

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
		SyntheticWorkFile:        configuration.Go.SyntheticWorkFile,
		IncludeTests:             configuration.Go.IncludeTests,
		GoBuildTags:              configuration.Go.BuildTags,
		GoAllowNetwork:           configuration.Go.AllowNetwork,
		GoMaximumLoads:           configuration.Go.MaximumLoads,
		TypeScriptMaximumWorkers: configuration.TypeScript.MaximumWorkers,
		TypeScriptWorker:         configuration.TypeScript.WorkerCommand,
		RustAnalyzer:             configuration.Rust.AnalyzerCommand,
		RustTargetDirectory:      configuration.Rust.TargetDirectory,
		RustMaximumWorkspaces:    configuration.Rust.MaximumWorkspaces,
		RustFeatures:             configuration.Rust.Features,
		RustAllFeatures:          configuration.Rust.AllFeatures,
		RustNoDefaultFeatures:    configuration.Rust.NoDefaultFeatures,
		RustCfgs:                 configuration.Rust.Cfgs,
		RustBuildScripts:         configuration.Rust.BuildScripts,
		RustProcMacros:           configuration.Rust.ProcMacros,
		RustIncludeTests:         configuration.Rust.IncludeTests,
		RustAllowNetwork:         configuration.Rust.AllowNetwork,
		RustSysroot:              configuration.Rust.Sysroot,
		CacheMode:                indexer.CacheMode(configuration.Indexing.FactCache),
		CacheDirectory:           configuration.Indexing.FactCachePath,
		Root:                     filepath.Dir(configuration.Storage.DatabasePath),
		Store:                    generation.DefaultConfig(),
	}
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
}

// RunFull indexes all repositories, validates the facts, and publishes one
// canonical generation. It does not modify the repository registry; callers
// that manage a candidate registry commit it separately.
func RunFull(ctx context.Context, options FullOptions) (FullResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Root == "" {
		return FullResult{}, fmt.Errorf("full index: root is required")
	}
	if options.ResolverVersion == "" {
		return FullResult{}, fmt.Errorf("full index: resolver version is required")
	}

	factSet, indexReport, err := indexer.Full(ctx, indexer.FullOptions{
		Repositories:             options.Repositories,
		SyntheticWorkFile:        options.SyntheticWorkFile,
		IncludeTests:             options.IncludeTests,
		GoBuildTags:              options.GoBuildTags,
		GoAllowNetwork:           options.GoAllowNetwork,
		GoMaximumLoads:           options.GoMaximumLoads,
		TypeScriptMaximumWorkers: options.TypeScriptMaximumWorkers,
		TypeScriptWorker:         options.TypeScriptWorker,
		RustAnalyzer:             options.RustAnalyzer,
		RustTargetDirectory:      options.RustTargetDirectory,
		RustMaximumWorkspaces:    options.RustMaximumWorkspaces,
		RustFeatures:             options.RustFeatures,
		RustAllFeatures:          options.RustAllFeatures,
		RustNoDefaultFeatures:    options.RustNoDefaultFeatures,
		RustCfgs:                 options.RustCfgs,
		RustBuildScripts:         options.RustBuildScripts,
		RustProcMacros:           options.RustProcMacros,
		RustIncludeTests:         options.RustIncludeTests,
		RustAllowNetwork:         options.RustAllowNetwork,
		RustSysroot:              options.RustSysroot,
		WorkingDirectory:         options.WorkingDirectory,
		CacheMode:                options.CacheMode,
		CacheDirectory:           options.CacheDirectory,
		Progress:                 options.Progress,
	})
	result := FullResult{IndexReport: indexReport}
	if err != nil {
		return result, fmt.Errorf("index repositories: %w", err)
	}
	result.Counts = countsFromFacts(factSet)

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
	})
	result.RebuildReport = rebuildReport
	if err != nil {
		return result, fmt.Errorf("rebuild graph: %w", err)
	}
	if !rebuildReport.Passed {
		return result, fmt.Errorf("rebuild graph did not pass its gates")
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
