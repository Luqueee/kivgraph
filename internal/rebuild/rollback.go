package rebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// ErrRollbackFailed reports that a rollback did not reach a switched
// generation. Like ErrRebuildFailed, the wrapped detail names exactly what
// stopped it — no destination, a digest mismatch, a broken invariant — so a
// caller can report the cause without re-deriving it from the report.
var ErrRollbackFailed = errors.New("rollback failed")

// Role names one of the three pointers LUQUE-0905 requires the generation
// store to expose. None of the three renames anything on disk:
// generations/<id> and CURRENT are the load bearing, already exhaustively
// tested layout, and stay exactly as they are. graph.active is the
// generation CURRENT names, graph.next is the <id>.tmp candidate
// generation.Store.Publish builds and swaps in, and graph.backup is the
// generation a rollback would restore — the generation that was active a
// moment ago, retained on purpose instead of being pruned.
type Role string

const (
	RoleActive Role = "graph.active"
	RoleNext   Role = "graph.next"
	RoleBackup Role = "graph.backup"
)

// LayoutOptions locates a generation store.
type LayoutOptions struct {
	Root  string
	Store generation.Config
}

// Layout reports the three roles the plan requires.
type Layout struct {
	Active    generation.Generation
	Backup    generation.Generation
	HasBackup bool
	NextID    string
	Retained  []string
}

// Roles resolves graph.active, graph.next and graph.backup for the
// generation store rooted at options.Root. A store that has never
// published a generation is not an error: Layout.Active is the zero
// Generation and Layout.HasBackup is false, so a caller renders an empty
// layout instead of failing — the same stance generation.Store.Current
// already takes for CURRENT alone.
func Roles(ctx context.Context, options LayoutOptions) (Layout, error) {
	store, err := openGenerationStore(options.Root, options.Store)
	if err != nil {
		return Layout{}, fmt.Errorf("open generation store: %w", err)
	}

	var layout Layout
	active, err := store.Current(ctx)
	switch {
	case err == nil:
		layout.Active = active
	case errors.Is(err, generation.ErrNoCurrent):
		// No publication has ever run: an empty active role, not a failure.
	default:
		return Layout{}, fmt.Errorf("read active generation: %w", err)
	}

	backup, err := store.Backup(ctx)
	switch {
	case err == nil:
		layout.Backup = backup
		layout.HasBackup = true
	case errors.Is(err, generation.ErrNoBackup):
		// BACKUP == CURRENT (or nothing has ever been active): no backup,
		// and that is a self-consistent state, not a failure to report.
	default:
		return Layout{}, fmt.Errorf("read backup generation: %w", err)
	}

	nextID, err := store.NextID(ctx)
	if err != nil {
		return Layout{}, fmt.Errorf("compute next generation id: %w", err)
	}
	layout.NextID = nextID

	published, err := store.List(ctx)
	if err != nil {
		return Layout{}, fmt.Errorf("list retained generations: %w", err)
	}
	ids := make([]string, 0, len(published))
	for _, entry := range published {
		ids = append(ids, entry.ID)
	}
	sort.Strings(ids)
	layout.Retained = ids

	return layout, nil
}

// RollbackOptions configures one rollback.
type RollbackOptions struct {
	Root         string
	Store        generation.Config
	GenerationID string

	// Counts and Integrity default to the ladybug implementations, exactly
	// like Options.Counts and Options.Integrity already do for Run; tests
	// substitute them so rollback validation is exercised without cgo.
	Counts    func(context.Context, string) (map[string]int64, error)
	Integrity func(context.Context, string) (ladybug.CanonicalIntegrityReport, error)
}

// RollbackReport accounts for one rollback.
type RollbackReport struct {
	From       generation.Generation
	To         generation.Generation
	Digest     string
	Expected   string
	Invariants ladybug.CanonicalIntegrityReport
	Passed     bool
}

// Rollback switches CURRENT back to a previously published generation. The
// destination is options.GenerationID when given, otherwise the registered
// graph.backup; with neither, there is nowhere defined to roll back to and
// Rollback fails without touching the store.
//
// Before switching anything, the destination is revalidated exactly like a
// fresh publish would be: its snapshot digest is recomputed from Counts and
// compared against the snapshot.sha256 the original rebuild's snapshot
// stage wrote for it, using the very same canonicalSnapshotDigest
// calculation — so rollback and rebuild can never disagree about what a
// generation's digest means — and Integrity must report all six LUQUE-0904
// invariants passing. Both checks run inside the generation.ValidateFunc
// generation.Store.Restore calls before it ever rewrites CURRENT, so a
// failing check leaves the active generation untouched by the store's own
// construction: Rollback does not need, and does not attempt, its own undo
// path. A successful Restore also makes the generation that was active a
// moment ago the new graph.backup, so a rollback can always be rolled
// forward again.
func Rollback(ctx context.Context, options RollbackOptions) (RollbackReport, error) {
	store, err := openGenerationStore(options.Root, options.Store)
	if err != nil {
		return RollbackReport{}, fmt.Errorf("%w: open generation store: %v", ErrRollbackFailed, err)
	}

	from, err := store.Current(ctx)
	if err != nil {
		return RollbackReport{}, fmt.Errorf("%w: read active generation: %v", ErrRollbackFailed, err)
	}
	report := RollbackReport{From: from}

	targetID := options.GenerationID
	if targetID == "" {
		backup, backupErr := store.Backup(ctx)
		if backupErr != nil {
			if errors.Is(backupErr, generation.ErrNoBackup) {
				return report, fmt.Errorf("%w: no backup generation is registered and no generation id was given: nowhere to roll back to", ErrRollbackFailed)
			}
			return report, fmt.Errorf("%w: read backup generation: %v", ErrRollbackFailed, backupErr)
		}
		targetID = backup.ID
	}

	counts := options.Counts
	if counts == nil {
		counts = ladybug.CanonicalTableCounts
	}
	verifyIntegrity := options.Integrity
	if verifyIntegrity == nil {
		verifyIntegrity = ladybug.VerifyCanonicalIntegrity
	}

	// Both gates always run when they can, the same "both halves always
	// run" convention Run's own integrity checkpoint follows: a failure
	// report should never leave the caller guessing whether the half it
	// cannot see would also have failed.
	validate := func(validateCtx context.Context, candidate generation.Generation) error {
		report.To = candidate

		expectedDigest, digestErr := readSnapshotDigest(candidate.Path)
		if digestErr != nil {
			return digestErr
		}
		report.Expected = expectedDigest

		tables, countsErr := counts(validateCtx, candidate.DatabasePath)
		if countsErr != nil {
			return fmt.Errorf("read canonical table counts: %w", countsErr)
		}
		observedDigest := canonicalSnapshotDigest(tables)
		report.Digest = observedDigest
		digestMatched := observedDigest == expectedDigest

		invariants, invariantsErr := verifyIntegrity(validateCtx, candidate.DatabasePath)
		if invariantsErr != nil {
			return fmt.Errorf("verify canonical integrity: %w", invariantsErr)
		}
		report.Invariants = invariants

		switch {
		case !digestMatched && !invariants.Passed:
			return fmt.Errorf("snapshot digest mismatch (recomputed %s, generation %s recorded %s) and %d invariant violation(s)",
				observedDigest, candidate.ID, expectedDigest, invariants.Violations())
		case !digestMatched:
			return fmt.Errorf("snapshot digest mismatch: recomputed %s, generation %s recorded %s", observedDigest, candidate.ID, expectedDigest)
		case !invariants.Passed:
			return fmt.Errorf("integrity check failed: %d invariant violation(s)", invariants.Violations())
		}
		return nil
	}

	if err := store.Restore(ctx, targetID, validate); err != nil {
		return report, fmt.Errorf("%w: %v", ErrRollbackFailed, err)
	}

	report.Passed = true
	return report, nil
}

// readSnapshotDigest reads the digest writeSnapshotDigest recorded next to
// a published generation's database. A generation missing snapshot.sha256
// (published before this ticket, or corrupted) fails explicitly here
// instead of being blindly reactivated: a generation with no recorded
// digest must never become CURRENT again.
func readSnapshotDigest(generationPath string) (string, error) {
	digestPath := filepath.Join(generationPath, snapshotFileName)
	data, err := os.ReadFile(digestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("generation has no %s: cannot verify its digest before reactivating it", snapshotFileName)
		}
		return "", fmt.Errorf("read %s: %w", digestPath, err)
	}
	return strings.TrimSpace(string(data)), nil
}
