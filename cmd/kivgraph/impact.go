package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/gitdiff"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// impactOptions carries the flags of `kivgraph impact`.
type impactOptions struct {
	ConfigPath string
	Base       string
	Depth      int
	MaxNodes   int
	Limit      int
	AllowStale bool
	JSONOutput bool
}

func impactFlagSet(options *impactOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("impact", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "read this configuration instead of the project's or the default one")
	flags.StringVar(&options.Base, "base", "", "the ref to measure the working tree against (required)")
	flags.IntVar(&options.Depth, "depth", tools.DefaultBlastRadiusDepth, "how many hops of dependents to follow")
	flags.IntVar(&options.MaxNodes, "max-nodes", tools.DefaultBlastRadiusMaxNodes, "stop the traversal after this many symbols")
	flags.IntVar(&options.Limit, "limit", tools.DefaultBlastRadiusLimit, "show at most this many production symbols; the count is always whole")
	flags.BoolVar(&options.AllowStale, "allow-stale", false, "answer even when the graph is older than the working tree")
	flags.BoolVar(&options.JSONOutput, "json", false, "write the result as JSON")
	return flags
}

// impactResult is what `--json` writes: the engine's report with where it came
// from.
type impactResult struct {
	Base       string `json:"base"`
	BaseCommit string `json:"base_commit"`
	Freshness  string `json:"freshness"`
	tools.ImpactReport
}

// runImpact answers what a diff reaches: the symbols the changed lines belong to,
// everything that depends on them, and the tests among those apart.
//
// It reads the graph the last index published. The changed lines are lines of the
// working tree, so they mean something to that graph only while the graph was
// built from that tree; a graph older than the tree would put them on the wrong
// symbols, which is why a stale one refuses to answer unless asked to.
func runImpact(args []string, stdout, stderr io.Writer) int {
	var options impactOptions
	flags := impactFlagSet(&options)
	if parsed, code := parseCommandFlags("impact", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "impact: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Base == "" {
		writeCommandError(stderr, "impact: --base is required: name the ref to measure against, for example --base origin/main")
		return 2
	}
	if options.Depth < 1 || options.Depth > tools.MaximumBlastRadiusDepth {
		writeCommandError(stderr, "impact: --depth must be between 1 and %d, got %d", tools.MaximumBlastRadiusDepth, options.Depth)
		return 2
	}
	if options.MaxNodes < 1 || options.MaxNodes > tools.MaximumBlastRadiusMaxNodes {
		writeCommandError(stderr, "impact: --max-nodes must be between 1 and %d, got %d", tools.MaximumBlastRadiusMaxNodes, options.MaxNodes)
		return 2
	}
	if options.Limit < 1 {
		writeCommandError(stderr, "impact: --limit must be at least 1, got %d", options.Limit)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := computeImpact(ctx, options, stderr)
	if err != nil {
		writeCommandError(stderr, "impact: %v", err)
		return 1
	}
	if options.JSONOutput {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			writeCommandError(stderr, "impact: encode result: %v", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	writeImpact(stdout, result, options)
	return 0
}

func computeImpact(ctx context.Context, options impactOptions, stderr io.Writer) (impactResult, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return impactResult{}, fmt.Errorf("resolve current directory: %w", err)
	}
	root, err := gitdiff.Root(ctx, workingDirectory)
	if err != nil {
		return impactResult{}, err
	}
	diff, err := gitdiff.Compare(ctx, root, options.Base)
	if err != nil {
		return impactResult{}, err
	}

	configPath := options.ConfigPath
	if projectConfig := filepath.Join(root, ".kivgraph", "config.yaml"); configPath == "" {
		if _, statErr := os.Stat(projectConfig); statErr == nil {
			configPath = projectConfig
		}
	}
	loaded, err := config.LoadProfile(configPath, "")
	if err != nil {
		return impactResult{}, fmt.Errorf("load configuration: %w", err)
	}
	store, err := openConfiguredSnapshot(ctx, loaded)
	if err != nil {
		return impactResult{}, fmt.Errorf("open the published graph: %w", err)
	}
	defer store.Close()
	snapshot := store.Load()
	if snapshot == nil {
		if failure := store.LoadFailure(); failure != nil {
			return impactResult{}, fmt.Errorf("the published graph could not be read: %w", failure)
		}
		return impactResult{}, errors.New("no graph is published yet: run \"kivgraph index\" first")
	}

	state, err := impactFreshness(ctx, loaded, snapshot.Metadata().ID)
	if err != nil {
		return impactResult{}, err
	}
	switch state.State {
	case "stale":
		if !options.AllowStale {
			return impactResult{}, fmt.Errorf("the graph is older than the working tree (%s), so the changed lines would land on the wrong symbols: run \"kivgraph index\" first, or pass --allow-stale", state.Detail)
		}
		writeWarning(stderr, "impact: the graph is older than the working tree (%s); symbols may be misplaced", state.Detail)
	case "fresh":
	default:
		writeWarning(stderr, "impact: the graph's freshness is %s (%s)", state.State, state.Detail)
	}

	impactOptions := tools.ImpactOptions{
		RepositoryRoot: root, Changes: diff.Changes,
		Depth: options.Depth, MaxNodes: options.MaxNodes, Limit: options.Limit,
	}
	report, err := tools.Impact(ctx, snapshot, impactOptions)
	if errors.Is(err, tools.ErrRepositoryNotIndexed) {
		// A linked worktree is the same repository at another path. The graph
		// was built from the checkout that is registered, so it is used -- and
		// said, because the two trees may not hold the same lines.
		if registered, found := registeredCheckoutOf(loaded, root); found {
			writeWarning(stderr, "impact: this is a linked worktree; the graph was built from %s, and symbols may be misplaced where the two differ", registered)
			impactOptions.RepositoryRoot = registered
			report, err = tools.Impact(ctx, snapshot, impactOptions)
		}
	}
	if err != nil {
		if errors.Is(err, tools.ErrRepositoryNotIndexed) {
			return impactResult{}, fmt.Errorf("%w; register it with \"kivgraph index\" or use --config", err)
		}
		return impactResult{}, err
	}
	return impactResult{Base: options.Base, BaseCommit: diff.BaseCommit, Freshness: state.State, ImpactReport: report}, nil
}

// impactFreshness asks whether the published generation still describes the
// registered sources.
func impactFreshness(ctx context.Context, loaded config.Loaded, generation uint64) (freshness.Status, error) {
	registry, err := workspace.NewRegistry(ctx, loaded.Repositories)
	if err != nil {
		return freshness.Status{}, fmt.Errorf("resolve the repository registry: %w", err)
	}
	return freshness.Check(ctx, filepath.Dir(loaded.Config.Storage.DatabasePath), generation, registry.List()), nil
}

func writeImpact(stdout io.Writer, result impactResult, options impactOptions) {
	styles := styleFor(stdout)
	fmt.Fprintf(stdout, "impact of %s..working tree in %s (graph %d, %s)\n\n",
		result.Base, result.Repository, result.SnapshotID, result.Freshness)

	if len(result.Touched) == 0 && len(result.NotIndexed) == 0 && len(result.NoSymbol) == 0 {
		writeInfo(stdout, "impact: nothing differs from %s", result.Base)
		return
	}
	fmt.Fprintf(stdout, "%stouched%s (%d)\n", styles.dim, styles.reset, len(result.Touched))
	for _, row := range result.Touched {
		fmt.Fprintf(stdout, "  %s  %s:%d-%d  %s%s\n", row.QualifiedName, row.FilePath, row.StartLine, row.EndLine, row.Kind, testMark(row.Test))
	}
	if len(result.Touched) == 0 {
		fmt.Fprintln(stdout, "  none: no changed line belongs to a symbol of the graph")
		fmt.Fprintln(stdout)
	} else {
		fmt.Fprintf(stdout, "\n%saffected%s %d", styles.dim, styles.reset, result.Affected)
		if result.TraversalTruncated {
			fmt.Fprintf(stdout, " or more (stopped at %d symbols)", result.MaxNodes)
		}
		fmt.Fprintf(stdout, ", to depth %d\n", result.Depth)
		fmt.Fprintf(stdout, "%sproduction%s (%d)\n", styles.dim, styles.reset, result.ProductionTotal)
		for _, row := range result.Production {
			fmt.Fprintf(stdout, "  %s  %s:%d  depth %d\n", row.QualifiedName, row.FilePath, row.StartLine, row.Depth)
		}
		if result.ProductionTotal > len(result.Production) {
			fmt.Fprintf(stdout, "  ... %d more (--limit %d shows them, --json carries the count)\n", result.ProductionTotal-len(result.Production), options.Limit)
		}
		fmt.Fprintf(stdout, "%stests%s (%d files)\n", styles.dim, styles.reset, len(result.ByTestFile))
		for _, file := range result.ByTestFile {
			fmt.Fprintf(stdout, "  %s  (%d)\n", file.FilePath, len(file.Symbols))
		}
		fmt.Fprintln(stdout)
	}
	for _, file := range result.NotIndexed {
		fmt.Fprintf(stdout, "%snot in the graph%s  %s  (%s)\n", styles.dim, styles.reset, file.Path, file.Reason)
	}
	for _, gap := range result.NoSymbol {
		span := fmt.Sprintf("%d-%d", gap.StartLine, gap.EndLine)
		if gap.EndLine == 0 {
			span = "whole file"
		}
		fmt.Fprintf(stdout, "%sno symbol%s  %s:%s\n", styles.dim, styles.reset, gap.Path, span)
	}
}

func testMark(test bool) string {
	if test {
		return " (test)"
	}
	return ""
}

// registeredCheckoutOf answers the registered repository that shares a Git
// common directory with root, which is what makes two paths one repository. It
// declines when two registered entries do: nothing says which one is meant.
func registeredCheckoutOf(loaded config.Loaded, root string) (string, bool) {
	common, err := watcher.GitCommonDirectory(root)
	if err != nil {
		return "", false
	}
	matched, matches := "", 0
	for _, repository := range loaded.Repositories.Repositories {
		registered, err := watcher.GitCommonDirectory(repository.Path)
		if err != nil || registered != common {
			continue
		}
		matched = repository.Path
		matches++
	}
	return matched, matches == 1
}
