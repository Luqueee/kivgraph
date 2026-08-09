package indexing

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

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
	TypeScriptWorker  string
	WorkingDirectory  string
	Root              string
	ResolverVersion   string
	Store             generation.Config
	Metrics           *metrics.Registry

	Progress        func(indexer.ProgressEvent)
	RebuildProgress func(rebuild.StageName)
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
		Repositories:      options.Repositories,
		SyntheticWorkFile: options.SyntheticWorkFile,
		IncludeTests:      options.IncludeTests,
		TypeScriptWorker:  options.TypeScriptWorker,
		WorkingDirectory:  options.WorkingDirectory,
		Progress:          options.Progress,
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
