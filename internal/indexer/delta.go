package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// ErrUpdateFailed reports that an incremental update did not reach a usable
// graph. The wrapped detail names what stopped it.
var ErrUpdateFailed = errors.New("incremental update failed")

// Route is the publication strategy chosen for one incremental update.
type Route string

const (
	// RouteNoop means the delta would change nothing.
	RouteNoop Route = "NOOP"
	// RouteDelta applies the change transactionally to graph.active.
	RouteDelta Route = "DELTA"
	// RouteRepublish rebuilds into graph.next and swaps it in atomically.
	RouteRepublish Route = "REPUBLISH"
)

// DefaultRepublishRatio is the share of the indexed files a delta may
// restate before a full republish becomes the better route. Above it the
// delta stops being cheaper than a clean bulk load, and the republish also
// buys an atomic swap and a fresh graph.backup instead of mutating the
// generation queries are being served from.
const DefaultRepublishRatio = 0.5

// Decision explains which route an update takes and why.
type Decision struct {
	Route  Route  `json:"route"`
	Reason string `json:"reason"`
	// ChangedFiles are the durable file keys the delta withdraws and,
	// except for removals, restates.
	ChangedFiles []string `json:"changed_files,omitempty"`
	// TotalFiles is the size of the indexed state the ratio is measured
	// against.
	TotalFiles int64 `json:"total_files"`
	// ForcedBy names the actions that made a republish mandatory.
	ForcedBy []InvalidationAction `json:"forced_by,omitempty"`
}

// republishForcingActions are the actions a per-file delta cannot express.
// Both change the identity or the resolution of packages themselves, which
// no file-scoped retirement and restatement withdraws.
var republishForcingActions = []InvalidationAction{ActionRebuildRegistry, ActionReindexProject}

// Decide chooses the route for the plans and the computed delta. It never
// touches the store: the caller supplies the layout facts it needs.
//
// hasActive reports whether a generation is currently published;
// totalFiles is the file count of the state the delta would be applied to.
func Decide(plans []InvalidationPlan, delta facts.Delta, hasActive bool, totalFiles int64, republishRatio float64) Decision {
	decision := Decision{
		TotalFiles:   totalFiles,
		ChangedFiles: changedFileKeys(delta),
	}
	if republishRatio <= 0 {
		republishRatio = DefaultRepublishRatio
	}

	forced := forcingActions(plans)
	switch {
	case len(forced) != 0:
		decision.Route = RouteRepublish
		decision.ForcedBy = forced
		decision.Reason = fmt.Sprintf("%d plan action(s) cannot be expressed as a file delta", len(forced))
	case delta.Empty():
		decision.Route = RouteNoop
		decision.Reason = "delta changes nothing"
	case !hasActive:
		decision.Route = RouteRepublish
		decision.Reason = "no active generation to apply a delta to"
	case totalFiles > 0 && float64(len(decision.ChangedFiles)) > republishRatio*float64(totalFiles):
		decision.Route = RouteRepublish
		decision.Reason = fmt.Sprintf("delta restates %d of %d file(s), above the %.2f republish ratio",
			len(decision.ChangedFiles), totalFiles, republishRatio)
	default:
		decision.Route = RouteDelta
		decision.Reason = fmt.Sprintf("delta restates %d file(s) on the active generation", len(decision.ChangedFiles))
	}
	return decision
}

func forcingActions(plans []InvalidationPlan) []InvalidationAction {
	seen := make(map[InvalidationAction]struct{}, len(republishForcingActions))
	forced := make([]InvalidationAction, 0, len(republishForcingActions))
	for _, action := range republishForcingActions {
		for _, plan := range plans {
			if !plan.Has(action) {
				continue
			}
			if _, duplicate := seen[action]; duplicate {
				break
			}
			seen[action] = struct{}{}
			forced = append(forced, action)
			break
		}
	}
	return forced
}

func changedFileKeys(delta facts.Delta) []string {
	keys := make([]string, 0, len(delta.ReplacedFiles)+len(delta.RemovedFiles))
	keys = append(keys, delta.ReplacedFiles...)
	keys = append(keys, delta.RemovedFiles...)
	sort.Strings(keys)
	return keys
}

// UpdateOptions configures one incremental update.
type UpdateOptions struct {
	Root  string
	Store generation.Config

	// Plans are the invalidation plans that motivated this update.
	Plans []InvalidationPlan
	// Previous is the indexed state the active generation holds; Next is
	// the state the language engines just produced for it.
	Previous facts.Set
	Next     facts.Set

	// RepublishRatio overrides DefaultRepublishRatio.
	RepublishRatio float64

	// GenerationID, ResolverVersion and SnapshotID are used by the
	// republish route; SnapshotID also stamps the provenance of every edge
	// a delta upserts.
	GenerationID    string
	ResolverVersion string
	SnapshotID      int64

	// Layout, ApplyDelta, Counts and Republish default to the real
	// implementations; tests substitute them so the orchestration runs
	// without cgo, exactly as rebuild.Options already allows.
	Layout     func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error)
	ApplyDelta func(context.Context, string, facts.Delta, ladybug.CanonicalLoadOptions) (ladybug.CanonicalMutationResult, error)
	Counts     func(context.Context, string) (map[string]int64, error)
	Republish  func(context.Context, rebuild.Options) (rebuild.Report, error)
}

// UpdateResult accounts for one incremental update.
type UpdateResult struct {
	Decision Decision
	Delta    facts.Delta
	// Generation is the generation serving queries once the update is done:
	// the mutated active generation, or the newly published one.
	Generation generation.Generation
	// Mutation is populated on the delta route.
	Mutation ladybug.CanonicalMutationResult
	// SnapshotDigest is the refreshed digest recorded for the mutated
	// generation on the delta route.
	SnapshotDigest string
	// Rebuild is populated on the republish route.
	Rebuild rebuild.Report
	Passed  bool
}

// Update computes the delta between the previous and the next indexed state,
// decides how to publish it, and carries it out.
//
// On the delta route the change is applied transactionally to graph.active
// and that generation's snapshot.sha256 is rewritten from the row counts the
// mutation left behind: rollback revalidates a destination by recomputing
// that digest, so a generation mutated in place without refreshing it could
// never be rolled back to again.
//
// On the republish route nothing touches graph.active: rebuild.Run builds
// graph.next, verifies it and swaps it in, leaving the previous generation
// as graph.backup.
func Update(ctx context.Context, options UpdateOptions) (UpdateResult, error) {
	var result UpdateResult
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("%w: %v", ErrUpdateFailed, err)
	}

	delta, err := facts.Diff(options.Previous, options.Next)
	if err != nil {
		return result, fmt.Errorf("%w: diff indexed state: %w", ErrUpdateFailed, err)
	}
	result.Delta = delta

	readLayout := options.Layout
	if readLayout == nil {
		readLayout = rebuild.Roles
	}
	layout, err := readLayout(ctx, rebuild.LayoutOptions{Root: options.Root, Store: options.Store})
	if err != nil {
		return result, fmt.Errorf("%w: read generation layout: %w", ErrUpdateFailed, err)
	}

	hasActive := layout.Active.DatabasePath != ""
	result.Decision = Decide(options.Plans, delta, hasActive, int64(len(options.Previous.Files)), options.RepublishRatio)

	switch result.Decision.Route {
	case RouteNoop:
		result.Generation = layout.Active
		result.Passed = true
		return result, nil
	case RouteDelta:
		return applyDeltaRoute(ctx, options, layout, delta, result)
	default:
		return republishRoute(ctx, options, layout, result)
	}
}

func applyDeltaRoute(ctx context.Context, options UpdateOptions, layout rebuild.Layout, delta facts.Delta, result UpdateResult) (UpdateResult, error) {
	apply := options.ApplyDelta
	if apply == nil {
		apply = ladybug.ApplyCanonicalDelta
	}
	counts := options.Counts
	if counts == nil {
		counts = ladybug.CanonicalTableCounts
	}

	mutation, err := apply(ctx, layout.Active.DatabasePath, delta, ladybug.CanonicalLoadOptions{
		SnapshotID:      options.SnapshotID,
		ResolverVersion: options.ResolverVersion,
	})
	if err != nil {
		return result, fmt.Errorf("%w: apply delta to %s: %w", ErrUpdateFailed, rebuild.RoleActive, err)
	}
	result.Mutation = mutation

	tables, err := counts(ctx, layout.Active.DatabasePath)
	if err != nil {
		return result, fmt.Errorf("%w: read mutated table counts: %w", ErrUpdateFailed, err)
	}
	digest, err := rebuild.RefreshSnapshotDigest(filepath.Dir(layout.Active.DatabasePath), tables)
	if err != nil {
		return result, fmt.Errorf("%w: refresh snapshot digest: %w", ErrUpdateFailed, err)
	}

	result.SnapshotDigest = digest
	result.Generation = layout.Active
	result.Passed = true
	return result, nil
}

func republishRoute(ctx context.Context, options UpdateOptions, layout rebuild.Layout, result UpdateResult) (UpdateResult, error) {
	republish := options.Republish
	if republish == nil {
		republish = rebuild.Run
	}
	generationID := options.GenerationID
	if generationID == "" {
		generationID = layout.NextID
	}

	report, err := republish(ctx, rebuild.Options{
		Root:            options.Root,
		GenerationID:    generationID,
		Facts:           options.Next,
		ResolverVersion: options.ResolverVersion,
		SnapshotID:      options.SnapshotID,
		Store:           options.Store,
	})
	result.Rebuild = report
	if err != nil {
		return result, fmt.Errorf("%w: republish: %w", ErrUpdateFailed, err)
	}
	if !report.Passed {
		return result, fmt.Errorf("%w: republish did not pass", ErrUpdateFailed)
	}

	result.Generation = report.Publication.Generation
	result.Passed = true
	return result, nil
}
