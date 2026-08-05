package rebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/luque/internal/storage/ladybug"
)

// publishTwoGenerations runs Run twice against the same root with the same
// fact set, so rollback tests start from a store that actually has a
// graph.backup: firstID is what graph.backup names, secondID is
// graph.active.
func publishTwoGenerations(t *testing.T, root string) (firstID, secondID string) {
	t.Helper()
	set := sampleFacts()
	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if _, err := Run(context.Background(), buildOptions(t, root, "000002", set)); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	return "000001", "000002"
}

// rollbackOptions wires the same fake Counts/Integrity hooks buildOptions
// gives Run, so rollback tests exercise Rollback's own orchestration
// without cgo, deterministically, whether or not the ladybug build tag is
// set.
func rollbackOptions(root, generationID string) RollbackOptions {
	return RollbackOptions{
		Root:         root,
		GenerationID: generationID,
		Counts:       fakeCounts,
		Integrity:    fakeIntegrityPassing,
	}
}

// TestRolesReportsActiveBackupAndNextIDAfterTwoPublications is half of the
// (a) contract: two publications leave graph.active on the newer
// generation, graph.backup on the older one, NextID past both, and
// Retained naming exactly the two.
func TestRolesReportsActiveBackupAndNextIDAfterTwoPublications(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)

	layout, err := Roles(context.Background(), LayoutOptions{Root: root})
	if err != nil {
		t.Fatalf("Roles() error = %v", err)
	}
	if layout.Active.ID != secondID {
		t.Fatalf("Active.ID = %q, want %q", layout.Active.ID, secondID)
	}
	if !layout.HasBackup || layout.Backup.ID != firstID {
		t.Fatalf("Backup = %+v (HasBackup=%v), want id %q", layout.Backup, layout.HasBackup, firstID)
	}
	if layout.NextID == "" || layout.NextID == firstID || layout.NextID == secondID {
		t.Fatalf("NextID = %q, want a fresh id past %q and %q", layout.NextID, firstID, secondID)
	}
	if len(layout.Retained) != 2 || layout.Retained[0] != firstID || layout.Retained[1] != secondID {
		t.Fatalf("Retained = %v, want exactly [%q %q]", layout.Retained, firstID, secondID)
	}
}

// TestRolesOnEmptyStoreDoesNotFail is the other half of the (a) contract: a
// store that has never published anything is a legitimate, reportable
// layout, not an error.
func TestRolesOnEmptyStoreDoesNotFail(t *testing.T) {
	root := t.TempDir()
	layout, err := Roles(context.Background(), LayoutOptions{Root: root})
	if err != nil {
		t.Fatalf("Roles() on empty store error = %v", err)
	}
	if layout.Active.ID != "" {
		t.Fatalf("Active = %+v, want the zero Generation", layout.Active)
	}
	if layout.HasBackup {
		t.Fatal("HasBackup = true, want false on an empty store")
	}
	if layout.NextID == "" {
		t.Fatal("NextID is empty, want the id the first publication would use")
	}
	if len(layout.Retained) != 0 {
		t.Fatalf("Retained = %v, want empty", layout.Retained)
	}
}

// TestRollbackDigestMismatchDoesNotSwitch is the (b) contract: a
// recomputed digest that disagrees with a generation's stored
// snapshot.sha256 blocks the switch and leaves graph.active untouched.
// Integrity is kept passing so the failure can only come from the digest.
func TestRollbackDigestMismatchDoesNotSwitch(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)

	options := rollbackOptions(root, firstID)
	options.Counts = fakeCountsWithMismatch("Symbol", 1)

	report, err := Rollback(context.Background(), options)
	if err == nil {
		t.Fatal("Rollback() error = nil, want a digest mismatch error")
	}
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("errors.Is(err, ErrRollbackFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("RollbackReport.Passed = true, want false")
	}
	if report.Digest == "" || report.Expected == "" || report.Digest == report.Expected {
		t.Fatalf("Digest = %q, Expected = %q, want two different non-empty digests", report.Digest, report.Expected)
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("err = %v, want it to name a digest mismatch", err)
	}

	if got := currentGenerationID(t, root); got != secondID {
		t.Fatalf("CURRENT = %q, want %q (unchanged)", got, secondID)
	}
}

// TestRollbackInvariantViolationDoesNotSwitch is the (c) contract: a broken
// LUQUE-0904 invariant blocks the switch even when the digest matches
// exactly.
func TestRollbackInvariantViolationDoesNotSwitch(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)

	options := rollbackOptions(root, firstID)
	options.Integrity = fakeIntegrityWithViolation(ladybug.RuleDuplicateStableKey, ladybug.IntegrityViolation{
		Rule:   ladybug.RuleDuplicateStableKey,
		Table:  "Package",
		Key:    "pkg:go:acme/widgets:widgets",
		Detail: "also used by table File",
	})

	report, err := Rollback(context.Background(), options)
	if err == nil {
		t.Fatal("Rollback() error = nil, want an invariant violation error")
	}
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("errors.Is(err, ErrRollbackFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("RollbackReport.Passed = true, want false")
	}
	if report.Digest != report.Expected {
		t.Fatalf("Digest = %q, Expected = %q, want them to match: the failure must come from invariants alone", report.Digest, report.Expected)
	}
	if report.Invariants.Passed {
		t.Fatal("Invariants.Passed = true, want false")
	}

	if got := currentGenerationID(t, root); got != secondID {
		t.Fatalf("CURRENT = %q, want %q (unchanged)", got, secondID)
	}
}

// TestRollbackSwitchesAndInvertsRoles is the (d) contract: a clean
// rollback moves CURRENT to the backup and, per point 6 of the plan, makes
// the generation that used to be active the new backup, so a caller can
// roll forward again.
func TestRollbackSwitchesAndInvertsRoles(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)

	report, err := Rollback(context.Background(), rollbackOptions(root, firstID))
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("RollbackReport.Passed = false, want true; report=%+v", report)
	}
	if report.From.ID != secondID {
		t.Fatalf("From.ID = %q, want %q", report.From.ID, secondID)
	}
	if report.To.ID != firstID {
		t.Fatalf("To.ID = %q, want %q", report.To.ID, firstID)
	}
	if report.Digest != report.Expected {
		t.Fatalf("Digest = %q, Expected = %q, want them to match on a clean rollback", report.Digest, report.Expected)
	}
	if !report.Invariants.Passed {
		t.Fatal("Invariants.Passed = false, want true")
	}

	if got := currentGenerationID(t, root); got != firstID {
		t.Fatalf("CURRENT = %q, want %q", got, firstID)
	}

	layout, err := Roles(context.Background(), LayoutOptions{Root: root})
	if err != nil {
		t.Fatalf("Roles() error = %v", err)
	}
	if layout.Active.ID != firstID {
		t.Fatalf("Active.ID = %q, want %q", layout.Active.ID, firstID)
	}
	if !layout.HasBackup || layout.Backup.ID != secondID {
		t.Fatalf("Backup = %+v (HasBackup=%v), want id %q: rollback must invert the roles", layout.Backup, layout.HasBackup, secondID)
	}
}

// TestRollbackWithoutBackupOrExplicitIDFails is the (e) contract: with a
// single publication (no backup yet) and no explicit generation id, there
// is nowhere defined to roll back to.
func TestRollbackWithoutBackupOrExplicitIDFails(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()
	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	_, err := Rollback(context.Background(), rollbackOptions(root, ""))
	if err == nil {
		t.Fatal("Rollback() error = nil, want a no-backup error")
	}
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("errors.Is(err, ErrRollbackFailed) = false; err = %v", err)
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001 (unchanged)", got)
	}
}

// TestRollbackMissingSnapshotDigestFailsValidation is the (f) contract: a
// generation with no snapshot.sha256 cannot be blindly reactivated.
func TestRollbackMissingSnapshotDigestFailsValidation(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)

	digestPath := filepath.Join(root, "generations", firstID, snapshotFileName)
	if err := os.Remove(digestPath); err != nil {
		t.Fatalf("remove %s: %v", digestPath, err)
	}

	report, err := Rollback(context.Background(), rollbackOptions(root, firstID))
	if err == nil {
		t.Fatal("Rollback() error = nil, want a missing digest error")
	}
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("errors.Is(err, ErrRollbackFailed) = false; err = %v", err)
	}
	if !strings.Contains(err.Error(), snapshotFileName) {
		t.Fatalf("err = %v, want it to name %s", err, snapshotFileName)
	}
	if report.Passed {
		t.Fatal("RollbackReport.Passed = true, want false")
	}

	if got := currentGenerationID(t, root); got != secondID {
		t.Fatalf("CURRENT = %q, want %q (unchanged)", got, secondID)
	}
}

// TestRunPrunesGenerationsNeitherActiveNorBackup is the (g) contract: a
// third publication prunes the first generation, which is neither active
// nor backup once the third lands, and records it in Report.Pruned.
func TestRunPrunesGenerationsNeitherActiveNorBackup(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()
	firstID, secondID := publishTwoGenerations(t, root)

	third, err := Run(context.Background(), buildOptions(t, root, "000003", set))
	if err != nil {
		t.Fatalf("third Run() error = %v", err)
	}
	if !third.Passed {
		t.Fatalf("third Report.Passed = false, want true; stages=%+v", third.Stages)
	}
	if len(third.Pruned) != 1 || third.Pruned[0] != firstID {
		t.Fatalf("Pruned = %v, want exactly [%q]", third.Pruned, firstID)
	}

	if _, err := os.Stat(filepath.Join(root, "generations", firstID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation %s should have been pruned: stat err = %v", firstID, err)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", secondID)); err != nil {
		t.Fatalf("generation %s (new backup) should still exist: stat err = %v", secondID, err)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", "000003")); err != nil {
		t.Fatalf("generation 000003 (active) should still exist: stat err = %v", err)
	}

	publishStage, ok := stageByName(third.Stages, StagePublish)
	if !ok || !publishStage.Passed {
		t.Fatalf("publish stage = %+v, want a pass", publishStage)
	}
	if !strings.Contains(publishStage.Detail, firstID) {
		t.Fatalf("publish stage Detail = %q, want it to mention pruned generation %q", publishStage.Detail, firstID)
	}
}

// TestRollbackDefaultsCountsAndIntegrityToLadybug confirms RollbackOptions
// wires its Counts and Integrity defaults exactly like Options already
// does for Run: a caller that leaves them nil reaches the real
// ladybug.CanonicalTableCounts / ladybug.VerifyCanonicalIntegrity, not a
// silently skipped check.
func TestRollbackDefaultsCountsAndIntegrityToLadybug(t *testing.T) {
	root := t.TempDir()
	firstID, _ := publishTwoGenerations(t, root)

	report, err := Rollback(context.Background(), RollbackOptions{Root: root, GenerationID: firstID})
	if err == nil {
		t.Fatal("Rollback() error = nil, want the default ladybug hooks to reject a database the fake loader never wrote")
	}
	if !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("errors.Is(err, ErrRollbackFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("RollbackReport.Passed = true, want false")
	}
	if !strings.Contains(err.Error(), "ladybug ") {
		t.Fatalf("err = %v, want it to name a ladybug error: that is what proves Counts/Integrity defaulted to the real implementations", err)
	}
}
