package integrations

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAProjectSkillIsNeverALink is the negative that shapes the whole design.
//
// A project-scoped path lives inside the repository and is committed. A symlink
// to an absolute path under this machine's home directory would arrive on every
// other clone -- and in CI -- pointing at a directory that does not exist, so a
// skill that worked for whoever installed it would be broken for everyone else.
func TestAProjectSkillIsNeverALink(t *testing.T) {
	manager, _, project := testManager(t)
	if _, err := manager.InstallSkill(TargetCodex, ScopeProject, false, false); err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	path := filepath.Join(project, ".agents", "skills", "kivgraph", "SKILL.md")
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("a project-scoped skill was installed as a symlink; it would be committed broken")
	}
	if _, err := os.Stat(manager.canonicalSkillPath()); err == nil {
		t.Fatal("a project install materialised a canonical file it never points at")
	}
}

// TestEditingTheCanonicalReachesEveryClient is the reason for the change. One
// file is edited and three clients say something different, without any of them
// being reinstalled.
func TestEditingTheCanonicalReachesEveryClient(t *testing.T) {
	manager, home, _ := testManager(t)
	clients := []Target{TargetClaudeCode, TargetCodex, TargetOpenCode}
	for _, target := range clients {
		if _, err := manager.InstallSkill(target, ScopeUser, false, false); err != nil {
			t.Fatalf("InstallSkill(%s) error = %v", target, err)
		}
	}
	const edited = "# Kivgraph\n\nAsk the graph before you grep.\n"
	if err := os.WriteFile(manager.canonicalSkillPath(), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range clients {
		path, err := manager.skillPath(target, ScopeUser)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if string(data) != edited {
			t.Fatalf("%s still reads the shipped skill; the edit did not reach it", target)
		}
	}
	_ = home
}

// TestAnUpgradeKeepsAnEditedCanonical is the promise that makes editing worth
// doing. Reinstalling is what an upgrade does, and it must not discard the
// change that made the skill worth changing.
func TestAnUpgradeKeepsAnEditedCanonical(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	const edited = "# mine\n"
	if err := os.WriteFile(manager.canonicalSkillPath(), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	data, err := os.ReadFile(manager.canonicalSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != edited {
		t.Fatalf("reinstalling overwrote the edit: %q", data)
	}
	if plan.Status != "managed" {
		t.Fatalf("plan = %#v", plan)
	}

	// The status says so, so nobody has to diff it to find out.
	status, err := manager.StatusSkill(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.Detail, "local edits") {
		t.Fatalf("status does not report the edit: %q", status.Detail)
	}

	// And --force is how the shipped version is taken back.
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, true); err != nil {
		t.Fatalf("forced InstallSkill() error = %v", err)
	}
	data, err = os.ReadFile(manager.canonicalSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == edited {
		t.Fatal("--force did not restore the shipped skill")
	}
	path, err := manager.skillPath(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("--force did not leave a skill link at %q: %v", path, err)
	}
	if linkTarget != manager.canonicalSkillPath() {
		t.Fatalf("--force link target = %q, want %q", linkTarget, manager.canonicalSkillPath())
	}
	linkedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read refreshed skill through link: %v", err)
	}
	if !bytes.Equal(linkedData, embeddedSkill) {
		t.Fatal("--force did not restore the shipped skill through the existing link")
	}
}

// TestAnOlderCopyIsUpgradedWithoutForcing keeps the migration from asking for a
// flag it does not need: the bytes at the path are the ones we would write, so
// replacing them with a link loses nothing.
func TestAnOlderCopyIsUpgradedWithoutForcing(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, embeddedSkill, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := manager.StatusSkill(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != statusSuperseded {
		t.Fatalf("an earlier install reads as %q, want %q", status.Status, statusSuperseded)
	}
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the copy was not upgraded to a link")
	}
}

// TestAnEditedCopyIsRefusedAndThenKept covers the one migration that loses
// something: a copy the user already changed in place, before there was a
// canonical file to change instead.
func TestAnEditedCopyIsRefusedAndThenKept(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const theirs = "# theirs\n"
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("an edited skill was replaced without --force")
	}
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, true); err != nil {
		t.Fatalf("forced InstallSkill() error = %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.kivgraph.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("the edited copy was not kept: %q", backups)
	}
	kept, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != theirs {
		t.Fatalf("the backup holds %q, not the edit", kept)
	}
}

// TestRemoveTakesTheLinkAndKeepsTheCanonical holds the asymmetry: a client is
// unregistered, an edit is not thrown away.
func TestRemoveTakesTheLinkAndKeepsTheCanonical(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	const edited = "# mine\n"
	if err := os.WriteFile(manager.canonicalSkillPath(), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveSkill() error = %v", err)
	}
	path, err := manager.skillPath(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the link survived the remove: %v", err)
	}
	data, err := os.ReadFile(manager.canonicalSkillPath())
	if err != nil {
		t.Fatalf("remove deleted the edited canonical: %v", err)
	}
	if string(data) != edited {
		t.Fatalf("the canonical holds %q", data)
	}
}

// TestALinkSomewhereElseIsNotOurs keeps another tool's link from being adopted
// or silently replaced.
func TestALinkSomewhereElseIsNotOurs(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "elsewhere.md"), path); err != nil {
		t.Fatal(err)
	}
	status, err := manager.StatusSkill(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusSkill() error = %v", err)
	}
	if status.Status != "incompatible" {
		t.Fatalf("a foreign link reads as %q", status.Status)
	}
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("a foreign link was replaced without --force")
	}
}

// TestDryRunLinksNothing is the promise the flag makes, on the path that now
// creates a link and a canonical file rather than one copy.
func TestDryRunLinksNothing(t *testing.T) {
	manager, home, _ := testManager(t)
	plan, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, true, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if plan.Status != "would-install" {
		t.Fatalf("plan = %#v", plan)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("a dry run created %v (err %v)", entries, err)
	}
}

// TestADanglingLinkIsNotManaged is a regression, and the way it was found is
// the point: every test above installed and then asked, so the canonical always
// existed. Deleting it -- which a tidy-up, a restore, or a home directory synced
// between machines will do -- leaves a link no client can read a skill through,
// and status called that "managed". A status that says the skill is installed
// when nothing can load it is worse than one that says nothing.
func TestADanglingLinkIsNotManaged(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manager.canonicalSkillPath()); err != nil {
		t.Fatal(err)
	}
	status, err := manager.StatusSkill(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusSkill() error = %v", err)
	}
	if status.Status != statusBroken {
		t.Fatalf("a dangling link reads as %q, want %q", status.Status, statusBroken)
	}
	if !strings.Contains(status.Detail, "does not exist") {
		t.Fatalf("status does not say what is wrong: %q", status.Detail)
	}

	// Install repairs it, and says it changed something rather than
	// reporting the no-op it would have reported before.
	plan, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if !plan.Changed || plan.Status != "installed" {
		t.Fatalf("repairing plan = %#v", plan)
	}
	path, err := manager.skillPath(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the link still leads nowhere: %v", err)
	}
	if !bytes.Equal(data, embeddedSkill) {
		t.Fatal("the restored canonical is not the shipped skill")
	}
}

// TestADanglingLinkIsStillOursToRemove keeps a broken link from needing --force
// to clean up. It is ours; nothing is lost by taking it.
func TestADanglingLinkIsStillOursToRemove(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manager.canonicalSkillPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveSkill() error = %v", err)
	}
	path, err := manager.skillPath(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the broken link survived: %v", err)
	}
}

// TestEveryPlacementReportsSomethingAReaderCanAct on is the half of status
// nothing covered: each state a client path can be in, and the sentence it
// produces. A status word with no explanation is a status nobody can use.
func TestEveryPlacementReportsSomethingAReaderCanActOn(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		arrange func(t *testing.T, manager Manager, path string)
		status  string
		says    string
	}{
		{"nothing there", func(*testing.T, Manager, string) {}, "absent", "not installed"},
		{"a copy from an earlier install", func(t *testing.T, _ Manager, path string) {
			writeAt(t, path, embeddedSkill)
		}, statusSuperseded, "earlier install"},
		{"an edited copy", func(t *testing.T, _ Manager, path string) {
			writeAt(t, path, []byte("# theirs\n"))
		}, "incompatible", "does not match"},
		{"a link somewhere else", func(t *testing.T, manager Manager, path string) {
			linkAt(t, path, filepath.Join(manager.homeDir, "elsewhere.md"))
		}, "incompatible", "not Kivgraph's"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			manager, home, _ := testManager(t)
			path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
			testCase.arrange(t, manager, path)
			plan, err := manager.StatusSkill(TargetClaudeCode, ScopeUser)
			if err != nil {
				t.Fatalf("StatusSkill() error = %v", err)
			}
			if plan.Status != testCase.status {
				t.Fatalf("status = %q, want %q", plan.Status, testCase.status)
			}
			if !strings.Contains(plan.Detail, testCase.says) {
				t.Fatalf("detail %q does not say %q", plan.Detail, testCase.says)
			}
		})
	}
}

// TestRemovingWhatIsNotThereChangesNothing keeps a remove on a clean machine
// from reporting work it did not do.
func TestRemovingWhatIsNotThereChangesNothing(t *testing.T) {
	manager, _, _ := testManager(t)
	plan, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("RemoveSkill() error = %v", err)
	}
	if plan.Status != "absent" || plan.Changed {
		t.Fatalf("plan = %#v", plan)
	}
}

// TestRemovingAnEditedCopyKeepsItUnderForce covers the migration case from the
// other side: the copy is not ours, so it is refused, and forcing it preserves
// what was there.
func TestRemovingAnEditedCopyKeepsItUnderForce(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
	writeAt(t, path, []byte("# theirs\n"))
	if _, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, false, false); err == nil {
		t.Fatal("an edited copy was removed without --force")
	}
	plan, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, false, true)
	if err != nil {
		t.Fatalf("forced RemoveSkill() error = %v", err)
	}
	if plan.Status != "removed" {
		t.Fatalf("plan = %#v", plan)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.kivgraph.bak"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("the edited copy was not kept: %q (err %v)", backups, err)
	}
}

// TestADryRunRemoveTakesNothing is the promise the flag makes on the way out.
func TestADryRunRemoveTakesNothing(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.RemoveSkill(TargetClaudeCode, ScopeUser, true, false)
	if err != nil {
		t.Fatalf("RemoveSkill() error = %v", err)
	}
	if plan.Status != "would-remove" {
		t.Fatalf("plan = %#v", plan)
	}
	path, err := manager.skillPath(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("a dry run took the link: %v", err)
	}
}

// TestADryRunRepairLeavesTheCanonicalMissing covers the branch a broken link
// takes when nothing is meant to be written yet.
func TestADryRunRepairLeavesTheCanonicalMissing(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manager.canonicalSkillPath()); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, true, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if plan.Status != "would-install" || !plan.DryRun {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := os.Stat(manager.canonicalSkillPath()); !os.IsNotExist(err) {
		t.Fatalf("a dry run restored the canonical: %v", err)
	}
}

// TestInstallingOverALinkLeavesTheCanonicalAlone covers the ordinary repeat
// install: the link is ours and the file behind it already says what it should.
func TestInstallingOverALinkLeavesTheCanonicalAlone(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(manager.canonicalSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, true, false)
	if err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}
	if plan.Status != "managed" || plan.Changed {
		t.Fatalf("a dry run over a healthy link = %#v", plan)
	}
	after, err := os.Stat(manager.canonicalSkillPath())
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("a repeat install rewrote a canonical that needed nothing")
	}
}

func writeAt(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func linkAt(t *testing.T, path, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// TestADirectoryWhereTheSkillGoesIsAnError keeps something that is neither a
// file nor a link from being read as absent, which would make install create a
// link over it and fail in a way that names nothing.
func TestADirectoryWhereTheSkillGoesIsAnError(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "skills", "kivgraph", "SKILL.md")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StatusSkill(TargetClaudeCode, ScopeUser); err == nil {
		t.Fatal("a directory read as a skill")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error does not say what is wrong: %v", err)
	}
	if _, err := manager.InstallSkill(TargetClaudeCode, ScopeUser, false, true); err == nil {
		t.Fatal("install wrote over a directory")
	}
}

// TestACanonicalThatIsNotAFileIsReported covers the same rule one level down:
// the canonical is read through readDestination, which refuses a link, so a
// link left there has to surface rather than be treated as an edit.
func TestACanonicalThatIsNotAFileIsReported(t *testing.T) {
	manager, home, _ := testManager(t)
	linkAt(t, manager.canonicalSkillPath(), filepath.Join(home, "somewhere.md"))
	if _, err := manager.StatusSkill(TargetClaudeCode, ScopeUser); err == nil {
		t.Fatal("a canonical that is a link read as an ordinary file")
	}
}
